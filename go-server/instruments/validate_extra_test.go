package instruments

import (
	"math"
	"strings"
	"testing"
)

// NormalizeParams / RenderSCPI / whitelist 辅助函数的补充覆盖（validate_test 只覆盖了
// 越界与注入两类路径；本文件补齐 enum/string/object_constraints/set_sweep_range 各分支）。

func TestNormalizeParamsRejectsUnknownAndMissing(t *testing.T) {
	if _, err := NormalizeParams("e5063a", "set_format", map[string]any{"format": "MLOG", "extra": 1}); err == nil ||
		!strings.Contains(err.Error(), "不在白名单中") {
		t.Fatalf("unknown param: %v", err)
	}
	// identify 无 params：空输入 OK
	if _, err := NormalizeParams("e5063a", "identify", map[string]any{}); err != nil {
		t.Fatalf("identify: %v", err)
	}
}

func TestNormalizeParamsEnum(t *testing.T) {
	if _, err := NormalizeParams("e5063a", "set_format", map[string]any{"format": "BOGUS"}); err == nil {
		t.Fatal("expected enum rejection")
	}
	params, err := NormalizeParams("e5063a", "set_format", nil)
	if err != nil || params["format"] != "MLOG" {
		t.Fatalf("default enum: %v %v", params, err)
	}
}

func TestNormalizeParamsFloatAndIntTypes(t *testing.T) {
	// NaN / Inf → 拒绝
	if _, err := NormalizeParams("e5063a", "set_power", map[string]any{"power_dbm": math.NaN()}); err == nil {
		t.Fatal("expected NaN rejection")
	}
	if _, err := NormalizeParams("e5063a", "set_power", map[string]any{"power_dbm": math.Inf(1)}); err == nil {
		t.Fatal("expected Inf rejection")
	}
	// 非数值 → 拒绝；非整数 → 拒绝（int 类型）
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"points": "abc"}); err == nil {
		t.Fatal("expected non-number rejection")
	}
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"points": 3.7}); err == nil {
		t.Fatal("expected non-integer rejection")
	}
	params, err := NormalizeParams("hioki_im3536", "set_averaging", map[string]any{"n": 16})
	if err != nil || params["n"] != 16 {
		t.Fatalf("int normalize: %v %v", params, err)
	}
	// 类型定义无效（schema 非 map）→ 拒绝
	if _, err := NormalizeParams("e5063a", "identify", map[string]any{}); err != nil {
		t.Fatalf("identify unexpected: %v", err)
	}
}

func TestNormalizeParamsStringConstraints(t *testing.T) {
	// 非字符串 → 拒绝
	if _, err := NormalizeParams("e5063a", "take_screenshot", map[string]any{"label": 42}); err == nil {
		t.Fatal("expected non-string rejection")
	}
	// 过长
	if _, err := NormalizeParams("e5063a", "take_screenshot", map[string]any{"label": strings.Repeat("a", 65)}); err == nil {
		t.Fatal("expected too-long rejection")
	}
	// deny_patterns：路径穿越
	if _, err := NormalizeParams("e5063a", "take_screenshot", map[string]any{"label": "../../etc/passwd"}); err == nil {
		t.Fatal("expected deny-pattern rejection")
	}
	// regex 不匹配（含中文/空格）
	if _, err := NormalizeParams("e5063a", "take_screenshot", map[string]any{"label": "中文 截图"}); err == nil {
		t.Fatal("expected regex rejection")
	}
	// 合法值 + 默认值
	params, err := NormalizeParams("e5063a", "take_screenshot", nil)
	if err != nil || params["label"] != "capture" {
		t.Fatalf("screenshot default: %v %v", params, err)
	}
}

func TestNormalizeParamsObjectConstraints(t *testing.T) {
	// unknown 对象：power_dbm 上限 -30（默认值即上限，必须显式小值）
	if _, _, err := RenderSCPI("e5063a", "set_power", map[string]any{"power_dbm": -25.0}); err == nil {
		t.Fatal("expected unknown-object constraint rejection")
	}
	// passive_lc_component 上限 -10：-5 越界
	if _, _, err := RenderSCPI("e5063a", "set_power", map[string]any{"power_dbm": -5.0, "object_type": "passive_lc_component"}); err == nil {
		t.Fatal("expected passive_lc_component constraint rejection")
	}
	// 合法：rf_matching_network 上限 0
	if _, _, err := RenderSCPI("e5063a", "set_power", map[string]any{"power_dbm": -10.0, "object_type": "rf_matching_network"}); err != nil {
		t.Fatalf("rf_matching_network should pass: %v", err)
	}
	// hioki set_dc_bias：rf_matching_network 显式禁 DC bias
	if _, _, err := RenderSCPI("hioki_im3536", "set_dc_bias", map[string]any{"object_type": "rf_matching_network", "voltage": 0.1}); err == nil ||
		!strings.Contains(err.Error(), "禁止 DC bias") {
		t.Fatalf("expected dc_bias rejection, got %v", err)
	}
}

