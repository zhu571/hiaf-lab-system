package agent

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

type countingExecutor struct{ calls int }

func (e *countingExecutor) Execute(AgentCandidateAction, string) error {
	e.calls++
	return nil
}

func TestQueueAndCandidateLifecyclePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const userID = "00000000-0000-0000-0000-00000000a140"
	const reportID = "00000000-0000-0000-0000-00000000a141"
	const failedReportID = "00000000-0000-0000-0000-00000000a142"
	defer func() {
		db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id IN ($1, $2))`, reportID, failedReportID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id IN ($1, $2)`, reportID, failedReportID)
		db.Exec(`DELETE FROM daily_reports WHERE id IN ($1, $2)`, reportID, failedReportID)
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}()
	db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id IN ($1, $2))`, reportID, failedReportID)
	db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id IN ($1, $2)`, reportID, failedReportID)
	db.Exec(`DELETE FROM daily_reports WHERE id IN ($1, $2)`, reportID, failedReportID)
	db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, 'agent-integration-user', 'unused')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2099-01-15', $2)`, reportID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, reportID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE pending_agent_tasks SET created_at = '2000-01-01' WHERE report_id = $1`, reportID); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(db)
	svc := NewService(repo)
	executor := &countingExecutor{}
	svc.SetExecutor(executor)
	task, err := svc.Claim(30)
	if err != nil || task == nil || task.ReportID != reportID {
		t.Fatalf("claim = %#v, %v", task, err)
	}
	if task.ClaimToken == nil || len(*task.ClaimToken) != 64 {
		t.Fatalf("claim token = %#v", task.ClaimToken)
	}
	// 所有权校验：错误 token 的 complete/fail 必须被拒且不改变任务状态（028）。
	if _, err := svc.Complete(task.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1", ClaimToken: "wrong-token",
	}); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("complete with wrong token err = %v", err)
	}
	if _, err := svc.Fail(task.ID, "wrong owner", "wrong-token"); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("fail with wrong token err = %v", err)
	}
	task, err = svc.Complete(task.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1", ClaimToken: *task.ClaimToken,
		RawTextSnapshot: "RF 匹配原始文本", ReportDate: "2099-01-15",
		Candidates: []CandidateInput{
			{ActionType: "create_issue", Payload: json.RawMessage(`{"title":"test"}`)},
			{ActionType: "create_experience", Payload: json.RawMessage(`{"title":"test","content":"test"}`)},
		},
	})
	if err != nil || task.Status != TaskDone {
		t.Fatalf("complete = %#v, %v", task, err)
	}
	// 030 快照落库：raw_text_sha256 由 Go 侧计算，report_date 原样写入。
	wantSHA := sha256.Sum256([]byte("RF 匹配原始文本"))
	if task.RawTextSHA256 == nil || *task.RawTextSHA256 != hex.EncodeToString(wantSHA[:]) {
		t.Fatalf("raw_text_sha256 = %#v", task.RawTextSHA256)
	}
	if task.ReportDate == nil || *task.ReportDate != "2099-01-15" {
		t.Fatalf("report_date = %#v", task.ReportDate)
	}
	var storedSHA, storedDate string
	if err := db.QueryRow(
		`SELECT raw_text_sha256, report_date::text FROM pending_agent_tasks WHERE id = $1`, task.ID,
	).Scan(&storedSHA, &storedDate); err != nil || storedSHA != *task.RawTextSHA256 || storedDate != "2099-01-15" {
		t.Fatalf("stored snapshot = %q %q, %v", storedSHA, storedDate, err)
	}
	listed, err := svc.ListCandidates(CandidatePending, 1, 20)
	if err != nil || len(listed.Items) == 0 {
		t.Fatalf("list candidates = %#v, %v", listed, err)
	}
	var candidate, rejectedCandidate AgentCandidateAction
	for _, item := range listed.Items {
		if item.TaskID == task.ID {
			if candidate.ID == "" {
				candidate = item
			} else {
				rejectedCandidate = item
			}
		}
	}
	if candidate.ID == "" || rejectedCandidate.ID == "" {
		t.Fatal("completed task candidate was not listed")
	}
	approved, err := svc.ApproveCandidate(candidate.ID, userID)
	if err != nil || approved.Status != CandidateExecuted || executor.calls != 1 {
		t.Fatalf("approve = %#v, calls=%d, err=%v", approved, executor.calls, err)
	}
	if _, err := svc.ApproveCandidate(candidate.ID, userID); err != nil || executor.calls != 1 {
		t.Fatalf("repeat approve calls=%d, err=%v", executor.calls, err)
	}
	rejected, err := svc.RejectCandidate(rejectedCandidate.ID, userID, "not useful")
	if err != nil || rejected.Status != CandidateRejected {
		t.Fatalf("reject = %#v, err=%v", rejected, err)
	}

	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2099-01-16', $2)`, failedReportID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, failedReportID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE pending_agent_tasks SET created_at = '2000-01-02' WHERE report_id = $1`, failedReportID); err != nil {
		t.Fatal(err)
	}
	// 双处理根治验证（缺口 B）：租约过期被重领后 token 轮换，旧 owner 的迟到写被拒。
	stale, err := svc.Claim(30)
	if err != nil || stale == nil || stale.ReportID != failedReportID {
		t.Fatalf("claim stale task = %#v, %v", stale, err)
	}
	if _, err := db.Exec(`UPDATE pending_agent_tasks SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := svc.Claim(30)
	if err != nil || reclaimed == nil || reclaimed.ID != stale.ID {
		t.Fatalf("reclaim = %#v, %v", reclaimed, err)
	}
	if reclaimed.ClaimToken == nil || *reclaimed.ClaimToken == *stale.ClaimToken {
		t.Fatalf("reclaim must rotate claim token: %#v -> %#v", stale.ClaimToken, reclaimed.ClaimToken)
	}
	if _, err := svc.Fail(reclaimed.ID, "stale write", *stale.ClaimToken); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("stale fail err = %v", err)
	}
	if _, err := svc.Complete(reclaimed.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1", ClaimToken: *stale.ClaimToken,
	}); !errors.Is(err, ErrInvalidLease) {
		t.Fatalf("stale complete err = %v", err)
	}
	// 用当前 token 正常 fail 计入第 1 次尝试，随后循环完成剩余两次。
	if _, err := svc.Fail(reclaimed.ID, "temporary model error", *reclaimed.ClaimToken); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE pending_agent_tasks SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, reclaimed.ID)
	for attempt := 2; attempt <= 3; attempt++ {
		failedTask, err := svc.Claim(30)
		if err != nil || failedTask == nil || failedTask.ReportID != failedReportID {
			t.Fatalf("claim failed task attempt %d = %#v, %v", attempt, failedTask, err)
		}
		failedTask, err = svc.Fail(failedTask.ID, "temporary model error", *failedTask.ClaimToken)
		if err != nil {
			t.Fatal(err)
		}
		want := TaskFailed
		if attempt == 3 {
			want = TaskDead
		}
		if failedTask.Status != want || failedTask.Attempts != attempt {
			t.Fatalf("attempt %d status=%s attempts=%d", attempt, failedTask.Status, failedTask.Attempts)
		}
		if attempt < 3 {
			db.Exec(`UPDATE pending_agent_tasks SET next_attempt_at = now() - interval '1 second' WHERE id = $1`, failedTask.ID)
		}
	}
}

type fakeTraceReportReader struct{ report *TraceReport }

func (f fakeTraceReportReader) GetReportCurrent(string, string, string) (*TraceReport, error) {
	return f.report, nil
}

type fakeTraceAuditReader struct{ events []AuditEvent }

func (f fakeTraceAuditReader) ListByAgentTaskID(string) ([]AuditEvent, error) { return f.events, nil }

type fakeTraceResolver struct{ result *TraceResult }

func (f fakeTraceResolver) IssueByCandidateID(string) (*TraceResult, error)      { return f.result, nil }
func (f fakeTraceResolver) ExperienceByCandidateID(string) (*TraceResult, error) { return f.result, nil }

func TestCandidateTracePostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const userID = "00000000-0000-0000-0000-00000000c140"
	const reportID = "00000000-0000-0000-0000-00000000c141"
	const legacyReportID = "00000000-0000-0000-0000-00000000c142"
	defer func() {
		db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id IN ($1, $2))`, reportID, legacyReportID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id IN ($1, $2)`, reportID, legacyReportID)
		db.Exec(`DELETE FROM daily_reports WHERE id IN ($1, $2)`, reportID, legacyReportID)
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}()
	db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id IN ($1, $2))`, reportID, legacyReportID)
	db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id IN ($1, $2)`, reportID, legacyReportID)
	db.Exec(`DELETE FROM daily_reports WHERE id IN ($1, $2)`, reportID, legacyReportID)
	db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, 'agent-trace-user', 'unused')`, userID); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{reportID, legacyReportID} {
		if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, $3::date, $2)`, id, userID, fmt.Sprintf("2099-03-0%d", i+1)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE daily_reports SET content_status = 'submitted' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE pending_agent_tasks SET created_at = $2 WHERE report_id = $1`, id, fmt.Sprintf("2000-01-0%d", i+1)); err != nil {
			t.Fatal(err)
		}
	}

	repo := NewRepository(db)
	svc := NewService(repo)
	svc.SetReportReader(fakeTraceReportReader{report: &TraceReport{ID: reportID, ReportDate: "2099-03-01", RawText: "当前日报文本"}})
	svc.SetAuditReader(fakeTraceAuditReader{events: []AuditEvent{{ID: 1, RequestID: "req_trace_1", Action: "agent.tasks.complete"}}})
	svc.SetResultResolver(fakeTraceResolver{result: &TraceResult{Title: "产物标题", URL: "/projects/prj/issues/iss"}})

	// 正常任务：complete 带快照 + 一个 create_issue 候选。
	task, err := svc.Claim(30)
	if err != nil || task == nil || task.ReportID != reportID {
		t.Fatalf("claim = %#v, %v", task, err)
	}
	task, err = svc.Complete(task.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1", ClaimToken: *task.ClaimToken,
		RawTextSnapshot: "快照文本", ReportDate: "2099-03-01",
		Candidates: []CandidateInput{{ActionType: "create_issue", Payload: json.RawMessage(`{"title":"trace 候选"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListCandidates(CandidatePending, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var candidateID string
	for _, item := range listed.Items {
		if item.TaskID == task.ID {
			candidateID = item.ID
		}
	}
	if candidateID == "" {
		t.Fatal("candidate not listed")
	}

	trace, err := svc.GetCandidateTrace(candidateID, userID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if trace.Candidate == nil || trace.Candidate.ID != candidateID {
		t.Fatalf("trace candidate = %#v", trace.Candidate)
	}
	if trace.Task.Model == nil || *trace.Task.Model != "test" || trace.Task.PromptVersion == nil || *trace.Task.PromptVersion != "v1" {
		t.Fatalf("trace task model = %#v %#v", trace.Task.Model, trace.Task.PromptVersion)
	}
	if trace.Task.RawTextSnapshot == nil || *trace.Task.RawTextSnapshot != "快照文本" {
		t.Fatalf("trace snapshot = %#v", trace.Task.RawTextSnapshot)
	}
	if trace.Task.RawTextSHA256 == nil || len(*trace.Task.RawTextSHA256) != 64 {
		t.Fatalf("trace sha256 = %#v", trace.Task.RawTextSHA256)
	}
	if trace.Task.ReportDate == nil || *trace.Task.ReportDate != "2099-03-01" {
		t.Fatalf("trace report_date = %#v", trace.Task.ReportDate)
	}
	if trace.Report == nil || trace.Report.RawText != "当前日报文本" {
		t.Fatalf("trace report = %#v", trace.Report)
	}
	if len(trace.Audit) != 1 || trace.Audit[0].RequestID != "req_trace_1" {
		t.Fatalf("trace audit = %#v", trace.Audit)
	}
	if trace.Result == nil || trace.Result.Title != "产物标题" {
		t.Fatalf("trace result = %#v", trace.Result)
	}

	// 存量任务降级：complete 不带快照，trace 三字段为 null。
	legacy, err := svc.Claim(30)
	if err != nil || legacy == nil || legacy.ReportID != legacyReportID {
		t.Fatalf("claim legacy = %#v, %v", legacy, err)
	}
	legacy, err = svc.Complete(legacy.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1", ClaimToken: *legacy.ClaimToken,
		Candidates: []CandidateInput{{ActionType: "create_issue", Payload: json.RawMessage(`{"title":"legacy 候选"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err = svc.ListCandidates(CandidatePending, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	var legacyCandidateID string
	for _, item := range listed.Items {
		if item.TaskID == legacy.ID {
			legacyCandidateID = item.ID
		}
	}
	legacyTrace, err := svc.GetCandidateTrace(legacyCandidateID, userID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if legacyTrace.Task.RawTextSnapshot != nil || legacyTrace.Task.RawTextSHA256 != nil || legacyTrace.Task.ReportDate != nil {
		t.Fatalf("legacy trace must degrade to null: %#v", legacyTrace.Task)
	}

	if _, err := svc.GetCandidateTrace("00000000-0000-0000-0000-000000009999", userID, "admin"); !errors.Is(err, ErrCandidateNotFound) {
		t.Fatalf("trace missing candidate err = %v", err)
	}
}
