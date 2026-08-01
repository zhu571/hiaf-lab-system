package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoggerLinef 验证行流写入：每行以换行结尾，文件与 tee 都收到。
func TestLoggerLinef(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "session.log")
	var tee strings.Builder

	l, err := NewLogger(logPath, &tee)
	if err != nil {
		t.Fatalf("NewLogger: %v", err)
	}
	l.Linef("[UPDATE] hello %s", "world")
	l.Linef("===== 步骤 1/7：预检 =====")
	l.Close()

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("log must end with newline for tailSessionLog 行流可见")
	}
	if !strings.Contains(string(data), "[UPDATE] hello world") {
		t.Errorf("log missing line: %q", data)
	}
	if !strings.Contains(string(data), "===== 步骤 1/7：预检 =====") {
		t.Errorf("log missing step header: %q", data)
	}
	if !strings.Contains(tee.String(), "===== 步骤 1/7：预检 =====") {
		t.Errorf("tee missing step header: %q", tee.String())
	}
}

// TestWriteDoneMarkerContract 验证 marker 与 I3 契约：finishFromMarker/recoverFromDisk 可解析。
func TestWriteDoneMarkerContract(t *testing.T) {
	dir := t.TempDir()
	markerPath := filepath.Join(dir, "session.done")

	m := DoneMarker{ExitCode: 0, OldSHA: "aaa", NewSHA: "bbb", EndedAt: nowUTC()}
	if err := WriteDoneMarker(markerPath, m); err != nil {
		t.Fatalf("WriteDoneMarker: %v", err)
	}

	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("marker 不是合法 JSON: %v", err)
	}
	for _, k := range []string{"exit_code", "old_sha", "new_sha", "ended_at"} {
		if _, ok := parsed[k]; !ok {
			t.Errorf("marker 缺字段 %s: %v", k, parsed)
		}
	}

	// 二次写入幂等（覆盖式，不追加）
	m2 := DoneMarker{ExitCode: 1, OldSHA: "aaa", NewSHA: "ccc", EndedAt: nowUTC()}
	if err := WriteDoneMarker(markerPath, m2); err != nil {
		t.Fatalf("WriteDoneMarker(2nd): %v", err)
	}
	data2, _ := os.ReadFile(markerPath)
	var parsed2 map[string]any
	_ = json.Unmarshal(data2, &parsed2)
	if parsed2["exit_code"] != float64(1) {
		t.Errorf("marker 二次写入非幂等覆盖: %v", parsed2)
	}
}

// TestNowUTC 验证 ended_at 为 RFC3339（与 finishFromMarker 的 time.Parse(RFC3339) 兼容）。
func TestNowUTC(t *testing.T) {
	ts := nowUTC()
	if !strings.HasSuffix(ts, "Z") && !strings.Contains(ts, "+") {
		t.Errorf("ended_at 应为带时区的 RFC3339: %q", ts)
	}
}
