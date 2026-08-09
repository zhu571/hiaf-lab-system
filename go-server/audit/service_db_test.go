package audit

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// requireChainSchema 在 029 未迁移的库上跳过（函数/触发器缺失时链测试无从谈起）。
func requireChainSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	var hasFn, hasTrigger bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_proc WHERE proname = 'audit_chain_content')`).Scan(&hasFn); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname = 'audit_log_chain_bi')`).Scan(&hasTrigger); err != nil {
		t.Fatal(err)
	}
	if !hasFn || !hasTrigger {
		t.Skip("029 迁移未应用（audit_chain_content/audit_log_chain_bi 缺失）")
	}
}

func cleanupChainTestRows(db *sql.DB) {
	db.Exec(`DELETE FROM audit_log
			 WHERE request_id LIKE 'chain_test_%' OR request_id LIKE 'evt_test_%' OR request_id LIKE 'byrid_test_%'`)
}

// insertChained 复刻应用层 insertAuditLog 的写入顺序：同事务先取 advisory lock 再
// INSERT（id 分配在锁内、id 序==链序），避免与并发写入者产生"id 序 ≠ 锁序"分叉。
func insertChained(t *testing.T, db *sql.DB, requestID, action, actorType, detail string) int64 {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(714001)`); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := tx.QueryRow(
		`INSERT INTO audit_log (request_id, username, method, path, action, status_code, client_ip, actor_type, detail)
		 VALUES ($1, 'tester', 'POST', '/api/v1/test', $2, 200, '', $3, $4::jsonb)
		 RETURNING id`, requestID, action, actorType, detail,
	).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestVerifyChainPostgres(t *testing.T) {
	db := openTestDB(t)
	requireChainSchema(t, db)
	ctx := context.Background()
	svc := NewService(db)
	cleanupChainTestRows(db)
	t.Cleanup(func() { cleanupChainTestRows(db) })

	var base int64
	if err := db.QueryRow(`SELECT COALESCE(max(id), 0) FROM audit_log`).Scan(&base); err != nil {
		t.Fatal(err)
	}
	insertChained(t, db, "chain_test_1", "chain.test.alpha", "user", `{"i":1}`)
	id2 := insertChained(t, db, "chain_test_2", "chain.test.beta", "agent", `{"i":2}`)
	id3 := insertChained(t, db, "chain_test_3", "chain.test.alpha", "system", `{"i":3}`)

	// 触发器自动入链：三行 hash 非空、prev_hash 链式衔接。
	var unchained int
	if err := db.QueryRow(
		`SELECT count(*) FROM audit_log a
		 WHERE a.id > $1 AND a.id <= $2
		   AND (a.hash IS NULL
		        OR a.prev_hash <> COALESCE((SELECT p.hash FROM audit_log p WHERE p.id < a.id ORDER BY p.id DESC LIMIT 1), repeat('0', 64)))`,
		base, id3,
	).Scan(&unchained); err != nil {
		t.Fatal(err)
	}
	if unchained != 0 {
		t.Fatalf("unchained rows = %d, want 0", unchained)
	}

	// 区间校验（from_id 以 base 行为锚点）。
	res, err := svc.VerifyChain(ctx, base+1, id3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Checked < 3 {
		t.Fatalf("range verify = %+v, want valid && checked>=3", res)
	}

	// 篡改中间行 → 内容重算不匹配，first_broken_id 指向被篡改行。
	if _, err := db.Exec(`UPDATE audit_log SET action = 'chain.tampered' WHERE id = $1`, id2); err != nil {
		t.Fatal(err)
	}
	res, err = svc.VerifyChain(ctx, base+1, id3)
	if err != nil {
		t.Fatal(err)
	}
	if res.Valid || res.FirstBrokenID == nil || *res.FirstBrokenID != id2 {
		t.Fatalf("tampered range verify = %+v, want broken at %d", res, id2)
	}

	// 全链扫描（from_id=0，创世块锚定）同样检出。
	full, err := svc.VerifyChain(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if full.Valid || full.FirstBrokenID == nil {
		t.Fatalf("full verify after tamper = %+v, want invalid", full)
	}
}

func TestListEventsPostgres(t *testing.T) {
	db := openTestDB(t)
	requireChainSchema(t, db)
	ctx := context.Background()
	svc := NewService(db)
	cleanupChainTestRows(db)
	t.Cleanup(func() { cleanupChainTestRows(db) })

	insertChained(t, db, "evt_test_1", "evt.test.alpha", "user", `{"n":1}`)
	insertChained(t, db, "evt_test_2", "evt.test.beta", "agent", `{"n":2}`)
	insertChained(t, db, "evt_test_3", "evt.test.alpha", "system", `{"n":3}`)

	// action 过滤。
	l, err := svc.ListEvents(ctx, EventFilter{Action: "evt.test.alpha", Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 2 || len(l.Items) != 2 {
		t.Fatalf("action filter: total = %d, items = %d, want 2/2", l.Total, len(l.Items))
	}
	// 最新在前 + hash 链字段输出。
	if l.Items[0].RequestID != "evt_test_3" || l.Items[1].RequestID != "evt_test_1" {
		t.Fatalf("order = %q, %q, want evt_test_3, evt_test_1", l.Items[0].RequestID, l.Items[1].RequestID)
	}
	if l.Items[0].Hash == nil || l.Items[0].PrevHash == nil {
		t.Fatalf("hash fields missing: %+v", l.Items[0])
	}
	if l.Items[0].ActorType != "system" {
		t.Fatalf("actor_type = %q, want system", l.Items[0].ActorType)
	}

	// actor_type 过滤（与 action 组合，隔离其他测试的行）。
	l, err = svc.ListEvents(ctx, EventFilter{Action: "evt.test.beta", ActorType: "agent", Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 1 || l.Items[0].RequestID != "evt_test_2" {
		t.Fatalf("actor filter: %+v", l)
	}

	// 分页：per_page=1 翻页。
	l, err = svc.ListEvents(ctx, EventFilter{Action: "evt.test.alpha", Page: 2, PerPage: 1})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 2 || len(l.Items) != 1 || l.Items[0].RequestID != "evt_test_1" {
		t.Fatalf("page 2: %+v", l)
	}

	// 时间过滤：未来起点 → 0；很久以前终点 → 0。
	future := time.Now().Add(time.Hour)
	l, err = svc.ListEvents(ctx, EventFilter{Action: "evt.test.alpha", From: future, Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 0 {
		t.Fatalf("from future: total = %d, want 0", l.Total)
	}
	past := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	l, err = svc.ListEvents(ctx, EventFilter{Action: "evt.test.alpha", To: past, Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 0 {
		t.Fatalf("to past: total = %d, want 0", l.Total)
	}
}

func TestListByRequestIDPostgres(t *testing.T) {
	db := openTestDB(t)
	requireChainSchema(t, db)
	ctx := context.Background()
	svc := NewService(db)
	cleanupChainTestRows(db)
	t.Cleanup(func() { cleanupChainTestRows(db) })

	insertChained(t, db, "byrid_test_1", "byrid.test", "user", `{"k":"v"}`)
	records, err := svc.ListByRequestID(ctx, "byrid_test_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	rec := records[0]
	if rec.Hash == nil || rec.PrevHash == nil {
		t.Fatalf("hash fields missing: %+v", rec)
	}
	if rec.Detail["k"] != "v" {
		t.Fatalf("detail = %v", rec.Detail)
	}
}

// TestListByAgentTaskIDPostgres trace 审计段回归（029 前已有）：按 agent_task_id 捞取。
func TestListByAgentTaskIDPostgres(t *testing.T) {
	db := openTestDB(t)

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

// TestConcurrentWritesChainValidPostgres 并发正确性回归：多协程经应用层写入路径
// （WriteSystemAudit→insertAuditLog，advisory lock 前置到 INSERT 同事务）并发写审计，
// id 分配发生在锁内、id 序==链序，链不得出现" id 序 ≠ 锁序"分叉。
func TestConcurrentWritesChainValidPostgres(t *testing.T) {
	db := openTestDB(t)
	requireChainSchema(t, db)
	ctx := context.Background()
	cleanupChainTestRows(db)
	t.Cleanup(func() { cleanupChainTestRows(db) })

	var base int64
	if err := db.QueryRow(`SELECT COALESCE(max(id), 0) FROM audit_log`).Scan(&base); err != nil {
		t.Fatal(err)
	}

	const writers = 16
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := middleware.WriteSystemAudit(ctx, db, "chain.concurrent", map[string]any{"i": i}); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	// 区间校验：base 之后写入的行（本测试的 16 行 + 可能的并发外部行）链必须完好。
	res, err := NewService(db).VerifyChain(ctx, base+1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.Checked < writers {
		t.Fatalf("concurrent range verify = %+v, want valid && checked>=%d", res, writers)
	}
}
