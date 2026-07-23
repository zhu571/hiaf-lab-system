package issues

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeUpdateRequestRejectsAIFields(t *testing.T) {
	for _, body := range []string{`{"ai_generated":false}`, `{"agent_task_id":null}`} {
		req := httptest.NewRequest("PATCH", "/", strings.NewReader(body))
		if _, err := decodeUpdateRequest(req); err == nil {
			t.Fatalf("expected immutable field rejection for %s", body)
		}
	}
}
