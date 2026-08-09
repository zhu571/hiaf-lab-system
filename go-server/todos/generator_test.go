package todos

import (
	"context"
	"errors"
	"testing"
	"time"
)

func auditInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return -1
}

func TestSelectIssues(t *testing.T) {
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	iss := func(id, sev string, day int) IssueSnapshot {
		return IssueSnapshot{ID: id, Title: "i" + id, Severity: sev, OccurredAt: base.AddDate(0, 0, day)}
	}
	issues := []IssueSnapshot{
		iss("1", "low", -3),
		iss("2", "high", -1),
		iss("3", "critical", -5),
		iss("4", "medium", 0),
	}
	inflight := map[string]bool{"2": true}

	got := selectIssues(issues, inflight)
	// 在途 2 被跳过；排序 critical > high > medium > low，同 severity 按 occurred_at 新→旧
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d: %+v", len(got), got)
	}
	if got[0].ID != "3" || got[1].ID != "4" || got[2].ID != "1" {
		t.Fatalf("unexpected order: %+v", got)
	}

	// >8 取舍
	many := make([]IssueSnapshot, 0, 12)
	for i := 0; i < 12; i++ {
		many = append(many, iss(string(rune('a'+i)), "medium", -i))
	}
	got = selectIssues(many, map[string]bool{})
	if len(got) != maxIssueItems {
		t.Fatalf("expected %d, got %d", maxIssueItems, len(got))
	}
}

func TestGenerateForUserIssueOnly(t *testing.T) {
	d := newTestDeps()
	u := UserSnapshot{ID: "u1", Username: "alice"}
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	d.snap.issues["u1"] = []IssueSnapshot{
		{ID: "iss1", Title: "真空度异常", Severity: "high", OccurredAt: base},
		{ID: "iss2", Title: "低温阀渗漏", Severity: "low", OccurredAt: base},
	}
	d.llm.dailyResp = []LLMItem{{Title: "延续任务", Priority: PriorityMedium}}
	svc := d.service()

	// 有 issue + 有日报 → LLM 也调
	n, err := svc.generateForUser(context.Background(), u, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 created, got %d", n)
	}
	if d.llm.dailyCalls != 1 {
		t.Fatalf("expected 1 llm call, got %d", d.llm.dailyCalls)
	}
	// issue 映射 high→high、low→low
	var highSeen, lowSeen bool
	for _, todo := range d.repo.todos {
		if todo.IssueID != nil && *todo.IssueID == "iss1" {
			highSeen = todo.Priority == PriorityHigh
		}
		if todo.IssueID != nil && *todo.IssueID == "iss2" {
			lowSeen = todo.Priority == PriorityLow
		}
	}
	if !highSeen || !lowSeen {
		t.Fatalf("severity→priority mapping failed: high=%v low=%v", highSeen, lowSeen)
	}
}

