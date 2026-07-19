package rfmatch

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeUpdateRequestRejectsIdentityFields(t *testing.T) {
	request := httptest.NewRequest("PATCH", "/api/v1/rf-matching/id", strings.NewReader(`{"frequency_mhz":3}`))
	if _, err := decodeUpdateRequest(request); err == nil {
		t.Fatal("decodeUpdateRequest accepted frequency_mhz")
	}
}
