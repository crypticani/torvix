package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/crypticani/torvix/internal/core/alerting"
	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/core/reporting"
	"github.com/crypticani/torvix/internal/domain"
)

type Handler struct {
	mux           *http.ServeMux
	collector     *collect.Service
	analytics     *analytics.Service
	forecasting   *forecasting.Service
	reporting     *reporting.Service
	alerting      *alerting.Service
	metrics       http.Handler
	lookbackDays  int
	retentionDays int
	ingestions    *ingestionJobStore
	grafana       grafanaOptions
	logger        *slog.Logger
}

type HandlerOptions struct {
	LookbackDays       int
	RetentionDays      int
	GrafanaAuthEnabled bool
	GrafanaAuthToken   string
	GrafanaMetrics     GrafanaMetricsRecorder
	Logger             *slog.Logger
}

type GrafanaMetricsRecorder interface {
	ObserveGrafanaCostStats(window string, totalCost float64, serviceCount, anomalyCount int)
}

type grafanaOptions struct {
	authEnabled bool
	authToken   string
	metrics     GrafanaMetricsRecorder
}

func New(collector *collect.Service, analytics *analytics.Service, forecasting *forecasting.Service, reporting *reporting.Service, alerting *alerting.Service, reg *prometheus.Registry) *Handler {
	return NewWithLookback(collector, analytics, forecasting, reporting, alerting, reg, 30)
}

func NewWithLookback(collector *collect.Service, analytics *analytics.Service, forecasting *forecasting.Service, reporting *reporting.Service, alerting *alerting.Service, reg *prometheus.Registry, lookbackDays int) *Handler {
	return NewWithOptions(collector, analytics, forecasting, reporting, alerting, reg, HandlerOptions{LookbackDays: lookbackDays})
}

func NewWithOptions(collector *collect.Service, analytics *analytics.Service, forecasting *forecasting.Service, reporting *reporting.Service, alerting *alerting.Service, reg *prometheus.Registry, opts HandlerOptions) *Handler {
	lookbackDays := opts.LookbackDays
	if lookbackDays <= 0 {
		lookbackDays = 30
	}
	retentionDays := opts.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 90
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		mux:           http.NewServeMux(),
		collector:     collector,
		analytics:     analytics,
		forecasting:   forecasting,
		reporting:     reporting,
		alerting:      alerting,
		metrics:       promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
		lookbackDays:  lookbackDays,
		retentionDays: retentionDays,
		ingestions:    newIngestionJobStore(),
		logger:        logger,
		grafana: grafanaOptions{
			authEnabled: opts.GrafanaAuthEnabled,
			authToken:   strings.TrimSpace(opts.GrafanaAuthToken),
			metrics:     opts.GrafanaMetrics,
		},
	}
	h.routes()
	return h
}

