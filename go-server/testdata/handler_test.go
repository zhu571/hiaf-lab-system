package testdata

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeUpdateRequestRejectsImmutableFields(t *testing.T) {
	request := httptest.NewRequest("PATCH", "/api/v1/test-data/id", strings.NewReader(`{"run_id":"x"}`))
	if _, err := decodeUpdateRequest(request); err == nil {
		t.Fatal("decodeUpdateRequest accepted immutable run_id")
	}
}
