package common

import "net/http"

// Error is a service-layer error carrying an API error code that handlers
// can translate into an HTTP error response.
type Error struct {
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Message }

// NewError creates a coded error, e.g. NewError("bad_request", "模板类型不匹配", nil).
func NewError(code, message string, details map[string]any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

// StatusForCode maps an API error code to an HTTP status, defaulting to 400.
func StatusForCode(code string) int {
	switch code {
	case "unauthorized":
		return http.StatusUnauthorized
	case "permission_denied":
		return http.StatusForbidden
	case "not_found", "template_not_found", "run_step_not_found", "experiment_run_not_found":
		return http.StatusNotFound
	case "status_conflict", "duplicate_idempotency_key":
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
