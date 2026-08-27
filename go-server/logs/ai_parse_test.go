package logs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

const aiParseOKBody = `{"status":"ok","logs":[
	{"category":"assembly","project_id":"prj_1","content":"装配匹配电路","raw_snippet":"worked","occurred_at":"2026-08-06T09:00:00+08:00"},
	{"category":"test","project_id":"prj_1","content":"测低温传感器","raw_snippet":"worked","occurred_at":"2026-08-06T14:00:00+08:00"}
],"summary":"完成装配并开展传感器测试。","question":null,"reason":null,"model":"deepseek-v4-pro","prompt_version":"1.2"}`

func aiParseService(t *testing.T, report DailyReport, upstream http.HandlerFunc) *Service {
	t.Helper()
	svc := NewService(newFakeRepo(report, nil), "Asia/Shanghai", fakeAccess{
		canAccess: true,
		projects:  []middleware.ProjectSummary{{ID: "prj_1", Name: "靶站"}},
	})
	if upstream != nil {
		server := httptest.NewServer(upstream)
		t.Cleanup(server.Close)
		svc.ConfigureParser(server.URL, "token")
	}
	return svc
}

func aiParseOKUpstream(t *testing.T, body string) (http.HandlerFunc, *map[string]any) {
	t.Helper()
	var got map[string]any
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}, &got
}

func TestAiParseOKPassthrough(t *testing.T) {
	upstream, got := aiParseOKUpstream(t, aiParseOKBody)
	svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), upstream)

	result, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member")
	if err != nil {
		t.Fatalf("AiParse returned error: %v", err)
	}
	if result.Status != "ok" || len(result.Logs) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Logs[0].Category != CategoryAssembly || result.Logs[0].ProjectID != "prj_1" {
		t.Fatalf("unexpected first log: %+v", result.Logs[0])
	}
	if result.Logs[0].RawSnippet != "worked" || result.PromptVersion != "1.2" {
		t.Fatalf("raw snippet or prompt version was not preserved: %+v", result)
	}
	// projects 由服务端注入，report_date 取日报日期
	if (*got)["raw_text"] != "worked" || (*got)["report_date"] != "2026-07-14" {
		t.Fatalf("unexpected upstream payload: %v", *got)
	}
	projects, ok := (*got)["projects"].([]any)
	if !ok || len(projects) != 1 {
		t.Fatalf("upstream payload missing injected projects: %v", *got)
	}
}

func TestAiParseAllowsPolishedRawSnippet(t *testing.T) {
	rawText := "测试了一下qpig两个rf之间的电阻是4.4M欧姆"
	body := `{"status":"ok","logs":[{"category":"test","project_id":"prj_1","content":"测量 q-pig 电阻","raw_snippet":"测试了q-pig两个rf之间的电阻为4.4M欧姆","occurred_at":"2026-08-06T09:00:00+08:00"}],"summary":"完成电阻测量。"}`
	upstream, _ := aiParseOKUpstream(t, body)
	if _, err := aiParseService(t, testReport("usr_1", ReportStatusDraft, rawText), upstream).AiParse(context.Background(), "report_1", "usr_1", "member"); err != nil {
		t.Fatalf("polished raw_snippet was rejected: %v", err)
	}
}

func TestAiParseClarifyAndRejectedPassthrough(t *testing.T) {
	for _, tt := range []struct{ status, body string }{
		{"clarify", `{"status":"clarify","logs":[],"question":"哪个项目？","reason":null}`},
		{"rejected", `{"status":"rejected","logs":[],"question":null,"reason":"与工作无关"}`},
	} {
		t.Run(tt.status, func(t *testing.T) {
			upstream, _ := aiParseOKUpstream(t, tt.body)
			svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), upstream)
			result, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member")
			if err != nil {
				t.Fatalf("AiParse returned error: %v", err)
			}
			if result.Status != tt.status {
				t.Fatalf("status = %q, want %q", result.Status, tt.status)
			}
		})
	}
}

