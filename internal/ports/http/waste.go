package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func (h *Handler) wasteSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.waste == nil {
		writeJSON(w, http.StatusOK, waste.Summary{
			FindingsBySeverity: map[string]int64{},
			FindingsByRule:     map[string]int64{},
			FindingsByRegion:   map[string]int64{},
			FindingsByScope:    map[string]int64{},
			FindingsByService:  map[string]int64{},
			TopFindings:        []waste.Finding{},
		})
		return
	}
	summary, err := h.waste.Summary(r.Context(), wasteFilters(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (h *Handler) wasteFindings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch || r.URL.Path != "/api/v1/waste/findings" {
		h.wasteFindingByID(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.waste == nil {
		writeJSON(w, http.StatusOK, []waste.Finding{})
		return
	}
	findings, err := h.waste.ListFindings(r.Context(), wasteFilters(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, findings)
}

func (h *Handler) wasteFindingByID(w http.ResponseWriter, r *http.Request) {
	if h.waste == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "waste finding not found"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/waste/findings/")
	statusPath := strings.HasSuffix(path, "/status")
	idPart := strings.TrimSuffix(path, "/status")
	id, err := strconv.ParseInt(strings.Trim(idPart, "/"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "waste finding not found"})
		return
	}
	if statusPath {
		h.updateWasteFindingStatus(w, r, id)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	finding, err := h.waste.GetFinding(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "waste finding not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, finding)
}

func (h *Handler) updateWasteFindingStatus(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request body"})
		return
	}
	finding, err := h.waste.UpdateFindingStatus(r.Context(), id, body.Status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "waste finding not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, finding)
}

func (h *Handler) wasteRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.waste == nil {
		writeJSON(w, http.StatusOK, []waste.RuleInfo{})
		return
	}
	writeJSON(w, http.StatusOK, h.waste.Rules())
}

func wasteFilters(r *http.Request) waste.FindingFilters {
	q := r.URL.Query()
	filters := waste.FindingFilters{
		Provider:     domain.Provider(strings.TrimSpace(q.Get("provider"))),
		Region:       strings.TrimSpace(q.Get("region")),
		ScopeID:      strings.TrimSpace(q.Get("scope_id")),
		ScopeName:    strings.TrimSpace(q.Get("scope_name")),
		Service:      strings.TrimSpace(q.Get("service")),
		ResourceType: strings.TrimSpace(q.Get("resource_type")),
		RuleID:       strings.TrimSpace(q.Get("rule_id")),
		Severity:     strings.TrimSpace(q.Get("severity")),
		Status:       strings.TrimSpace(q.Get("status")),
		Limit:        100,
	}
	if limit := positiveInt(q.Get("limit")); limit > 0 {
		filters.Limit = limit
	}
	if offset := positiveInt(q.Get("offset")); offset > 0 {
		filters.Offset = offset
	}
	if minConfidence, ok := positiveFloat(q.Get("min_confidence")); ok {
		filters.MinConfidence = &minConfidence
	}
	if minWaste, ok := positiveFloat(q.Get("min_estimated_monthly_waste")); ok {
		filters.MinEstimatedMonthlyWaste = &minWaste
	}
	return filters
}

func positiveInt(value string) int {
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func positiveFloat(value string) (float64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}
