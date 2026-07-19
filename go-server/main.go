package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/attachments"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/rfmatch"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/sensors"
	"github.com/zhu571/hiaf-lab-system/go-server/testdata"
)

//go:embed static
var frontendFiles embed.FS

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
	port := commonEnv("PORT", "8000")

	authRepo := auth.NewRepository(db)
	mw.TokenVersionValidator = func(userID string, version int) bool {
		user, err := authRepo.GetByID(userID)
		if err != nil || user == nil {
			return false
		}
		// disabled 用户的 access token 立即失效，即使 token_version 仍然匹配。
		return user.TokenVersion == version && !user.Disabled
	}
	authSvc := auth.NewService(authRepo, []byte(jwtSecret))
	authHandler := auth.NewHandler(authSvc)
	projectsRepo := projects.NewRepository(db)
	projectsSvc := projects.NewService(projectsRepo)
	projectsHandler := projects.NewHandler(projectsSvc)
	logsRepo := logs.NewRepository(db)
	logsSvc := logs.NewService(logsRepo, "Asia/Shanghai", logs.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	logsHandler := logs.NewHandler(logsSvc)
	auditHandler := audit.NewHandler(db)
	agentRepo := agent.NewRepository(db)
	agentSvc := agent.NewService(agentRepo)
	agentHandler := agent.NewHandler(agentSvc)
	issuesRepo := issues.NewRepository(db)
	issuesSvc := issues.NewService(issuesRepo, issues.ProjectAccessAdapter{DB: db, Repo: projectsRepo}, agentSvc)
	issuesHandler := issues.NewHandler(issuesSvc)
	experiencesRepo := experiences.NewRepository(db)
	experiencesSvc := experiences.NewService(experiencesRepo, experiences.ProjectAccessAdapter{Repo: projectsRepo}, agentSvc)
	experiencesHandler := experiences.NewHandler(experiencesSvc)
	runsRepo := runs.NewRepository(db)
	runsSvc := runs.NewService(runsRepo, runs.ProjectAccessAdapter{Repo: projectsRepo})
	runsHandler := runs.NewHandler(runsSvc)
	assemblyRepo := assembly.NewRepository(db)
	assemblySvc := assembly.NewService(assemblyRepo, assembly.ProjectAccessAdapter{Repo: projectsRepo})
	assemblyHandler := assembly.NewHandler(assemblySvc)
	testDataRepo := testdata.NewRepository(db)
	testDataSvc := testdata.NewService(testDataRepo, testdata.ProjectAccessAdapter{Repo: projectsRepo},
		testdata.NewHTTPRunValidator("http://127.0.0.1:"+port))
	testDataHandler := testdata.NewHandler(testDataSvc)
	rfMatchingRepo := rfmatch.NewRepository(db)
	rfMatchingSvc := rfmatch.NewService(rfMatchingRepo, rfmatch.ProjectAccessAdapter{Repo: projectsRepo})
	rfMatchingHandler := rfmatch.NewHandler(rfMatchingSvc)
	attachmentsRepo := attachments.NewRepository(db)
	attachmentsSvc := attachments.NewService(attachmentsRepo,
		attachments.NewHTTPPermissionChecker("http://127.0.0.1:"+port),
		commonEnv("ATTACHMENT_DIR", "./uploads/"))
	attachmentsHandler := attachments.NewHandler(attachmentsSvc)
	agentSvc.SetExecutor(candidateExecutor{issues: issuesSvc, experiences: experiencesSvc})
	sensorsSvc, err := sensors.NewService()
	if err != nil {
		slog.Error("failed to create sensors service", "error", err)
		os.Exit(1)
	}
	instrumentsSvc, err := instruments.NewService()
	if err != nil {
		slog.Error("failed to create instruments service", "error", err)
		os.Exit(1)
	}
	e5063aWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
		InstrumentID: "e5063a",
		Addr:         "10.51.12.157:5025",
		Terminator:   "\n",
	})
	hiokiWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
		InstrumentID: "hioki_im3536",
		Addr:         "10.51.12.101:3500",
		Terminator:   "\r\n",
	})
	workers := map[string]*instruments.InstrumentWorker{
		"e5063a":       e5063aWorker,
		"hioki_im3536": hiokiWorker,
	}
	for id, worker := range workers {
		if err := worker.Start(); err != nil {
			slog.Warn("instrument worker unavailable", "instrument_id", id, "error", err)
		}
	}
	instrumentsHandler := instruments.NewHandler(instrumentsSvc, workers)
	sensorsHandler := sensors.NewHandler(sensorsSvc)

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(mw.RequestID)
	r.Use(mw.CORS)
	r.Use(mw.CSRF)
	r.Use(middleware.Logger)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})

	r.Mount("/api/v1/auth", authHandler.Routes(mw.Audit(db)))
	r.Route("/api/v1/admin/users", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin))
		r.Use(mw.Audit(db))
		r.Get("/", authHandler.AdminListUsers)
		r.Post("/", authHandler.AdminCreateUser)
		r.Patch("/{id}", authHandler.AdminUpdateUser)
		r.Post("/{id}/reset-password", authHandler.AdminResetPassword)
	})
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Get("/{request_id}", auditHandler.GetByRequestID)
	})
	r.Route("/api/v1/agent/tasks", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAgent))
		r.With(mw.Audit(db)).Post("/claim", agentHandler.Claim)
		r.Route("/{id}", func(r chi.Router) {
			r.Use(mw.QueueTaskContext(db))
			r.Use(mw.Audit(db))
			r.Post("/complete", agentHandler.Complete)
			r.Post("/fail", agentHandler.Fail)
		})
	})
	r.Route("/api/v1/agent/candidates", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin, auth.RoleMaintainer))
		r.Use(mw.Audit(db))
		r.Get("/", agentHandler.ListCandidates)
		r.Post("/{id}/approve", agentHandler.ApproveCandidate)
		r.Post("/{id}/reject", agentHandler.RejectCandidate)
	})
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Get("/", logsHandler.ListReports)
		r.Post("/today", logsHandler.GetOrCreateTodayReport)
		r.Get("/by-date", logsHandler.GetReportByDate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetReportByID)
			r.Patch("/", logsHandler.UpdateReportRawText)
			r.Post("/submit", logsHandler.SubmitReport)
		})
	})
	r.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Get("/", projectsHandler.List)
		r.Post("/", projectsHandler.Create)

		r.Route("/{id}", func(r chi.Router) {
			r.Use(mw.RequireProjectPermission(db, mw.PermRead))
			r.Get("/", projectsHandler.GetByID)
			r.Get("/members", projectsHandler.ListMembers)
			r.Get("/issues", issuesHandler.List)
			r.Get("/logs", logsHandler.ListLogs)
			r.Get("/experiment-runs", runsHandler.List)
			r.Post("/experiment-runs", runsHandler.Create)
			r.Get("/assembly", assemblyHandler.List)
			r.Post("/assembly", assemblyHandler.Create)
			r.Get("/test-data", testDataHandler.List)
			r.Post("/test-data", testDataHandler.Create)
			r.Get("/rf-matching", rfMatchingHandler.List)
			r.Post("/rf-matching", rfMatchingHandler.Create)

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
	r.Route("/api/v1/experiment-runs", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", runsHandler.GetByID)
			r.Patch("/", runsHandler.Update)
			r.Delete("/", runsHandler.SoftDelete)
			r.Post("/daily-reports/{report_id}", runsHandler.AddReportLink)
			r.Delete("/daily-reports/{report_id}", runsHandler.RemoveReportLink)
		})
	})
	r.Route("/api/v1/assembly", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Post("/reorder", assemblyHandler.Reorder)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", assemblyHandler.GetByID)
			r.Patch("/", assemblyHandler.Update)
			r.Delete("/", assemblyHandler.SoftDelete)
		})
	})
	r.Route("/api/v1/test-data", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", testDataHandler.GetByID)
			r.Patch("/", testDataHandler.Update)
			r.Delete("/", testDataHandler.MarkInvalid)
		})
	})
	r.Route("/api/v1/rf-matching", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", rfMatchingHandler.GetByID)
			r.Patch("/", rfMatchingHandler.Update)
			r.Delete("/", rfMatchingHandler.MarkVoid)
		})
	})
	r.Route("/api/v1/logs", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetLog)
			r.Patch("/", logsHandler.UpdateLog)
		})
	})
	r.Route("/api/v1/issues", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
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
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Get("/", experiencesHandler.List)
		r.Post("/", experiencesHandler.Create)
		r.Post("/candidates", experiencesHandler.Create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", experiencesHandler.GetByID)
			r.Patch("/", experiencesHandler.Update)
			r.Post("/publish", experiencesHandler.Publish)
			r.Post("/archive", experiencesHandler.Archive)
		})
	})
	r.Route("/api/v1/attachments", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
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
	r.Route("/api/v1/instruments", func(r chi.Router) {
		r.Get("/", instrumentsHandler.ListInstruments)
		r.Get("/whitelist", instrumentsHandler.GetWhitelist)
		r.Get("/{id}/status", instrumentsHandler.InstrumentStatus)
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthRequired)
			r.Use(mw.AgentContext(db))
			r.Use(mw.Audit(db))
			r.Post("/{id}/emergency-stop", instrumentsHandler.EmergencyStop)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/commands", instrumentsHandler.ExecuteCommand)
			r.Route("/piezo", func(r chi.Router) {
				r.Get("/status", instrumentsHandler.PiezoStatus)
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin))
					r.Post("/start", instrumentsHandler.PiezoStart)
					r.Post("/stop", instrumentsHandler.PiezoStop)
					r.Post("/setpoint", instrumentsHandler.PiezoSetpoint)
				})
			})
		})
	})
	r.Route("/api/v1/sensors", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Get("/latest", sensorsHandler.Latest)
		r.Get("/history", sensorsHandler.History)
	})

	// Serve embedded frontend with SPA fallback
	staticFS, fsErr := fs.Sub(frontendFiles, "static")
	if fsErr != nil {
		slog.Error("failed to mount embedded frontend", "error", fsErr)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(staticFS))
	indexHTML, err := staticFS.Open("index.html")
	if err != nil {
		slog.Error("embedded frontend missing index.html", "error", err)
		os.Exit(1)
	}
	indexHTML.Close()

	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve frontend for API routes
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: rewrite to index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}))

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

