package httpapi

import (
	"net/http"
	"sort"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type dashboardMeta struct {
	From          time.Time `json:"from,omitempty"`
	To            time.Time `json:"to,omitempty"`
	RetentionDays int       `json:"retention_days"`
	Source        string    `json:"source"`
	Message       string    `json:"message,omitempty"`
	GeneratedAt   time.Time `json:"generated_at"`
}

type dashboardOverviewResponse struct {
	Meta dashboardMeta            `json:"meta"`
	Data domain.DashboardOverview `json:"data"`
}

type dashboardTimeseriesResponse struct {
	Meta dashboardMeta      `json:"meta"`
	Data []grafanaCostPoint `json:"data"`
}

type dashboardBreakdownRow struct {
	Name      string          `json:"name"`
	Provider  domain.Provider `json:"provider,omitempty"`
	TotalCost float64         `json:"total_cost"`
	Rank      int             `json:"rank,omitempty"`
}

type dashboardBreakdownResponse struct {
	Meta dashboardMeta           `json:"meta"`
	Data []dashboardBreakdownRow `json:"data"`
}

type dashboardAnomaliesResponse struct {
	Meta dashboardMeta             `json:"meta"`
	Data []domain.DashboardAnomaly `json:"data"`
}

type dashboardIngestionStatusResponse struct {
	Meta dashboardMeta                 `json:"meta"`
	Data domain.IngestionStatusSummary `json:"data"`
}

func (h *Handler) dashboardOverview(w http.ResponseWriter, r *http.Request) {
	meta := h.dashboardMeta(time.Time{}, time.Time{}, "")
	if h.analytics == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "analytics service is not configured"})
		return
	}
	overview, err := h.analytics.DashboardOverview(r.Context(), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dashboardOverviewResponse{Meta: meta, Data: overview})
}

func (h *Handler) dashboardCostTimeseries(w http.ResponseWriter, r *http.Request) {
	from, to, meta, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, dashboardTimeseriesResponse{Meta: meta, Data: []grafanaCostPoint{}})
		return
	}
	window := grafanaWindow(r)
	rows, err := h.analytics.DashboardCostSummaries(r.Context(), window, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	points := make([]grafanaCostPoint, 0, len(rows))
	for _, row := range rows {
		points = append(points, grafanaCostPoint{
			Time:      row.PeriodStart,
			Metric:    grafanaMetricFromDashboard(row),
			Value:     row.TotalCost,
			Provider:  row.Provider,
			AccountID: row.AccountID,
			Service:   row.Service,
		})
	}
	writeJSON(w, http.StatusOK, dashboardTimeseriesResponse{Meta: meta, Data: points})
}

func (h *Handler) dashboardCostByCategory(w http.ResponseWriter, r *http.Request) {
	h.dashboardBreakdown(w, r, "category")
}

func (h *Handler) dashboardCostByProvider(w http.ResponseWriter, r *http.Request) {
	h.dashboardBreakdown(w, r, "provider")
}

func (h *Handler) dashboardCostByService(w http.ResponseWriter, r *http.Request) {
	h.dashboardBreakdown(w, r, "service")
}

func (h *Handler) dashboardBreakdown(w http.ResponseWriter, r *http.Request, dimension string) {
	from, to, meta, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, dashboardBreakdownResponse{Meta: meta, Data: []dashboardBreakdownRow{}})
		return
	}
	rows, err := h.analytics.DashboardCostSummaries(r.Context(), "daily", from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type key struct {
		name     string
		provider domain.Provider
	}
	totals := map[key]float64{}
	for _, row := range rows {
		k := key{name: row.Category}
		switch dimension {
		case "provider":
			k.name = string(row.Provider)
			k.provider = row.Provider
		case "service":
			k.name = row.Service
			k.provider = row.Provider
		}
		if k.name == "" {
			k.name = "unknown"
		}
		totals[k] += row.TotalCost
	}
	out := make([]dashboardBreakdownRow, 0, len(totals))
	for k, total := range totals {
		out = append(out, dashboardBreakdownRow{Name: k.name, Provider: k.provider, TotalCost: total})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCost == out[j].TotalCost {
			return out[i].Name < out[j].Name
		}
		return out[i].TotalCost > out[j].TotalCost
	})
	limit := grafanaLimit(r, 15)
	if dimension != "service" {
		limit = len(out)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	for i := range out {
		out[i].Rank = i + 1
	}
	writeJSON(w, http.StatusOK, dashboardBreakdownResponse{Meta: meta, Data: out})
}

