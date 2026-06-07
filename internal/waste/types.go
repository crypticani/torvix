package waste

import (
	"context"
	"time"

	"github.com/crypticani/torvix/internal/domain"
)

const (
	ResourceBlockVolume      = "block_volume"
	ResourceBootVolume       = "boot_volume"
	ResourceComputeInstance  = "compute_instance"
	ResourcePublicIP         = "public_ip"
	ResourceReservedPublicIP = "reserved_public_ip"

	RelationshipBlockVolumeAttachedToInstance = "block_volume_attached_to_instance"
	RelationshipBootVolumeAttachedToInstance  = "boot_volume_attached_to_instance"
	RelationshipPublicIPAssignedToPrivateIP   = "public_ip_assigned_to_private_ip"

	RuleOCIDetachedBlockVolume       = "OCI_DETACHED_BLOCK_VOLUME"
	RuleOCIDetachedBootVolume        = "OCI_DETACHED_BOOT_VOLUME"
	RuleOCIStoppedComputePaidStorage = "OCI_STOPPED_COMPUTE_WITH_PAID_STORAGE"
	RuleOCIUnusedReservedPublicIP    = "OCI_UNUSED_RESERVED_PUBLIC_IP"

	StatusOpen          = "open"
	StatusAccepted      = "accepted"
	StatusIgnored       = "ignored"
	StatusFalsePositive = "false_positive"
	StatusFixed         = "fixed"
	StatusResolved      = "resolved"
)

type Config struct {
	Enabled                bool
	Provider               domain.Provider
	ScanIntervalHours      int
	MinResourceAgeDays     int
	StoppedInstanceMinDays int
	OldBackupDays          int
	MinCostThreshold       float64
	HighMonthlyThreshold   float64
	Currency               string
	EnableTagExclusions    bool
	ExclusionTagKeys       []string
}

type Resource struct {
	ID                 int64
	Provider           domain.Provider
	ResourceID         string
	ResourceName       string
	ResourceType       string
	Region             string
	ScopeID            string
	ScopeName          string
	LifecycleState     string
	AvailabilityDomain string
	TimeCreated        *time.Time
	Tags               map[string]string
	Raw                map[string]any
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	LastSeenRunID      string
	Active             bool
	MissingSince       *time.Time
	InactiveAt         *time.Time
}

type Relationship struct {
	ID               int64
	Provider         domain.Provider
	SourceResourceID string
	TargetResourceID string
	RelationshipType string
	Region           string
	ScopeID          string
	DetectedAt       time.Time
	Raw              map[string]any
}

type InventoryRun struct {
	ID        string
	Provider  domain.Provider
	Region    string
	ScopeID   string
	Status    string
	StartedAt time.Time
	Metadata  map[string]any
}

type RelationshipScope struct {
	Provider         domain.Provider
	Region           string
	ScopeID          string
	RelationshipType string
}

type CostSignal struct {
	Last7dCost  float64
	Last30dCost float64
	Currency    string
	HasLast7d   bool
	HasLast30d  bool
}

type Finding struct {
	ID                    int64
	Provider              domain.Provider
	ResourceID            string
	ResourceName          string
	ResourceType          string
	Region                string
	ScopeID               string
	ScopeName             string
	Service               string
	RuleID                string
	Severity              string
	Confidence            float64
	EstimatedMonthlyWaste *float64
	Currency              string
	Summary               string
	Recommendation        string
	Evidence              map[string]any
	Status                string
	DetectedAt            time.Time
	LastSeenAt            time.Time
	ResolvedAt            *time.Time
	AIEnrichment          *domain.AIEnrichment `json:"ai_enrichment,omitempty"`
}

type FindingFilters struct {
	Provider                 domain.Provider
	Region                   string
	ScopeID                  string
	ScopeName                string
	Service                  string
	ResourceType             string
	RuleID                   string
	Severity                 string
	Status                   string
	MinConfidence            *float64
	MinEstimatedMonthlyWaste *float64
	Limit                    int
	Offset                   int
}

type Summary struct {
	TotalOpenFindings     int64
	EstimatedMonthlyWaste float64
	FindingsBySeverity    map[string]int64
	FindingsByRule        map[string]int64
	FindingsByRegion      map[string]int64
	FindingsByScope       map[string]int64
	FindingsByService     map[string]int64
	TopFindings           []Finding
}

type RuleInfo struct {
	RuleID         string `json:"rule_id"`
	Provider       string `json:"provider"`
	ResourceType   string `json:"resource_type"`
	SeverityBasis  string `json:"severity_basis"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

type InventoryResult struct {
	Provider              domain.Provider
	ResourcesScanned      int
	RelationshipsScanned  int
	ResourcesUpserted     int
	RelationshipsUpserted int
	Skipped               bool
	Message               string
}

type DetectionResult struct {
	Provider         domain.Provider
	ResourcesScanned int
	FindingsCreated  int
	FindingsUpdated  int
	FindingsResolved int
	ResourcesSkipped int
	Skipped          bool
	Message          string
	Duration         time.Duration
}

type InventoryProvider interface {
	Provider() domain.Provider
	Sync(ctx context.Context) (InventoryResult, error)
}

type Repository interface {
	StartCloudInventoryRun(ctx context.Context, run InventoryRun) (string, error)
	CompleteCloudInventoryRun(ctx context.Context, runID, status, errMessage string) error
	MarkMissingCloudResourcesInactive(ctx context.Context, provider domain.Provider, region, runID string) (int, error)
	UpsertCloudResources(ctx context.Context, resources []Resource) error
	ReplaceCloudRelationships(ctx context.Context, provider domain.Provider, relationships []Relationship) error
	ReplaceCloudRelationshipsScoped(ctx context.Context, scope RelationshipScope, relationships []Relationship) error
	ListCloudResources(ctx context.Context, provider domain.Provider) ([]Resource, error)
	ListCloudRelationships(ctx context.Context, provider domain.Provider) ([]Relationship, error)
	GetResourceCostSignal(ctx context.Context, provider domain.Provider, resourceID string, now time.Time) (CostSignal, error)
	UpsertWasteFinding(ctx context.Context, finding Finding) (string, error)
	ResolveMissingWasteFindings(ctx context.Context, provider domain.Provider, ruleIDs []string, seen map[string]struct{}) (int, error)
	ListWasteFindings(ctx context.Context, filters FindingFilters) ([]Finding, error)
	GetWasteFinding(ctx context.Context, id int64) (Finding, error)
	UpdateWasteFindingStatus(ctx context.Context, id int64, status string) (Finding, error)
}

type Detector interface {
	Run(ctx context.Context) (DetectionResult, error)
	Rules() []RuleInfo
	ListFindings(ctx context.Context, filters FindingFilters) ([]Finding, error)
	GetFinding(ctx context.Context, id int64) (Finding, error)
	UpdateFindingStatus(ctx context.Context, id int64, status string) (Finding, error)
	Summary(ctx context.Context, filters FindingFilters) (Summary, error)
}
