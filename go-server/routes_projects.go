package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/rfmatch"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/scans"
	"github.com/zhu571/hiaf-lab-system/go-server/testdata"
)

// registerProjectRoutes 注册项目列表/创建与项目工作区子资源（详情/成员/日志/问题/批次/装配/数据/扫描）。
func registerProjectRoutes(
	r chi.Router,
	db *sql.DB,
	projectsHandler *projects.Handler,
	issuesHandler *issues.Handler,
	logsHandler *logs.Handler,
	runsHandler *runs.Handler,
	assemblyHandler *assembly.Handler,
	testDataHandler *testdata.Handler,
	testDataCurveHandler *testdata.CurveHandler,
	rfMatchingHandler *rfmatch.Handler,
	scansHandler *scans.Handler,
) {
	r.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Use(middleware.AgentContext(db))
		r.Use(middleware.Audit(db))
		r.Use(middleware.RequireIdempotencyKey(db))
		r.Get("/", projectsHandler.List)
		// D11：项目创建限 maintainer+admin（viewer 不可），service 内同语义校验纵深。
		r.With(middleware.RequireRole(auth.RoleAdmin, auth.RoleMaintainer)).Post("/", projectsHandler.Create)

		r.Route("/{id}", func(r chi.Router) {
			r.Use(middleware.RequireProjectPermission(db, middleware.PermRead))
			r.Get("/", projectsHandler.GetByID)
			r.Get("/members", projectsHandler.ListMembers)
			r.Get("/issues", issuesHandler.List)
			r.Get("/logs", logsHandler.ListLogs)
			r.With(middleware.RequireProjectPermission(db, middleware.PermReadTeamReports)).Get("/daily-reports", logsHandler.ListTeamReports)
			r.With(middleware.RequireProjectPermission(db, middleware.PermReadTeamReports)).Get("/daily-reports/{report_id}", logsHandler.GetTeamReport)
			r.Get("/experiment-runs", runsHandler.List)
			r.Post("/experiment-runs", runsHandler.Create)
			r.Get("/assembly", assemblyHandler.List)
			r.Post("/assembly", assemblyHandler.Create)
			r.Post("/assembly/apply-template", assemblyHandler.ApplyTemplate)
			r.Get("/test-data", testDataHandler.List)
			r.Post("/test-data", testDataHandler.Create)
			r.Post("/test-data/batch", testDataHandler.CreateBatch)
			r.Get("/test-data-curves", testDataCurveHandler.List)
			r.Post("/test-data-curves", testDataCurveHandler.Create)
			r.Get("/rf-matching", rfMatchingHandler.List)
			r.Post("/rf-matching", rfMatchingHandler.Create)
			registerProjectScanRoutes(r, scansHandler)
			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermManageProject))
				r.Patch("/", projectsHandler.Update)
				r.Post("/transition", projectsHandler.TransitionStatus)
			})

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermManageMembers))
				r.Post("/members", projectsHandler.AddMember)
				r.Patch("/members/{userID}", projectsHandler.UpdateMemberRole)
				r.Delete("/members/{userID}", projectsHandler.RemoveMember)
			})

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermCreateLog))
				r.Post("/logs", logsHandler.CreateLog)
			})

			r.Group(func(r chi.Router) {
				r.Use(middleware.RequireProjectPermission(db, middleware.PermCreateIssue))
				r.Post("/issues", issuesHandler.Create)
			})
		})
	})
}