func (h *Handler) dashboardAnomalies(w http.ResponseWriter, r *http.Request) {
	from, to, meta, ok := h.dashboardRange(r)
	if !ok {
		meta.Message = "no anomalies detected"
		writeJSON(w, http.StatusOK, dashboardAnomaliesResponse{Meta: meta, Data: []domain.DashboardAnomaly{}})
		return
	}
	severity := r.URL.Query().Get("severity")
	rows, err := h.analytics.DashboardAnomalies(r.Context(), from, to, severity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []domain.DashboardAnomaly{}
	}
	if len(rows) == 0 {
		meta.Message = "no anomalies detected"
	}
	writeJSON(w, http.StatusOK, dashboardAnomaliesResponse{Meta: meta, Data: rows})
}

func (h *Handler) dashboardIngestionStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.analytics.LatestIngestionStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if status.Providers == nil {
		status.Providers = []domain.ProviderIngestionStatus{}
	}
	writeJSON(w, http.StatusOK, dashboardIngestionStatusResponse{
		Meta: h.dashboardMeta(time.Time{}, time.Time{}, ""),
		Data: status,
	})
}

func (h *Handler) dashboardRange(r *http.Request) (time.Time, time.Time, dashboardMeta, bool) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -h.lookbackDays)
	if v := r.URL.Query().Get("from"); v != "" {
		t, _, err := parseDashboardTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, h.dashboardMeta(from, to, "from must use YYYY-MM-DD or RFC3339"), false
		}
		from = t.UTC()
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, dateOnly, err := parseDashboardTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, h.dashboardMeta(from, to, "to must use YYYY-MM-DD or RFC3339"), false
		}
		to = t.UTC()
		if dateOnly {
			to = to.AddDate(0, 0, 1)
		}
	}
	availableFrom := now.AddDate(0, 0, -h.retentionDays)
	if !to.After(from) {
		return from, to, h.dashboardMeta(from, to, "empty range: to must be after from"), false
	}
	if !to.After(availableFrom) || from.After(now) {
		return from, to, h.dashboardMeta(from, to, "requested range is outside the 90-day retention window"), false
	}
	if from.Before(availableFrom) {
		from = availableFrom
	}
	maxFrom := to.AddDate(0, 0, -h.retentionDays)
	if from.Before(maxFrom) {
		from = maxFrom
	}
	return from, to, h.dashboardMeta(from, to, ""), true
}

func parseDashboardTime(value string) (time.Time, bool, error) {
	if t, err := time.Parse(time.DateOnly, value); err == nil {
		return t.UTC(), true, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), false, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	return t.UTC(), false, err
}

func (h *Handler) dashboardMeta(from, to time.Time, message string) dashboardMeta {
	return dashboardMeta{
		From:          from.UTC(),
		To:            to.UTC(),
		RetentionDays: h.retentionDays,
		Source:        "precomputed",
		Message:       message,
		GeneratedAt:   time.Now().UTC(),
	}
}

func grafanaMetricFromDashboard(row domain.DashboardCostSummary) string {
	service := row.Service
	if service == "" {
		service = "unknown"
	}
	if row.AccountID == "" {
		return string(row.Provider) + "/" + service
	}
	return string(row.Provider) + "/" + row.AccountID + "/" + service
}
