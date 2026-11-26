package main

import (
	"database/sql"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerInstrumentRoutes 注册仪器状态/白名单/gascell/流程/租约/审批/压电等路由。
func registerInstrumentRoutes(r chi.Router, db *sql.DB, instrumentsHandler *instruments.Handler, flowEnabled bool) {
	r.Route("/api/v1/instruments", func(r chi.Router) {
		// parse-result 是只读解析接口：与 ListInstruments 同级鉴权，但不需要 Idempotency-Key。
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired)
			r.Use(middleware.AgentContext(db))
			r.Use(middleware.Audit(db))
			r.Post("/{id}/parse-result", instrumentsHandler.ParseResult)
		})
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired)
			r.Use(middleware.AgentContext(db))
			r.Use(middleware.Audit(db))
			r.Use(middleware.RequireIdempotencyKey(db))
			r.Get("/", instrumentsHandler.ListInstruments)
			r.Get("/whitelist", instrumentsHandler.GetWhitelist)
			r.Get("/gascell/status", instrumentsHandler.GasCellStatus)
			r.Route("/gascell", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin))
					r.Post("/params", instrumentsHandler.GasCellParams)
					r.Post("/start", instrumentsHandler.GasCellStart)
					r.Post("/stop", instrumentsHandler.GasCellStop)
					r.Post("/valve", instrumentsHandler.GasCellValve)
					r.Put("/safety/a5-max", instrumentsHandler.GasCellA5Max)
					r.Post("/safety/a5-clear", instrumentsHandler.GasCellA5Clear)
				})
			})
			r.Get("/{id}/status", instrumentsHandler.InstrumentStatus)
			r.Get("/{id}/flows/{flow_id}", instrumentsHandler.GetFlow)
			r.With(middleware.Timeout(90*time.Second)).Post("/{id}/nl-commands", instrumentsHandler.InterpretCommand)
			r.Post("/{id}/nl-execute", instrumentsHandler.NLExecute)
			r.Post("/{id}/emergency-stop", instrumentsHandler.EmergencyStop)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/manual-check", instrumentsHandler.ConfirmManualCheck)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases", instrumentsHandler.CreateLease)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases/{lease_id}/renew", instrumentsHandler.RenewLease)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases/{lease_id}/release", instrumentsHandler.ReleaseLease)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/approvals", instrumentsHandler.RequestApproval)
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/approvals/{approval_id}/approve", instrumentsHandler.ApproveCommand)
			if flowEnabled {
				r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows", instrumentsHandler.CreateFlow)
				r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows/{flow_id}/approve", instrumentsHandler.ApproveFlow)
				r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows/{flow_id}/stop", instrumentsHandler.StopFlow)
			}
			r.With(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/commands", instrumentsHandler.ExecuteCommand)
			r.Route("/piezo", func(r chi.Router) {
				r.Get("/status", instrumentsHandler.PiezoStatus)
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireRole(auth.RoleMaintainer, auth.RoleAdmin))
					r.Post("/start", instrumentsHandler.PiezoStart)
					r.Post("/stop", instrumentsHandler.PiezoStop)
					r.Post("/setpoint", instrumentsHandler.PiezoSetpoint)
				})
			})
		})
	})
}
