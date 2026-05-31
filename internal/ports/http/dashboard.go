package httpapi

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
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

type dashboardCostSummary struct {
	TotalCost float64 `json:"total_cost"`
}

type dashboardCostSummaryResponse struct {
	Meta dashboardMeta        `json:"meta"`
	Data dashboardCostSummary `json:"data"`
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

type dashboardFilterOption struct {
	Text  string `json:"__text"`
	Value string `json:"__value"`
}

type dashboardFilterOptionsResponse struct {
	Meta dashboardMeta           `json:"meta"`
	Data []dashboardFilterOption `json:"data"`
}

type dashboardAnomaliesResponse struct {
	Meta dashboardMeta             `json:"meta"`
	Data []domain.DashboardAnomaly `json:"data"`
}

type dashboardCostIncreasesResponse struct {
	Meta dashboardMeta         `json:"meta"`
	Data []domain.CostVariance `json:"data"`
}

const dashboardIncompleteDailySpendRatio = 0.25

type dashboardCostDriverRow struct {
	Region      string  `json:"region"`
	Compartment string  `json:"compartment"`
	Service     string  `json:"service"`
	TotalCost   float64 `json:"total_cost"`
	Percentage  float64 `json:"percentage"`
}

type dashboardCostDriversResponse struct {
	Meta dashboardMeta            `json:"meta"`
	Data []dashboardCostDriverRow `json:"data"`
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
	provider, hasProvider := dashboardProvider(r)
	var (
		overview domain.DashboardOverview
		err      error
	)
	if hasProvider {
		overview, err = h.dashboardOverviewForProvider(r.Context(), provider, time.Now().UTC())
	} else {
		overview, err = h.analytics.DashboardOverview(r.Context(), time.Now().UTC())
	}
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
	provider, hasProvider := dashboardProvider(r)
	type timeseriesKey struct {
		at        time.Time
		provider  domain.Provider
		accountID string
	}
	totals := map[timeseriesKey]float64{}
	filters := dashboardFiltersFromRequest(r)
	for _, row := range rows {
		if hasProvider && row.Provider != provider {
			continue
		}
		if !dashboardRowMatchesFilters(row, filters) {
			continue
		}
		k := timeseriesKey{at: row.PeriodStart, provider: row.Provider, accountID: row.AccountID}
		totals[k] += row.TotalCost
	}
	points := make([]grafanaCostPoint, 0, len(totals))
	for k, total := range totals {
		points = append(points, grafanaCostPoint{
			Time:      k.at,
			Metric:    dashboardTotalMetric(k.provider, k.accountID),
			Value:     total,
			Provider:  k.provider,
			AccountID: k.accountID,
			Service:   "total",
		})
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Time.Equal(points[j].Time) {
			if points[i].Provider == points[j].Provider {
				return points[i].AccountID < points[j].AccountID
			}
			return points[i].Provider < points[j].Provider
		}
		return points[i].Time.Before(points[j].Time)
	})
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

func (h *Handler) dashboardCostByCompartment(w http.ResponseWriter, r *http.Request) {
	h.dashboardBreakdown(w, r, "compartment")
}

func (h *Handler) dashboardCostByRegion(w http.ResponseWriter, r *http.Request) {
	h.dashboardBreakdown(w, r, "region")
}

func (h *Handler) dashboardFilterOptions(w http.ResponseWriter, r *http.Request) {
	dimension := strings.TrimSpace(r.URL.Query().Get("dimension"))
	switch dimension {
	case "region", "compartment", "service":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dimension must be one of region, compartment, service"})
		return
	}
	from, to, meta, ok := h.dashboardFilterOptionsRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, dashboardFilterOptionsResponse{Meta: meta, Data: []dashboardFilterOption{}})
		return
	}
	rows, err := h.analytics.DashboardCostSummaries(r.Context(), "daily", from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	provider, hasProvider := dashboardProvider(r)
	filters := dashboardFiltersFromRequest(r)
	switch dimension {
	case "region":
		filters.Region = ""
	case "compartment":
		filters.Compartment = ""
	case "service":
		filters.Service = ""
	}
	values := map[string]struct{}{}
	for _, row := range rows {
		if hasProvider && row.Provider != provider {
			continue
		}
		if !dashboardRowMatchesFilters(row, filters) {
			continue
		}
		values[dashboardDimensionValue(row, dimension)] = struct{}{}
	}
	out := make([]dashboardFilterOption, 0, len(values))
	for value := range values {
		out = append(out, dashboardFilterOption{Text: value, Value: value})
	}
	sort.Slice(out, func(i, j int) bool {
		left := strings.ToLower(out[i].Text)
		right := strings.ToLower(out[j].Text)
		if left == right {
			return out[i].Text < out[j].Text
		}
		return left < right
	})
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "values") {
		flat := make([]string, 0, len(out))
		for _, option := range out {
			flat = append(flat, option.Value)
		}
		writeJSON(w, http.StatusOK, flat)
		return
	}
	writeJSON(w, http.StatusOK, dashboardFilterOptionsResponse{Meta: meta, Data: out})
}

