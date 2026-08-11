package steptemplates

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

// startMockPlanner 起一个 /v1/step-plan mock，回放预设响应。
func startMockPlanner(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

// fakeStepRepo 实现 stepRepo 接口，记录调用并回放预设结果。
type fakeStepRepo struct {
	tmpl    *StepTemplate
	items   []StepTemplateItem
	list    []StepTemplate
	total   int
	err     error
	created *StepTemplate
	update  *StepTemplate
	deleted string

	createCalls, replaceCalls, softDeleteCalls int
}

func (f *fakeStepRepo) Create(t *StepTemplate, items []StepTemplateItem) (*StepTemplate, error) {
	f.createCalls++
	f.created = t
	if f.err != nil {
		return nil, f.err
	}
	return f.tmpl, nil
}

func (f *fakeStepRepo) GetByID(id string) (*StepTemplate, error) {
	return f.tmpl, f.err
}

func (f *fakeStepRepo) GetTemplateWithItems(id string) (*StepTemplate, []StepTemplateItem, error) {
	return f.tmpl, f.items, f.err
}

func (f *fakeStepRepo) List(kind, query string, page, perPage int) ([]StepTemplate, int, error) {
	return f.list, f.total, f.err
}

func (f *fakeStepRepo) Update(id string, req UpdateTemplateRequest) (*StepTemplate, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.update, nil
}

func (f *fakeStepRepo) ReplaceItems(templateID string, items []StepTemplateItem) error {
	f.replaceCalls++
	f.items = items
	return f.err
}

func (f *fakeStepRepo) SoftDelete(id string) error {
	f.softDeleteCalls++
	f.deleted = id
	return f.err
}

func newTestService() (*Service, *fakeStepRepo) {
	repo := &fakeStepRepo{}
	return NewService(repo, nil), repo
}

func validCreateReq() CreateTemplateRequest {
	return CreateTemplateRequest{
		Name: " 真空安装模板 ",
		Kind: "assembly",
		Items: []ItemDef{
			{Name: " 检漏 ", StepOrder: 2, DependsOnOrder: intPtr(1)},
			{Name: " 安装 ", StepOrder: 1},
		},
	}
}

func TestSvcCreate(t *testing.T) {
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.Create("u1", auth.RoleAgent, validCreateReq()); !errors.Is(err, ErrAgentRejected) {
			t.Fatalf("agent: %v", err)
		}
	})
	t.Run("viewer forbidden", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.Create("u1", "viewer", validCreateReq()); !errors.Is(err, ErrForbidden) {
			t.Fatalf("viewer: %v", err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		svc, _ := newTestService()
		req := validCreateReq()
		req.Name = "  "
		if _, err := svc.Create("u1", "admin", req); err == nil {
			t.Fatal("empty name must error")
		}
	})
	t.Run("invalid kind", func(t *testing.T) {
		svc, _ := newTestService()
		req := validCreateReq()
		req.Kind = "bogus"
		if _, err := svc.Create("u1", "admin", req); err == nil {
			t.Fatal("bad kind must error")
		}
	})
	t.Run("too many items", func(t *testing.T) {
		svc, _ := newTestService()
		req := validCreateReq()
		for i := 0; i <= MaxItems; i++ {
			req.Items = append(req.Items, ItemDef{Name: "x", StepOrder: i + 1})
		}
		if _, err := svc.Create("u1", "admin", req); err == nil {
			t.Fatal("> MaxItems must error")
		}
	})
	t.Run("success normalizes items", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = &StepTemplate{ID: "t1"}
		got, err := svc.Create("u-creator", "maintainer", validCreateReq())
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != "t1" {
			t.Fatalf("id: %v", got.ID)
		}
		if repo.created.Name != "真空安装模板" {
			t.Fatalf("name not trimmed: %q", repo.created.Name)
		}
		if repo.created.CreatedBy == nil || *repo.created.CreatedBy != "u-creator" {
			t.Fatalf("created_by: %+v", repo.created.CreatedBy)
		}
		if len(repo.created.Items) != 0 {
			t.Fatalf("template items should be empty (items passed separately): %+v", repo.created)
		}
	})
}

