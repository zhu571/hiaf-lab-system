package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/agent"
	"github.com/zhu571/hiaf-lab-system/go-server/alert"
	"github.com/zhu571/hiaf-lab-system/go-server/ask"
	"github.com/zhu571/hiaf-lab-system/go-server/assembly"
	"github.com/zhu571/hiaf-lab-system/go-server/attachments"
	"github.com/zhu571/hiaf-lab-system/go-server/audit"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/automation"
	"github.com/zhu571/hiaf-lab-system/go-server/common"
	"github.com/zhu571/hiaf-lab-system/go-server/experiences"
	"github.com/zhu571/hiaf-lab-system/go-server/instruments"
	"github.com/zhu571/hiaf-lab-system/go-server/issues"
	"github.com/zhu571/hiaf-lab-system/go-server/logs"
	mw "github.com/zhu571/hiaf-lab-system/go-server/middleware"
	"github.com/zhu571/hiaf-lab-system/go-server/notify"
	"github.com/zhu571/hiaf-lab-system/go-server/projects"
	"github.com/zhu571/hiaf-lab-system/go-server/rfmatch"
	"github.com/zhu571/hiaf-lab-system/go-server/runs"
	"github.com/zhu571/hiaf-lab-system/go-server/sensors"
	"github.com/zhu571/hiaf-lab-system/go-server/steptemplates"
	"github.com/zhu571/hiaf-lab-system/go-server/system"
	"github.com/zhu571/hiaf-lab-system/go-server/testdata"
	"github.com/zhu571/hiaf-lab-system/go-server/todos"
	"github.com/zhu571/hiaf-lab-system/go-server/translations"
	"github.com/zhu571/hiaf-lab-system/go-server/weekly"
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

	serviceToken, err := common.ReadSecret("/run/secrets/service_token", "SERVICE_TOKEN")
	if err != nil {
		slog.Warn("service token 未配置（todos scheduler 将无法拉取日报）", "error", err)
	}
	mw.SetServiceToken(serviceToken)

	db, err := common.OpenDB()
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	port := commonEnv("PORT", "8000")

	// 告警中心模块（方案 2026-08-09_alert-center）：先于各接入点构造，
	// 经窄接口注入 middleware/auth/instruments/todos（模块间不跨模块直连）。
	alertRepo := alert.NewRepository(db)
	alertSvc := alert.NewService(alertRepo, alertNotifySender{}, db)
	alertSvc.SetClickURL(notify.WebURL + "/alerts")
	alertHandler := alert.NewHandler(alertSvc)
	// middleware 内的告警触发点（agent 缺 acting_user_id / SERVICE_TOKEN 校验失败）
	// 走注入器，避免 middleware → alert 的 import 环（先例：SetAgentQueueProvider）。
	mw.SetAlertReporter(func(ctx context.Context, level, source, title, detail string) error {
		_, err := alertSvc.Report(ctx, level, source, title, detail)
		return err
	})

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
	authHandler.SetAlertReporter(alertSvc)
	projectsRepo := projects.NewRepository(db)
	issuesRepo := issues.NewRepository(db)
	logsRepo := logs.NewRepository(db)
	projectsSvc := projects.NewService(projectsRepo, issuesRepo, logsRepo)
	projectsHandler := projects.NewHandler(projectsSvc)
	logsSvc := logs.NewService(logsRepo, "Asia/Shanghai", logs.ProjectAccessAdapter{DB: db, Repo: projectsRepo})
	translationSvc := translations.NewService(translations.NewRepository(db))
	translationSvc.SetAuditWriter(func(ctx context.Context, action string, detail map[string]any) error {
		return mw.WriteSystemAudit(ctx, db, action, detail)
	})
	translationSvc.SetSourceReader(translationSourceReader{repo: logsRepo})
	translationSvc.AutoConfigure()
	logsSvc.SetTranslations(translationSvc)
	if err := logsSvc.AutoConfigure(); err != nil {
		slog.Warn("logs ai-parse autoconfigure failed", "error", err)
	}
	logsHandler := logs.NewHandler(logsSvc)
	auditSvc := audit.NewService(db)
	auditHandler := audit.NewHandler(auditSvc)
	agentRepo := agent.NewRepository(db)
	agentSvc := agent.NewService(agentRepo)
	// /metrics 的 lab_agent_queue_depth 数据源：经 provider 注入，middleware 不跨模块直读表。
	mw.SetAgentQueueProvider(func() float64 {
		n, err := agentSvc.QueueDepth(context.Background())
		if err != nil {
			slog.Warn("agent queue depth query failed", "error", err)
			return 0
		}
		return float64(n)
	})
	agentHandler := agent.NewHandler(agentSvc)
	automationRepo := automation.NewRepository(db)
	automationSvc := automation.NewService(automationRepo)
	automationHandler := automation.NewHandler(automationSvc)
	askRepo := ask.NewRepository(db)
	askSvc := ask.NewService(askRepo, db)
	askSvc.AutoConfigure()
	askSvc.StartupCheck(context.Background())
	askHandler := ask.NewHandler(askSvc)
	issuesSvc := issues.NewService(issuesRepo, issues.ProjectAccessAdapter{DB: db, Repo: projectsRepo}, agentSvc)
	issuesHandler := issues.NewHandler(issuesSvc)
	experiencesRepo := experiences.NewRepository(db)
	experiencesSvc := experiences.NewService(experiencesRepo, experiences.ProjectAccessAdapter{Repo: projectsRepo}, agentSvc)
	// AI-2 经验候选提取：issue 数据源（issues 仓储）与 LLM client（py-agent）构造期注入。
	experiencesSvc.SetIssueSource(experienceIssueBridge{repo: issuesRepo})
	experiencesSvc.SetExtractor(experiences.NewHTTPExtractClient())
	experiencesHandler := experiences.NewHandler(experiencesSvc)
	runsRepo := runs.NewRepository(db)
	runsSvc := runs.NewService(runsRepo, runs.ProjectAccessAdapter{Repo: projectsRepo})
	runsHandler := runs.NewHandler(runsSvc)
	assemblyRepo := assembly.NewRepository(db)
	assemblySvc := assembly.NewService(assemblyRepo, assembly.ProjectAccessAdapter{Repo: projectsRepo})
	assemblyHandler := assembly.NewHandler(assemblySvc)
	templatesRepo := steptemplates.NewRepository(db)
	templatesSvc := steptemplates.NewService(templatesRepo, db)
	templatesSvc.AutoConfigure()
	templatesHandler := steptemplates.NewHandler(templatesSvc)
	assemblySvc.ConfigureTemplates(templateReaderBridge{repo: templatesRepo})
	runsSvc.ConfigureTemplates(runTemplateReaderBridge{repo: templatesRepo})
	testDataRepo := testdata.NewRepository(db)
	selfBase := commonEnv("SELF_BASE_URL", "http://127.0.0.1:"+port)
	testDataSvc := testdata.NewService(testDataRepo, testdata.ProjectAccessAdapter{Repo: projectsRepo},
		testdata.NewHTTPRunValidator(selfBase))
	testDataHandler := testdata.NewHandler(testDataSvc)
	rfMatchingRepo := rfmatch.NewRepository(db)
	rfMatchingSvc := rfmatch.NewService(rfMatchingRepo, rfmatch.ProjectAccessAdapter{Repo: projectsRepo})
	rfMatchingHandler := rfmatch.NewHandler(rfMatchingSvc)
	attachmentsRepo := attachments.NewRepository(db)
	// R3：实体权限检查改 main 构造期注入桥接（各模块既有读路径 + 项目 ACL），
	// 不再走无认证的回环 HTTP permission-check（无模块实现且 fail-open）。
	attachmentsSvc := attachments.NewService(attachmentsRepo,
		attachmentPermissionBridge{
			db:       db,
			logs:     logsSvc,
			issues:   issuesSvc,
			assembly: assemblySvc,
			runs:     runsSvc,
			testdata: testDataSvc,
			rfmatch:  rfMatchingSvc,
			projects: projectsRepo,
		},
		commonEnv("ATTACHMENT_DIR", "./uploads/"))
	attachmentsHandler := attachments.NewHandler(attachmentsSvc)
	agentSvc.SetExecutor(candidateExecutor{issues: issuesSvc, experiences: experiencesSvc})
	// trace 端点（C8）的三个只读注入：日报当前值（logs）、审计行（audit）、
	// 执行产物反查（issues/experiences）——agent 模块不跨模块读表。
	agentSvc.SetReportReader(reportReaderBridge{svc: logsSvc})
	agentSvc.SetAuditReader(auditReaderBridge{svc: auditSvc})
	agentSvc.SetResultResolver(resultResolverBridge{issues: issuesRepo, experiences: experiencesRepo})
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
	e5063aAddr := commonEnv("E5063A_ADDR", "10.51.12.157:5025")
	hiokiAddr := commonEnv("HIOKI_IM3536_ADDR", "10.51.12.101:3500")
	e5063aWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
		InstrumentID: "e5063a",
		Addr:         e5063aAddr,
		Terminator:   "\n",
		Reporter:     alertSvc,
	})
	hiokiWorker := instruments.NewInstrumentWorker(instruments.WorkerConfig{
		InstrumentID: "hioki_im3536",
		Addr:         hiokiAddr,
		Terminator:   "\r\n",
		Reporter:     alertSvc,
	})
	workers := map[string]*instruments.InstrumentWorker{
		"e5063a":       e5063aWorker,
		"hioki_im3536": hiokiWorker,
	}
	if addr := os.Getenv("KEYSIGHT_33210A_ADDR"); addr != "" {
		workers["keysight_33210a"] = instruments.NewInstrumentWorker(instruments.WorkerConfig{
			InstrumentID: "keysight_33210a", Addr: addr, Terminator: "\n", Reporter: alertSvc,
		})
	}
	for id, worker := range workers {
		if err := worker.Start(); err != nil {
			slog.Warn("instrument worker unavailable", "instrument_id", id, "error", err)
		}
	}
	instrumentsHandler := instruments.NewHandler(instrumentsSvc, db, workers)
	instrumentsHandler.SetAlertReporter(alertSvc)
	// M6 灰度止血开关（设计 §15）：INSTRUMENT_FLOW_ENABLED 默认关闭，
	// 关闭时不注册 flow 写端点、不启动 FlowRecovery；GET 进度与急停不受影响。
	flowEnabled := instruments.FlowEnabled()
	if !flowEnabled {
		slog.Info("instrument flows disabled (set INSTRUMENT_FLOW_ENABLED to enable)")
	}
	if flowEnabled {
		instrumentsHandler.StartFlowRecovery(context.Background())
	}
	sensorsHandler := sensors.NewHandler(sensorsSvc)

	repoRoot := commonEnv("REPO_ROOT", "/opt/hiaf-lab-system")
	systemSvc := system.NewService(repoRoot)
	systemHandler := system.NewHandler(systemSvc)

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		slog.Error("failed to load Asia/Shanghai timezone", "error", err)
		os.Exit(1)
	}
	todosRepo := todos.NewRepository(db)
	todosSvc := todos.NewService(
		todosRepo,
		issuesRepo,
		todos.NewSnapshot(db),
		todos.NewDBPermChecker(db),
		todos.NewAuditWriter(db),
		todos.NewHTTPLLMPlanner(),
		todos.NewHTTPReportFetcher(selfBase),
		todos.NewNtfyCLIClient(todos.NewExecNtfyRunner()),
		todos.NewNtfyPublisher(),
		loc, time.Now,
	)
	todosHandler := todos.NewHandler(todosSvc)

	// 周报模块（AI-1）：跨模块只读（daily_reports/issues）与落库（experiences）
	// 全部经 main_bridges.go 窄接口注入，weekly 不直读任何业务表。
	weeklySvc := weekly.NewService(
		weeklyReportReaderBridge{repo: logsRepo},
		weeklyIssueStatsBridge{repo: issuesRepo},
		weeklyExperienceBridge{repo: experiencesRepo},
		weekly.NewHTTPLLMClient(),
		weeklyNotifier{},
		loc, time.Now,
	)
	weeklyHandler := weekly.NewHandler(weeklySvc)

	r := chi.NewRouter()
	// 来源门必须先于 chi RealIP：RealIP 会改写 r.RemoteAddr，先取数才能拿到真实 TCP 对端。
	r.Use(mw.SourceGate())
	r.Use(middleware.RealIP)
	r.Use(mw.RequestID)
	r.Use(mw.ServiceToken())
	r.Use(mw.CORS)
	r.Use(mw.CSRF)
	// 顺序约束：Metrics 在 RequestID 之后（request_id 已就绪）、RequestLogger 之前
	// （日志条目构造时 request_id 已注入 context，P2-1/P2-2）。
	r.Use(mw.Metrics)
	r.Use(middleware.RequestLogger(mw.SlogLogFormatter{}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		common.WriteSuccess(w, r, map[string]string{"status": "ok"})
	})
	// /metrics 不经过 AuthRequired（运维探针可达；ServiceToken 白名单只拦
	// /api/v1/daily-reports/by-date 与 /api/v1/ask/execute，/metrics 不受影响）。
	r.Get("/metrics", mw.MetricsHandler)

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
	r.Route("/api/v1/admin/invitation-codes", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", authHandler.AdminListInvitationCodes)
		r.Post("/", authHandler.AdminCreateInvitationCode)
		r.Post("/{id}/revoke", authHandler.AdminRevokeInvitationCode)
	})
	// 自动化规则（C9 规则引擎一期）— admin only，写操作需审计+幂等
	r.Route("/api/v1/admin/automation", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Mount("/", automationHandler.Routes())
	})
	// 系统更新 — admin only
	r.Route("/api/v1/admin/system", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin))

		// 版本查询 — 只读，无审计/幂等
		r.Get("/version", systemHandler.GetVersion)

		// 触发更新 — 写操作，需审计+幂等
		r.Group(func(r chi.Router) {
			r.Use(mw.Audit(db))
			r.Use(mw.RequireIdempotencyKey(db))
			r.Post("/update", systemHandler.TriggerUpdate)
		})

		// SSE 日志流 — 流式，无审计/幂等
		r.Get("/update/stream/{sessionId}", systemHandler.UpdateStream)
	})
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		// /verify 与 /events 必须先于 /{request_id} 注册（chi 按注册序匹配，静态段否则会被吞）。
		r.Get("/verify", auditHandler.VerifyChain)
		r.Get("/events", auditHandler.ListEvents)
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
			// R8：租约续约——worker 在前置 HTTP 链 / LLM 长耗时阶段周期调用，
			// 与 complete/fail 同套 claim_token 所有权校验（service 层）。
			r.Post("/renew", agentHandler.Renew)
		})
	})
	r.Route("/api/v1/agent/candidates", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.RequireRole(auth.RoleAdmin, auth.RoleMaintainer))
		r.Use(mw.Audit(db))
		r.Get("/", agentHandler.ListCandidates)
		r.Get("/{id}/trace", agentHandler.TraceCandidate)
		r.Post("/{id}/approve", agentHandler.ApproveCandidate)
		r.Post("/{id}/reject", agentHandler.RejectCandidate)
	})
	r.Route("/api/v1/daily-reports", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", logsHandler.ListReports)
		r.Post("/today", logsHandler.GetOrCreateTodayReport)
		r.Get("/by-date", logsHandler.GetReportByDate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetReportByID)
			r.Patch("/", logsHandler.UpdateReportRawText)
			r.Post("/submit", logsHandler.SubmitReport)
			r.Post("/ai-parse", logsHandler.AiParseReport)
			r.Post("/translations", logsHandler.Translation)
			r.Patch("/translations", logsHandler.Translation)
			r.Post("/translations/", logsHandler.Translation)
			r.Patch("/translations/", logsHandler.Translation)
		})
	})
	r.Route("/api/v1/projects", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", projectsHandler.List)
		// D11：项目创建限 maintainer+admin（viewer 不可），service 内同语义校验纵深。
		r.With(mw.RequireRole(auth.RoleAdmin, auth.RoleMaintainer)).Post("/", projectsHandler.Create)

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
			r.Post("/assembly/apply-template", assemblyHandler.ApplyTemplate)
			r.Get("/test-data", testDataHandler.List)
			r.Post("/test-data", testDataHandler.Create)
			r.Post("/test-data/batch", testDataHandler.CreateBatch)
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
		r.Use(mw.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", runsHandler.GetByID)
			r.Patch("/", runsHandler.Update)
			r.Delete("/", runsHandler.SoftDelete)
			r.Post("/daily-reports/{report_id}", runsHandler.AddReportLink)
			r.Delete("/daily-reports/{report_id}", runsHandler.RemoveReportLink)
			r.Get("/steps", runsHandler.HandleListSteps)
			r.Post("/steps", runsHandler.HandleCreateStep)
			r.Post("/steps/apply-template", runsHandler.HandleApplyTemplate)
		})
	})
	r.Route("/api/v1/run-steps", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Post("/reorder", runsHandler.HandleReorderSteps)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", runsHandler.HandleUpdateStep)
			r.Delete("/", runsHandler.HandleDeleteStep)
		})
	})
	r.Route("/api/v1/assembly", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Post("/reorder", assemblyHandler.Reorder)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", assemblyHandler.GetByID)
			r.Patch("/", assemblyHandler.Update)
			r.Delete("/", assemblyHandler.SoftDelete)
		})
	})
	r.Route("/api/v1/step-templates", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", templatesHandler.List)
		r.Post("/", templatesHandler.Create)
		r.Post("/generate", templatesHandler.Generate)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", templatesHandler.GetByID)
			r.Patch("/", templatesHandler.Update)
			r.Delete("/", templatesHandler.SoftDelete)
			r.Patch("/items", templatesHandler.ReplaceItems)
		})
	})
	r.Route("/api/v1/test-data", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
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
		r.Use(mw.RequireIdempotencyKey(db))
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
		r.Use(mw.RequireIdempotencyKey(db))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", logsHandler.GetLog)
			r.Patch("/", logsHandler.UpdateLog)
			r.Post("/translations", logsHandler.Translation)
			r.Patch("/translations", logsHandler.Translation)
		})
	})
	r.Route("/api/v1/issues", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
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
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", experiencesHandler.List)
		r.Post("/", experiencesHandler.Create)
		r.Post("/candidates", experiencesHandler.Create)
		// AI-2 经验候选提取：maintainer+ 手动触发（对齐 /api/v1/weekly/summary 角色门槛）。
		r.With(mw.RequireRole(auth.RoleAdmin, auth.RoleMaintainer)).
			Post("/extract-candidates", experiencesHandler.ExtractCandidates)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", experiencesHandler.GetByID)
			r.Patch("/", experiencesHandler.Update)
			r.Post("/publish", experiencesHandler.Publish)
			r.Post("/archive", experiencesHandler.Archive)
		})
	})
	// 周报（AI-1）：手动触发生成，maintainer+ 权限；写接口：审计 + Idempotency-Key。
	// 定时调度独立于 HTTP（weeklyScheduler goroutine，每周日 20:00）。
	r.Route("/api/v1/weekly", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.With(mw.RequireRole(auth.RoleAdmin, auth.RoleMaintainer),
			mw.RequireIdempotencyKey(db)).Post("/summary", weeklyHandler.Summary)
	})
	r.Route("/api/v1/attachments", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
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
		// parse-result 是只读解析接口：与 ListInstruments 同级鉴权，但不需要 Idempotency-Key。
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthRequired)
			r.Use(mw.AgentContext(db))
			r.Use(mw.Audit(db))
			r.Post("/{id}/parse-result", instrumentsHandler.ParseResult)
		})
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthRequired)
			r.Use(mw.AgentContext(db))
			r.Use(mw.Audit(db))
			r.Use(mw.RequireIdempotencyKey(db))
			r.Get("/", instrumentsHandler.ListInstruments)
			r.Get("/whitelist", instrumentsHandler.GetWhitelist)
			r.Get("/gascell/status", instrumentsHandler.GasCellStatus)
			r.Route("/gascell", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin))
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
			r.Post("/{id}/nl-commands", instrumentsHandler.InterpretCommand)
			r.Post("/{id}/nl-execute", instrumentsHandler.NLExecute)
			r.Post("/{id}/emergency-stop", instrumentsHandler.EmergencyStop)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/manual-check", instrumentsHandler.ConfirmManualCheck)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases", instrumentsHandler.CreateLease)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases/{lease_id}/renew", instrumentsHandler.RenewLease)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/leases/{lease_id}/release", instrumentsHandler.ReleaseLease)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/approvals", instrumentsHandler.RequestApproval)
			r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/approvals/{approval_id}/approve", instrumentsHandler.ApproveCommand)
			if flowEnabled {
				r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows", instrumentsHandler.CreateFlow)
				r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows/{flow_id}/approve", instrumentsHandler.ApproveFlow)
				r.With(mw.RequireRole(auth.RoleMaintainer, auth.RoleAdmin)).Post("/{id}/flows/{flow_id}/stop", instrumentsHandler.StopFlow)
			}
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
	r.Route("/api/v1/ws", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Get("/gascell", instrumentsHandler.GasCellStream)
	})
	r.Route("/api/v1/sensors", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Get("/latest", sensorsHandler.Latest)
		r.Get("/history", sensorsHandler.History)
	})
	r.Route("/api/v1/todos", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.AgentContext(db))
		r.Use(mw.Audit(db))
		r.Use(mw.RequireIdempotencyKey(db))
		r.Get("/", todosHandler.List)
		r.Post("/", todosHandler.Create)
		r.Post("/llm-parse", todosHandler.ParseLLM)
		r.Post("/llm-add", todosHandler.LLMAdd)
		r.Get("/notification-topic", todosHandler.NotificationTopic)
		r.Post("/notification-topic/provision", todosHandler.Provision)
		r.Post("/notification-topic/redeem", todosHandler.Redeem)
		r.Route("/{id}", func(r chi.Router) {
			r.Patch("/", todosHandler.Edit)
			r.Patch("/done", todosHandler.Done)
			r.Patch("/defer", todosHandler.Defer)
			r.Delete("/", todosHandler.Delete)
		})
	})

	r.Route("/api/v1/ask", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthRequired)
			r.Use(mw.Audit(db))
			r.Use(mw.RequireIdempotencyKey(db))
			r.Post("/chat", askHandler.Chat)
			r.Get("/history", askHandler.History)
			r.Get("/history/{id}", askHandler.HistoryByID)
		})
		r.Group(func(r chi.Router) {
			r.Use(mw.AuthRequired)
			r.Use(mw.Audit(db))
			r.Post("/execute", askHandler.Execute)
		})
	})

	// 告警中心（方案 2026-08-09_alert-center，§4 鉴权矩阵）：
	//   report/resolve 双通道 —— 内部 SERVICE_TOKEN（白名单见 service_token.go，
	//   CSRF/幂等 IsServiceCall 豁免）→ AuthRequired 放行；resolve 用户通道
	//   JWT + RequireRoleOrService(admin, maintainer) + CSRF + Idempotency-Key。
	//   report/resolve 均要求 Idempotency-Key。
	//   list/detail 全员 JWT 可读。不挂 AgentContext（report/resolve 拒收 agent 代理头）。
	r.Route("/api/v1/alerts", func(r chi.Router) {
		r.Use(mw.AuthRequired)
		r.Use(mw.Audit(db))
		r.With(mw.RequireIdempotencyKey(db)).Post("/report", alertHandler.Report)
		r.With(mw.RequireRoleOrService(auth.RoleAdmin, auth.RoleMaintainer),
			mw.RequireIdempotencyKey(db)).Post("/resolve", alertHandler.Resolve)
		r.Get("/", alertHandler.List)
		r.Get("/{id}", alertHandler.Get)
	})

	// Serve embedded frontend with SPA fallback
	staticFS, fsErr := fs.Sub(frontendFiles, "static")
	if fsErr != nil {
		slog.Error("failed to mount embedded frontend", "error", fsErr)
		os.Exit(1)
	}
	indexHTML, err := staticFS.Open("index.html")
	if err != nil {
		slog.Error("embedded frontend missing index.html", "error", err)
		os.Exit(1)
	}
	indexHTML.Close()

	spa := spaHandler(staticFS)
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve frontend for API routes
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			http.NotFound(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	}))

	// 优雅关闭：SIGINT/SIGTERM → scheduler 不再起新批、在途批完成（≤30s），HTTP 10s 内排空。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	translationSvc.Start(ctx)
	if os.Getenv("TODOS_SCHEDULER_ENABLED") != "false" {
		sched := todos.NewScheduler(todosSvc, loc, time.Now)
		// 连续失败告警收敛到告警中心（warning/todos），由 alert 模块聚合去重后推送。
		sched.SetAlertReporter(func(title, msg string) error {
			_, err := alertSvc.Report(context.Background(), "warning", "todos", title, msg)
			return err
		})
		go sched.Run(ctx)
		slog.Info("todos scheduler enabled")
	}
	// 周报定时调度（AI-1）：每周日 20:00；WEEKLY_SUMMARY_AUTHOR_ID 未配置时跳过。
	if os.Getenv("WEEKLY_SCHEDULER_ENABLED") != "false" {
		weeklySched := weekly.NewScheduler(weeklySvc, os.Getenv("WEEKLY_SUMMARY_AUTHOR_ID"), loc, time.Now)
		weeklySched.SetAlertReporter(func(title, msg string) error {
			_, err := alertSvc.Report(context.Background(), "warning", "weekly", title, msg)
			return err
		})
		go weeklySched.Run(ctx)
		slog.Info("weekly scheduler enabled")
	}
	// ask_history 快照保留任务（P2-3）：立即执行一次 + 每 24h，90 天前快照置 NULL。
	go askSvc.StartRetentionTask(ctx)
	// 告警中心维护任务：TTL 24h 兜底（启动立即 + 每小时）+ 90 天滚动清理（每日 04:00）。
	go alertSvc.StartMaintenance(ctx)

	srv := &http.Server{Addr: ":" + port, Handler: r}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("http server shutdown", "error", err)
		}
	}()
	slog.Info("server starting", "port", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}

// spaHandler serves the embedded frontend: existing files are served directly,
// missing file-like paths (with an extension, e.g. /assets/index-xxx.js) get a
// real 404 instead of the SPA fallback — falling back to index.html there makes
// the browser refuse text/html as JS/CSS and renders a blank page — and every
// other non-file path falls back to index.html for client-side routing.
func spaHandler(staticFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(staticFS))
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, err := fs.Stat(staticFS, name); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: rewrite to index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

func commonEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