func TestAiParseGuards(t *testing.T) {
	upstream, _ := aiParseOKUpstream(t, aiParseOKBody)
	tests := []struct {
		name     string
		report   DailyReport
		reportID string
		userID   string
		want     error
	}{
		{"not found", testReport("usr_1", ReportStatusDraft, "worked"), "missing", "usr_1", ErrReportNotFound},
		{"not owner", testReport("usr_other", ReportStatusDraft, "worked"), "report_1", "usr_1", ErrNotReportOwner},
		{"already submitted", testReport("usr_1", ReportStatusSubmitted, "worked"), "report_1", "usr_1", ErrAlreadySubmitted},
		{"empty raw text", testReport("usr_1", ReportStatusDraft, "  "), "report_1", "usr_1", ErrEmptyRawText},
		{"raw text too long", testReport("usr_1", ReportStatusDraft, strings.Repeat("工", 4001)), "report_1", "usr_1", ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := aiParseService(t, tt.report, upstream)
			_, err := svc.AiParse(context.Background(), tt.reportID, tt.userID, "member")
			if !errors.Is(err, tt.want) {
				t.Fatalf("AiParse error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAiParseRateLimited(t *testing.T) {
	upstream, _ := aiParseOKUpstream(t, aiParseOKBody)
	svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), upstream)
	for i := 0; i < 10; i++ {
		if _, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member"); err != nil {
			t.Fatalf("call %d failed: %v", i+1, err)
		}
	}
	if _, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("11th call error = %v, want %v", err, ErrRateLimited)
	}
}

func TestAiParseUpstreamMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       error
	}{
		{"py 422 parse failed", http.StatusUnprocessableEntity, `{"error":"daily_parse_failed"}`, ErrAiParseFailed},
		{"py 400 bad request", http.StatusBadRequest, `{"error":"bad_request"}`, ErrInvalidInput},
		{"py 500 upstream", http.StatusInternalServerError, `{"error":"provider_unavailable"}`, ErrUpstream},
		{"py 200 invalid status", http.StatusOK, `{"status":"done","logs":[]}`, ErrUpstream},
		{"py 200 out-of-scope project", http.StatusOK, `{"status":"ok","logs":[{"category":"assembly","project_id":"prj_x","content":"x","occurred_at":"2026-08-06T09:00:00+08:00"}]}`, ErrAiParseFailed},
		{"py 200 invalid category", http.StatusOK, `{"status":"ok","logs":[{"category":"rf_matching","project_id":"prj_1","content":"x","occurred_at":"2026-08-06T09:00:00+08:00"}]}`, ErrAiParseFailed},
		{"py 200 invalid occurred_at", http.StatusOK, `{"status":"ok","logs":[{"category":"assembly","project_id":"prj_1","content":"x","occurred_at":"昨天上午"}]}`, ErrAiParseFailed},
		{"py 200 short raw snippet", http.StatusOK, `{"status":"ok","logs":[{"category":"assembly","project_id":"prj_1","content":"x","raw_snippet":"ork","occurred_at":"2026-08-06T09:00:00+08:00"}]}`, ErrAiParseFailed},
		{"py 200 unrelated raw snippet", http.StatusOK, `{"status":"ok","logs":[{"category":"assembly","project_id":"prj_1","content":"x","raw_snippet":"other","occurred_at":"2026-08-06T09:00:00+08:00"}]}`, ErrAiParseFailed},
		{"py 200 empty logs", http.StatusOK, `{"status":"ok","logs":[]}`, ErrAiParseFailed},
		{"py 200 clarify without question", http.StatusOK, `{"status":"clarify","logs":[],"question":"","reason":null}`, ErrAiParseFailed},
		{"py 200 rejected without reason", http.StatusOK, `{"status":"rejected","logs":[],"question":null,"reason":" "}`, ErrAiParseFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member")
			if !errors.Is(err, tt.want) {
				t.Fatalf("AiParse error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAiParseUnconfiguredUpstream(t *testing.T) {
	svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), nil)
	if _, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("AiParse error = %v, want %v", err, ErrUpstream)
	}
}

func TestAiParseUpstreamTimeout(t *testing.T) {
	svc := aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(aiParseOKBody))
	})
	svc.client = &http.Client{Timeout: 50 * time.Millisecond}
	if _, err := svc.AiParse(context.Background(), "report_1", "usr_1", "member"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("AiParse error = %v, want %v", err, ErrUpstream)
	}
}

