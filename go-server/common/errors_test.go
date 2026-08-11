package common

import (
	"net/http"
	"testing"
)

func TestNewError(t *testing.T) {
	e := NewError("bad_request", "模板类型不匹配", map[string]any{"kind": "x"})
	if e.Code != "bad_request" || e.Message != "模板类型不匹配" || e.Details["kind"] != "x" {
		t.Fatalf("NewError: %+v", e)
	}
	if e.Error() != "模板类型不匹配" {
		t.Fatalf("Error() = %q", e.Error())
	}
}

func TestStatusForCode(t *testing.T) {
	cases := map[string]int{
		"unauthorized":              http.StatusUnauthorized,
		"permission_denied":         http.StatusForbidden,
		"not_found":                 http.StatusNotFound,
		"template_not_found":        http.StatusNotFound,
		"run_step_not_found":        http.StatusNotFound,
		"experiment_run_not_found":  http.StatusNotFound,
		"status_conflict":           http.StatusConflict,
		"duplicate_idempotency_key": http.StatusConflict,
		"bad_request":               http.StatusBadRequest,
		"anything_else":             http.StatusBadRequest,
		"":                          http.StatusBadRequest,
	}
	for code, want := range cases {
		if got := StatusForCode(code); got != want {
			t.Errorf("StatusForCode(%q) = %d, want %d", code, got, want)
		}
	}
}
