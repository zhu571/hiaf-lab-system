package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/automation"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAutomationRoutes 注册自动化规则（admin only，审计+幂等）。
func registerAutomationRoutes(r chi.Router, db *sql.DB, automationHandler *automation.Handler) {
	r.Route("/api/v1/admin/automation", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Mount("/", automationHandler.Routes())
	})
}