func aiParseHTTPRequest(t *testing.T, h *Handler, reportID, idemKey string) *httptest.ResponseRecorder {
	t.Helper()
	middleware.SetJWTSecret([]byte("ai-parse-test-secret"))
	token, err := middleware.GenerateToken("usr_1", "user", "member", 1, []byte("ai-parse-test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/daily-reports/"+reportID+"/ai-parse", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", reportID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	middleware.AuthRequired(http.HandlerFunc(h.AiParseReport)).ServeHTTP(rr, req)
	return rr
}

func errorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	return body.Error.Code
}

func TestAiParseHandlerRequiresIdempotencyKey(t *testing.T) {
	h := NewHandler(aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), nil))
	rr := aiParseHTTPRequest(t, h, "report_1", "")
	if rr.Code != http.StatusBadRequest || errorCode(t, rr) != "missing_idempotency_key" {
		t.Fatalf("status = %d code = %q, want 400 missing_idempotency_key", rr.Code, errorCode(t, rr))
	}
}

func TestAiParseHandlerErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		report     DailyReport
		reportID   string
		upstream   http.HandlerFunc
		wantStatus int
		wantCode   string
	}{
		{"not found", testReport("usr_1", ReportStatusDraft, "worked"), "missing", nil, http.StatusNotFound, "report_not_found"},
		{"not owner", testReport("usr_other", ReportStatusDraft, "worked"), "report_1", nil, http.StatusForbidden, "permission_denied"},
		{"already submitted", testReport("usr_1", ReportStatusSubmitted, "worked"), "report_1", nil, http.StatusBadRequest, "already_submitted"},
		{"empty raw text", testReport("usr_1", ReportStatusDraft, " "), "report_1", nil, http.StatusBadRequest, "empty_raw_text"},
		{"too long", testReport("usr_1", ReportStatusDraft, strings.Repeat("工", 4001)), "report_1", nil, http.StatusBadRequest, "bad_request"},
		{"unconfigured upstream", testReport("usr_1", ReportStatusDraft, "worked"), "report_1", nil, http.StatusBadGateway, "upstream_error"},
		{"py 422 maps to ai_parse_failed", testReport("usr_1", ReportStatusDraft, "worked"), "report_1", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"daily_parse_failed"}`))
		}, http.StatusBadRequest, "ai_parse_failed"},
		{"py 500 maps to upstream_error", testReport("usr_1", ReportStatusDraft, "worked"), "report_1", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"provider_unavailable"}`))
		}, http.StatusBadGateway, "upstream_error"},
		{"py 200 invalid log fails closed", testReport("usr_1", ReportStatusDraft, "worked"), "report_1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","logs":[{"category":"nope","project_id":"prj_1","content":"x","occurred_at":"2026-08-06T09:00:00+08:00"}]}`))
		}, http.StatusBadRequest, "ai_parse_failed"},
		{"py 200 missing summary fails closed", testReport("usr_1", ReportStatusDraft, "worked"), "report_1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Replace(aiParseOKBody, `"summary":"完成装配并开展传感器测试。"`, `"summary":""`, 1)))
		}, http.StatusBadRequest, "ai_parse_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(aiParseService(t, tt.report, tt.upstream))
			rr := aiParseHTTPRequest(t, h, tt.reportID, "idem-"+tt.name)
			if rr.Code != tt.wantStatus || errorCode(t, rr) != tt.wantCode {
				t.Fatalf("status = %d code = %q, want %d %s", rr.Code, errorCode(t, rr), tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestAiParseHandlerRateLimit429(t *testing.T) {
	upstream, _ := aiParseOKUpstream(t, aiParseOKBody)
	h := NewHandler(aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), upstream))
	var rr *httptest.ResponseRecorder
	for i := 0; i < 11; i++ {
		rr = aiParseHTTPRequest(t, h, "report_1", "idem-rate-"+strings.Repeat("k", i+1))
	}
	if rr.Code != http.StatusTooManyRequests || errorCode(t, rr) != "too_many_requests" {
		t.Fatalf("11th call status = %d code = %q, want 429 too_many_requests", rr.Code, errorCode(t, rr))
	}
}

func TestAiParseHandlerOK(t *testing.T) {
	upstream, _ := aiParseOKUpstream(t, aiParseOKBody)
	h := NewHandler(aiParseService(t, testReport("usr_1", ReportStatusDraft, "worked"), upstream))
	rr := aiParseHTTPRequest(t, h, "report_1", "idem-ok")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data AiParseResult `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Data.Status != "ok" || len(body.Data.Logs) != 2 {
		t.Fatalf("unexpected data: %+v", body.Data)
	}
	if body.Data.Summary == nil || *body.Data.Summary == "" {
		t.Fatalf("summary missing: %+v", body.Data)
	}
}
