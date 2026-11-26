package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerGasCellWSRoute 注册 gascell WebSocket 实时流。
func registerGasCellWSRoute(r chi.Router, instrumentsHandler *instruments.Handler) {
	r.Route("/api/v1/ws", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Get("/gascell", instrumentsHandler.GasCellStream)
	})
}
