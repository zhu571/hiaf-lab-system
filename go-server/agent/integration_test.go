package agent

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
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
