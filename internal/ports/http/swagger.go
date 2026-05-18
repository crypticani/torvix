package httpapi

import "time"

// ErrorResponse represents an API error response.
// @Description Error response returned when an operation fails.
type ErrorResponse struct {
	Error string `json:"error" example:"internal server error"`
}

// StatusResponse represents a simple status message.
// @Description Generic status response for operations.
type StatusResponse struct {
	Status string `json:"status" example:"ok"`
}

// IngestAcceptedResponse represents an accepted background ingestion job.
// @Description Response returned when ingestion has been queued for background processing.
type IngestAcceptedResponse struct {
	JobID     string    `json:"job_id" example:"1760000000000000000"`
	Status    string    `json:"status" example:"queued"`
	Message   string    `json:"message" example:"ingestion queued and running in the background"`
	StatusURL string    `json:"status_url" example:"/api/v1/ingest/status/1760000000000000000"`
	QueuedAt  time.Time `json:"queued_at"`
}

// IngestionJobResponse represents background ingestion job status.
// @Description Current or completed background ingestion job status.
type IngestionJobResponse struct {
	JobID           string           `json:"job_id" example:"1760000000000000000"`
	Status          string           `json:"status" example:"running"`
	QueuedAt        time.Time        `json:"queued_at"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	Days            int              `json:"days,omitempty" example:"30"`
	Since           time.Time        `json:"since,omitempty"`
	DurationSeconds float64          `json:"duration_seconds,omitempty" example:"12.4"`
	Results         []IngestResponse `json:"results,omitempty"`
	Error           string           `json:"error,omitempty" example:""`
}

// IngestResponse represents the result of an ingestion run for a single provider.
// @Description Per-provider ingestion result with metrics.
type IngestResponse struct {
	Provider        string  `json:"provider" example:"oci"`
	FilesProcessed  int     `json:"files_processed" example:"5"`
	FilesSkipped    int     `json:"files_skipped" example:"2"`
	SkippedOldFiles int     `json:"skipped_old_files" example:"1"`
	RecordsParsed   int     `json:"records_parsed" example:"1234"`
	RecordsInserted int     `json:"records_inserted" example:"1234"`
	DurationSeconds float64 `json:"duration_seconds" example:"12.4"`
	Error           string  `json:"error,omitempty" example:""`
}
