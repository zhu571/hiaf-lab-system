package alert

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// ---------- 纯逻辑单测（无 DB） ----------

// fakeSender 记录发送调用（Send=仅 ntfy / SendBoth=ntfy+MeoW 双通道）；
// failErr 非 nil 时发送返回该错误（测发送失败 detail 记录）。
type fakeSender struct {
	mu      sync.Mutex
	sends   []sentMsg
	both    []sentMsg
	failErr error
}

type sentMsg struct {
	topic, title, msg, clickURL, priority string
	tags                                  []string
}

func (f *fakeSender) Send(topic, title, msg, clickURL, priority string, tags []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	f.sends = append(f.sends, sentMsg{topic, title, msg, clickURL, priority, tags})
	return nil
}

func (f *fakeSender) SendBoth(topic, title, msg, clickURL, priority string, tags []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return f.failErr
	}
	f.both = append(f.both, sentMsg{topic, title, msg, clickURL, priority, tags})
	return nil
}

func (f *fakeSender) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends) + len(f.both)
}

func TestSendPolicy(t *testing.T) {
	cases := []struct {
		level, priority string
		tags            []string
		both            bool
	}{
		{LevelCritical, "urgent", []string{"rotating_light"}, true},
		{LevelError, "high", []string{"warning"}, true},
		{LevelWarning, "high", []string{"warning"}, false},
		{LevelInfo, "default", nil, false},
	}
	for _, tc := range cases {
		p, tags, both := sendPolicy(tc.level)
		if p != tc.priority || both != tc.both || !equalTags(tags, tc.tags) {
			t.Fatalf("sendPolicy(%s) = (%s,%v,%v), want (%s,%v,%v)",
				tc.level, p, tags, both, tc.priority, tc.tags, tc.both)
		}
	}
}

func equalTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 字段级校验（防洪）：枚举/长度校验在触碰 DB 前完成（repo 为 nil 也不 panic）。
func TestReportValidation(t *testing.T) {
	svc := NewService(nil, &fakeSender{}, nil)
	cases := []struct {
		name, level, source, title, detail string
	}{
		{"bad level", "fatal", "security", "t", "d"},
		{"empty level", "", "security", "t", "d"},
		{"bad source", "warning", "unknown", "t", "d"},
		{"empty title", "warning", "security", "", "d"},
		{"title too long", "warning", "security", string(make([]rune, 257)), "d"},
		{"detail too long", "warning", "security", "t", string(make([]rune, 2001))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Report(context.Background(), tc.level, tc.source, tc.title, tc.detail)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestResolveBySourceValidation(t *testing.T) {
	svc := NewService(nil, &fakeSender{}, nil)
	if err := svc.ResolveBySource(context.Background(), "", "", ResolvedBySystem); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty source/title must be rejected, got %v", err)
	}
	if err := svc.ResolveBySource(context.Background(), "nope", "t", ResolvedBySystem); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad source must be rejected, got %v", err)
	}
}

// ---------- 集成测试（TEST_DATABASE_URL，CI/本地按 scripts/test-go.sh 应用全量迁移 001-036） ----------

func openAlertTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Skip("TEST_DATABASE_URL unreachable")
	}
	return db
}

