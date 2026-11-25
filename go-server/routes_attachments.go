package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/attachments"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// registerAttachmentRoutes 注册附件列表/上传/详情/下载/链接/软删除。
func registerAttachmentRoutes(r chi.Router, db *sql.DB, attachmentsHandler *attachments.Handler) {
	r.Route("/api/v1/attachments", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", attachmentsHandler.List)
		r.Post("/", attachmentsHandler.Upload)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", attachmentsHandler.GetByID)
			r.Get("/content", attachmentsHandler.Download)
			r.Post("/links", attachmentsHandler.AddLink)
			r.Delete("/links/{link_id}", attachmentsHandler.RemoveLink)
			r.Delete("/", attachmentsHandler.SoftDelete)
		})
	})
}
