package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/crypticani/cloudpulse/internal/core/alerting"
	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/collect"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/core/reporting"
	"github.com/crypticani/cloudpulse/internal/domain"
)

type Handler struct {
	mux         *http.ServeMux
	collector   *collect.Service
	analytics   *analytics.Service
	forecasting *forecasting.Service
	reporting   *reporting.Service
	alerting    *alerting.Service
	metrics     http.Handler
}

func New(collector *collect.Service, analytics *analytics.Service, forecasting *forecasting.Service, reporting *reporting.Service, alerting *alerting.Service, reg *prometheus.Registry) *Handler {
	h := &Handler{
		mux:         http.NewServeMux(),
		collector:   collector,
		analytics:   analytics,
		forecasting: forecasting,
		reporting:   reporting,
		alerting:    alerting,
		metrics:     promhttp.HandlerFor(reg, promhttp.HandlerOpts{}),
	}
	h.routes()
	return h
}

func (h *Handler) routes() {
	h.mux.HandleFunc("/healthz", h.health)
	h.mux.HandleFunc("/api/v1/ingest", h.ingest)
	h.mux.HandleFunc("/api/v1/analytics/summary", h.summary)
	h.mux.HandleFunc("/api/v1/analytics/anomalies", h.anomalies)
	h.mux.HandleFunc("/api/v1/analytics/forecast", h.forecast)
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
//	@Description	Returns the health status of the CloudPulse API server.
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
//	@Description	Triggers collection of billing data from all enabled cloud providers for the last 24 hours.
//	@Tags			Ingestion
//	@Produce		json
//	@Success		202	{object}	StatusResponse	"Ingestion completed successfully"
//	@Failure		405	{string}	string			"Method not allowed"
//	@Failure		500	{object}	ErrorResponse	"Ingestion failed"
//	@Router			/api/v1/ingest [post]
func (h *Handler) ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	if err := h.collector.Run(r.Context(), since); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ingestion completed"})
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
	from, to := parseRange(r.Context(), r)
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
	from, to := parseRange(r.Context(), r)
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
	from, to := parseRange(r.Context(), r)
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
//	@Description	Builds a daily cost report including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Daily report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/daily [get]
func (h *Handler) dailyReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r.Context(), r)
	report, err := h.reporting.Build(r.Context(), "daily", from, to)
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
//	@Description	Builds a weekly cost report including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Weekly report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/weekly [get]
func (h *Handler) weeklyReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r.Context(), r)
	report, err := h.reporting.Build(r.Context(), "weekly", from, to)
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
//	@Description	Builds a monthly cost report including summary, anomalies, and forecast. Optionally delivers via configured webhooks.
//	@Tags			Reports
//	@Produce		json
//	@Param			from	query		string	false	"Start date (YYYY-MM-DD). Defaults to 30 days ago."	example(2025-01-01)
//	@Param			to		query		string	false	"End date (YYYY-MM-DD). Defaults to today."			example(2025-01-31)
//	@Param			deliver	query		string	false	"Set to 'true' to send report via webhooks."			Enums(true, false)	default(false)
//	@Success		200		{object}	domain.Report	"Monthly report"
//	@Failure		500		{object}	ErrorResponse	"Report generation failed"
//	@Failure		502		{object}	ErrorResponse	"Webhook delivery failed"
//	@Router			/api/v1/reports/monthly [get]
func (h *Handler) monthlyReport(w http.ResponseWriter, r *http.Request) {
	from, to := parseRange(r.Context(), r)
	report, err := h.reporting.Build(r.Context(), "monthly", from, to)
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

func (h *Handler) maybeDeliver(r *http.Request, report domain.Report) error {
	if r.URL.Query().Get("deliver") != "true" || h.alerting == nil {
		return nil
	}
	return h.alerting.SendReport(r.Context(), report)
}

func parseRange(_ context.Context, r *http.Request) (time.Time, time.Time) {
	to := time.Now().UTC()
	from := to.AddDate(0, 0, -30)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.DateOnly, v); err == nil {
			to = t
		}
	}
	return from, to
}

func writeJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}