type candidateExecutor struct {
	issues      *issues.Service
	experiences *experiences.Service
}

func (e candidateExecutor) Execute(candidate agent.AgentCandidateAction, actingUserID string) error {
	switch candidate.ActionType {
	case "create_issue":
		if candidate.ProjectID == nil {
			return fmt.Errorf("create_issue candidate has no project_id")
		}
		var req issues.CreateIssueRequest
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		req.AiGenerated = true
		req.AgentTaskID = &candidate.TaskID
		_, err := e.issues.Create(*candidate.ProjectID, actingUserID, auth.RoleAgent, req)
		return err
	case "add_comment":
		var req struct {
			IssueID string `json:"issue_id"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		_, err := e.issues.AddComment(req.IssueID, actingUserID, auth.RoleAgent, issues.AddCommentRequest{Content: req.Content})
		return err
	case "create_experience":
		if candidate.ProjectID == nil {
			return fmt.Errorf("create_experience candidate has no project_id")
		}
		var req experiences.CreateExperienceRequest
		if err := json.Unmarshal(candidate.Payload, &req); err != nil {
			return err
		}
		req.ProjectID = candidate.ProjectID
		req.AiGenerated = true
		req.AgentTaskID = &candidate.TaskID
		_, err := e.experiences.Create(actingUserID, auth.RoleAgent, req)
		return err
	default:
		return fmt.Errorf("unsupported candidate action %q", candidate.ActionType)
	}
}