func (h *Handler) routes() {
	h.mux.HandleFunc("/healthz", h.health)
	h.mux.HandleFunc("/api/v1/ingest", h.ingest)
	h.mux.HandleFunc("/api/v1/ingest/status/", h.ingestStatus)
	h.mux.HandleFunc("/api/v1/analytics/summary", h.summary)
	h.mux.HandleFunc("/api/v1/analytics/variance", h.variance)
	h.mux.HandleFunc("/api/v1/analytics/anomalies", h.anomalies)
	h.mux.HandleFunc("/api/v1/analytics/forecast", h.forecast)
	h.mux.HandleFunc("/api/v1/dashboard/overview", h.withGrafanaAuth(h.dashboardOverview))
	h.mux.HandleFunc("/api/v1/dashboard/cost-timeseries", h.withGrafanaAuth(h.dashboardCostTimeseries))
	h.mux.HandleFunc("/api/v1/dashboard/cost-by-category", h.withGrafanaAuth(h.dashboardCostByCategory))
	h.mux.HandleFunc("/api/v1/dashboard/cost-by-service", h.withGrafanaAuth(h.dashboardCostByService))
	h.mux.HandleFunc("/api/v1/dashboard/cost-by-provider", h.withGrafanaAuth(h.dashboardCostByProvider))
	h.mux.HandleFunc("/api/v1/dashboard/cost-by-compartment", h.withGrafanaAuth(h.dashboardCostByCompartment))
	h.mux.HandleFunc("/api/v1/dashboard/cost-by-region", h.withGrafanaAuth(h.dashboardCostByRegion))
	h.mux.HandleFunc("/api/v1/dashboard/filter-options", h.withGrafanaAuth(h.dashboardFilterOptions))
	h.mux.HandleFunc("/api/v1/dashboard/oci-cost-summary", h.withGrafanaAuth(h.dashboardOCICostSummary))
	h.mux.HandleFunc("/api/v1/dashboard/oci-cost-drivers", h.withGrafanaAuth(h.dashboardOCICostDrivers))
	h.mux.HandleFunc("/api/v1/dashboard/cost-increases", h.withGrafanaAuth(h.dashboardCostIncreases))
	h.mux.HandleFunc("/api/v1/dashboard/cost-decreases", h.withGrafanaAuth(h.dashboardCostDecreases))
	h.mux.HandleFunc("/api/v1/dashboard/anomalies", h.withGrafanaAuth(h.dashboardAnomalies))
	h.mux.HandleFunc("/api/v1/dashboard/ingestion-status", h.withGrafanaAuth(h.dashboardIngestionStatus))
	h.mux.HandleFunc("/api/v1/grafana/timeseries/cost", h.withGrafanaAuth(h.grafanaCostTimeseries))
	h.mux.HandleFunc("/api/v1/grafana/table/top-services", h.withGrafanaAuth(h.grafanaTopServices))
	h.mux.HandleFunc("/api/v1/grafana/table/anomalies", h.withGrafanaAuth(h.grafanaAnomalies))
	h.mux.HandleFunc("/api/v1/grafana/stat/summary", h.withGrafanaAuth(h.grafanaSummary))
	h.mux.HandleFunc("/api/v1/reports/daily", h.dailyReport)
	h.mux.HandleFunc("/api/v1/reports/weekly", h.weeklyReport)
	h.mux.HandleFunc("/api/v1/reports/monthly", h.monthlyReport)
	h.mux.Handle("/metrics", h.metrics)
	h.mux.Handle("/swagger/", httpSwagger.WrapHandler)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// health godoc
//
//	@Summary		Health check
//	@Description	Returns the health status of the Torvix API server.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	StatusResponse	"Service is healthy"
//	@Router			/healthz [get]
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ingest godoc
//
//	@Summary		Trigger billing data ingestion
//	@Description	Queues collection of billing data from all enabled cloud providers within the configured rolling ingestion window and returns immediately. Completion is available through the returned status URL and enabled alerting targets.
//	@Tags			Ingestion
//	@Produce		json
//	@Param			days	query		int		false	"Number of days to look back for ingestion. Defaults to configured rolling lookback, normally 30."	example(30)
//	@Success		202	{object}	IngestAcceptedResponse	"Ingestion queued for background processing"
//	@Failure		405	{string}	string			"Method not allowed"
//	@Failure		503	{object}	ErrorResponse	"Ingestion service unavailable"
//	@Router			/api/v1/ingest [post]
func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.collector == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingestion collector is not configured"})
		return
	}
	days := 0
	if v := r.URL.Query().Get("days"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			days = d
		}
	}
	var since time.Time
	if days > 0 {
		since = time.Now().UTC().AddDate(0, 0, -days)
	}
	job, started := h.ingestions.enqueue(days, since)
	if started {
		go h.runIngestionJob(job.JobID, since)
	}

	statusURL := "/api/v1/ingest/status/" + job.JobID
	message := "ingestion queued and running in the background"
	if !started {
		message = "ingestion is already running; duplicate request was not started"
	}
	writeJSON(w, http.StatusAccepted, IngestAcceptedResponse{
		JobID:     job.JobID,
		Status:    job.Status,
		Message:   message,
		StatusURL: statusURL,
		QueuedAt:  job.QueuedAt,
	})
}

// ingestStatus godoc
//
//	@Summary		Get ingestion job status
//	@Description	Returns the current or completed status for a background ingestion job.
//	@Tags			Ingestion
//	@Produce		json
//	@Param			job_id	path		string	true	"Ingestion job ID"
//	@Success		200		{object}	IngestionJobResponse	"Ingestion job status"
//	@Failure		404		{object}	ErrorResponse			"Ingestion job not found"
//	@Router			/api/v1/ingest/status/{job_id} [get]
func (h *Handler) ingestStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/api/v1/ingest/status/")
	if jobID == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ingestion job not found"})
		return
	}
	job, ok := h.ingestions.get(jobID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "ingestion job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) runIngestionJob(jobID string, since time.Time) {
	h.ingestions.markRunning(jobID)
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("ingestion panic: %v", recovered)
			job := h.ingestions.complete(jobID, ingestionStatusFailed, nil, err)
			h.notifyIngestionComplete(job)
		}
	}()
	results, err := h.collector.Run(context.Background(), since)
	resp := ingestionResponses(results)
	status := ingestionStatusSuccess
	if err != nil && len(resp) > 0 {
		status = ingestionStatusPartial
	}
	if err != nil && len(resp) == 0 {
		status = ingestionStatusFailed
	}
	job := h.ingestions.complete(jobID, status, resp, err)
	h.notifyIngestionComplete(job)
}

