package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/common"
)

func TestTimeoutReturns504(t *testing.T) {
	h := Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ask", nil)
	req = req.WithContext(common.SetRequestID(req.Context(), "req-timeout"))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestTimeoutDoesNotRewriteStartedResponse(t *testing.T) {
	h := Timeout(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		<-r.Context().Done()
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ask", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("response rewritten: status=%d body=%q", rec.Code, rec.Body.String())
	}
}
