package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/electrical"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/sensors"
)

// registerMonitoringRoutes 注册传感器/电器监控读接口与单次采集。
func registerMonitoringRoutes(r chi.Router, db *sql.DB, sensorsHandler *sensors.Handler, electricalHandler *electrical.Handler) {
	r.Route("/api/v1/sensors", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Get("/latest", sensorsHandler.Latest)
		r.Get("/history", sensorsHandler.History)
	})
	r.Route("/api/v1/electrical", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/latest", electricalHandler.Latest)
		r.Get("/history", electricalHandler.History)
		r.Post("/single-shot", electricalHandler.SingleShot)
	})
}
