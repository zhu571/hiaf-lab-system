package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		rb.Append(strings.Repeat("x", 1))
	}
	snap := rb.Snapshot()
	if len(snap) != 5 {
		t.Fatalf("expected 5 buffered lines, got %d", len(snap))
	}
	for i, line := range snap {
		if line != "x" {
			t.Errorf("line %d corrupted", i)
		}
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
		ID:        "upd_httptest",
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		logFile:   t.TempDir() + "/lab-update-upd_httptest.log",
		doneFile:  t.TempDir() + "/lab-update-upd_httptest.done",
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
