package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/alert"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAlertRoutes 注册告警 report/resolve（内部/维护者）与 list/detail。
func registerAlertRoutes(r chi.Router, db *sql.DB, alertHandler *alert.Handler) {
	r.Route("/api/v1/alerts", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.With(middleware.RequireIdempotencyKey(db)).Post("/report", alertHandler.Report)
		r.With(middleware.RequireRoleOrService(auth.RoleAdmin, auth.RoleMaintainer),
			middleware.RequireIdempotencyKey(db)).Post("/resolve", alertHandler.Resolve)
		r.Get("/", alertHandler.List)
		r.Get("/{id}", alertHandler.Get)
	})
}
