package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/sensors"
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
	logsSvc := logs.NewService(logsRepo, "Asia/Shanghai", logs.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	logsHandler := logs.NewHandler(logsSvc)
	issuesRepo := issues.NewRepository(db)
	issuesSvc := issues.NewService(issuesRepo, issues.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	issuesHandler := issues.NewHandler(issuesSvc)
	experiencesRepo := experiences.NewRepository(db)
	experiencesSvc := experiences.NewService(experiencesRepo, experiences.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	experiencesHandler := experiences.NewHandler(experiencesSvc)
	instrumentsSvc := instruments.NewService()
	instrumentsHandler := instruments.NewHandler(instrumentsSvc)
	sensorsSvc := sensors.NewService()
	sensorsHandler := sensors.NewHandler(sensorsSvc)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(mw.RequestID)
	r.Use(mw.CORS)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})

	r.Mount("/api/v1/auth", authHandler.Routes(mw.Audit(db)))
	r.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Get("/", projectsHandler.List)
		r.Post("/", projectsHandler.Create)

		r.Route("/{id}", func(r chi.Router) {
			r.Use(mw.RequireProjectPermission(db, mw.PermRead))
			r.Get("/", projectsHandler.GetByID)
			r.Get("/members", projectsHandler.ListMembers)
			r.Get("/issues", issuesHandler.List)
			r.Get("/logs", logsHandler.ListLogs)

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectPermission(db, mw.PermManageProject))
				r.Patch("/", projectsHandler.Update)
				r.Post("/transition", projectsHandler.TransitionStatus)
			})

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectPermission(db, mw.PermManageMembers))
				r.Post("/members", projectsHandler.AddMember)
				r.Patch("/members/{userID}", projectsHandler.UpdateMemberRole)
				r.Delete("/members/{userID}", projectsHandler.RemoveMember)
			})

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectPermission(db, mw.PermCreateLog))
				r.Post("/logs", logsHandler.CreateLog)
			})

			r.Group(func(r chi.Router) {
				r.Use(mw.RequireProjectPermission(db, mw.PermCreateIssue))
				r.Post("/issues", issuesHandler.Create)
			})
		})
	})
	r.Route("/api/v1/logs", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetLog)
			r.Patch("/", logsHandler.UpdateLog)
		})
	})
	r.Route("/api/v1/issues", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", issuesHandler.GetByID)
			r.Patch("/", issuesHandler.Update)
			r.Post("/transition", issuesHandler.Transition)
			r.Post("/comments", issuesHandler.AddComment)
		})
	})
	r.Route("/api/v1/experiences", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Get("/", experiencesHandler.List)
		r.Post("/", experiencesHandler.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", experiencesHandler.GetByID)
			r.Patch("/", experiencesHandler.Update)
			r.Post("/publish", experiencesHandler.Publish)
			r.Post("/archive", experiencesHandler.Archive)
		})
	})
	r.Route("/api/v1/instruments", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Route("/piezo", func(r chi.Router) {
			r.Get("/status", instrumentsHandler.PiezoStatus)
			r.Post("/start", instrumentsHandler.PiezoStart)
			r.Post("/stop", instrumentsHandler.PiezoStop)
			r.Post("/setpoint", instrumentsHandler.PiezoSetpoint)
		})
	})
	r.Route("/api/v1/sensors", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.Get("/latest", sensorsHandler.Latest)
		r.Get("/history", sensorsHandler.History)
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