func TestGenerateForUserLLMCappedAtFour(t *testing.T) {
	d := newTestDeps()
	d.reports.report = "昨日做了 rf 匹配"
	items := make([]LLMItem, 0, 6)
	for _, title := range []string{"事项1", "事项2", "事项3", "事项4", "事项5", "事项6"} {
		items = append(items, LLMItem{Title: title, Priority: PriorityMedium})
	}
	d.llm.dailyResp = items
	svc := d.service()

	n, err := svc.generateForUser(context.Background(), UserSnapshot{ID: "u1"}, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	// LLM 补充项独立封顶 4（方案 §2：issue ≤ 8 + LLM ≤ 4）
	if n != maxLLMItems {
		t.Fatalf("LLM items must be capped at %d, got %d", maxLLMItems, n)
	}
}

func TestGenerateForUserTwoSourcesEmptySkipsLLM(t *testing.T) {
	d := newTestDeps()
	d.snap.issues["u1"] = nil
	d.reports.report = ""
	svc := d.service()

	n, err := svc.generateForUser(context.Background(), UserSnapshot{ID: "u1"}, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
	if d.llm.dailyCalls != 0 {
		t.Fatalf("LLM must be skipped when both sources empty, got %d calls", d.llm.dailyCalls)
	}
}

func TestGenerateForUserLLMFailureKeepsIssues(t *testing.T) {
	d := newTestDeps()
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	d.snap.issues["u1"] = []IssueSnapshot{{ID: "iss1", Title: "真空度异常", Severity: "high", OccurredAt: base}}
	d.llm.dailyErr = errors.New("LLM 超时")
	svc := d.service()

	n, err := svc.generateForUser(context.Background(), UserSnapshot{ID: "u1"}, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("issue items must survive LLM failure, got %d", n)
	}
}

func TestGenerateForUserExistingTitleDedup(t *testing.T) {
	d := newTestDeps()
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	// issue 标题 "真空 异常"；LLM 返回同标题不同空白/大小写 → 跳过
	d.snap.issues["u1"] = []IssueSnapshot{{ID: "iss1", Title: "真空  异常", Severity: "medium", OccurredAt: base}}
	d.llm.dailyResp = []LLMItem{
		{Title: "真空 异常", Priority: PriorityHigh}, // normalize 后与 issue 重复
		{Title: "新任务", Priority: PriorityLow},
		{Title: "新任务", Priority: PriorityMedium}, // 与上一条重复
	}
	svc := d.service()

	n, err := svc.generateForUser(context.Background(), UserSnapshot{ID: "u1"}, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 created (1 issue + 1 llm), got %d", n)
	}
	// LLM 非法 priority clamp
	var llmTodo *Todo
	for _, todo := range d.repo.todos {
		if todo.Source == SourceDailyLLM {
			llmTodo = todo
		}
	}
	if llmTodo == nil {
		t.Fatal("no llm todo created")
	}
	if llmTodo.Priority != PriorityLow {
		t.Fatalf("llm priority not preserved: %s", llmTodo.Priority)
	}
}

func TestGenerateForUserInflightIssueSkipped(t *testing.T) {
	d := newTestDeps()
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	d.snap.issues["u1"] = []IssueSnapshot{{ID: "iss1", Title: "真空度异常", Severity: "high", OccurredAt: base}}
	// 已有在途待办（同 issue）
	d.repo.inflightOverride = map[string]bool{"iss1": true}
	svc := d.service()

	n, err := svc.generateForUser(context.Background(), UserSnapshot{ID: "u1"}, "2026-08-08")
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("inflight issue must be skipped, got %d", n)
	}
}

func TestGenerateForDatePerUserFailureIndependent(t *testing.T) {
	d := newTestDeps()
	base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
	d.snap.users = []UserSnapshot{{ID: "u1", Username: "a"}, {ID: "u2", Username: "b"}, {ID: "u3", Username: "c"}}
	d.snap.issues["u1"] = []IssueSnapshot{{ID: "iss1", Title: "任务一", Severity: "high", OccurredAt: base}}
	d.snap.issues["u2"] = []IssueSnapshot{{ID: "iss2", Title: "任务二", Severity: "high", OccurredAt: base}}
	d.snap.issues["u3"] = []IssueSnapshot{{ID: "iss3", Title: "任务三", Severity: "high", OccurredAt: base}}
	d.snap.activeUsersErr = nil
	// u2 的 issue 聚合失败 → 只跳过 u2，不阻塞整批；u1/u3 的 LLM 失败只静默跳过延续部分
	d.snap.issuesErr = map[string]error{"u2": errors.New("snapshot 失败")}
	d.llm.dailyErr = errors.New("LLM 超时")
	d.llm.dailyResp = nil
	svc := d.service()

	if err := svc.GenerateForDate(context.Background(), "2026-08-08"); err != nil {
		t.Fatal(err)
	}
	if got := auditInt(d.audit.lastDetail()["succeeded"]); got != 2 {
		t.Fatalf("expected 2 succeeded, got %v", got)
	}
	if got := auditInt(d.audit.lastDetail()["created"]); got != 2 {
		t.Fatalf("expected 2 created (u1/u3 issue items), got %v", got)
	}
}

func TestGenerateForDateConcurrencyLimit(t *testing.T) {
	d := newTestDeps()
	// 生成上限 4：并发插桩难以直接断言，这里验证多用户整体完成 + 审计一行
	for i := 0; i < 10; i++ {
		uid := string(rune('a' + i))
		d.snap.users = append(d.snap.users, UserSnapshot{ID: uid})
		base := time.Date(2026, 8, 7, 10, 0, 0, 0, testLoc)
		d.snap.issues[uid] = []IssueSnapshot{{ID: "iss" + uid, Title: "任务" + uid, Severity: "medium", OccurredAt: base}}
	}
	if err := d.service().GenerateForDate(context.Background(), "2026-08-08"); err != nil {
		t.Fatal(err)
	}
	if got := auditInt(d.audit.lastDetail()["users"]); got != 10 {
		t.Fatalf("expected 10 users, got %v", got)
	}
	if len(d.audit.actions) != 1 || d.audit.actions[0] != ActionGenerate {
		t.Fatalf("expected one generate audit row, got %v", d.audit.actions)
	}
}

func TestRunIssueSync(t *testing.T) {
	d := newTestDeps()
	// 在途待办关联已 resolved issue → cancelled，且 completed_at 为空
	d.resolver.terminalIDs["iss1"] = true
	svc := d.service()
	svc.Create("u1", "member", CreateRequest{Title: "x"}) // 无 issue
	item, err := svc.Create("u1", "member", CreateRequest{Title: "y"})
	if err != nil {
		t.Fatal(err)
	}
	iid := "iss1"
	d.repo.todos[item.ID].IssueID = &iid

	if err := svc.RunIssueSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := d.repo.GetByID(item.ID)
	if got.Status != StatusCancelled || got.CompletedAt != nil || got.CompletedBy != nil {
		t.Fatalf("issue sync failed: %+v", got)
	}
	if got := auditInt(d.audit.lastDetail()["count"]); got != 1 {
		t.Fatalf("expected count 1, got %v", got)
	}
	// 幂等：重复执行不再计入
	if err := svc.RunIssueSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := auditInt(d.audit.lastDetail()["count"]); got != 0 {
		t.Fatalf("issue sync must be idempotent, count = %v", got)
	}
}

func TestRunIssueSyncNonTerminalUntouched(t *testing.T) {
	d := newTestDeps()
	// resolver 不返回该 issue → 在途待办保持 pending
	svc := d.service()
	item, err := svc.Create("u1", "member", CreateRequest{Title: "y"})
	if err != nil {
		t.Fatal(err)
	}
	iid := "iss-not-terminal"
	d.repo.todos[item.ID].IssueID = &iid

	if err := svc.RunIssueSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := d.repo.GetByID(item.ID)
	if got.Status != StatusPending {
		t.Fatalf("non-terminal issue todo must stay pending, got %+v", got)
	}
	if got := auditInt(d.audit.lastDetail()["count"]); got != 0 {
		t.Fatalf("expected count 0, got %v", got)
	}
}

func TestRunIssueSyncResolverError(t *testing.T) {
	d := newTestDeps()
	d.resolver.err = errors.New("resolver down")
	if err := d.service().RunIssueSync(context.Background()); err == nil {
		t.Fatal("expected resolver error to propagate")
	}
	if d.repo.issueSyncCalls != 0 {
		t.Fatal("repo.IssueSync must not be called when resolver fails")
	}
}

func TestRunRolloverAudit(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", "member", CreateRequest{Title: "x"})
	d.repo.todos[created.ID].CreatedFor = "2026-08-01"

	if err := svc.RunRollover(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail := d.audit.lastDetail()
	if auditInt(detail["count"]) != 1 || detail["date"] != "2026-08-07" {
		t.Fatalf("unexpected rollover audit: %v", detail)
	}
}

func TestRunCleanupAudit(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, _ := svc.Create("u1", "member", CreateRequest{Title: "旧任务"})
	d.repo.todos[created.ID].CreatedFor = "2026-06-01"
	d.repo.todos[created.ID].Status = StatusDone

	if err := svc.RunCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail := d.audit.lastDetail()
	if auditInt(detail["done_cancelled"]) != 1 || auditInt(detail["inflight"]) != 0 {
		t.Fatalf("unexpected cleanup audit: %v", detail)
	}
	if d.repo.lastCleanupCreatedFor != "2026-07-08" {
		t.Fatalf("expected 30-day cutoff 2026-07-08, got %s", d.repo.lastCleanupCreatedFor)
	}
}
