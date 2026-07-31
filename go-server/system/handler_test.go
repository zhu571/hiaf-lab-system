package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestStripANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b[32m[UPDATE]\x1b[0m hello", "[UPDATE] hello"},
		{"[UPDATE] plain", "[UPDATE] plain"},
		{"\x1b[1;31mERROR\x1b[0m", "ERROR"},
		{"", ""},
	}
	for _, c := range cases {
		if got := stripANSI(c.in); got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStepRegexParsesStepLine(t *testing.T) {
	m := stepRe.FindStringSubmatch("[UPDATE] ===== 步骤 1/7：预检 =====")
	if m == nil {
		t.Fatal("step regex did not match")
	}
	if m[1] != "1" || m[2] != "7" || m[3] != "预检" {
		t.Errorf("unexpected groups: %v", m)
	}
}

func TestRingBufferKeepsWriteOrder(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := 0; i < 12; i++ {
		rb.Append(i+1, time.Now().Format(time.RFC3339Nano), "x")
	}
	snap := rb.Snapshot()
	if len(snap) != 5 {
		t.Fatalf("expected 5 buffered lines, got %d", len(snap))
	}
	for i, ln := range snap {
		if ln.text != "x" {
			t.Errorf("line %d corrupted", i)
		}
		if want := i + 8; ln.seq != want { // 12 条中保留最后 5 条，seq 8..12
			t.Errorf("line %d seq = %d, want %d", i, ln.seq, want)
		}
	}
}

func TestReplayPreservesSeqAndTS(t *testing.T) {
	sess := &UpdateSession{
		ID:        "upd_replayseq",
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		maxSubs:   4,
	}
	sess.ingestLine("first")
	sess.ingestLine("===== 步骤 1/2：预检 =====")
	evts := sess.replaySnapshot()
	if len(evts) != 2 {
		t.Fatalf("expected 2 replayed events, got %d", len(evts))
	}
	if evts[0].Seq != 1 || evts[0].Text != "first" || evts[0].Timestamp == "" {
		t.Errorf("bad first event: %+v", evts[0])
	}
	if evts[1].Seq != 2 || evts[1].Type != "step" || evts[1].Step != 1 || evts[1].StepTotal != 2 {
		t.Errorf("bad step event: %+v", evts[1])
	}
}

// done 事件必须在历史回放之后可达，即使回放行数超过 channel 缓冲（修复 #2）。
func TestSubscribeDeliversDoneAfterLongReplay(t *testing.T) {
	svc := NewService(t.TempDir())
	sess := &UpdateSession{
		ID:        "upd_replaytest",
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		maxSubs:   4,
	}
	svc.mu.Lock()
	svc.sessions[sess.ID] = sess
	svc.mu.Unlock()
	for i := 0; i < 600; i++ { // 超过 subBufferSize=512
		sess.ingestLine("[UPDATE] line")
	}

	ch, stop, err := svc.Subscribe(sess.ID)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	defer stop()

	// 回放进行中触发 finish（模拟脚本在回放期间结束）
	svc.finish(sess, 0, true)

	var sawDone bool
	var lineCount int
	seenSeq := map[int]bool{}
	timeout := time.After(5 * time.Second)
loop:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				break loop
			}
			if seenSeq[evt.Seq] { // 与前端一致，按 seq 去重
				continue
			}
			seenSeq[evt.Seq] = true
			switch evt.Type {
			case "line":
				lineCount++
			case "done":
				if !evt.Success {
					t.Error("expected success=true")
				}
				sawDone = true
			}
		case <-timeout:
			t.Fatalf("timeout: saw %d lines, done=%v", lineCount, sawDone)
		}
	}
	if !sawDone {
		t.Fatal("did not receive done event")
	}
	if lineCount != 600 {
		t.Errorf("expected 600 replayed lines, got %d", lineCount)
	}
}

func TestSubscribeLimitsSubscribers(t *testing.T) {
	sess := &UpdateSession{
		subs:    make(map[chan SSEEvent]struct{}),
		done:    make(chan struct{}),
		maxSubs: 2,
	}
	if _, err := sess.subscribe(); err != nil {
		t.Fatalf("first subscribe failed: %v", err)
	}
	if _, err := sess.subscribe(); err != nil {
		t.Fatalf("second subscribe failed: %v", err)
	}
	if _, err := sess.subscribe(); err != ErrTooManySubscribers {
		t.Errorf("expected ErrTooManySubscribers, got %v", err)
	}
}

