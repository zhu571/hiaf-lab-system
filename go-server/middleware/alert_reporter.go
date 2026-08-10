package middleware

import (
	"context"
	"log/slog"
)

// middleware 内的告警触发点（agent.go 缺 acting_user_id、service_token.go 校验失败）
// 经此注入点上报告警中心。middleware 不跨模块直连 alert 模块（避免 import 环），
// 由 main.go 在构造期注入 alertSvc.Report——与 SetAgentQueueProvider 注入先例一致。
var alertReporter func(ctx context.Context, level, source, title, detail string) error

// SetAlertReporter 注入告警上报器（main.go 装配 alertSvc.Report）。
func SetAlertReporter(fn func(ctx context.Context, level, source, title, detail string) error) {
	alertReporter = fn
}

// ReportAlert 异步上报告警（触发点用；未注入或失败仅记日志，不影响业务路径）。
func ReportAlert(level, source, title, detail string) {
	if alertReporter == nil {
		return
	}
	go func() {
		if err := alertReporter(context.Background(), level, source, title, detail); err != nil {
			slog.Error("alert report failed", "error", err, "source", source, "title", title)
		}
	}()
}
