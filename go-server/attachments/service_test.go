package attachments

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPPermissionChecker(t *testing.T) {
	const entityID = "7e0128b5-ff65-4f7c-bdc1-4c2f419ed5c0"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/issues/"+entityID+"/permission-check" {
			if r.URL.Query().Get("user_id") != "user-1" || r.URL.Query().Get("action") != "write" {
				t.Fatalf("unexpected permission query: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"allowed":false}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	checker := NewHTTPPermissionChecker(server.URL)
	allowed, err := checker.Check(EntityIssue, entityID, "user-1", "write")
	if err != nil || allowed {
		t.Fatalf("expected explicit denial, allowed=%v err=%v", allowed, err)
	}
	allowed, err = checker.Check(EntityLog, entityID, "user-1", "read")
	if err != nil || !allowed {
		t.Fatalf("expected temporary 404 fallback, allowed=%v err=%v", allowed, err)
	}
}