func TestTriggerRejectsWhenRunning(t *testing.T) {
	svc := NewService(t.TempDir())
	running := &UpdateSession{ID: "upd_running", Status: "running"}
	svc.mu.Lock()
	svc.active = running
	svc.mu.Unlock()

	if _, err := svc.Trigger(); err == nil {
		t.Fatal("expected ErrUpdateInProgress")
	}
}

func TestTriggerScriptMissing(t *testing.T) {
	svc := NewService(t.TempDir()) // 临时目录下没有 .hermes/update.sh
	_, err := svc.Trigger()
	if err != ErrScriptMissing {
		t.Errorf("expected ErrScriptMissing, got %v", err)
	}
}

func TestGetVersionDegradesGracefully(t *testing.T) {
	svc := NewService(t.TempDir())
	info, err := svc.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion returned error: %v", err)
	}
	if info.CanUpdate {
		t.Error("expected can_update=false in non-git temp dir")
	}
	if info.Latest != "" || info.Current != "" {
		t.Errorf("expected empty hashes, got current=%q latest=%q", info.Current, info.Latest)
	}
}

func TestHandlerUpdateStreamEmitsFrames(t *testing.T) {
	svc := NewService(t.TempDir())
	sess := &UpdateSession{
		ID:        "upd_1234567890",
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   t.TempDir() + "/lab-update-upd_1234567890.log",
		doneFile:  t.TempDir() + "/lab-update-upd_1234567890.done",
		maxSubs:   4,
	}
	svc.mu.Lock()
	svc.sessions[sess.ID] = sess
	svc.mu.Unlock()

	sess.ingestLine("[UPDATE] 当前 commit: abc1234")

	r := chi.NewRouter()
	r.Get("/api/v1/admin/system/update/stream/{sessionId}", NewHandler(svc).UpdateStream)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update/stream/"+sess.ID, nil)
	w := httptest.NewRecorder()

	// SSE handler 阻塞直到 session 结束；用 goroutine 跑并在稍后 finish。
	doneCh := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(doneCh)
	}()
	time.Sleep(50 * time.Millisecond)
	svc.finish(sess, 0, true)
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("UpdateStream did not return after finish")
	}

	body := w.Body.String()
	if !strings.Contains(body, "text") || !strings.Contains(body, "abc1234") {
		t.Errorf("stream body missing line event: %s", body)
	}
	if !strings.Contains(body, `"type":"done"`) {
		t.Errorf("stream body missing done event: %s", body)
	}
}

