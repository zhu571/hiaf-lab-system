package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/scans"
)

// registerProjectScanRoutes 注册项目下的 scans 列表/标签/上传入口。
func registerProjectScanRoutes(r chi.Router, scansHandler *scans.Handler) {
	r.Get("/scans", scansHandler.List)
	r.Get("/scans/tags", scansHandler.Tags)
	r.Post("/scans", scansHandler.Upload)
}

// registerScanRoutes 注册 /api/v1/scans 详情/文件/ingest/说明/作废。
func registerScanRoutes(r chi.Router, db *sql.DB, scansHandler *scans.Handler) {
	r.Route("/api/v1/scans", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Get("/{id}", scansHandler.GetByID)
		r.Post("/ingest", scansHandler.Ingest)
		r.Get("/{id}/file/{kind}", scansHandler.File)
		r.Get("/{id}/thumb", scansHandler.Thumb)
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireIdempotencyKey(db))
			r.Patch("/{id}", scansHandler.Update)
			r.Post("/{id}/void", scansHandler.Void)
		})
	})
}
