package weekly

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func handlerReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	return r.WithContext(common.SetRequestID(r.Context(), "req-weekly-test"))
}

func genTestToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	middleware.SetJWTSecret([]byte("test-secret-32-bytes-long!!!!!"))
	tok, err := middleware.GenerateToken(userID, username, role, 1, []byte("test-secret-32-bytes-long!!!!!"))
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func newHandlerService() (*Service, *fakeReportReader, *fakeExperienceStore) {
	reports := &fakeReportReader{entries: sampleReports()}
	store := &fakeExperienceStore{}
	svc := NewService(reports, &fakeIssueStatsReader{}, store, &fakeLLM{}, &fakeNotifier{}, testLoc, testNow)
	return svc, reports, store
}

// authed 走真实 AuthRequired 中间件注入 claims（对齐 todos handler_test 先例）。
func authed(t *testing.T, hfn http.HandlerFunc, role string) http.HandlerFunc {
	t.Helper()
	token := genTestToken(t, "usr_1", "tester", role)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
		middleware.AuthRequired(http.HandlerFunc(hfn)).ServeHTTP(w, r)
	})
}

func TestHandlerSummaryRequiresAuth(t *testing.T) {
	svc, _, _ := newHandlerService()
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	h.Summary(rr, handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestHandlerSummaryHappy(t *testing.T) {
	svc, _, _ := newHandlerService()
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr, handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var envelope struct {
		Data SummaryResult `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Title != "周报 2026-08-03 ~ 2026-08-09" || envelope.Data.Reused {
		t.Fatalf("unexpected result: %+v", envelope.Data)
	}
}

func TestHandlerSummaryNotifyFalse(t *testing.T) {
	svc, _, _ := newHandlerService()
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr,
		handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", map[string]any{"notify": false}))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandlerSummaryInvalidBody(t *testing.T) {
	svc, _, _ := newHandlerService()
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr,
		handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", `{bad`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandlerSummaryInvalidWeekStart(t *testing.T) {
	svc, _, _ := newHandlerService()
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr,
		handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", map[string]any{"week_start": "2026-08-05"}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandlerSummaryNoReports(t *testing.T) {
	svc, reports, _ := newHandlerService()
	reports.entries = nil
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr, handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandlerSummaryUpstreamError(t *testing.T) {
	svc, _, _ := newHandlerService()
	svc.llm = &fakeLLM{err: ErrUpstream}
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr, handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}

func TestHandlerSummaryInternalError(t *testing.T) {
	svc, _, store := newHandlerService()
	store.saveErr = errors.New("db down")
	h := NewHandler(svc)
	rr := httptest.NewRecorder()
	authed(t, h.Summary, auth.RoleMaintainer).ServeHTTP(rr, handlerReq(t, http.MethodPost, "/api/v1/weekly/summary", nil))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}