func ingestionResponses(results []collect.ProviderResult) []IngestResponse {
	resp := make([]IngestResponse, 0, len(results))
	for _, pr := range results {
		resp = append(resp, IngestResponse{
			Provider:              pr.Provider,
			FilesProcessed:        pr.FilesProcessed,
			FilesSkipped:          pr.FilesSkipped,
			SkippedOldFiles:       pr.SkippedOldFiles,
			RecordsParsed:         pr.RecordsParsed,
			RecordsWithinLookback: pr.RecordsWithinLookback,
			RecordsSkippedOld:     pr.RecordsSkippedOld,
			RecordsInserted:       pr.RecordsInserted,
			DurationSeconds:       pr.Duration.Seconds(),
			Error:                 pr.Error,
		})
	}
	return resp
}

func (h *Handler) notifyIngestionComplete(job IngestionJobResponse) {
	if h.alerting == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := h.alerting.SendNotification(ctx, ingestionNotification(job)); err != nil {
		h.logger.Error("failed to deliver ingestion completion alert", "job_id", job.JobID, "error", err)
	}
}

func ingestionNotification(job IngestionJobResponse) alerting.Notification {
	filesProcessed, filesSkipped, skippedOld, recordsParsed, recordsWithinLookback, recordsSkippedOld, recordsInserted := 0, 0, 0, 0, 0, 0, 0
	for _, result := range job.Results {
		filesProcessed += result.FilesProcessed
		filesSkipped += result.FilesSkipped
		skippedOld += result.SkippedOldFiles
		recordsParsed += result.RecordsParsed
		recordsWithinLookback += result.RecordsWithinLookback
		recordsSkippedOld += result.RecordsSkippedOld
		recordsInserted += result.RecordsInserted
	}

	severity := "success"
	if job.Status == ingestionStatusPartial {
		severity = "warning"
	}
	if job.Status == ingestionStatusFailed {
		severity = "error"
	}
	message := fmt.Sprintf("Background ingestion finished with status %s.", job.Status)
	if job.Error != "" {
		message += " Error: " + job.Error
	}

	return alerting.Notification{
		Title:    "Torvix Ingestion " + titleForNotification(job.Status),
		Severity: severity,
		Message:  message,
		Fields: []alerting.NotificationField{
			{Name: "Job ID", Value: job.JobID},
			{Name: "Files processed", Value: strconv.Itoa(filesProcessed)},
			{Name: "Files skipped", Value: strconv.Itoa(filesSkipped)},
			{Name: "Old files skipped", Value: strconv.Itoa(skippedOld)},
			{Name: "Records parsed", Value: strconv.Itoa(recordsParsed)},
			{Name: "Records within lookback", Value: strconv.Itoa(recordsWithinLookback)},
			{Name: "Old records skipped", Value: strconv.Itoa(recordsSkippedOld)},
			{Name: "Records inserted", Value: strconv.Itoa(recordsInserted)},
			{Name: "Duration", Value: fmt.Sprintf("%.1fs", job.DurationSeconds)},
		},
	}
}

func titleForNotification(status string) string {
	switch status {
	case ingestionStatusSuccess:
		return "Succeeded"
	case ingestionStatusPartial:
		return "Partially Failed"
	case ingestionStatusFailed:
		return "Failed"
	default:
		return strings.ReplaceAll(status, "_", " ")
	}
}

// summary godoc
//
//	@Summary		Get cost summary
//	@Description	Returns aggregated cost data over a time range, grouped by the specified window (daily, weekly, or monthly).
//	@Tags			Analytics
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Param			window	query		string	false	"Aggregation window: daily, weekly, or monthly."		Enums(daily, weekly, monthly)	default(daily)
//	@Success		200		{array}		domain.AggregatedCost	"Cost aggregation results"
//	@Failure		500		{object}	ErrorResponse			"Aggregation failed"
//	@Router			/api/v1/analytics/summary [get]
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	from, to := h.parseRange(r)
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "daily"
	}
	data, err := h.analytics.AggregateWindow(r.Context(), from, to, window)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// variance godoc
