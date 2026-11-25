package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/rfmatch"
	"github.com/zhu571/hiaf-lab-system/go-server/testdata"
)

// registerAssemblyRoutes 注册装配步骤列表/重排/详情/更新/删除。
func registerAssemblyRoutes(r chi.Router, db *sql.DB, assemblyHandler *assembly.Handler) {
	r.Route("/api/v1/assembly", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Post("/reorder", assemblyHandler.Reorder)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", assemblyHandler.GetByID)
			r.Patch("/", assemblyHandler.Update)
			r.Delete("/", assemblyHandler.SoftDelete)
		})
	})
}

// registerTestDataRoutes 注册测试数据与曲线详情/更新/作废。
func registerTestDataRoutes(r chi.Router, db *sql.DB, testDataHandler *testdata.Handler, curveHandler *testdata.CurveHandler) {
	r.Route("/api/v1/test-data", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", testDataHandler.GetByID)
			r.Patch("/", testDataHandler.Update)
			r.Delete("/", testDataHandler.MarkInvalid)
		})
	})
	r.Route("/api/v1/test-data-curves", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", curveHandler.GetByID)
			r.Patch("/", curveHandler.Update)
			r.Delete("/", curveHandler.Void)
		})
	})
}

// registerRFMatchingRoutes 注册 RF 匹配记录详情/更新/作废。
func registerRFMatchingRoutes(r chi.Router, db *sql.DB, rfMatchingHandler *rfmatch.Handler) {
	r.Route("/api/v1/rf-matching", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", rfMatchingHandler.GetByID)
			r.Patch("/", rfMatchingHandler.Update)
			r.Delete("/", rfMatchingHandler.MarkVoid)
		})
	})
}
