package agent

import (
	"database/sql"
	"encoding/json"
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
	task, err = svc.Complete(task.ID, CompleteTaskRequest{
		Result: json.RawMessage(`{"ok":true}`), Model: "test", PromptVersion: "v1",
		Candidates: []CandidateInput{
			{ActionType: "create_issue", Payload: json.RawMessage(`{"title":"test"}`)},
			{ActionType: "create_experience", Payload: json.RawMessage(`{"title":"test","content":"test"}`)},
		},
	})
	if err != nil || task.Status != TaskDone {
		t.Fatalf("complete = %#v, %v", task, err)
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
	for attempt := 1; attempt <= 3; attempt++ {
		failedTask, err := svc.Claim(30)
		if err != nil || failedTask == nil || failedTask.ReportID != failedReportID {
			t.Fatalf("claim failed task attempt %d = %#v, %v", attempt, failedTask, err)
		}
		failedTask, err = svc.Fail(failedTask.ID, "temporary model error")
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
