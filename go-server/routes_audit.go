package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAuditRoutes 注册审计链校验/事件列表/按 request_id 查询。
func registerAuditRoutes(r chi.Router, auditHandler *audit.Handler) {
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		// /verify 与 /events 必须先于 /{request_id} 注册（chi 按注册序匹配，静态段否则会被吞）。
		r.Get("/verify", auditHandler.VerifyChain)
		r.Get("/events", auditHandler.ListEvents)
		r.Get("/{request_id}", auditHandler.GetByRequestID)
	})
}