func TestSvcGetByID(t *testing.T) {
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.GetByID("t1", "u1", auth.RoleAgent); !errors.Is(err, ErrAgentRejected) {
			t.Fatal(err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = nil
		if _, err := svc.GetByID("t1", "u1", "member"); !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("want ErrTemplateNotFound, got %v", err)
		}
	})
	t.Run("found", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = &StepTemplate{ID: "t1", Name: "x"}
		got, err := svc.GetByID("t1", "u1", "member")
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "x" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestSvcList(t *testing.T) {
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.List(auth.RoleAgent, "", "", 1, 20); !errors.Is(err, ErrAgentRejected) {
			t.Fatal(err)
		}
	})
	t.Run("pagination clamps and nil items", func(t *testing.T) {
		svc, repo := newTestService()
		repo.list = nil
		repo.total = 0
		result, err := svc.List("member", "", "", 0, 0) // page<1→1, perPage<1→20
		if err != nil {
			t.Fatal(err)
		}
		if result.Page != 1 || result.PerPage != 20 || result.Items == nil || len(result.Items) != 0 {
			t.Fatalf("clamped result: %+v", result)
		}
		_, err = svc.List("member", "", "", 2, 500) // perPage>100→100
		if err != nil {
			t.Fatal(err)
		}
	})
	t.Run("forwards kind and query", func(t *testing.T) {
		svc, repo := newTestService()
		repo.list = []StepTemplate{{ID: "t1"}}
		repo.total = 1
		result, err := svc.List("member", "experiment", "靶", 2, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 1 || result.Total != 1 {
			t.Fatalf("result: %+v", result)
		}
	})
}

