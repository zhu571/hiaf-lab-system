package audit

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func TestListByAgentTaskIDPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const taskID = "00000000-0000-0000-0000-00000000c340"
	defer db.Exec(`DELETE FROM audit_log WHERE agent_task_id = $1 OR request_id LIKE 'trace_test_%'`, taskID)
	db.Exec(`DELETE FROM audit_log WHERE agent_task_id = $1 OR request_id LIKE 'trace_test_%'`, taskID)
	if _, err := db.Exec(
		`INSERT INTO audit_log (request_id, username, method, path, action, status_code, client_ip, actor_type, agent_task_id)
		 VALUES ('trace_test_1', 'agent@system', 'POST', '/api/v1/agent/tasks/x/complete', 'agent.tasks.complete', 200, '', 'agent', $1)`, taskID,
	); err != nil {
		t.Fatal(err)
	}
	// 干扰行：不带 agent_task_id，不应被捞出。
	if _, err := db.Exec(
		`INSERT INTO audit_log (request_id, username, method, path, action, status_code, client_ip, actor_type)
		 VALUES ('trace_test_2', 'usr_1', 'POST', '/api/v1/issues', 'issues.create', 201, '', 'user')`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO audit_log (request_id, username, method, path, action, status_code, client_ip, actor_type, agent_task_id)
		 VALUES ('trace_test_3', 'admin', 'POST', '/api/v1/agent/candidates/y/approve', 'agent.candidates.approve', 200, '', 'user', $1)`, taskID,
	); err != nil {
		t.Fatal(err)
	}

	svc := NewService(db)
	records, err := svc.ListByAgentTaskID(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	if records[0].RequestID != "trace_test_1" || records[1].RequestID != "trace_test_3" {
		t.Fatalf("order = %q, %q", records[0].RequestID, records[1].RequestID)
	}
	if records[0].ActorType != "agent" {
		t.Fatalf("actor_type = %q", records[0].ActorType)
	}
	if records[0].AgentTaskID == nil || *records[0].AgentTaskID != taskID {
		t.Fatalf("agent_task_id = %#v", records[0].AgentTaskID)
	}
}
