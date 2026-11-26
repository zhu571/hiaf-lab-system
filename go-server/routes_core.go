package main

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerCoreRoutes 注册 health/metrics、auth 挂载与 admin 用户/邀请码路由。
func registerCoreRoutes(r chi.Router, db *sql.DB, authHandler *auth.Handler) {
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})
	// /metrics 不经过 AuthRequired（运维探针可达；ServiceToken 白名单只拦
	// /api/v1/daily-reports/by-date 与 /api/v1/ask/execute，/metrics 不受影响）。
	r.Get("/metrics", middleware.MetricsHandler)

	r.Mount("/api/v1/auth", authHandler.Routes(middleware.Audit(db)))
	r.Route("/api/v1/admin/users", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin))
		r.Use(middleware.Audit(db))
		r.Get("/", authHandler.AdminListUsers)
		r.Post("/", authHandler.AdminCreateUser)
		r.Patch("/{id}", authHandler.AdminUpdateUser)
		r.Post("/{id}/reset-password", authHandler.AdminResetPassword)
	})
	r.Route("/api/v1/admin/invitation-codes", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", authHandler.AdminListInvitationCodes)
		r.Post("/", authHandler.AdminCreateInvitationCode)
		r.Post("/{id}/revoke", authHandler.AdminRevokeInvitationCode)
	})
}