func TestSvcUpdate(t *testing.T) {
	name := " 新名字 "
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.Update("t1", "u1", auth.RoleAgent, UpdateTemplateRequest{Name: &name}); !errors.Is(err, ErrAgentRejected) {
			t.Fatal(err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = nil
		if _, err := svc.Update("t1", "u1", "member", UpdateTemplateRequest{Name: &name}); !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("forbidden", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		if _, err := svc.Update("t1", "other", "viewer", UpdateTemplateRequest{Name: &name}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("want forbidden, got %v", err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		empty := "  "
		if _, err := svc.Update("t1", "owner-1", "viewer", UpdateTemplateRequest{Name: &empty}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("want invalid input, got %v", err)
		}
		long := strings.Repeat("n", 257)
		if _, err := svc.Update("t1", "owner-1", "viewer", UpdateTemplateRequest{Name: &long}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("want invalid input for long name, got %v", err)
		}
	})
	t.Run("repo returns nil after update", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		repo.update = nil
		if _, err := svc.Update("t1", "owner-1", "viewer", UpdateTemplateRequest{Name: &name}); !errors.Is(err, ErrTemplateNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	})
	t.Run("success trims name", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		repo.update = &StepTemplate{ID: "t1", Name: "新名字"}
		got, err := svc.Update("t1", "owner-1", "viewer", UpdateTemplateRequest{Name: &name})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "新名字" {
			t.Fatalf("name not trimmed: %q", got.Name)
		}
	})
}

func TestSvcReplaceItems(t *testing.T) {
	req := ReplaceItemsRequest{Items: []ItemDef{
		{Name: "b", StepOrder: 2, DependsOnOrder: intPtr(1)},
		{Name: "a", StepOrder: 1},
	}}
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if err := svc.ReplaceItems("t1", "u1", auth.RoleAgent, req); !errors.Is(err, ErrAgentRejected) {
			t.Fatal(err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = nil
		if err := svc.ReplaceItems("t1", "u1", "member", req); !errors.Is(err, ErrTemplateNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("forbidden", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		if err := svc.ReplaceItems("t1", "other", "member", req); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("success normalizes", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		if err := svc.ReplaceItems("t1", "owner-1", "member", req); err != nil {
			t.Fatal(err)
		}
		if repo.replaceCalls != 1 || len(repo.items) != 2 {
			t.Fatalf("replace: calls=%d items=%d", repo.replaceCalls, len(repo.items))
		}
		if repo.items[0].Name != "a" || repo.items[0].StepOrder != 1 || repo.items[0].DependsOnOrder != nil {
			t.Fatalf("items not reordered: %+v", repo.items)
		}
		if repo.items[1].Name != "b" || repo.items[1].StepOrder != 2 || *repo.items[1].DependsOnOrder != 1 {
			t.Fatalf("items not reordered: %+v", repo.items)
		}
	})
}

func TestSvcSoftDelete(t *testing.T) {
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if err := svc.SoftDelete("t1", "u1", auth.RoleAgent); !errors.Is(err, ErrAgentRejected) {
			t.Fatal(err)
		}
	})
	t.Run("not found", func(t *testing.T) {
		svc, repo := newTestService()
		repo.tmpl = nil
		if err := svc.SoftDelete("t1", "u1", "member"); !errors.Is(err, ErrTemplateNotFound) {
			t.Fatal(err)
		}
	})
	t.Run("forbidden", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		if err := svc.SoftDelete("t1", "other", "member"); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
	})
	t.Run("success", func(t *testing.T) {
		svc, repo := newTestService()
		creator := "owner-1"
		repo.tmpl = &StepTemplate{ID: "t1", CreatedBy: &creator}
		if err := svc.SoftDelete("t1", "owner-1", "member"); err != nil {
			t.Fatal(err)
		}
		if repo.deleted != "t1" {
			t.Fatalf("deleted id: %q", repo.deleted)
		}
	})
}

func TestValidateCreateRequest(t *testing.T) {
	valid := func() CreateTemplateRequest {
		return CreateTemplateRequest{Name: "x", Kind: "assembly", Items: []ItemDef{{Name: "a", StepOrder: 1}}}
	}
	if err := validateCreateRequest(valid()); err != nil {
		t.Fatalf("valid: %v", err)
	}
	req := valid()
	req.Name = ""
	if err := validateCreateRequest(req); err == nil {
		t.Fatal("empty name must error")
	}
	req = valid()
	req.Name = strings.Repeat("n", 257)
	if err := validateCreateRequest(req); err == nil {
		t.Fatal("long name must error")
	}
	req = valid()
	req.Kind = "x"
	if err := validateCreateRequest(req); err == nil {
		t.Fatal("bad kind must error")
	}
	req = valid()
	req.Items = nil
	if err := validateCreateRequest(req); err == nil {
		t.Fatal("no items must error")
	}
	req = valid()
	for i := 0; i < MaxItems; i++ {
		req.Items = append(req.Items, ItemDef{Name: "x", StepOrder: i + 2})
	}
	if err := validateCreateRequest(req); err == nil {
		t.Fatal("too many items must error")
	}
}

func TestSvcGenerate(t *testing.T) {
	t.Run("agent rejected", func(t *testing.T) {
		svc, _ := newTestService()
		_, err := svc.Generate(context.Background(), "u1", auth.RoleAgent, GenerateRequest{})
		if !errors.Is(err, ErrAgentRejected) {
			t.Fatalf("agent: %v", err)
		}
	})
	t.Run("not configured", func(t *testing.T) {
		svc, _ := newTestService()
		_, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "未配置") {
			t.Fatalf("unconfigured: %v", err)
		}
	})
	t.Run("bad kind", func(t *testing.T) {
		svc, _ := newTestService()
		svc.plannerURL = "http://x"
		svc.plannerToken = "t"
		_, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "bogus", Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "kind") {
			t.Fatalf("bad kind: %v", err)
		}
	})
	t.Run("empty or long prompt", func(t *testing.T) {
		svc, _ := newTestService()
		svc.plannerURL = "http://x"
		svc.plannerToken = "t"
		if _, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "  "}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("empty prompt: %v", err)
		}
		long := strings.Repeat("p", 4001)
		if _, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: long}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("long prompt: %v", err)
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		svc, _ := newTestService()
		for i := 0; i < 10; i++ {
			if !svc.allowOne("u-rl") {
				t.Fatalf("call %d allowed", i+1)
			}
		}
		_, err := svc.Generate(context.Background(), "u-rl", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "频繁") {
			t.Fatalf("rate limited: %v", err)
		}
	})
}

