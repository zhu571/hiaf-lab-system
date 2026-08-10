// Package alert 实现告警中心模块（方案 2026-08-09_alert-center）：
// 统一上报（Report）、聚合去重（10min 窗口 + 部分唯一索引防双发）、
// 状态管理（active/resolved + TTL 兜底 + 90 天滚动清理）、历史可查（List/Get）。
// 发送经 main.go 构造期注入的窄接口 Sender（不直接 import notify），保持模块可测。
package alert

import (
	"context"
	"errors"
	"time"
)

// 常量定义（与迁移 035 CHECK 约束对齐）。
const (
	// Topic 是告警中心统一 ntfy 主题（与 notify.AlertTopic 同值；
	// alert 模块不 import notify，经 main.go 注入的 Sender 桥接）。
	Topic = "lab-alerts"

	LevelInfo     = "info"
	LevelWarning  = "warning"
	LevelError    = "error"
	LevelCritical = "critical"

	SourceSecurity    = "security"
	SourceInstruments = "instruments"
	SourceTodos       = "todos"
	SourceUpdater     = "updater"
	SourceAgent       = "agent"
	SourceIOC         = "ioc"
	SourceWatchdog    = "watchdog"

	StatusActive   = "active"
	StatusResolved = "resolved"

	// ResolvedBySystem 内部恢复上报的 resolve 操作者；ResolvedByTTL 为 TTL 兜底。
	ResolvedBySystem = "system"
	ResolvedByTTL    = "ttl"

	// dedupWindow 是聚合去重窗口：last_seen 距今 ≤10min 合并计数、不重发 ntfy。
	dedupWindow = 10 * time.Minute
	// ttlAge 是 TTL 兜底：active 且 last_seen 超过 24h → 置 resolved。
	ttlAge = 24 * time.Hour
	// cleanupAge 是滚动清理：resolved 且 resolved_at 超过 90 天 → DELETE。
	cleanupAge = 90 * 24 * time.Hour

	// 字段级校验（防洪，方案 §4）。
	maxTitleLen  = 256
	maxDetailLen = 2000

	// maxListLimit 是列表 limit 上限（默认 50）。
	maxListLimit = 200
)

var (
	// ErrNotFound 查询不到告警记录（Get/List 用）。
	ErrNotFound = errors.New("告警记录不存在")
	// ErrInvalidInput 请求参数非法（level/source 枚举、长度、二选一校验）。
	ErrInvalidInput = errors.New("请求参数无效")
)

// validLevels / validSources / validStatuses 与迁移 035 CHECK 约束一一对应。
var (
	validLevels = map[string]bool{
		LevelInfo: true, LevelWarning: true, LevelError: true, LevelCritical: true,
	}
	validSources = map[string]bool{
		SourceSecurity: true, SourceInstruments: true, SourceTodos: true,
		SourceUpdater: true, SourceAgent: true, SourceIOC: true, SourceWatchdog: true,
	}
	validStatuses = map[string]bool{StatusActive: true, StatusResolved: true}
)

// Alert 是 alerts 表的一行记录（List/Get 响应字段，方案 §7）。
type Alert struct {
	ID              string     `json:"id"`
	Level           string     `json:"level"`
	Source          string     `json:"source"`
	Title           string     `json:"title"`
	Detail          string     `json:"detail"`
	Status          string     `json:"status"`
	OccurrenceCount int        `json:"occurrence_count"`
	FirstSeen       time.Time  `json:"first_seen"`
	LastSeen        time.Time  `json:"last_seen"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	ResolvedBy      string     `json:"resolved_by"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ReportResult 是 POST /alerts/report 的响应 data。
// Deduplicated=true 表示窗口内合并（未发 ntfy），供调用方与端到端测试断言。
type ReportResult struct {
	AlertID         string `json:"alert_id"`
	Deduplicated    bool   `json:"deduplicated"`
	OccurrenceCount int    `json:"occurrence_count"`
}

// ReportRequest 是 POST /alerts/report 的请求体。
type ReportRequest struct {
	Level  string `json:"level"`
	Source string `json:"source"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// ResolveRequest 是 POST /alerts/resolve 的请求体（二选一：id 或 source+title）。
type ResolveRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Title  string `json:"title"`
}

// Sender 是 alert 模块的发送窄接口（main.go 构造期注入 notify 适配器，
// 不直接 import notify，保持可测性——先例：todos 的 ntfy 注入惯例）。
// Send 仅发 ntfy；SendBoth 走 ntfy + MeoW 双通道（critical/error，等价现状
// notify.SecurityAlert / InstrumentEmergency 的 sendBoth，不降低送达率）。
type Sender interface {
	Send(topic, title, msg, clickURL, priority string, tags []string) error
	SendBoth(topic, title, msg, clickURL, priority string, tags []string) error
}

// Reporter 是其他模块（middleware/auth/instruments）构造期注入的告警窄接口：
// Report 上报一条告警（调用方可忽略返回）；ResolveBySource 按 source+title
// 幂等解除 active 告警（内部恢复上报，如仪器重连成功）。
type Reporter interface {
	Report(ctx context.Context, level, source, title, detail string) (*ReportResult, error)
	ResolveBySource(ctx context.Context, source, title, resolvedBy string) error
}