func TestHandlerUpdateStreamSessionNotFound(t *testing.T) {
	svc := NewService(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update/stream/nope", nil)
	w := httptest.NewRecorder()
	NewHandler(svc).UpdateStream(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlerGetVersion(t *testing.T) {
	svc := NewService(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/version", nil)
	w := httptest.NewRecorder()
	NewHandler(svc).GetVersion(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Data VersionInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad response body: %v", err)
	}
	if resp.Data.CanUpdate {
		t.Error("expected can_update=false")
	}
}

// 多个 goroutine 并发广播/订阅不应死锁或丢订阅。
func TestBroadcastConcurrent(t *testing.T) {
	sess := &UpdateSession{
		subs:    make(map[chan SSEEvent]struct{}),
		done:    make(chan struct{}),
		maxSubs: 4,
	}
	var subs []chan SSEEvent
	for i := 0; i < 3; i++ {
		ch, err := sess.subscribe()
		if err != nil {
			t.Fatalf("subscribe: %v", err)
		}
		subs = append(subs, ch)
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.broadcast(SSEEvent{Seq: 1, Type: "line", Text: "x"})
		}()
	}
	wg.Wait()
	for _, ch := range subs {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("expected buffered event")
		}
	}
}

// sessionID 必须匹配白名单 upd_[a-z0-9]{10}，否则一律 404（路径穿越防护）。
func TestSubscribeRejectsInvalidSessionID(t *testing.T) {
	svc := NewService(t.TempDir())
	for _, id := range []string{"..%2F..", "upd_bb", "../../etc/passwd", "upd_ABCDEFGHIJ", "upd_12345678901"} {
		if _, _, err := svc.Subscribe(id); err != ErrSessionNotFound {
			t.Errorf("Subscribe(%q) error = %v, want ErrSessionNotFound", id, err)
		}
	}
}

func TestHandlerUpdateStreamRejectsPathTraversal(t *testing.T) {
	svc := NewService(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update/stream/..%2F..%2Fsecret", nil)
	w := httptest.NewRecorder()
	NewHandler(svc).UpdateStream(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestNanoid(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := nanoid(10)
		if err != nil {
			t.Fatalf("nanoid error: %v", err)
		}
		if len(id) != 10 {
			t.Fatalf("nanoid length = %d, want 10", len(id))
		}
		if !sessionIDRe.MatchString("upd_" + id) {
			t.Errorf("nanoid produced invalid charset: %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate nanoid: %q", id)
		}
		seen[id] = true
	}
}

// 并发订阅同一个磁盘 session：只恢复一次，后到者复用先到者的内存 session。
func TestConcurrentSubscribeRecoversOnce(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_recover123"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	donePath := logPath + ".done"
	if err := os.WriteFile(logPath, []byte("[UPDATE] line1\n[UPDATE] line2\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	marker, _ := json.Marshal(map[string]any{"exit_code": 0, "ended_at": time.Now().Format(time.RFC3339)})
	if err := os.WriteFile(donePath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	const n = 8
	// 并发订阅数超过默认 maxSubs(4)：放宽到 n，否则部分并发订阅会撞上限导致测试抖动。
	// 本测试验证的是"并发恢复只产生一个 session 指针"，不是订阅上限。
	svc.maxSubs = n
	var wg sync.WaitGroup
	ptrs := make([]*UpdateSession, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, _, err := svc.Subscribe(id)
			if err != nil {
				t.Errorf("Subscribe %d: %v", i, err)
				return
			}
			for evt := range ch {
				if evt.Type == "done" {
					break
				}
			}
			sess, ok := svc.SessionStatus(id)
			if ok {
				ptrs[i] = sess
			}
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if ptrs[i] == nil || ptrs[i] != ptrs[0] {
			t.Errorf("subscriber %d got a different session than subscriber 0", i)
		}
	}
}

// sweep 只清已完成且超期的 session，并删除日志/marker/pid 文件。
func TestSweepOnceRemovesFiles(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_sweep12345"
	sess := &UpdateSession{
		ID:        id,
		Status:    "done",
		DoneAt:    time.Now().Add(-2 * historyTTL),
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   filepath.Join(svc.logDir, "lab-update-"+id+".log"),
		doneFile:  filepath.Join(svc.logDir, "lab-update-"+id+".done"),
		pidFile:   filepath.Join(svc.logDir, "lab-update-"+id+".pid"),
		maxSubs:   4,
	}
	files := []string{sess.logFile, sess.doneFile, sess.pidFile}
	for _, p := range files {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	svc.mu.Lock()
	svc.sessions[id] = sess
	svc.active = sess
	svc.mu.Unlock()

	svc.sweepOnce()

	svc.mu.Lock()
	_, ok := svc.sessions[id]
	activeNil := svc.active == nil
	svc.mu.Unlock()
	if ok {
		t.Error("session was not swept")
	}
	if !activeNil {
		t.Error("active pointer not cleared")
	}
	for _, p := range files {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file not removed: %s", p)
		}
	}
}

// 磁盘存在 .log + .done marker：恢复为 done，历史含 done 事件。
func TestRecoverFromDiskDoneMarker(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_done123456"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	donePath := logPath + ".done"
	if err := os.WriteFile(logPath, []byte("[UPDATE] 步骤一\n[UPDATE] 步骤二\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	marker, _ := json.Marshal(map[string]any{"exit_code": 0, "old_sha": "aaa", "new_sha": "bbb"})
	if err := os.WriteFile(donePath, marker, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk returned nil")
	}
	if sess.Status != "done" {
		t.Errorf("status = %q, want done", sess.Status)
	}
	if sess.ExitCode != 0 || sess.OldSHA != "aaa" || sess.NewSHA != "bbb" {
		t.Errorf("bad recovery fields: %+v", sess)
	}
	evts := sess.replaySnapshot()
	if len(evts) != 3 {
		t.Fatalf("expected 3 events (2 lines + done), got %d", len(evts))
	}
	if evts[2].Type != "done" || !evts[2].Success {
		t.Errorf("expected done event, got %+v", evts[2])
	}
}

// marker 损坏 → 判中断并投 error 事件。
func TestRecoverFromDiskCorruptedMarker(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_corrupt123"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	if err := os.WriteFile(logPath, []byte("[UPDATE] 某行\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(logPath+".done", []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk returned nil")
	}
	if sess.Status != "done" {
		t.Errorf("status = %q, want done", sess.Status)
	}
	evts := sess.replaySnapshot()
	if len(evts) != 2 || evts[1].Type != "error" {
		t.Fatalf("expected error event for corrupted marker, got %+v", evts)
	}
}

// 无 marker 且 runner 已死 → 判中断并投 error 事件。
func TestRecoverFromDiskInterrupted(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_interrupt0"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	if err := os.WriteFile(logPath, []byte("[UPDATE] 某行\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk returned nil")
	}
	if sess.Status != "done" {
		t.Errorf("status = %q, want done (interrupted)", sess.Status)
	}
	evts := sess.replaySnapshot()
	if len(evts) != 2 || evts[1].Type != "error" {
		t.Fatalf("expected interrupted error event, got %+v", evts)
	}
}

// 磁盘日志末尾无换行的 partial 行（中断写入的残缺行）恢复时丢弃，且恢复的 done 事件带 Timestamp。
func TestRecoverFromDiskDropsPartialLine(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_partial123"
	logPath := filepath.Join(svc.logDir, "lab-update-"+id+".log")
	if err := os.WriteFile(logPath, []byte("[UPDATE] 第一行\n[UPDATE] 第二行\n[UPDATE] 残缺部分"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := os.WriteFile(logPath+".done", []byte(`{"exit_code":0}`), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	sess := svc.recoverFromDisk(id)
	if sess == nil {
		t.Fatal("recoverFromDisk returned nil")
	}
	evts := sess.replaySnapshot()
	if len(evts) != 3 { // 2 完整行 + done；partial 行不得出现
		t.Fatalf("expected 3 events, got %d: %+v", len(evts), evts)
	}
	if evts[0].Text != "[UPDATE] 第一行" || evts[1].Text != "[UPDATE] 第二行" {
		t.Errorf("unexpected replayed lines: %+v", evts)
	}
	if evts[2].Type != "done" {
		t.Fatalf("expected done event, got %+v", evts[2])
	}
	if evts[2].Timestamp == "" {
		t.Error("recovered done event missing Timestamp")
	}
}

// done 事件序号必须真正消费共享计数器：大于此前所有日志行的 seq，且不与之重复。
func TestFinishDoneSeqAfterAllLines(t *testing.T) {
	svc := NewService(t.TempDir())
	sess := &UpdateSession{
		ID:        "upd_seqtest000",
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		maxSubs:   4,
	}
	svc.mu.Lock()
	svc.sessions[sess.ID] = sess
	svc.mu.Unlock()
	for i := 0; i < 10; i++ {
		sess.ingestLine("line")
	}
	svc.finish(sess, 0, true)

	maxLineSeq := 0
	for _, ln := range sess.LogBuffer.Snapshot() {
		if ln.seq > maxLineSeq {
			maxLineSeq = ln.seq
		}
	}
	sess.mu.Lock()
	doneSeq := sess.doneEvent.Seq
	sess.mu.Unlock()
	if doneSeq <= maxLineSeq {
		t.Errorf("done seq %d must be > max line seq %d", doneSeq, maxLineSeq)
	}
	// done 后到达的迟到行必须被丢弃，防止其 seq 反超 done（回放时前端按 seq 去重会丢 done）
	sess.ingestLine("late line")
	if n := sess.LogBuffer.Snapshot(); len(n) != 10 {
		t.Errorf("late line was not dropped, buffer size = %d", len(n))
	}
}

func TestPIDFileHelpers(t *testing.T) {
	svc := NewService(t.TempDir())
	svc.logDir = t.TempDir()
	id := "upd_pidfile000"
	sess := &UpdateSession{
		ID:       id,
		logFile:  filepath.Join(svc.logDir, "lab-update-"+id+".log"),
		doneFile: filepath.Join(svc.logDir, "lab-update-"+id+".done"),
		pidFile:  filepath.Join(svc.logDir, "lab-update-"+id+".pid"),
	}
	if got := svc.readPIDFile(sess); got != 0 {
		t.Fatalf("readPIDFile on missing file = %d, want 0", got)
	}
	svc.writePIDFile(sess, 4242)
	if got := svc.readPIDFile(sess); got != 4242 {
		t.Fatalf("readPIDFile = %d, want 4242", got)
	}
	svc.removePIDFile(sess)
	if _, err := os.Stat(sess.pidFile); !os.IsNotExist(err) {
		t.Error("pid file not removed by removePIDFile")
	}
	if got := svc.readPIDFile(sess); got != 0 {
		t.Errorf("readPIDFile after remove = %d, want 0", got)
	}

	for _, p := range []string{sess.logFile, sess.doneFile, sess.pidFile} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	svc.removeSessionFiles(sess)
	for _, p := range []string{sess.logFile, sess.doneFile, sess.pidFile} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("file not removed by removeSessionFiles: %s", p)
		}
	}
}
