package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/weekly"
)

// registerWeeklyRoutes 注册周报手动生成（maintainer+，审计+幂等，360s 超时）。
func registerWeeklyRoutes(r chi.Router, db *sql.DB, weeklyHandler *weekly.Handler) {
	r.Route("/api/v1/weekly", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.Audit(db))
		r.With(middleware.RequireRole(auth.RoleAdmin, auth.RoleMaintainer),
			middleware.Timeout(360*time.Second),
			middleware.RequireIdempotencyKey(db)).Post("/summary", weeklyHandler.Summary)
	})
}
