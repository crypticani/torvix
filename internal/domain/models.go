package domain

import "time"

type Provider string

const (
	ProviderAWS   Provider = "aws"
	ProviderAzure Provider = "azure"
	ProviderGCP   Provider = "gcp"
	ProviderOCI   Provider = "oci"
)

type RawBillingRecord struct {
	Provider     Provider
	AccountID    string
	UsageStart   time.Time
	UsageEnd     time.Time
	Service      string
	Category     string
	SKU          string
	Region       string
	ResourceID   string
	Currency     string
	Cost         float64
	UsageAmount  float64
	UsageUnit    string
	Tags         map[string]string
	Meter        string
	RawData      map[string]any
	SourceObject string
}

type CanonicalCostRecord struct {
	Timestamp    time.Time
	Provider     Provider
	AccountID    string
	Service      string
	Category     string
	Region       string
	ResourceID   string
	Currency     string
	Cost         float64
	UsageAmount  float64
	UsageUnit    string
	Tags         map[string]string
	Meter        string
	RawData      map[string]any
	SourceObject string
}

type ProcessedReportFile struct {
	Provider     Provider
	Bucket       string
	ObjectName   string
	ETag         string
	LastModified time.Time
	ProcessedAt  time.Time
	RecordCount  int
	Status       string
	ErrorMessage string
}

type DataLifecycleMaintenance struct {
	RecordsDeleted   int64
	CompressedChunks int64
}

type DashboardCostSummary struct {
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	Provider           Provider  `json:"provider"`
	AccountID          string    `json:"account_id"`
	CompartmentID      string    `json:"compartment_id"`
	CompartmentName    string    `json:"compartment_name"`
	Service            string    `json:"service"`
	Category           string    `json:"category"`
	Region             string    `json:"region"`
	TotalCost          float64   `json:"total_cost"`
	PreviousPeriodCost float64   `json:"previous_period_cost"`
	AbsoluteChange     float64   `json:"absolute_change"`
	PercentageChange   float64   `json:"percentage_change"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type DashboardAnomaly struct {
	DetectedAt      time.Time `json:"detected_at"`
	PeriodStart     time.Time `json:"period_start"`
	Provider        Provider  `json:"provider"`
	AccountID       string    `json:"account_id"`
	Category        string    `json:"category"`
	Service         string    `json:"service"`
	Region          string    `json:"region"`
	ObservedCost    float64   `json:"observed_cost"`
	ExpectedCost    float64   `json:"expected_cost"`
	AbsoluteDelta   float64   `json:"absolute_delta"`
	PercentageDelta float64   `json:"percentage_delta"`
	Severity        string    `json:"severity"`
	DetectionMethod string    `json:"detection_method"`
	Explanation     string    `json:"explanation"`
	CreatedAt       time.Time `json:"created_at"`
}

type DashboardOverview struct {
	Current30DaySpend  float64   `json:"current_30_day_spend"`
	Previous30DaySpend float64   `json:"previous_30_day_spend"`
	PercentageChange   float64   `json:"percentage_change"`
	AnomalyCount       int       `json:"anomaly_count"`
	LatestIngestionAt  time.Time `json:"latest_ingestion_at"`
}

type ProviderIngestionStatus struct {
	Provider                  Provider  `json:"provider"`
	LastSuccessfulIngestionAt time.Time `json:"last_successful_ingestion_at"`
	CheckpointUpdatedAt       time.Time `json:"checkpoint_updated_at"`
	LatestReportProcessedAt   time.Time `json:"latest_report_processed_at"`
	FilesProcessed            int64     `json:"files_processed"`
	RecordsProcessed          int64     `json:"records_processed"`
	LastStatus                string    `json:"last_status"`
	LastError                 string    `json:"last_error"`
}

type IngestionStatusSummary struct {
	LatestIngestionAt time.Time                 `json:"latest_ingestion_at"`
	Providers         []ProviderIngestionStatus `json:"providers"`
}

type AggregatedCost struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Provider    Provider  `json:"provider"`
	AccountID   string    `json:"account_id"`
	Service     string    `json:"service"`
	TotalCost   float64   `json:"total_cost"`
}

type CostVariance struct {
	Period              string    `json:"period"`
	CurrentWindowStart  time.Time `json:"current_window_start"`
	CurrentWindowEnd    time.Time `json:"current_window_end"`
	PreviousWindowStart time.Time `json:"previous_window_start"`
	PreviousWindowEnd   time.Time `json:"previous_window_end"`
	Provider            Provider  `json:"provider"`
	AccountID           string    `json:"account_id"`
	Service             string    `json:"service"`
	CompartmentID       string    `json:"compartment_id"`
	CompartmentName     string    `json:"compartment_name"`
	CurrentCost         float64   `json:"current_cost"`
	PreviousCost        float64   `json:"previous_cost"`
	Delta               float64   `json:"delta"`
	PercentChange       float64   `json:"percent_change"`
	Direction           string    `json:"direction"`
}

type Anomaly struct {
	Date               time.Time `json:"date"`
	Provider           Provider  `json:"provider"`
	AccountID          string    `json:"account_id"`
	Service            string    `json:"service"`
	Baseline           float64   `json:"baseline"`
	Actual             float64   `json:"actual"`
	ZScore             float64   `json:"z_score"`
	PercentDeviation   float64   `json:"percent_deviation"`
	MovingAverageDelta float64   `json:"moving_average_delta"`
	Severity           string    `json:"severity"`
}

type ForecastPoint struct {
	Date           time.Time `json:"date"`
	Provider       Provider  `json:"provider"`
	AccountID      string    `json:"account_id"`
	Service        string    `json:"service"`
	ForecastCost   float64   `json:"forecast_cost"`
	ConfidenceLow  float64   `json:"confidence_low"`
	ConfidenceHigh float64   `json:"confidence_high"`
}

type Report struct {
	Period    string           `json:"period"`
	Generated time.Time        `json:"generated"`
	Summary   []AggregatedCost `json:"summary"`
	Anomalies []Anomaly        `json:"anomalies"`
	Forecast  []ForecastPoint  `json:"forecast"`
}
