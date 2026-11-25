package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAgentRoutes 注册 agent 任务 claim/complete/fail/renew 与候选审核路由。
func registerAgentRoutes(r chi.Router, db *sql.DB, agentHandler *agent.Handler) {
	r.Route("/api/v1/agent/tasks", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAgent))
		r.With(middleware.Audit(db)).Post("/claim", agentHandler.Claim)
		r.Route("/{id}", func(r chi.Router) {
			r.Use(middleware.QueueTaskContext(db))
			r.Use(middleware.Audit(db))
			r.Post("/complete", agentHandler.Complete)
			r.Post("/fail", agentHandler.Fail)
			// R8：租约续约——worker 在前置 HTTP 链 / LLM 长耗时阶段周期调用，
			// 与 complete/fail 同套 claim_token 所有权校验（service 层）。
			r.Post("/renew", agentHandler.Renew)
		})
	})
	r.Route("/api/v1/agent/candidates", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.RequireRole(auth.RoleAdmin, auth.RoleMaintainer))
		r.Use(middleware.Audit(db))
		r.Get("/", agentHandler.ListCandidates)
		r.Get("/{id}/trace", agentHandler.TraceCandidate)
		r.Post("/{id}/approve", agentHandler.ApproveCandidate)
		r.Post("/{id}/reject", agentHandler.RejectCandidate)
	})
}
