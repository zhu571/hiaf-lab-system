package experiences

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhu571/hiaf-lab-system/go-server/auth"
)

// AI-2 经验候选提取：service 层单测（fake issueSource + fake extractor，不依赖 DB）。

type fakeIssueSource struct {
	items []ResolvedIssue
	err   error
}

func (f *fakeIssueSource) ResolvedIssuesSince(ctx context.Context, since time.Time, limit int) ([]ResolvedIssue, error) {
	return f.items, f.err
}

type fakeExtractor struct {
	resp *ExtractLLMResponse
	err  error
}

func (f *fakeExtractor) Extract(ctx context.Context, req ExtractLLMRequest) (*ExtractLLMResponse, error) {
	return f.resp, f.err
}

func newExtractTestService(t *testing.T, source issueSource, llm extractLLM) *Service {
	t.Helper()
	svc := NewService(newFakeExperienceRepo(), fakeProjectAccess{})
	svc.SetIssueSource(source)
	svc.SetExtractor(llm)
	return svc
}

func TestExtractCandidatesPermission(t *testing.T) {
	svc := newExtractTestService(t, &fakeIssueSource{}, &fakeExtractor{})

	// member/viewer → ErrForbidden；admin/maintainer 放行（无 issue 时返回空）
	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMember, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member: got %v, want ErrForbidden", err)
	}
	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleViewer, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer: got %v, want ErrForbidden", err)
	}
	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleAdmin, 0); err != nil {
		t.Fatalf("admin: got %v", err)
	}
	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); err != nil {
		t.Fatalf("maintainer: got %v", err)
	}
}

func TestExtractCandidatesNotConfigured(t *testing.T) {
	svc := NewService(newFakeExperienceRepo(), fakeProjectAccess{})
	_, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleAdmin, 0)
	if !errors.Is(err, ErrExtractNotConfigured) {
		t.Fatalf("unconfigured: got %v, want ErrExtractNotConfigured", err)
	}
}

func TestExtractCandidatesEmptyIssuesSkipsLLM(t *testing.T) {
	llm := &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{}}}
	svc := newExtractTestService(t, &fakeIssueSource{items: []ResolvedIssue{}}, llm)

	result, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if result == nil || len(result.Items) != 0 {
		t.Fatalf("result = %+v, want empty items", result)
	}
}

func TestExtractCandidatesCreatesDraftExperiences(t *testing.T) {
	projectID := "prj_1"
	source := &fakeIssueSource{items: []ResolvedIssue{
		{ID: "iss_1", ProjectID: projectID, Title: "匹配效率偏低", Description: "间隙过大",
			Comments: []string{"已调整，效率 92%"}, RunID: ptrString("run_1")},
	}}
	llm := &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{
		{IssueID: "iss_1", Title: "匹配效率排查经验", Content: "先查间隙再校准匹配点",
			Tags: []string{"rf", "matching"}, Confidence: 0.88},
	}}}
	svc := newExtractTestService(t, source, llm)

	result, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.IssueID != "iss_1" || item.Confidence != 0.88 || item.Experience.ProjectID == nil ||
		*item.Experience.ProjectID != projectID {
		t.Fatalf("item = %+v", item)
	}
	// 落库草稿：ai_generated=true + candidate 状态 + tags 含 ai_extracted 标记
	exp := item.Experience
	if !exp.AiGenerated || exp.Status != StatusCandidate {
		t.Fatalf("experience = %+v, want ai_generated candidate draft", exp)
	}
	hasTag := false
	for _, tag := range exp.Tags {
		if tag == aiExtractedTag {
			hasTag = true
		}
	}
	if !hasTag {
		t.Fatalf("tags = %+v, want ai_extracted marker", exp.Tags)
	}
}

func TestExtractCandidatesRejectsInvalidLLMOutput(t *testing.T) {
	projectID := "prj_1"
	source := &fakeIssueSource{items: []ResolvedIssue{
		{ID: "iss_1", ProjectID: projectID, Title: "t", Description: "d"},
	}}

	// status 非 ok
	badStatus := newExtractTestService(t, source, &fakeExtractor{resp: &ExtractLLMResponse{Status: "rejected"}})
	if _, err := badStatus.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); !errors.Is(err, ErrInvalidLLMOutput) {
		t.Fatalf("bad status: got %v, want ErrInvalidLLMOutput", err)
	}

	// issue_id 不在输入集
	badIssue := newExtractTestService(t, source, &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{
		{IssueID: "iss_bogus", Title: "t", Content: "c", Confidence: 0.5},
	}}})
	if _, err := badIssue.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); !errors.Is(err, ErrInvalidLLMOutput) {
		t.Fatalf("bad issue_id: got %v, want ErrInvalidLLMOutput", err)
	}

	// 条目过多
	tooMany := newExtractTestService(t, source, &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 0.5},
	}}})
	if _, err := tooMany.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); !errors.Is(err, ErrInvalidLLMOutput) {
		t.Fatalf("too many: got %v, want ErrInvalidLLMOutput", err)
	}

	// 空 title / 超长 content / confidence 越界
	for _, entry := range []ExtractEntry{
		{IssueID: "iss_1", Title: "  ", Content: "c", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "", Confidence: 0.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: 1.5},
		{IssueID: "iss_1", Title: "t", Content: "c", Confidence: -0.1},
	} {
		svc := newExtractTestService(t, source, &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{entry}}})
		if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); !errors.Is(err, ErrInvalidLLMOutput) {
			t.Fatalf("entry %+v: got %v, want ErrInvalidLLMOutput", entry, err)
		}
	}
}

func TestExtractCandidatesForwardsUpstreamError(t *testing.T) {
	source := &fakeIssueSource{items: []ResolvedIssue{{ID: "iss_1", ProjectID: "prj_1", Title: "t", Description: "d"}}}
	upstream := &fakeExtractor{err: ErrExtractUpstream}
	svc := newExtractTestService(t, source, upstream)

	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); !errors.Is(err, ErrExtractUpstream) {
		t.Fatalf("got %v, want ErrExtractUpstream", err)
	}
}

func TestExtractCandidatesTrimsAndNormalizesTags(t *testing.T) {
	projectID := "prj_1"
	source := &fakeIssueSource{items: []ResolvedIssue{
		{ID: "iss_1", ProjectID: projectID, Title: "t", Description: "d"},
	}}
	llm := &fakeExtractor{resp: &ExtractLLMResponse{Status: "ok", Entries: []ExtractEntry{
		{IssueID: "iss_1", Title: "t", Content: "c", Tags: []string{" RF ", "rf", "Matching"}, Confidence: 0.6},
	}}}
	svc := newExtractTestService(t, source, llm)

	result, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	tags := result.Items[0].Experience.Tags
	// 归一化后：rf、matching + ai_extracted 标记；去重、去空
	if len(tags) != 3 {
		t.Fatalf("tags = %+v, want [rf matching ai_extracted]", tags)
	}
	for _, want := range []string{"rf", "matching", aiExtractedTag} {
		found := false
		for _, tag := range tags {
			if tag == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("tags = %+v, missing %q", tags, want)
		}
	}
}

func TestExtractCandidatesLimitsDays(t *testing.T) {
	// 默认 7 天：源返回空集即可验证链路可走（days 收敛不在此层断言）
	svc := newExtractTestService(t, &fakeIssueSource{items: []ResolvedIssue{}}, &fakeExtractor{})
	if _, err := svc.ExtractCandidates(context.Background(), "usr_1", auth.RoleMaintainer, 0); err != nil {
		t.Fatalf("default days: %v", err)
	}
}

func ptrString(v string) *string { return &v }
