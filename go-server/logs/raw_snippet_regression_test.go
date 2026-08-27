package logs

// 真实场景回归验证（2026-08-20）：08-19 日报误报样本
// 原文 vs AI 改写日志 + raw_snippet，验证 rawTextHasMatchingLog 严格模式不再误报。

import (
	"testing"
)

func TestRealReport_20260819_NoFalsePositive(t *testing.T) {
	// 08-19 真实原文（截取核心部分）
	rawText := "今天拆掉了外腔，测试了一下qpig两个rf之间的电阻是4.4M欧姆，rf与qpigdc-之间的电阻是2.2M欧姆，电阻没有问题。测量了qpigdc-与地之间的四个电容，发现四个对地电容之间有两个电容坏掉了。"

	// AI 整理的日志（改写版 content）+ 逐字原文分段 raw_snippet
	snip1 := "今天拆掉了外腔，测试了一下qpig两个rf之间的电阻是4.4M欧姆，rf与qpigdc-之间的电阻是2.2M欧姆，电阻没有问题"
	snip2 := "测量了qpigdc-与地之间的四个电容，发现四个对地电容之间有两个电容坏掉了"

	items := []Log{
		{Content: "拆掉了外腔，测试了q-pig两个rf之间的电阻为4.4M欧姆，rf与qpigdc-之间的电阻为2.2M欧姆，电阻没有问题", RawSnippet: &snip1},
		{Content: "测量了qpigdc-与地之间的四个电容，发现四个对地电容中有两个电容坏掉了", RawSnippet: &snip2},
	}

	if !rawTextHasMatchingLog(rawText, items) {
		t.Fatalf("严格模式误报：原文全部被 snippet 覆盖，不应触发 raw_text_without_matching_log\n原文: %s", rawText)
	}
}

func TestRawSnippetAllowsMinorRewordingOnly(t *testing.T) {
	rawText := "测试了一下qpig两个rf之间的电阻是4.4M欧姆。"
	if !containsRawTextSegment(rawText, "测试了q-pig两个rf之间的电阻为4.4M欧姆") {
		t.Fatal("minor AI rewording should match its source segment")
	}
	for _, snippet := range []string{"电阻是4.4M欧姆", "今天完成了低温系统检漏"} {
		if containsRawTextSegment(rawText, snippet) {
			t.Fatalf("untraceable snippet matched source: %q", snippet)
		}
	}
}

func TestRealReport_20260819_MissingSegment(t *testing.T) {
	// 原文 2 段，AI 只覆盖 1 段 → 应触发警告（漏内容检测）
	rawText := "今天拆掉了外腔，测试了一下qpig两个rf之间的电阻是4.4M欧姆。测量了qpigdc-与地之间的四个电容。"

	snip1 := "今天拆掉了外腔，测试了一下qpig两个rf之间的电阻是4.4M欧姆"

	items := []Log{
		{Content: "拆掉了外腔，测试了q-pig电阻", RawSnippet: &snip1},
	}

	if rawTextHasMatchingLog(rawText, items) {
		t.Fatalf("漏段检测失败：第二段未被覆盖，应返回 false")
	}
}

func TestRealReport_DuplicateSegment(t *testing.T) {
	// 重复分段按次数覆盖
	rawText := "完成测试。完成测试。"
	snip := "完成测试"
	items := []Log{
		{Content: "测试1", RawSnippet: &snip},
		{Content: "测试2", RawSnippet: &snip},
	}
	if !rawTextHasMatchingLog(rawText, items) {
		t.Fatalf("重复分段 2 次 + 2 条 snippet 应通过")
	}
	oneItem := []Log{{Content: "测试1", RawSnippet: &snip}}
	if rawTextHasMatchingLog(rawText, oneItem) {
		t.Fatalf("重复分段 2 次 + 1 条 snippet 应失败")
	}
}

func TestRealReport_StrictWithNil(t *testing.T) {
	// 严格数据混入 nil（手工日志）不降级
	rawText := "完成A。完成B。"
	snipA := "完成A"
	items := []Log{
		{Content: "AI日志A", RawSnippet: &snipA},
		{Content: "手工日志B"}, // nil snippet
	}
	if rawTextHasMatchingLog(rawText, items) {
		t.Fatalf("严格模式混入 nil 不应关闭严格检查：B 段未被 snippet 覆盖")
	}
}

func TestRealReport_AllNilFallback(t *testing.T) {
	// 全 nil（历史数据）走旧逻辑
	rawText := "今天拆掉了外腔，测试了一下qpig电阻"
	items := []Log{
		{Content: "拆掉了外腔"},
		{Content: "测试了qpig电阻"},
	}
	if !rawTextHasMatchingLog(rawText, items) {
		t.Fatalf("全 nil 旧逻辑：任一 content 子串命中应通过")
	}
	noMatch := []Log{{Content: "完全无关的内容"}}
	if rawTextHasMatchingLog(rawText, noMatch) {
		t.Fatalf("全 nil 旧逻辑：全部不命中应失败")
	}
}