func (h *Handler) dashboardOCICostSummary(w http.ResponseWriter, r *http.Request) {
	from, to, meta, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, dashboardCostSummaryResponse{Meta: meta, Data: dashboardCostSummary{}})
		return
	}
	rows, err := h.analytics.DashboardCostSummaries(r.Context(), "daily", from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	filters := dashboardFiltersFromRequest(r)
	var total float64
	for _, row := range rows {
		if row.Provider != domain.ProviderOCI || !dashboardRowMatchesFilters(row, filters) {
			continue
		}
		total += row.TotalCost
	}
	writeJSON(w, http.StatusOK, dashboardCostSummaryResponse{Meta: meta, Data: dashboardCostSummary{TotalCost: total}})
}

func (h *Handler) dashboardCostIncreases(w http.ResponseWriter, r *http.Request) {
	h.dashboardCostMovements(w, r, "increase")
}

func (h *Handler) dashboardCostDecreases(w http.ResponseWriter, r *http.Request) {
	h.dashboardCostMovements(w, r, "decrease")
}

func (h *Handler) dashboardCostMovements(w http.ResponseWriter, r *http.Request, direction string) {
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

	currentFrom, currentTo, previousFrom, previousTo := dashboardIncreaseWindows(period, asOf)
	if period == "daily" {
		var err error
		currentFrom, currentTo, previousFrom, previousTo, err = h.dashboardUsableDailyIncreaseWindow(r.Context(), r, currentFrom, currentTo, previousFrom, previousTo)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	rows, err := h.analytics.CompareVarianceWindows(r.Context(), period, currentFrom, currentTo, previousFrom, previousTo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	provider, hasProvider := dashboardProvider(r)
	out := make([]domain.CostVariance, 0, len(rows))
	for _, row := range rows {
		if hasProvider && row.Provider != provider {
			continue
		}
		if !dashboardCostMovementMatches(row, direction) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		left := math.Abs(out[i].Delta)
		right := math.Abs(out[j].Delta)
		if left == right {
			if out[i].CompartmentName == out[j].CompartmentName {
				return out[i].Service < out[j].Service
			}
			return out[i].CompartmentName < out[j].CompartmentName
		}
		return left > right
	})
	limit := grafanaLimit(r, 15)
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []domain.CostVariance{}
	}

	meta := h.dashboardMeta(currentFrom, currentTo, "")
	writeJSON(w, http.StatusOK, dashboardCostIncreasesResponse{Meta: meta, Data: out})
}

func dashboardCostMovementMatches(row domain.CostVariance, direction string) bool {
	switch direction {
	case "decrease":
		return row.Direction == "decrease" && row.Delta < 0
	default:
		return row.Direction == "increase" && row.Delta > 0
	}
}

func (h *Handler) dashboardUsableDailyIncreaseWindow(ctx context.Context, r *http.Request, currentFrom, currentTo, previousFrom, previousTo time.Time) (time.Time, time.Time, time.Time, time.Time, error) {
	originalCurrentFrom, originalCurrentTo := currentFrom, currentTo
	originalPreviousFrom, originalPreviousTo := previousFrom, previousTo
	for i := 0; i < 7; i++ {
		usable, err := h.dashboardDailyWindowHasUsableSpend(ctx, r, currentFrom, currentTo, previousFrom, previousTo)
		if err != nil {
			return time.Time{}, time.Time{}, time.Time{}, time.Time{}, err
		}
		if usable {
			return currentFrom, currentTo, previousFrom, previousTo, nil
		}
		currentTo = currentFrom
		currentFrom = currentFrom.AddDate(0, 0, -1)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, 0, -1)
	}
	return originalCurrentFrom, originalCurrentTo, originalPreviousFrom, originalPreviousTo, nil
}

