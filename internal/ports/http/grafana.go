package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/crypticani/torvix/internal/domain"
)

type grafanaCostPoint struct {
	Time      time.Time       `json:"time"`
	Metric    string          `json:"metric"`
	Value     float64         `json:"value"`
	Provider  domain.Provider `json:"provider"`
	AccountID string          `json:"account_id"`
	Service   string          `json:"service"`
}

type grafanaTopServiceRow struct {
	Rank      int             `json:"rank"`
	Service   string          `json:"service"`
	Provider  domain.Provider `json:"provider"`
	TotalCost float64         `json:"total_cost"`
}

type grafanaAnomalyRow struct {
	Date             time.Time       `json:"date"`
	Provider         domain.Provider `json:"provider"`
	AccountID        string          `json:"account_id"`
	Service          string          `json:"service"`
	Baseline         float64         `json:"baseline"`
	Actual           float64         `json:"actual"`
	ZScore           float64         `json:"z_score"`
	PercentDeviation float64         `json:"percent_deviation"`
	Severity         string          `json:"severity"`
}

type grafanaSummaryStat struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Window        string    `json:"window"`
	TotalCost     float64   `json:"total_cost"`
	SeriesCount   int       `json:"series_count"`
	ServiceCount  int       `json:"service_count"`
	ProviderCount int       `json:"provider_count"`
	AnomalyCount  int       `json:"anomaly_count"`
	GeneratedAt   time.Time `json:"generated_at"`
}

func (h *Handler) withGrafanaAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !h.authorizeGrafanaAPI(w, r) {
			return
		}
		next(w, r)
	}
}

func (h *Handler) withGrafanaAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.authorizeGrafanaAPI(w, r) {
			return
		}
		next(w, r)
	}
}

func (h *Handler) authorizeGrafanaAPI(w http.ResponseWriter, r *http.Request) bool {
	if !h.grafana.authEnabled {
		return true
	}
	if h.grafana.authToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "grafana api auth is enabled but no bearer token is configured"})
		return false
	}
	if bearerToken(r.Header.Get("Authorization")) != h.grafana.authToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "grafana api authorization failed"})
		return false
	}
	return true
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func (h *Handler) grafanaCostTimeseries(w http.ResponseWriter, r *http.Request) {
	from, to, _, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, []grafanaCostPoint{})
		return
	}
	window := grafanaWindow(r)
	summary, err := h.analytics.DashboardCostSummaries(r.Context(), window, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	points := make([]grafanaCostPoint, 0, len(summary))
	for _, row := range summary {
		points = append(points, grafanaCostPoint{
			Time:      row.PeriodStart,
			Metric:    grafanaMetricFromDashboard(row),
			Value:     row.TotalCost,
			Provider:  row.Provider,
			AccountID: row.AccountID,
			Service:   row.Service,
		})
	}
	writeJSON(w, http.StatusOK, points)
}

func (h *Handler) grafanaTopServices(w http.ResponseWriter, r *http.Request) {
	from, to, _, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, []grafanaTopServiceRow{})
		return
	}
	limit := grafanaLimit(r, 15)
	summary, err := h.analytics.DashboardCostSummaries(r.Context(), "daily", from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type key struct {
		provider domain.Provider
		service  string
	}
	totals := make(map[key]float64)
	for _, row := range summary {
		service := row.Service
		if service == "" {
			service = "unknown"
		}
		totals[key{provider: row.Provider, service: service}] += row.TotalCost
	}
	rows := make([]grafanaTopServiceRow, 0, len(totals))
	for k, total := range totals {
		rows = append(rows, grafanaTopServiceRow{
			Service:   k.service,
			Provider:  k.provider,
			TotalCost: total,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TotalCost == rows[j].TotalCost {
			return rows[i].Service < rows[j].Service
		}
		return rows[i].TotalCost > rows[j].TotalCost
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	for i := range rows {
		rows[i].Rank = i + 1
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) grafanaAnomalies(w http.ResponseWriter, r *http.Request) {
	from, to, _, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, []grafanaAnomalyRow{})
		return
	}
	limit := grafanaLimit(r, 25)
	anomalies, err := h.analytics.DashboardAnomalies(r.Context(), from, to, r.URL.Query().Get("severity"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	rows := make([]grafanaAnomalyRow, 0, len(anomalies))
	for _, anomaly := range anomalies {
		rows = append(rows, grafanaAnomalyRow{
			Date:             anomaly.PeriodStart,
			Provider:         anomaly.Provider,
			AccountID:        anomaly.AccountID,
			Service:          anomaly.Service,
			Baseline:         anomaly.ExpectedCost,
			Actual:           anomaly.ObservedCost,
			ZScore:           0,
			PercentDeviation: anomaly.PercentageDelta,
			Severity:         anomaly.Severity,
		})
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) grafanaSummary(w http.ResponseWriter, r *http.Request) {
	from, to, _, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, grafanaSummaryStat{From: from, To: to, Window: grafanaWindow(r), GeneratedAt: time.Now().UTC()})
		return
	}
	window := grafanaWindow(r)
	summary, err := h.analytics.DashboardCostSummaries(r.Context(), window, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	anomalies, err := h.analytics.DashboardAnomalies(r.Context(), from, to, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stat := grafanaSummaryFrom(summary, anomalies, from, to, window)
	if h.grafana.metrics != nil {
		h.grafana.metrics.ObserveGrafanaCostStats(stat.Window, stat.TotalCost, stat.ServiceCount, stat.AnomalyCount)
	}
	writeJSON(w, http.StatusOK, stat)
}

func grafanaSummaryFrom(summary []domain.DashboardCostSummary, anomalies []domain.DashboardAnomaly, from, to time.Time, window string) grafanaSummaryStat {
	services := make(map[string]struct{})
	providers := make(map[domain.Provider]struct{})
	var total float64
	for _, row := range summary {
		total += row.TotalCost
		if row.Service != "" {
			services[string(row.Provider)+"\x00"+row.Service] = struct{}{}
		}
		if row.Provider != "" {
			providers[row.Provider] = struct{}{}
		}
	}
	return grafanaSummaryStat{
		From:          from.UTC(),
		To:            to.UTC(),
		Window:        window,
		TotalCost:     total,
		SeriesCount:   len(summary),
		ServiceCount:  len(services),
		ProviderCount: len(providers),
		AnomalyCount:  len(anomalies),
		GeneratedAt:   time.Now().UTC(),
	}
}

func grafanaWindow(r *http.Request) string {
	switch r.URL.Query().Get("window") {
	case "weekly":
		return "weekly"
	case "monthly":
		return "monthly"
	default:
		return "daily"
	}
}

func grafanaLimit(r *http.Request, fallback int) int {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err == nil && limit > 0 && limit <= 100 {
			return limit
		}
	}
	return fallback
}

func grafanaMetric(row domain.AggregatedCost) string {
	service := row.Service
	if service == "" {
		service = "unknown"
	}
	if row.AccountID == "" {
		return string(row.Provider) + "/" + service
	}
	return string(row.Provider) + "/" + row.AccountID + "/" + service
}
