package common

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SuccessResponse is the standard success envelope.
type SuccessResponse struct {
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

// ErrorDetail carries a single error code and human-readable message.
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id"`
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) (err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err = json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("json encode failed", "error", err)
	}
	return err
}

// WriteSuccess writes a 200 OK success response.
func WriteSuccess(w http.ResponseWriter, r *http.Request, data any) {
	WriteJSON(w, http.StatusOK, SuccessResponse{
		Data:      data,
		RequestID: GetRequestID(r.Context()),
	})
}

// WriteCreated writes a 201 Created success response.
func WriteCreated(w http.ResponseWriter, r *http.Request, data any) {
	WriteJSON(w, http.StatusCreated, SuccessResponse{
		Data:      data,
		RequestID: GetRequestID(r.Context()),
	})
}

// WriteError writes a structured error response.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
			Details: details,
		},
		RequestID: GetRequestID(r.Context()),
	})
}