func (h *Handler) dashboardDailyWindowHasUsableSpend(ctx context.Context, r *http.Request, currentFrom, currentTo, previousFrom, previousTo time.Time) (bool, error) {
	rows, err := h.analytics.DashboardCostSummaries(ctx, "daily", previousFrom, currentTo)
	if err != nil {
		return false, err
	}
	provider, hasProvider := dashboardProvider(r)
	var currentTotal, previousTotal float64
	for _, row := range rows {
		if hasProvider && row.Provider != provider {
			continue
		}
		switch {
		case !row.PeriodStart.Before(currentFrom) && row.PeriodStart.Before(currentTo):
			currentTotal += row.TotalCost
		case !row.PeriodStart.Before(previousFrom) && row.PeriodStart.Before(previousTo):
			previousTotal += row.TotalCost
		}
	}
	if currentTotal <= 0 {
		return false, nil
	}
	if previousTotal > 0 && currentTotal/previousTotal < dashboardIncompleteDailySpendRatio {
		return false, nil
	}
	return true, nil
}

func dashboardIncreaseWindows(period string, asOf time.Time) (currentFrom, currentTo, previousFrom, previousTo time.Time) {
	currentTo = time.Date(asOf.UTC().Year(), asOf.UTC().Month(), asOf.UTC().Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case "weekly":
		currentFrom = currentTo.AddDate(0, 0, -7)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, 0, -7)
	case "monthly":
		currentFrom = currentTo.AddDate(0, -1, 0)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, -1, 0)
	default:
		currentFrom = currentTo.AddDate(0, 0, -1)
		previousTo = currentFrom
		previousFrom = previousTo.AddDate(0, 0, -1)
	}
	return currentFrom, currentTo, previousFrom, previousTo
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
	provider, hasProvider := dashboardProvider(r)
	filters := dashboardFiltersFromRequest(r)
	for _, row := range rows {
		if hasProvider && row.Provider != provider {
			continue
		}
		if !dashboardRowMatchesFilters(row, filters) {
			continue
		}
		k := key{name: row.Category}
		switch dimension {
		case "provider":
			k.name = string(row.Provider)
			k.provider = row.Provider
		case "service":
			k.name = dashboardDimensionValue(row, "service")
			k.provider = row.Provider
		case "compartment":
			k.name = dashboardDimensionValue(row, "compartment")
			k.provider = row.Provider
		case "region":
			k.name = dashboardDimensionValue(row, "region")
			k.provider = row.Provider
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
	if dimension != "service" && dimension != "compartment" {
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

func (h *Handler) dashboardOCICostDrivers(w http.ResponseWriter, r *http.Request) {
	from, to, meta, ok := h.dashboardRange(r)
	if !ok {
		writeJSON(w, http.StatusOK, dashboardCostDriversResponse{Meta: meta, Data: []dashboardCostDriverRow{}})
		return
	}
	rows, err := h.analytics.DashboardCostSummaries(r.Context(), "daily", from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	type key struct {
		region      string
		compartment string
		service     string
	}
	filters := dashboardFiltersFromRequest(r)
	totals := map[key]float64{}
	var grandTotal float64
	for _, row := range rows {
		if row.Provider != domain.ProviderOCI || !dashboardRowMatchesFilters(row, filters) {
			continue
		}
		k := key{
			region:      dashboardDimensionValue(row, "region"),
			compartment: dashboardDimensionValue(row, "compartment"),
			service:     dashboardDimensionValue(row, "service"),
		}
		totals[k] += row.TotalCost
		grandTotal += row.TotalCost
	}
	out := make([]dashboardCostDriverRow, 0, len(totals))
	for k, total := range totals {
		percentage := 0.0
		if grandTotal > 0 {
			percentage = (total / grandTotal) * 100
		}
		out = append(out, dashboardCostDriverRow{
			Region:      k.region,
			Compartment: k.compartment,
			Service:     k.service,
			TotalCost:   total,
			Percentage:  percentage,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCost == out[j].TotalCost {
			if out[i].Region == out[j].Region {
				if out[i].Compartment == out[j].Compartment {
					return out[i].Service < out[j].Service
				}
				return out[i].Compartment < out[j].Compartment
			}
			return out[i].Region < out[j].Region
		}
		return out[i].TotalCost > out[j].TotalCost
	})
	limit := grafanaLimit(r, 15)
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, dashboardCostDriversResponse{Meta: meta, Data: out})
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
	if provider, hasProvider := dashboardProvider(r); hasProvider {
		filtered := rows[:0]
		for _, row := range rows {
			if row.Provider == provider {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
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

func (h *Handler) dashboardFilterOptionsRange(r *http.Request) (time.Time, time.Time, dashboardMeta, bool) {
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
		return h.dashboardRange(r)
	}
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -h.retentionDays)
	return from, now, h.dashboardMeta(from, now, ""), true
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

func (h *Handler) dashboardOverviewForProvider(ctx context.Context, provider domain.Provider, now time.Time) (domain.DashboardOverview, error) {
	now = now.UTC()
	currentTo := now
	currentFrom := currentTo.AddDate(0, 0, -30)
	previousTo := currentFrom
	previousFrom := previousTo.AddDate(0, 0, -30)

	rows, err := h.analytics.DashboardCostSummaries(ctx, "daily", previousFrom, currentTo)
	if err != nil {
		return domain.DashboardOverview{}, err
	}
	var current, previous float64
	for _, row := range rows {
		if row.Provider != provider {
			continue
		}
		switch {
		case !row.PeriodStart.Before(currentFrom) && row.PeriodStart.Before(currentTo):
			current += row.TotalCost
		case !row.PeriodStart.Before(previousFrom) && row.PeriodStart.Before(previousTo):
			previous += row.TotalCost
		}
	}
	percentageChange := 0.0
	if previous > 0 {
		percentageChange = ((current - previous) / previous) * 100
	} else if current > 0 {
		percentageChange = 100
	}

	anomalies, err := h.analytics.DashboardAnomalies(ctx, currentFrom, currentTo, "")
	if err != nil {
		return domain.DashboardOverview{}, err
	}
	anomalyCount := 0
	for _, row := range anomalies {
		if row.Provider == provider {
			anomalyCount++
		}
	}

	status, err := h.analytics.LatestIngestionStatus(ctx)
	if err != nil {
		return domain.DashboardOverview{}, err
	}
	latest := status.LatestIngestionAt
	for _, row := range status.Providers {
		if row.Provider != provider {
			continue
		}
		if row.LastSuccessfulIngestionAt.After(latest) {
			latest = row.LastSuccessfulIngestionAt
		}
		if row.LatestReportProcessedAt.After(latest) {
			latest = row.LatestReportProcessedAt
		}
	}

	return domain.DashboardOverview{
		Current30DaySpend:  current,
		Previous30DaySpend: previous,
		PercentageChange:   percentageChange,
		AnomalyCount:       anomalyCount,
		LatestIngestionAt:  latest,
	}, nil
}

func dashboardProvider(r *http.Request) (domain.Provider, bool) {
	value := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("provider")))
	if value == "" {
		return "", false
	}
	return domain.Provider(value), true
}

type dashboardFilters struct {
	Region      string
	Compartment string
	Service     string
}

func dashboardFiltersFromRequest(r *http.Request) dashboardFilters {
	return dashboardFilters{
		Region:      dashboardNormalizeFilter(r.URL.Query().Get("region")),
		Compartment: dashboardNormalizeFilter(r.URL.Query().Get("compartment")),
		Service:     dashboardNormalizeFilter(r.URL.Query().Get("service")),
	}
}

func dashboardNormalizeFilter(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "all", "$__all", "__all", "*":
		return ""
	default:
		return value
	}
}

func dashboardRowMatchesFilters(row domain.DashboardCostSummary, filters dashboardFilters) bool {
	if filters.Region != "" && dashboardDimensionValue(row, "region") != filters.Region {
		return false
	}
	if filters.Compartment != "" && dashboardDimensionValue(row, "compartment") != filters.Compartment {
		return false
	}
	if filters.Service != "" && dashboardDimensionValue(row, "service") != filters.Service {
		return false
	}
	return true
}

func dashboardDimensionValue(row domain.DashboardCostSummary, dimension string) string {
	var value string
	switch dimension {
	case "region":
		value = row.Region
	case "compartment":
		value = row.CompartmentName
		if strings.TrimSpace(value) == "" {
			value = row.CompartmentID
		}
	case "service":
		value = row.Service
	default:
		value = row.Category
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
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

func dashboardTotalMetric(provider domain.Provider, accountID string) string {
	if accountID == "" {
		return string(provider) + "/total"
	}
	return string(provider) + "/" + accountID + "/total"
}
