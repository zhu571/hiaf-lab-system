package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestStepTitlesMatchStepRe 断言 I2 契约：7 步标题逐条能被 stepRe 解析，
// 且 步骤号/总数 与标题匹配，防止前端按步骤渲染被破坏。
func TestStepTitlesMatchStepRe(t *testing.T) {
	p := NewPipeline(&UpdateConfig{}, nil, &Logger{})
	steps := p.steps()
	if len(steps) != 7 {
		t.Fatalf("steps = %d, want 7", len(steps))
	}
	for i, st := range steps {
		if st.Num != i+1 {
			t.Errorf("step %d Num = %d", i, st.Num)
		}
		if st.Title != stepTitles[i] {
			t.Errorf("step %d title = %q, want %q", i, st.Title, stepTitles[i])
		}
		header := fmt.Sprintf("[UPDATE] ===== 步骤 %d/%d：%s =====", st.Num, len(steps), st.Title)
		m := stepRe.FindStringSubmatch(header)
		if m == nil {
			t.Errorf("stepRe 未匹配标题: %q", header)
			continue
		}
		if m[1] != strconv.Itoa(st.Num) || m[2] != strconv.Itoa(len(steps)) || m[3] != st.Title {
			t.Errorf("stepRe 解析不一致: %v", m)
		}
	}
}

// TestDoneMarkerReadableByFinishFromMarker 断言 I3 契约：Go 侧写的 marker 能被 finishFromMarker 解析。
func TestDoneMarkerReadableByFinishFromMarker(t *testing.T) {
	svc := NewService(t.TempDir())
	dir := t.TempDir()
	id := "upd_contract00"
	sess := &UpdateSession{
		ID:        id,
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		doneFile:  filepath.Join(dir, id+".done"),
		maxSubs:   4,
	}
	if err := WriteDoneMarker(sess.doneFile, DoneMarker{ExitCode: 0, OldSHA: "old", NewSHA: "new", EndedAt: nowUTC()}); err != nil {
		t.Fatalf("WriteDoneMarker: %v", err)
	}
	svc.finishFromMarker(sess)

	sess.mu.Lock()
	status, exitCode, oldSHA, newSHA := sess.Status, sess.ExitCode, sess.OldSHA, sess.NewSHA
	sess.mu.Unlock()
	if status != "done" || exitCode != 0 || oldSHA != "old" || newSHA != "new" {
		t.Errorf("finishFromMarker 结果异常: status=%q code=%d old=%q new=%q", status, exitCode, oldSHA, newSHA)
	}
}

// TestDoneMarkerCorruptedByFinishFromMarker 损坏 marker → 判失败。
func TestDoneMarkerCorruptedByFinishFromMarker(t *testing.T) {
	svc := NewService(t.TempDir())
	dir := t.TempDir()
	id := "upd_contract01"
	sess := &UpdateSession{
		ID:        id,
		Status:    "running",
		LogBuffer: NewRingBuffer(ringBufferCap),
		subs:      make(map[chan SSEEvent]struct{}),
		done:      make(chan struct{}),
		doneFile:  filepath.Join(dir, id+".done"),
		maxSubs:   4,
	}
	if err := os.WriteFile(sess.doneFile, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	svc.finishFromMarker(sess)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.Status != "done" || sess.ExitCode != -1 {
		t.Errorf("损坏 marker 应判失败: status=%q code=%d", sess.Status, sess.ExitCode)
	}
}

// TestMarkerJSONShape 断言 marker 的 JSON 字段形状与历史兼容（finish 的 writeMarker 也复用该形状）。
func TestMarkerJSONShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.done")
	if err := WriteDoneMarker(path, DoneMarker{ExitCode: 0, OldSHA: "a", NewSHA: "b", EndedAt: "2026-08-01T00:00:00Z"}); err != nil {
		t.Fatalf("WriteDoneMarker: %v", err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("marker 非 JSON: %v", err)
	}
	if raw["exit_code"] != float64(0) || raw["old_sha"] != "a" || raw["new_sha"] != "b" || raw["ended_at"] != "2026-08-01T00:00:00Z" {
		t.Errorf("marker 字段不匹配: %v", raw)
	}
}
