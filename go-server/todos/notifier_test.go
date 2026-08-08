package todos

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRenderPushTemplate(t *testing.T) {
	u := UserSnapshot{ID: "u1", Username: "alice", DisplayName: "张\x00三"}
	items := []Todo{
		{Title: "写日报", Priority: PriorityHigh, Source: SourceManual, OwnerDisplayName: "张\x00三"},
		{Title: "真空度\n检查", Priority: PriorityMedium, Source: SourceIssue},
		{Title: "延续任务", Priority: PriorityLow, Source: SourceDailyLLM},
		{Title: "自然语言添加的", Priority: PriorityMedium, Source: SourceLLM, OwnerDisplayName: "张三"},
	}
	hints := []CleanupHint{{DaysLeft: 1, Count: 2}, {DaysLeft: 3, Count: 1}}
	msg := renderPushTemplate("2026-08-07", u, items, hints)

	if !strings.Contains(msg, "📋 今日待办（8月7日 周五）") {
		t.Fatalf("missing header: %q", msg)
	}
	if !strings.Contains(msg, "（张三添加）") {
		t.Fatalf("display_name cleaning/source missing: %q", msg)
	}
	if !strings.Contains(msg, "（来自 issue）") || !strings.Contains(msg, "（昨日延续）") {
		t.Fatalf("system source labels missing: %q", msg)
	}
	if !strings.Contains(msg, "1 条由张三添加") || !strings.Contains(msg, "2 条由系统生成") {
		t.Fatalf("footer missing: %q", msg)
	}
	if !strings.Contains(msg, "（LLM 添加）") || !strings.Contains(msg, "1 条由 LLM 添加") {
		t.Fatalf("llm source label missing: %q", msg)
	}
	if !strings.Contains(msg, "2 条 1 天后、1 条 3 天后") {
		t.Fatalf("cleanup hint missing: %q", msg)
	}
	// 控制字符清洗
	if strings.Contains(msg, "\x00") || strings.Contains(msg, "\n检查") {
		t.Fatalf("control chars not cleaned: %q", msg)
	}
}

func TestPriorityLabel(t *testing.T) {
	if priorityLabel(PriorityHigh) != "高" || priorityLabel(PriorityLow) != "低" || priorityLabel(PriorityMedium) != "中" {
		t.Fatal("priorityLabel failed")
	}
}

func TestFormatDateCN(t *testing.T) {
	if got := formatDateCN("2026-08-07"); got != "8月7日 周五" {
		t.Fatalf("unexpected date: %s", got)
	}
}

func TestPublishWithRetry(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	svc.publishRetryDelay = 0
	// 401/403 → 重试一次成功
	d.pub.errs = []error{errPublishAuth, nil}
	if err := svc.publishWithRetry("lab-todos-x", "标题", "正文"); err != nil {
		t.Fatal(err)
	}
	if d.pub.calls != 2 {
		t.Fatalf("expected 2 publish attempts, got %d", d.pub.calls)
	}
	// 非 auth 错误不重试
	d2 := newTestDeps()
	svc2 := d2.service()
	svc2.publishRetryDelay = 0
	d2.pub.errs = []error{errors.New("boom")}
	if err := svc2.publishWithRetry("t", "h", "m"); err == nil {
		t.Fatal("expected error")
	}
	if d2.pub.calls != 1 {
		t.Fatalf("non-auth error must not retry, got %d calls", d2.pub.calls)
	}
	// auth 错误重试后仍失败 → 返回错误
	d3 := newTestDeps()
	svc3 := d3.service()
	svc3.publishRetryDelay = 0
	d3.pub.errs = []error{errPublishAuth, errPublishAuth}
	if err := svc3.publishWithRetry("t", "h", "m"); err == nil {
		t.Fatal("expected error after retry")
	}
	if d3.pub.calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", d3.pub.calls)
	}
}

func TestPushForUserEmptyListSkipsPublish(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	u := UserSnapshot{ID: "u1", Username: "alice"}
	if err := svc.pushForUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if d.pub.calls != 0 {
		t.Fatalf("empty list must skip publish, got %d", d.pub.calls)
	}
}

