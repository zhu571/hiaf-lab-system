package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	jwtSecret, err := common.ReadSecret("/run/secrets/jwt_key", "JWT_SECRET")
	if err != nil {
		slog.Error("failed to read jwt secret", "error", err)
		os.Exit(1)
	}
	mw.SetJWTSecret([]byte(jwtSecret))

	db, err := common.OpenDB()
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	authRepo := auth.NewRepository(db)
	mw.TokenVersionValidator = func(userID string, version int) bool {
		user, err := authRepo.GetByID(userID)
		if err != nil || user == nil {
			return false
		}
		return user.TokenVersion == version
	}
	authSvc := auth.NewService(authRepo, []byte(jwtSecret))
	authHandler := auth.NewHandler(authSvc)
	projectsRepo := projects.NewRepository(db)
	projectsSvc := projects.NewService(projectsRepo)
	projectsHandler := projects.NewHandler(projectsSvc)
	logsRepo := logs.NewRepository(db)
	logsSvc := logs.NewService(logsRepo, commonEnv("APP_TIMEZONE", "Asia/Shanghai"), logs.ProjectAccessAdapter{Repo: projectsRepo})
	logsHandler := logs.NewHandler(logsSvc)
	projectMemberLookup := func(projectID, userID string) (string, string, bool, error) {
		member, err := projectsRepo.GetMember(projectID, userID)
		if err != nil {
			return "", "", false, err
		}
		if member == nil {
			return "", "", false, nil
		}
		return member.Role, member.Status, true, nil
	}

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(mw.RequestID)
	r.Use(mw.CORS)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})

	r.Mount("/api/v1/auth", authHandler.Routes(mw.Audit(db)))
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Post("/", logsHandler.GetOrCreateTodayReport)
		r.Get("/", logsHandler.GetReportByDate)
		r.Get("/mine", logsHandler.ListMyReports)
		r.Patch("/{id}", logsHandler.UpdateReportRawText)
		r.Post("/{id}/submit", logsHandler.SubmitReport)
	})
	r.Route("/api/v1/logs", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Get("/{id}", logsHandler.GetLog)
		r.Patch("/{id}", logsHandler.UpdateLog)
	})
	r.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Get("/", projectsHandler.List)
		r.Post("/", projectsHandler.Create)

		r.Route("/{id}", func(r chi.Router) {
			r.Use(mw.RequireProjectAccess(projectMemberLookup, projects.RoleViewer))
			r.Get("/", projectsHandler.GetByID)
			r.Post("/transition", projectsHandler.TransitionStatus)
			r.Get("/members", projectsHandler.ListMembers)
			r.Get("/logs", logsHandler.ListLogs)

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectAccess(projectMemberLookup, projects.RoleMember))
				r.Post("/logs", logsHandler.CreateLog)
			})

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectAccess(projectMemberLookup, projects.RoleMaintainer))
				r.Patch("/", projectsHandler.Update)
				r.Post("/members", projectsHandler.AddMember)
				r.Patch("/members/{userID}", projectsHandler.UpdateMemberRole)
				r.Delete("/members/{userID}", projectsHandler.RemoveMember)
			})
		})
	})

	port := commonEnv("PORT", "8000")
	slog.Info("server starting", "port", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

func commonEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
