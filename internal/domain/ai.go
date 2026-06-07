package domain

import "time"

const (
	AIEntityAnomaly = "anomaly"
	AIEntityWaste   = "waste_finding"

	AIStatusCompleted = "completed"
	AIStatusFailed    = "failed"
)

type AIEnrichment struct {
	EntityType         string     `json:"entity_type"`
	EntityID           int64      `json:"entity_id"`
	InputHash          string     `json:"-"`
	Provider           string     `json:"provider"`
	Model              string     `json:"model"`
	Status             string     `json:"status"`
	Summary            string     `json:"summary"`
	LikelyCause        string     `json:"likely_cause"`
	BusinessImpact     string     `json:"business_impact"`
	RecommendedActions []string   `json:"recommended_actions"`
	Priority           string     `json:"priority"`
	Confidence         float64    `json:"confidence"`
	Error              string     `json:"-"`
	GeneratedAt        *time.Time `json:"generated_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}