func TestPushForUserEnsuresACLThenPublishes(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	created, err := svc.Create("u1", "member", CreateRequest{Title: "写日报"})
	if err != nil {
		t.Fatal(err)
	}
	u := UserSnapshot{ID: "u1", Username: "alice", DisplayName: "张三"}
	if err := svc.pushForUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	// ACL 先写：EnsureUser + EnsureAccess
	if len(d.ntfy.ensured) != 1 || d.ntfy.ensured[0] != "todo-alice" {
		t.Fatalf("expected ensure user todo-alice, got %v", d.ntfy.ensured)
	}
	if len(d.ntfy.accesses) != 1 || !strings.HasSuffix(d.ntfy.accesses[0], ":read-only") {
		t.Fatalf("expected read-only access, got %v", d.ntfy.accesses)
	}
	// 再推送
	if d.pub.calls != 1 || d.pub.topics[0] != topicForUser("u1") {
		t.Fatalf("unexpected publish: topics=%v", d.pub.topics)
	}
	// 幂等：第二次推送不再建号
	if err := svc.pushForUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	if len(d.ntfy.ensured) != 1 {
		t.Fatalf("ensure must be cached, got %d calls", len(d.ntfy.ensured))
	}
	// created todo 出现在推送正文
	if !strings.Contains(d.pub.messages[0], created.Title) {
		t.Fatalf("todo missing from message: %q", d.pub.messages[0])
	}
}

func TestCleanupHintsFor(t *testing.T) {
	d := newTestDeps()
	svc := d.service()
	// a(done, created_for 07-10): 28 天 → 剩余 2；b(pending, created_at 07-09): 29 天 → 1；
	// c(deferred, created_at 07-08): 30 天 → 0；d(>30 天) 不计
	t0 := time.Date(2026, 7, 9, 9, 0, 0, 0, testLoc)
	t1 := time.Date(2026, 7, 8, 9, 0, 0, 0, testLoc)
	old := time.Date(2026, 7, 10, 9, 0, 0, 0, testLoc)
	d.repo.hintCandidates = []Todo{
		{Title: "a", Status: StatusDone, CreatedFor: "2026-07-10", CreatedAt: old},
		{Title: "b", Status: StatusPending, CreatedAt: t0},
		{Title: "c", Status: StatusDeferred, CreatedAt: t1},
		{Title: "d", Status: StatusDone, CreatedFor: "2026-06-01", CreatedAt: old},
	}
	hints, err := svc.cleanupHintsFor("u1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 3 {
		t.Fatalf("expected 3 hint groups, got %+v", hints)
	}
	byDay := map[int]int{}
	for _, h := range hints {
		byDay[h.DaysLeft] = h.Count
	}
	if byDay[0] != 1 || byDay[1] != 1 || byDay[2] != 1 {
		t.Fatalf("unexpected hint groups: %+v", hints)
	}
}

func TestPushDailyAuditAndPartialFailure(t *testing.T) {
	d := newTestDeps()
	d.snap.users = []UserSnapshot{{ID: "u1", Username: "a"}, {ID: "u2", Username: "b"}}
	svc := d.service()
	// u1 有待办且推送成功；u2 空清单跳过（两用户都成功处理）
	svc.Create("u1", "member", CreateRequest{Title: "任务"})
	if err := svc.PushDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	detail := d.audit.lastDetail()
	if auditInt(detail["users"]) != 2 || auditInt(detail["pushed"]) != 2 {
		t.Fatalf("unexpected push audit: %v", detail)
	}
}

func TestPushDailyPartialFailureReturnsError(t *testing.T) {
	d := newTestDeps()
	d.snap.users = []UserSnapshot{{ID: "u1", Username: "a"}, {ID: "u2", Username: "b"}}
	svc := d.service()
	svc.Create("u1", "member", CreateRequest{Title: "任务1"})
	svc.Create("u2", "member", CreateRequest{Title: "任务2"})
	// u1 推送成功；u2 持续 401（重试一次后仍失败）
	d.pub.errs = []error{nil, errPublishAuth, errPublishAuth}
	err := svc.PushDaily(context.Background())
	// 失败计数口径（方案 §9）：任意用户失败都计入该批失败 → 返回 error
	if err == nil {
		t.Fatal("任意用户推送失败必须让批次返回 error（计入连续失败告警）")
	}
	// 审计仍按批落一行，含失败计数
	detail := d.audit.lastDetail()
	if auditInt(detail["pushed"]) != 1 || auditInt(detail["failed"]) != 1 {
		t.Fatalf("unexpected push audit: %v", detail)
	}
}