func TestSvcGenerateAgentResponses(t *testing.T) {
	mock := func(t *testing.T, body string, status int) (*Service, *httptest.Server) {
		svc := NewService(nil, nil)
		server := startMockPlanner(t, body, status)
		svc.ConfigurePlanner(server.URL, "tok")
		return svc, server
	}
	t.Run("invalid status", func(t *testing.T) {
		svc, server := mock(t, `{"status":"weird","name_suggestion":"t","model":"m"}`, 200)
		defer server.Close()
		_, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if !errors.Is(err, ErrUpstream) {
			t.Fatalf("invalid status: %v", err)
		}
	})
	t.Run("clarify", func(t *testing.T) {
		svc, server := mock(t, `{"status":"clarify","name_suggestion":"t","model":"m","question":"要装配什么？"}`, 200)
		defer server.Close()
		res, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != "clarify" || res.Question == nil || *res.Question != "要装配什么？" {
			t.Fatalf("clarify: %+v", res)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		svc, server := mock(t, `{"status":"rejected","name_suggestion":"t","model":"m","reason":"信息不足"}`, 200)
		defer server.Close()
		res, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != "rejected" || res.Reason == nil || *res.Reason != "信息不足" {
			t.Fatalf("rejected: %+v", res)
		}
	})
	t.Run("ok but invalid steps", func(t *testing.T) {
		svc, server := mock(t, `{"status":"ok","name_suggestion":"t","model":"m","steps":[{"name":"a","step_order":1},{"name":"b","step_order":1}]}`, 200)
		defer server.Close()
		_, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err == nil || !strings.Contains(err.Error(), "校验失败") {
			t.Fatalf("invalid steps: %v", err)
		}
	})
	t.Run("ok with reorder", func(t *testing.T) {
		svc, server := mock(t, `{"status":"ok","name_suggestion":"t","model":"m","steps":[{"name":"b","step_order":2,"depends_on_order":1},{"name":"a","step_order":1}]}`, 200)
		defer server.Close()
		res, err := svc.Generate(context.Background(), "u1", "member", GenerateRequest{Kind: "assembly", Prompt: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Steps) != 2 || res.Steps[0].Name != "a" || res.Steps[0].StepOrder != 1 {
			t.Fatalf("reordered: %+v", res.Steps)
		}
	})
}

func TestAutoConfigure(t *testing.T) {
	t.Run("url and token file", func(t *testing.T) {
		dir := t.TempDir()
		tokenFile := filepath.Join(dir, "token")
		if err := os.WriteFile(tokenFile, []byte("  file-token \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PY_AGENT_INTERPRET_URL", "http://agent:8080/")
		t.Setenv("PY_AGENT_INTERNAL_TOKEN_FILE", tokenFile)
		svc := NewService(nil, nil)
		if err := svc.AutoConfigure(); err != nil {
			t.Fatal(err)
		}
		if svc.plannerURL != "http://agent:8080" || svc.plannerToken != "file-token" {
			t.Fatalf("configured: %q %q", svc.plannerURL, svc.plannerToken)
		}
	})
	t.Run("no env", func(t *testing.T) {
		os.Unsetenv("PY_AGENT_INTERPRET_URL")
		os.Unsetenv("PY_AGENT_INTERNAL_TOKEN_FILE")
		svc := NewService(nil, nil)
		if err := svc.AutoConfigure(); err != nil {
			t.Fatal(err)
		}
		if svc.plannerURL != "" {
			t.Fatalf("should stay unconfigured: %q", svc.plannerURL)
		}
	})
}

func TestSvcAllowOne(t *testing.T) {
	svc := &Service{rlCalls: map[string][]time.Time{}}
	for i := 0; i < 10; i++ {
		if !svc.allowOne("u-a") {
			t.Fatalf("call %d", i+1)
		}
	}
	if svc.allowOne("u-a") {
		t.Fatal("11th must be rejected")
	}
	// 过期裁剪
	svc.rlCalls["u-b"] = []time.Time{time.Now().Add(-2 * time.Minute), time.Now().Add(-2 * time.Minute)}
	if !svc.allowOne("u-b") {
		t.Fatal("expired calls must be allowed")
	}
	if len(svc.rlCalls["u-b"]) != 1 {
		t.Fatalf("expected 1 kept call, got %d", len(svc.rlCalls["u-b"]))
	}
}
