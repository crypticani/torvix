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
