package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

type fakeService struct {
	listErr    error
	createErr  error
	startErr   error
	lastUser   string
	lastQuery  string
	lastStatus string
}

func (f *fakeService) Create(_ context.Context, userID string, _ CreateSessionRequest) (*Session, error) {
	f.lastUser = userID
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &Session{ID: "s1", UserID: userID, Title: "t"}, nil
}

func (f *fakeService) List(_ context.Context, userID, status, query string, _ int) ([]Session, error) {
	f.lastUser, f.lastStatus, f.lastQuery = userID, status, query
	if f.listErr != nil {
		return nil, f.listErr
	}
	return []Session{{ID: "s1", UserID: userID, Title: "t"}}, nil
}

func (f *fakeService) Get(_ context.Context, userID, id string) (*Session, error) {
	return &Session{ID: id, UserID: userID}, nil
}

func (f *fakeService) Update(_ context.Context, userID, id string, _ UpdateSessionRequest) (*Session, error) {
	return &Session{ID: id, UserID: userID}, nil
}

func (f *fakeService) Archive(_ context.Context, _, _ string) error { return nil }
func (f *fakeService) Messages(_ context.Context, _, _ string) ([]Message, error) {
	return nil, nil
}
func (f *fakeService) AddMessage(_ context.Context, _, _, content string) (*Message, error) {
	return &Message{ID: "m1", Role: "user", Content: content}, nil
}
func (f *fakeService) StartRun(_ context.Context, userID, sessionID, _ string) (*RunResult, error) {
	f.lastUser = userID
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &RunResult{Run: Run{ID: "r1", SessionID: sessionID, UserID: userID, Status: "done"}}, nil
}
func (f *fakeService) Usage(_ context.Context, userID string) (Usage, error) {
	return Usage{Date: "2026-11-26", Runs: 1, RunLimit: 100}, nil
}

func (f *fakeService) Export(_ context.Context, _, id string) (string, error) {
	return "# exported " + id, nil
}

func authedRequest(method, target string, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	claims := &middleware.UserClaims{UserID: "u1", Username: "tester"}
	return req.WithContext(middleware.WithUserClaims(req.Context(), claims))
}

func withParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandlerListPassesUserAndFilters(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	req := authedRequest(http.MethodGet, "/assistant/sessions?status=archived&q=rf", "")
	w := httptest.NewRecorder()
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if svc.lastUser != "u1" || svc.lastStatus != "archived" || svc.lastQuery != "rf" {
		t.Fatalf("unexpected args: %+v", svc)
	}
}

func TestHandlerCreateMapsProjectUnsupported(t *testing.T) {
	h := NewHandler(&fakeService{createErr: ErrProjectUnsupported})
	req := authedRequest(http.MethodPost, "/assistant/sessions", `{"project_id":"p1"}`)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandlerStartRunMapsUpstream(t *testing.T) {
	h := NewHandler(&fakeService{startErr: ErrUpstream})
	req := withParam(authedRequest(http.MethodPost, "/assistant/sessions/s1/runs", `{"content":"hi"}`), "id", "s1")
	w := httptest.NewRecorder()
	h.StartRun(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestHandlerStartRunStreamEmitsResult(t *testing.T) {
	h := NewHandler(&fakeService{})
	req := withParam(authedRequest(http.MethodPost, "/assistant/sessions/s1/runs/stream", `{"content":"hi"}`), "id", "s1")
	w := httptest.NewRecorder()
	h.StartRunStream(w, req)
	body := w.Body.String()
	for _, want := range []string{"event: start", "event: result", "event: done"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestHandlerUsageReturnsLimit(t *testing.T) {
	h := NewHandler(&fakeService{})
	req := authedRequest(http.MethodGet, "/assistant/usage", "")
	w := httptest.NewRecorder()
	h.Usage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Data Usage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Data.RunLimit != 100 || body.Data.Runs != 1 {
		t.Fatalf("unexpected usage: %+v", body.Data)
	}
}
