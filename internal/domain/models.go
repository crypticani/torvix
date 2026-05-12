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
	SKU          string
	Region       string
	ResourceID   string
	Currency     string
	Cost         float64
	UsageAmount  float64
	UsageUnit    string
	Tags         map[string]string
	SourceObject string
}

type CanonicalCostRecord struct {
	Date         time.Time
	Provider     Provider
	AccountID    string
	Service      string
	Region       string
	ResourceID   string
	Currency     string
	Cost         float64
	UsageAmount  float64
	UsageUnit    string
	Tags         map[string]string
	SourceObject string
}

type AggregatedCost struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Provider    Provider  `json:"provider"`
	AccountID   string    `json:"account_id"`
	Service     string    `json:"service"`
	TotalCost   float64   `json:"total_cost"`
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
	Date          time.Time `json:"date"`
	Provider      Provider  `json:"provider"`
	AccountID     string    `json:"account_id"`
	Service       string    `json:"service"`
	ForecastCost  float64   `json:"forecast_cost"`
	ConfidenceLow float64   `json:"confidence_low"`
	ConfidenceHigh float64  `json:"confidence_high"`
}

type Report struct {
	Period    string           `json:"period"`
	Generated time.Time        `json:"generated"`
	Summary   []AggregatedCost `json:"summary"`
	Anomalies []Anomaly        `json:"anomalies"`
	Forecast  []ForecastPoint  `json:"forecast"`
}
