package httpapi

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
