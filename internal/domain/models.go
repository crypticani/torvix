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