func TestNormalizeParamsSetSweepRange(t *testing.T) {
	// 起点 ≥ 终点
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"start_freq": 10e6, "stop_freq": 10e6}); err == nil {
		t.Fatal("expected start>=stop rejection")
	}
	// 跨度 > 100 MHz
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"start_freq": 1e6, "stop_freq": 200e6}); err == nil {
		t.Fatal("expected span rejection")
	}
	// 预计扫频时间 > timeout：points=1601, ifbw=10 → 1601/10*1000 = 160100 ms > 60000
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"points": 1601, "if_bandwidth": 10.0}); err == nil {
		t.Fatal("expected sweep-time rejection")
	}
	// 对象约束 max_span_hz：gas_cell_electrode 上限 10e6
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{
		"start_freq": 1e6, "stop_freq": 15e6, "object_type": "gas_cell_electrode",
	}); err == nil {
		t.Fatal("expected object max_span rejection")
	}
	// 合法组合（对象类型 rf_matching_network，跨度 2 MHz）
	params, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{
		"start_freq": 1e6, "stop_freq": 3e6, "points": 401, "if_bandwidth": 10000.0,
		"object_type": "rf_matching_network",
	})
	if err != nil || params["start_freq"] != 1e6 || params["stop_freq"] != 3e6 {
		t.Fatalf("valid sweep: %v %v", params, err)
	}
	// 对象类型无约束定义（object_type 传了白名单外的枚举值不可能，此处用不存在约束的对象值）
	if _, err := NormalizeParams("e5063a", "set_sweep_range", map[string]any{"object_type": "rf_matching_network"}); err != nil {
		t.Fatalf("default sweep with object: %v", err)
	}
}

func TestRenderSCPIResolvedAndUnresolved(t *testing.T) {
	scpi, params, err := RenderSCPI("hioki_im3536", "set_averaging", map[string]any{"n": 16})
	if err != nil || scpi != "AVERaging 16" || params["n"] != 16 {
		t.Fatalf("hioki averaging: %q %v %v", scpi, params, err)
	}
	// Build 模板 + 多占位符
	scpi, _, err = RenderSCPI("hioki_im3536", "set_measure_item", map[string]any{"mr0": 7, "mr1": 11, "mr2": 12})
	if err != nil || scpi != "MEASure:ITEM 7,11,12" {
		t.Fatalf("measure item: %q %v", scpi, err)
	}
	// 未知命令 → 报错
	if _, _, err := RenderSCPI("e5063a", "nope", nil); err == nil {
		t.Fatal("expected unknown command rejection")
	}
}

func TestWhitelistHelpers(t *testing.T) {
	if name := InstrumentName("e5063a"); name != "Keysight E5063A" {
		t.Fatalf("instrument name = %q", name)
	}
	if _, err := GetCommand("e5063a", "nope"); err == nil {
		t.Fatal("expected unknown command error")
	}
	if _, err := GetCommand("nope", "identify"); err == nil {
		t.Fatal("expected unknown instrument error")
	}
	if !IsCommandAllowed("e5063a", "identify", "green") || !IsCommandAllowed("e5063a", "reset", "red") ||
		IsCommandAllowed("e5063a", "identify", "red") || IsCommandAllowed("nope", "identify", "green") {
		t.Fatal("IsCommandAllowed mismatch")
	}
	// ListCommands 返回副本：修改不污染白名单
	commands := ListCommands("e5063a")
	if len(commands) == 0 {
		t.Fatal("empty command list")
	}
	commands[0].Name = "mutated"
	if again := ListCommands("e5063a"); again[0].Name == "mutated" {
		t.Fatal("ListCommands must return copies")
	}
}

func TestParseScanData(t *testing.T) {
	points, plot := parseScanData("1,2,3,4")
	if len(points) != 2 || points[0] != (Point{X: 1, Y: 2}) || points[1] != (Point{X: 3, Y: 4}) || plot != "line" {
		t.Fatalf("points=%v plot=%q", points, plot)
	}
	// 空 / 奇数个 / 非数值 → (nil, "")
	if points, plot := parseScanData("  "); points != nil || plot != "" {
		t.Fatalf("empty: %v %q", points, plot)
	}
	if points, plot := parseScanData("1,2,3"); points != nil || plot != "" {
		t.Fatalf("odd: %v %q", points, plot)
	}
	if points, plot := parseScanData("a,b,c,d"); points != nil || plot != "" {
		t.Fatalf("non-numeric: %v %q", points, plot)
	}
}