// newTestService 构造带注入时钟（可变，setClock 推进）与 fakeSender 的服务，
// 并清空 alerts 表与维护任务审计行（audit_log append-only，跨用例残留会污染计数）。
func newTestService(t *testing.T, base time.Time) (*Service, *fakeSender, *sql.DB, func(time.Time)) {
	t.Helper()
	db := openAlertTestDB(t)
	if _, err := db.Exec(`DELETE FROM alerts`); err != nil {
		t.Fatalf("clean alerts: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM audit_log WHERE action IN ('alerts.ttl','alerts.cleanup')`); err != nil {
		t.Fatalf("clean alert audit rows: %v", err)
	}
	sender := &fakeSender{}
	clock := base
	svc := NewService(NewRepository(db), sender, db, func() time.Time { return clock })
	setClock := func(t time.Time) { clock = t }
	return svc, sender, db, setClock
}

// TestReportWindowDedup 窗口内合并（occurrence_count+1 不重发）、窗口外复发
// （计数重置+重发）、first_seen 不重置、resolved 后同源新开 active 行。
func TestReportWindowDedup(t *testing.T) {
	base := time.Now()
	svc, sender, db, setClock := newTestService(t, base)
	defer db.Close()

	res, err := svc.Report(context.Background(), LevelCritical, SourceSecurity, "测试告警", "首次")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deduplicated || res.OccurrenceCount != 1 || res.AlertID == "" {
		t.Fatalf("first report: %+v", res)
	}
	firstID := res.AlertID
	if sender.total() != 1 {
		t.Fatalf("first report must send once, got %d", sender.total())
	}
	// critical → SendBoth（urgent + rotating_light）
	if len(sender.both) != 1 || sender.both[0].priority != "urgent" || sender.both[0].topic != Topic {
		t.Fatalf("critical must go via SendBoth with urgent priority, got %+v", sender.both)
	}
	firstSeen := mustAlert(t, db, firstID).FirstSeen

	// 窗口内（+1min）→ 合并计数，不重发。
	setClock(base.Add(time.Minute))
	res, err = svc.Report(context.Background(), LevelCritical, SourceSecurity, "测试告警", "窗口内合并")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Deduplicated || res.OccurrenceCount != 2 {
		t.Fatalf("in-window merge: %+v", res)
	}
	if sender.total() != 1 {
		t.Fatalf("in-window merge must not resend, got %d sends", sender.total())
	}

	// 窗口外（+12min，距 last_seen 11min > 10min 窗口）→ 计数重置 1、重发、first_seen 不重置。
	setClock(base.Add(12 * time.Minute))
	res, err = svc.Report(context.Background(), LevelCritical, SourceSecurity, "测试告警", "窗口外复发")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deduplicated || res.OccurrenceCount != 1 {
		t.Fatalf("out-of-window recur: %+v", res)
	}
	if sender.total() != 2 {
		t.Fatalf("out-of-window recur must resend, got %d sends", sender.total())
	}
	got := mustAlert(t, db, firstID)
	if !got.FirstSeen.Equal(firstSeen) {
		t.Fatalf("first_seen must never reset: %v → %v", firstSeen, got.FirstSeen)
	}
	if got.Status != StatusActive || got.ResolvedAt != nil {
		t.Fatalf("recur must reuse active row, got %+v", got)
	}

	// resolve 后同源再上报 → 新开 active 行（历史逐条保留）。
	if err := svc.ResolveBySource(context.Background(), SourceSecurity, "测试告警", ResolvedBySystem); err != nil {
		t.Fatal(err)
	}
	res, err = svc.Report(context.Background(), LevelCritical, SourceSecurity, "测试告警", "resolve 后复发")
	if err != nil {
		t.Fatal(err)
	}
	second := mustAlert(t, db, res.AlertID)
	if second.ID == firstID || second.Status != StatusActive || second.OccurrenceCount != 1 {
		t.Fatalf("after resolve must open a new active row: %+v", second)
	}
	if mustAlert(t, db, firstID).Status != StatusResolved {
		t.Fatal("old row must stay resolved")
	}
}

// TestReportSendFailureRecordsDetail 发送失败不影响落库/去重，但失败信息
// 必须追加进 detail（便于事后定位）。
func TestReportSendFailureRecordsDetail(t *testing.T) {
	svc, sender, db, _ := newTestService(t, time.Now())
	defer db.Close()
	sender.failErr = errors.New("ntfy timeout")

	res, err := svc.Report(context.Background(), LevelWarning, SourceUpdater, "发送失败测试", "正文")
	if err != nil {
		t.Fatal(err)
	}
	got := mustAlert(t, db, res.AlertID)
	if !strings.Contains(got.Detail, "通知发送失败: ntfy timeout") {
		t.Fatalf("detail must record send failure, got %q", got.Detail)
	}
	// 发送失败不重试、不算重发，但记录仍然落库且去重语义不变。
	res2, err := svc.Report(context.Background(), LevelWarning, SourceUpdater, "发送失败测试", "再报")
	if err != nil {
		t.Fatal(err)
	}
	if !res2.Deduplicated || res2.OccurrenceCount != 2 {
		t.Fatalf("dedup after send failure: %+v", res2)
	}
}

// TestResolveIdempotent 不匹配 active 行 → 幂等 success（不报错）。
func TestResolveIdempotent(t *testing.T) {
	base := time.Now()
	svc, _, db, _ := newTestService(t, base)
	defer db.Close()

	if err := svc.ResolveBySource(context.Background(), SourceIOC, "不存在的告警", ResolvedBySystem); err != nil {
		t.Fatalf("resolve without active row must be idempotent success, got %v", err)
	}
	res, err := svc.Report(context.Background(), LevelError, SourceInstruments, "仪器恢复失败: e5063a", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveByID(context.Background(), res.AlertID, "alice"); err != nil {
		t.Fatal(err)
	}
	// 再次 resolve 同一 id → 幂等。
	if _, err := svc.ResolveByID(context.Background(), res.AlertID, "alice"); err != nil {
		t.Fatalf("double resolve must be idempotent, got %v", err)
	}
	// resolved_by 记录操作者。
	got := mustAlert(t, db, res.AlertID)
	if got.ResolvedBy != "alice" || got.Status != StatusResolved || got.ResolvedAt == nil {
		t.Fatalf("resolve attribution: %+v", got)
	}
}

// TestTTLAndCleanup TTL 兜底幂等（连跑两次影响一致）+ 90 天清理幂等 + active 永不删。
func TestTTLAndCleanup(t *testing.T) {
	base := time.Now()
	svc, _, db, _ := newTestService(t, base)
	defer db.Close()

	// 两条 active：一条 last_seen 25h 前（过期）、一条当前。
	if _, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "过期告警", "x"); err != nil {
		t.Fatal(err)
	}
	expired := latestActive(t, db, SourceWatchdog, "过期告警").ID
	if _, err := db.Exec(`UPDATE alerts SET last_seen = now() - interval '25 hours' WHERE id = $1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Report(context.Background(), LevelWarning, SourceWatchdog, "未过期告警", "x"); err != nil {
		t.Fatal(err)
	}

	// 手工造一条 91 天前的 resolved 行。
	if _, err := svc.Report(context.Background(), LevelInfo, SourceUpdater, "将被清理", "x"); err != nil {
		t.Fatal(err)
	}
	oldResolved := latestActive(t, db, SourceUpdater, "将被清理")
	if _, err := svc.ResolveByID(context.Background(), oldResolved.ID, ResolvedBySystem); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE alerts SET resolved_at = now() - interval '91 days' WHERE id = $1`, oldResolved.ID); err != nil {
		t.Fatal(err)
	}

	// TTL 第一轮：仅过期行被 resolve。
	ttlCount, err := svc.RunTTLOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ttlCount != 1 {
		t.Fatalf("ttl count = %d, want 1", ttlCount)
	}
	if got := mustAlert(t, db, expired); got.Status != StatusResolved || got.ResolvedBy != ResolvedByTTL {
		t.Fatalf("expired alert must be ttl-resolved: %+v", got)
	}
	if got := mustAlert(t, db, latestActive(t, db, SourceWatchdog, "未过期告警").ID); got.Status != StatusActive {
		t.Fatal("fresh alert must stay active")
	}

	// TTL 第二轮：幂等 0 变更。
	if n, err := svc.RunTTLOnce(context.Background(), time.Now()); err != nil || n != 0 {
		t.Fatalf("ttl second run must be idempotent (n=0), got n=%d err=%v", n, err)
	}

	// 清理第一轮：仅 91 天前 resolved 行被删；TTL resolved 行（今天）与 active 行保留。
	cleanCount, err := svc.RunCleanupOnce(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if cleanCount != 1 {
		t.Fatalf("cleanup count = %d, want 1", cleanCount)
	}
	if _, err := svc.Get(context.Background(), oldResolved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("91d resolved row must be deleted, got %v", err)
	}
	if _, err := svc.Get(context.Background(), expired); err != nil {
		t.Fatalf("today-resolved ttl row must survive cleanup: %v", err)
	}
	if _, err := svc.Get(context.Background(), latestActive(t, db, SourceWatchdog, "未过期告警").ID); err != nil {
		t.Fatalf("active row must never be cleaned: %v", err)
	}

	// 清理第二轮：幂等 0 变更。
	if n, err := svc.RunCleanupOnce(context.Background(), time.Now()); err != nil || n != 0 {
		t.Fatalf("cleanup second run must be idempotent (n=0), got n=%d err=%v", n, err)
	}

	// 维护审计：变更轮次落审计、0 变更轮次不落。
	if n := countAudit(t, db, "alerts.ttl"); n != 1 {
		t.Fatalf("alerts.ttl audit rows = %d, want 1", n)
	}
	if n := countAudit(t, db, "alerts.cleanup"); n != 1 {
		t.Fatalf("alerts.cleanup audit rows = %d, want 1", n)
	}
}

// TestConcurrentReport 并发防双发：50 goroutine 同 source+title 同时上报 →
// 恰 1 条 active 行（部分唯一索引最终防线），行锁累加 occurrence_count=50，
// 且仅 1 次发送（同一窗口内合并）。ON CONFLICT 分支（xmax≠0）只可能撞窗口内
// 新行（窗口外的行会被 SELECT 命中走复发分支），deduplicated=!inserted 一律不重发。
func TestConcurrentReport(t *testing.T) {
	base := time.Now()
	svc, sender, db, _ := newTestService(t, base)
	defer db.Close()

	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Report(context.Background(), LevelWarning, SourceInstruments,
				"并发告警", fmt.Sprintf("第 %d 次", i))
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent report error: %v", err)
		}
	}

	var id string
	var count int
	if err := db.QueryRow(
		`SELECT id, occurrence_count FROM alerts
		  WHERE source = $1 AND title = $2 AND status = 'active'`,
		SourceInstruments, "并发告警",
	).Scan(&id, &count); err != nil {
		t.Fatalf("query active row: %v", err)
	}
	var activeTotal int
	if err := db.QueryRow(
		`SELECT count(*) FROM alerts WHERE source = $1 AND title = $2 AND status = 'active'`,
		SourceInstruments, "并发告警",
	).Scan(&activeTotal); err != nil {
		t.Fatal(err)
	}
	if activeTotal != 1 {
		t.Fatalf("must be exactly 1 active row (unique index final line), got %d", activeTotal)
	}
	if count != n {
		t.Fatalf("occurrence_count = %d, want %d (row lock must accumulate)", count, n)
	}
	if sender.total() != 1 {
		t.Fatalf("same-window burst must send exactly once, got %d", sender.total())
	}
}

