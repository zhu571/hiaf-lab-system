package system

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetectServicesGolden 以 testdata/detect_golden.txt 为唯一事实源跑 detect 矩阵。
func TestDetectServicesGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "detect_golden.txt"))
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Fatalf("golden line %d: expected 'paths<TAB>expected', got %q", lineNo, line)
		}
		var changed []string
		if parts[0] != "" {
			changed = strings.Split(parts[0], ",")
		}
		want := parts[1]

		got := DetectServices(changed)
		if got.String() != want {
			t.Errorf("golden line %d DetectServices(%v) = %q, want %q", lineNo, changed, got.String(), want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan golden: %v", err)
	}
}

// TestDetectServicesEmpty 空变更列表 → none。
func TestDetectServicesEmpty(t *testing.T) {
	if got := DetectServices(nil); !got.IsNone() || got.String() != "none" {
		t.Errorf("DetectServices(nil) = %v, want none", got)
	}
}

// TestAffectedHas 验证 All 视为影响全部、Has 命中、IsNone。
func TestAffectedHas(t *testing.T) {
	a := Affected{All: true}
	if !a.Has("migrate") || !a.Has("server") || a.IsNone() {
		t.Errorf("All 应影响全部服务: %+v", a)
	}
	b := Affected{Services: []string{"server"}}
	if b.Has("server") != true || b.Has("migrate") || b.IsNone() {
		t.Errorf("Has/IsNone 语义错误: %+v", b)
	}
	if b.String() != "server" {
		t.Errorf("String = %q, want server", b.String())
	}
}