//
//	@Summary		Compare cost variance
//	@Description	Compares OCI cost by service and compartment for the requested operational period. Daily compares yesterday with the previous day, weekly compares last week with the week before, and monthly compares last month with the month before.
//	@Tags			Analytics
//	@Produce		json
//	@Param			period	query		string	false	"Comparison period."	Enums(daily, weekly, monthly)	default(daily)
//	@Param			as_of	query		string	false	"Evaluation date (YYYY-MM-DD). Defaults to today."	example(2026-05-17)
//	@Success		200		{array}		domain.CostVariance	"Cost variance results"
//	@Failure		400		{object}	ErrorResponse			"Invalid period"
//	@Failure		500		{object}	ErrorResponse			"Comparison failed"
//	@Router			/api/v1/analytics/variance [get]
func (h *Handler) variance(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "daily"
	}
	switch period {
	case "daily", "weekly", "monthly":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "period must be one of daily, weekly, monthly"})
		return
	}
	asOf := time.Now().UTC()
	if v := r.URL.Query().Get("as_of"); v != "" {
		t, err := time.Parse(time.DateOnly, v)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "as_of must use YYYY-MM-DD"})
			return
		}
		asOf = t.UTC()
	}
	data, err := h.analytics.CompareVariance(r.Context(), period, asOf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// anomalies godoc
//
//	@Summary		Detect cost anomalies
//	@Description	Analyses cost data over a time range and returns any detected anomalies with severity, z-score, and deviation metrics.
//	@Tags			Analytics
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Success		200		{array}		domain.Anomaly	"Detected anomalies"
//	@Failure		500		{object}	ErrorResponse	"Detection failed"
//	@Router			/api/v1/analytics/anomalies [get]
func (h *Handler) anomalies(w http.ResponseWriter, r *http.Request) {
	from, to := h.parseRange(r)
	data, err := h.analytics.DetectAnomalies(r.Context(), from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// forecast godoc
//
//	@Summary		Get cost forecast
//	@Description	Generates a 7-day cost forecast based on historical data within the specified time range.
//	@Tags			Forecasting
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Success		200		{array}		domain.ForecastPoint	"Forecast data points"
//	@Failure		500		{object}	ErrorResponse			"Forecast generation failed"
//	@Router			/api/v1/analytics/forecast [get]
func (h *Handler) forecast(w http.ResponseWriter, r *http.Request) {
	from, to := h.parseRange(r)
	data, err := h.forecasting.Forecast(r.Context(), from, to, 7)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// dailyReport godoc
//
//	@Summary		Generate daily report
//	@Description	Builds a daily cost report for day-1 in the configured report timezone by default, including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to yesterday for daily reports."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today for daily reports."		example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Param			force	query		string	false	"Set to 'true' to resend even if this report was already delivered."	Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Daily report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/daily [get]
func (h *Handler) dailyReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.buildReport(r, "daily")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.maybeDeliver(r, report); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// weeklyReport godoc
//
//	@Summary		Generate weekly report
//	@Description	Builds a weekly cost report for the previous full Monday-to-Sunday week by default, including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to previous Monday."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to current Monday."		example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Param			force	query		string	false	"Set to 'true' to resend even if this report was already delivered."	Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Weekly report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/weekly [get]
func (h *Handler) weeklyReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.buildReport(r, "weekly")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.maybeDeliver(r, report); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// monthlyReport godoc
//
//	@Summary		Generate monthly report
//	@Description	Builds a monthly cost report for the last completed month by default, including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to the start of last completed month."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to the start of this month."				example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Param			force	query		string	false	"Set to 'true' to resend even if this report was already delivered."	Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Monthly report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/monthly [get]
func (h *Handler) monthlyReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.buildReport(r, "monthly")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := h.maybeDeliver(r, report); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) buildReport(r *http.Request, period string) (domain.Report, error) {
	if r.URL.Query().Get("from") == "" && r.URL.Query().Get("to") == "" {
		return h.reporting.BuildDefault(r.Context(), period, time.Now().UTC())
	}
	from, to := h.parseReportRange(r, period)
	return h.reporting.Build(r.Context(), period, from, to)
}

func (h *Handler) maybeDeliver(r *http.Request, report domain.Report) error {
	if r.URL.Query().Get("deliver") != "true" || h.alerting == nil {
		return nil
	}
	results := h.reporting.DeliverReport(r.Context(), h.alerting, report, reporting.DeliverOptions{
		Force: r.URL.Query().Get("force") == "true",
	})
	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}

func (h *Handler) parseRange(r *http.Request) (time.Time, time.Time) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -h.lookbackDays)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			from = t.UTC()
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			to = t.UTC().AddDate(0, 0, 1)
		}
	}
	return from, to
}

func (h *Handler) parseReportRange(r *http.Request, period string) (time.Time, time.Time) {
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
		return h.parseRange(r)
	}
	return defaultReportRange(period, time.Now().UTC())
}

func defaultReportRange(period string, now time.Time) (time.Time, time.Time) {
	return reporting.DefaultRange(period, now)
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