// TestListAndGet 列表分页/过滤与详情。
func TestListAndGet(t *testing.T) {
	base := time.Now()
	svc, _, db, _ := newTestService(t, base)
	defer db.Close()

	for i := 0; i < 3; i++ {
		if _, err := svc.Report(context.Background(), LevelWarning, SourceSecurity,
			fmt.Sprintf("列表告警%d", i), "x"); err != nil {
			t.Fatal(err)
		}
	}
	first := latestActive(t, db, SourceSecurity, "列表告警0")
	if _, err := svc.ResolveByID(context.Background(), first.ID, "alice"); err != nil {
		t.Fatal(err)
	}

	items, total, err := svc.List(context.Background(), StatusActive, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("active list: total=%d len=%d, want 2/2", total, len(items))
	}
	// active 按 last_seen DESC：最新的在前。
	if items[0].Title != "列表告警2" {
		t.Fatalf("active order by last_seen desc: first=%s", items[0].Title)
	}
	items, total, err = svc.List(context.Background(), StatusResolved, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ResolvedBy != "alice" {
		t.Fatalf("resolved list: %+v", items)
	}
	// 分页。
	items, total, err = svc.List(context.Background(), StatusActive, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 || items[0].Title != "列表告警1" {
		t.Fatalf("paged list: total=%d items=%+v", total, items)
	}
	// 非法 status。
	if _, _, err := svc.List(context.Background(), "bogus", 50, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad status must be rejected, got %v", err)
	}

	got, err := svc.Get(context.Background(), first.ID)
	if err != nil || got.Status != StatusResolved || got.Title != "列表告警0" {
		t.Fatalf("Get: %+v err=%v", got, err)
	}
	if _, err := svc.Get(context.Background(), "00000000-0000-0000-0000-00000000dead"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing alert must be ErrNotFound, got %v", err)
	}
}

// ---------- 工具 ----------

func mustAlert(t *testing.T, db *sql.DB, id string) *Alert {
	t.Helper()
	svc := NewService(NewRepository(db), &fakeSender{}, db)
	a, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return a
}

func latestActive(t *testing.T, db *sql.DB, source, title string) *Alert {
	t.Helper()
	var id string
	if err := db.QueryRow(
		`SELECT id FROM alerts WHERE source = $1 AND title = $2 AND status = 'active' ORDER BY created_at DESC LIMIT 1`,
		source, title,
	).Scan(&id); err != nil {
		t.Fatalf("latest active row: %v", err)
	}
	return mustAlert(t, db, id)
}

func countAudit(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}
