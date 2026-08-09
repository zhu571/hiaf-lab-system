package issues

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func TestCandidateIDRoundtripPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const userID = "00000000-0000-0000-0000-00000000b140"
	const projectID = "00000000-0000-0000-0000-00000000b141"
	const reportID = "00000000-0000-0000-0000-00000000b142"
	defer func() {
		db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN (SELECT id FROM issues WHERE project_id = $1)`, projectID)
		db.Exec(`DELETE FROM issues WHERE project_id = $1`, projectID)
		db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id = $1)`, reportID)
		db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id = $1`, reportID)
		db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID)
		db.Exec(`DELETE FROM projects WHERE id = $1`, projectID)
		db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}()
	db.Exec(`DELETE FROM issue_project_links WHERE issue_id IN (SELECT id FROM issues WHERE project_id = $1)`, projectID)
	db.Exec(`DELETE FROM issues WHERE project_id = $1`, projectID)
	db.Exec(`DELETE FROM agent_candidate_actions WHERE task_id IN (SELECT id FROM pending_agent_tasks WHERE report_id = $1)`, reportID)
	db.Exec(`DELETE FROM pending_agent_tasks WHERE report_id = $1`, reportID)
	db.Exec(`DELETE FROM daily_reports WHERE id = $1`, reportID)
	db.Exec(`DELETE FROM projects WHERE id = $1`, projectID)
	db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES ($1, 'issues-candidate-user', 'unused')`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO projects (id, code, name, status, owner_user_id, created_by) VALUES ($1, 'PRJ-B141', 'candidate-id-test', 'active', $2, $2)`, projectID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO daily_reports (id, report_date, author_id) VALUES ($1, '2099-02-01', $2)`, reportID, userID); err != nil {
		t.Fatal(err)
	}
	var taskID string
	if err := db.QueryRow(`INSERT INTO pending_agent_tasks (report_id, acting_user_id) VALUES ($1, $2) RETURNING id`, reportID, userID).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	var candidateID string
	if err := db.QueryRow(
		`INSERT INTO agent_candidate_actions (task_id, action_type, project_id, pool_action_key, payload)
		 VALUES ($1, 'create_issue', $2, $3, '{"title":"t"}'::jsonb) RETURNING id`,
		taskID, projectID, taskID+":create_issue:0",
	).Scan(&candidateID); err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(db)
	candidateIDCopy := candidateID
	created, err := repo.Create(projectID, userID, CreateIssueRequest{
		Title: "candidate backlink", AiGenerated: true, CandidateID: &candidateIDCopy,
	}, time.Now(), "2099-02-01")
	if err != nil {
		t.Fatal(err)
	}
	if created.CandidateID == nil || *created.CandidateID != candidateID {
		t.Fatalf("created candidate_id = %#v", created.CandidateID)
	}
	got, err := repo.GetByID(created.ID)
	if err != nil || got == nil {
		t.Fatalf("get = %#v, %v", got, err)
	}
	if got.CandidateID == nil || *got.CandidateID != candidateID {
		t.Fatalf("get candidate_id = %#v", got.CandidateID)
	}
	items, _, err := repo.List(projectID, IssueListParams{Status: StatusOpen, Page: 1, PerPage: 10})
	if err != nil || len(items) != 1 || items[0].CandidateID == nil || *items[0].CandidateID != candidateID {
		t.Fatalf("list = %#v, %v", items, err)
	}
	byCandidate, err := repo.GetByCandidateID(candidateID)
	if err != nil || byCandidate == nil || byCandidate.ID != created.ID {
		t.Fatalf("get by candidate = %#v, %v", byCandidate, err)
	}
	missing, err := repo.GetByCandidateID("00000000-0000-0000-0000-000000009999")
	if err != nil || missing != nil {
		t.Fatalf("get by missing candidate = %#v, %v", missing, err)
	}
}
