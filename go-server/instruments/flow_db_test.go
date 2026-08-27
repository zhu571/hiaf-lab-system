package instruments

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func allowFlowAuthorization(context.Context, string, string, string) error { return nil }

func TestSafetyAndFlowRepositoryPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// 种子 UUID 必须与其它包错开（CI -p 1 跨包共用同一测试库，runs 占用 f101–f105），
	// 且 cleanup 要删净种子用户：残留行会让其它包的 ON CONFLICT (id) DO NOTHING 静默失效。
	const creator = "00000000-0000-0000-0000-00000000f203"
	const approver = "00000000-0000-0000-0000-00000000f204"
	_, err = db.Exec(`INSERT INTO users (id,username,password_hash,role) VALUES ($1,'instrument-flow-creator','unused','maintainer'),($2,'instrument-flow-approver','unused','maintainer') ON CONFLICT (id) DO NOTHING`, creator, approver)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	safety := NewSafetyService(repo)
	lease, err := safety.CreateLease(ctx, "hioki_im3536", creator, "db flow test", 15*time.Minute, false, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM command_log WHERE user_id IN ($1,$2)`, creator, approver)
		db.Exec(`DELETE FROM instrument_flow_steps WHERE session_id IN (SELECT id FROM instrument_flow_sessions WHERE actor_id=$1)`, creator)
		db.Exec(`UPDATE instrument_flow_sessions SET approval_id=NULL WHERE actor_id=$1`, creator)
		db.Exec(`UPDATE instrument_approvals SET flow_session_id=NULL WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_flow_sessions WHERE actor_id=$1`, creator)
		db.Exec(`DELETE FROM instrument_approvals WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_leases WHERE user_id=$1`, creator)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, creator, approver)
		db.Close()
	})
	if _, err = safety.CreateLease(ctx, "hioki_im3536", approver, "must conflict", time.Minute, false, "maintainer"); !errors.Is(err, ErrLeaseBusy) {
		t.Fatalf("second lease err=%v", err)
	}
	if _, err = safety.RenewLease(ctx, lease.ID, creator, "longer measurement", 20*time.Minute); err != nil {
		t.Fatal(err)
	}
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer health.Close()
	svc := NewServiceWithGateway("http://unused")
	svc.ConfigureInterpreter(health.URL, "test")
	exec := NewFlowExecutor(repo, svc, nil)
	flow, err := exec.Create(ctx, "hioki_im3536", creator, creator, "req-db-flow", CreateFlowRequest{Objective: "在 1 kHz 到 10 kHz 取 3 个对数频点", LeaseID: lease.ID})
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != "awaiting_approval" || flow.Limits.MaxCommands != 9 || len(flow.FrequencyGrid) != 3 {
		t.Fatalf("created flow=%+v", flow)
	}
	// M2：GET/approve 响应必须携带审批人可核对的完整包络（含参数区间与审批有效期）。
	handler := NewHandler(svc, db)
	env := handler.flowEnvelope(ctx, flow)
	if env == nil || env.ApprovalStatus != "pending" || env.EnvelopeHash == "" || env.ApprovalExpiresAt == nil {
		t.Fatalf("pending envelope=%+v", env)
	}
	if len(env.AllowedCommands) != 2 || env.WhitelistVersion == "" || len(env.FrequencyGrid) != 3 {
		t.Fatalf("envelope commands/grid=%+v", env)
	}
	for _, c := range env.AllowedCommands {
		if c.Risk == "red" {
			t.Fatalf("envelope command must be non-red: %+v", c)
		}
		if c.Name == "set_frequency" && len(c.Params) == 0 {
			t.Fatal("set_frequency envelope must expose param ranges")
		}
	}
	if raw, jsonErr := json.Marshal(env); jsonErr != nil || strings.Contains(strings.ToLower(string(raw)), "scpi") {
		t.Fatalf("envelope must not leak SCPI templates: %s err=%v", raw, jsonErr)
	}
	if _, err = exec.Approve(ctx, flow.ID, creator, "maintainer"); !errors.Is(err, ErrApprovalSeparation) {
		t.Fatalf("self approval err=%v", err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			approved, approveErr := exec.Approve(ctx, flow.ID, approver, "maintainer")
			if approveErr == nil && approved.Status != "queued" {
				approveErr = errors.New("approved flow is not queued")
			}
			results <- approveErr
		}()
	}
	successes := 0
	for range 2 {
		if approveErr := <-results; approveErr == nil {
			successes++
		} else if !errors.Is(approveErr, ErrApprovalSeparation) {
			t.Fatalf("concurrent approve err=%v", approveErr)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent approve successes=%d, want 1", successes)
	}
	// 审批后包络呈现审批人与有效期（M1：API 审批链路可传递完整包络信息）。
	if approvedFlow, getErr := repo.GetFlow(ctx, flow.ID); getErr != nil {
		t.Fatal(getErr)
	} else {
		env := handler.flowEnvelope(ctx, approvedFlow)
		if env.ApprovalStatus != "approved" || env.ApprovedBy == nil || *env.ApprovedBy != approver {
			t.Fatalf("approved envelope=%+v", env)
		}
	}
	step := 1
	flowID := flow.ID
	if err = repo.InsertCommandLog(ctx, &CommandLogEntry{InstrumentID: "hioki_im3536", CommandName: "set_frequency", RiskLevel: "yellow", UserID: creator, LeaseID: &lease.ID, ApprovalID: flow.ApprovalID, WhitelistVersion: whitelistVersion, RequestID: "req-db-flow", FlowSessionID: &flowID, StepNo: &step, Phase: "requested"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM command_log WHERE flow_session_id=$1 AND step_no=1 AND phase='requested'`, flow.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("command log count=%d err=%v", n, err)
	}
}

func TestHiokiFlowExecutorPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const creator = "00000000-0000-0000-0000-00000000f201"
	const approver = "00000000-0000-0000-0000-00000000f202"
	_, err = db.Exec(`INSERT INTO users (id,username,password_hash,role) VALUES ($1,'instrument-flow-runner','unused','maintainer'),($2,'instrument-flow-run-approver','unused','maintainer') ON CONFLICT (id) DO NOTHING`, creator, approver)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM command_log WHERE user_id=$1`, creator)
		db.Exec(`DELETE FROM instrument_flow_steps WHERE session_id IN (SELECT id FROM instrument_flow_sessions WHERE actor_id=$1)`, creator)
		db.Exec(`UPDATE instrument_flow_sessions SET approval_id=NULL WHERE actor_id=$1`, creator)
		db.Exec(`UPDATE instrument_approvals SET flow_session_id=NULL WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_flow_sessions WHERE actor_id=$1`, creator)
		db.Exec(`DELETE FROM instrument_approvals WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_leases WHERE user_id=$1`, creator)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, creator, approver)
		db.Close()
	})
	inst := startFakeTCPInstrument(t, "")
	inst.responder = func(line string) string {
		if line == "FREQuency?" {
			return "777\n"
		}
		if line == "MEASure?" {
			return "48.2,-3.1,0,0\n"
		}
		return ""
	}
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "hioki_im3536", Addr: inst.addr, Terminator: "\n"})
	if err = worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	var flow *FlowSession
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			return
		}
		var payload struct {
			Trusted struct {
				Grid   []float64 `json:"frequency_grid"`
				Points []Point   `json:"points"`
			} `json:"trusted_context"`
			Untrusted struct {
				Previous map[string]any `json:"previous_step"`
			} `json:"untrusted_inputs"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			t.Error("decode decision payload")
			return
		}
		decision := FlowDecision{Reason: "deterministic test", PromptVersion: "test", Model: "fake"}
		if len(payload.Trusted.Points) == len(payload.Trusted.Grid) {
			decision.Decision = "complete"
		} else if payload.Untrusted.Previous != nil && payload.Untrusted.Previous["command"] == "set_frequency" {
			decision.Decision = "next_command"
			decision.Command = "measure_single"
			decision.Params = map[string]any{}
		} else {
			decision.Decision = "next_command"
			decision.Command = "set_frequency"
			decision.Params = map[string]any{"hz": payload.Trusted.Grid[len(payload.Trusted.Points)]}
		}
		_ = json.NewEncoder(w).Encode(decision)
	}))
	defer agent.Close()
	svc := NewServiceWithGateway("http://unused")
	svc.ConfigureInterpreter(agent.URL, "test")
	exec := NewFlowExecutor(repo, svc, map[string]*InstrumentWorker{"hioki_im3536": worker})
	exec.authorize = allowFlowAuthorization
	exec.settle = 0
	lease, err := NewSafetyService(repo).CreateLease(ctx, "hioki_im3536", creator, "executor db test", 15*time.Minute, false, "maintainer")
	if err != nil {
		t.Fatal(err)
	}
	flow, err = exec.Create(ctx, "hioki_im3536", creator, creator, "req-executor", CreateFlowRequest{Objective: "1 kHz 到 3 kHz 取 3 个线性频点", LeaseID: lease.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = exec.Approve(ctx, flow.ID, approver, "maintainer"); err != nil {
		t.Fatal(err)
	}
	exec.Run(ctx, flow.ID)
	finished, err := repo.GetFlow(ctx, flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "completed" || finished.Result == nil || len(finished.Result.Points) != 3 {
		t.Fatalf("finished flow=%+v", finished)
	}
	for _, point := range finished.Result.Points {
		if point.Y != 48.2 {
			t.Fatalf("empty/wrong parsed Z: %+v", point)
		}
	}
	inst.waitLine(t, "FREQuency 777")
}

func TestFlowPreSendGuardAfterSlowDecisionPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tests := []struct {
		name       string
		creator    string
		approver   string
		mutate     func(*testing.T, *Repository, *FlowSession, *Lease)
		wantStatus string
		wantCode   string
		rateWait   bool
	}{
		{"deadline expires during slow LLM", "00000000-0000-0000-0000-00000000f211", "00000000-0000-0000-0000-00000000f212", func(t *testing.T, repo *Repository, f *FlowSession, _ *Lease) {
			if _, err := repo.db.Exec(`UPDATE instrument_flow_sessions SET deadline_at=now()-interval '1 second' WHERE id=$1`, f.ID); err != nil {
				t.Fatal(err)
			}
		}, "timed_out", "deadline_exceeded", false},
		{"lease expires during slow LLM", "00000000-0000-0000-0000-00000000f213", "00000000-0000-0000-0000-00000000f214", func(t *testing.T, repo *Repository, _ *FlowSession, lease *Lease) {
			if _, err := repo.db.Exec(`UPDATE instrument_leases SET expires_at=now()-interval '1 second' WHERE id=$1`, lease.ID); err != nil {
				t.Fatal(err)
			}
		}, "failed", "lease_expired", false},
		{"approval expires during slow LLM", "00000000-0000-0000-0000-00000000f215", "00000000-0000-0000-0000-00000000f216", func(t *testing.T, repo *Repository, f *FlowSession, _ *Lease) {
			if _, err := repo.db.Exec(`UPDATE instrument_approvals SET expires_at=now()-interval '1 second' WHERE id=$1`, *f.ApprovalID); err != nil {
				t.Fatal(err)
			}
		}, "failed", "approval_expired", false},
		{"concurrent stop during slow LLM", "00000000-0000-0000-0000-00000000f217", "00000000-0000-0000-0000-00000000f218", func(t *testing.T, repo *Repository, f *FlowSession, _ *Lease) {
			if err := repo.StopFlow(context.Background(), f.ID, f.ActingUserID); err != nil {
				t.Fatal(err)
			}
		}, "stopped", "", true},
		{"permission revoked during slow LLM", "00000000-0000-0000-0000-00000000f219", "00000000-0000-0000-0000-00000000f220", func(t *testing.T, repo *Repository, f *FlowSession, _ *Lease) {
			if _, err := repo.db.Exec(`UPDATE users SET disabled=true WHERE id=$1`, f.ActingUserID); err != nil {
				t.Fatal(err)
			}
		}, "failed", "permission_revoked", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			creatorName := "flow-guard-" + tc.creator[len(tc.creator)-3:]
			approverName := "flow-guard-" + tc.approver[len(tc.approver)-3:]
			if _, err := db.Exec(`INSERT INTO users (id,username,password_hash,role) VALUES ($1,$2,'unused','maintainer'),($3,$4,'unused','maintainer')`, tc.creator, creatorName, tc.approver, approverName); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				db.Exec(`DELETE FROM command_log WHERE user_id=$1`, tc.creator)
				db.Exec(`DELETE FROM instrument_flow_steps WHERE session_id IN (SELECT id FROM instrument_flow_sessions WHERE actor_id=$1)`, tc.creator)
				db.Exec(`UPDATE instrument_flow_sessions SET approval_id=NULL WHERE actor_id=$1`, tc.creator)
				db.Exec(`UPDATE instrument_approvals SET flow_session_id=NULL WHERE requested_by=$1`, tc.creator)
				db.Exec(`DELETE FROM instrument_flow_sessions WHERE actor_id=$1`, tc.creator)
				db.Exec(`DELETE FROM instrument_approvals WHERE requested_by=$1`, tc.creator)
				db.Exec(`DELETE FROM instrument_leases WHERE user_id=$1`, tc.creator)
				db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, tc.creator, tc.approver)
			})

			inst := startFakeTCPInstrument(t, "")
			inst.responder = func(line string) string {
				if line == "FREQuency?" {
					return "777\n"
				}
				return ""
			}
			worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "hioki_im3536", Addr: inst.addr, Terminator: "\n", RateWindow: 150 * time.Millisecond})
			if err := worker.Start(); err != nil {
				t.Fatal(err)
			}
			defer worker.Stop()

			decisionStarted := make(chan struct{})
			releaseDecision := make(chan struct{})
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				close(decisionStarted)
				<-releaseDecision
				_ = json.NewEncoder(w).Encode(FlowDecision{Decision: "next_command", Command: "set_frequency", Params: map[string]any{"hz": 1000.0}, Reason: "slow test"})
			}))
			defer agent.Close()

			repo := NewRepository(db)
			svc := NewServiceWithGateway("http://unused")
			svc.ConfigureInterpreter(agent.URL, "test")
			exec := NewFlowExecutor(repo, svc, map[string]*InstrumentWorker{"hioki_im3536": worker})
			exec.authorize = func(ctx context.Context, actorID, actingUserID, _ string) error {
				for _, userID := range []string{actorID, actingUserID} {
					var role string
					var disabled bool
					if err := db.QueryRowContext(ctx, `SELECT role,disabled FROM users WHERE id=$1`, userID).Scan(&role, &disabled); err != nil {
						return err
					}
					if disabled || (role != "maintainer" && role != "admin") {
						return errors.New("instrument flow permission revoked")
					}
				}
				return nil
			}
			exec.settle = 0
			lease, err := NewSafetyService(repo).CreateLease(ctx, "hioki_im3536", tc.creator, "pre-send guard test", 15*time.Minute, false, "maintainer")
			if err != nil {
				t.Fatal(err)
			}
			flow, err := exec.Create(ctx, "hioki_im3536", tc.creator, tc.creator, "req-guard", CreateFlowRequest{Objective: "1 kHz 到 2 kHz 取 2 个线性频点", LeaseID: lease.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = exec.Approve(ctx, flow.ID, tc.approver, "maintainer"); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() {
				exec.Run(ctx, flow.ID)
				close(done)
			}()
			select {
			case <-decisionStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("slow decision was not reached")
			}
			tc.mutate(t, repo, flow, lease)
			if tc.rateWait {
				worker.mu.Lock()
				worker.lastCmdTimes = []time.Time{time.Now(), time.Now(), time.Now(), time.Now(), time.Now(), time.Now(), time.Now()}
				worker.mu.Unlock()
			}
			guardStarted := time.Now()
			close(releaseDecision)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("flow did not stop after guard failure")
			}
			if tc.rateWait && time.Since(guardStarted) < 100*time.Millisecond {
				t.Fatal("test did not exercise the rate-limit wait before the pre-send guard")
			}
			inst.assertNoLine(t, "FREQuency 1000")
			finished, err := repo.GetFlow(ctx, flow.ID)
			if err != nil {
				t.Fatal(err)
			}
			if finished.Status != tc.wantStatus || finished.ErrorCode != tc.wantCode {
				t.Fatalf("status=%s code=%s, want %s/%s", finished.Status, finished.ErrorCode, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

func TestFlowAuditAndStepPersistenceFailClosedPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const creator = "00000000-0000-0000-0000-00000000f221"
	const approver = "00000000-0000-0000-0000-00000000f222"
	if _, err := db.Exec(`INSERT INTO users (id,username,password_hash,role) VALUES ($1,'flow-persist-creator','unused','maintainer'),($2,'flow-persist-approver','unused','maintainer')`, creator, approver); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	t.Cleanup(func() {
		db.Exec(`DELETE FROM command_log WHERE user_id=$1`, creator)
		db.Exec(`DELETE FROM instrument_flow_steps WHERE session_id IN (SELECT id FROM instrument_flow_sessions WHERE actor_id=$1)`, creator)
		db.Exec(`UPDATE instrument_flow_sessions SET approval_id=NULL WHERE actor_id=$1`, creator)
		db.Exec(`UPDATE instrument_approvals SET flow_session_id=NULL WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_flow_sessions WHERE actor_id=$1`, creator)
		db.Exec(`DELETE FROM instrument_approvals WHERE requested_by=$1`, creator)
		db.Exec(`DELETE FROM instrument_leases WHERE user_id=$1`, creator)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, creator, approver)
		db.Close()
	})

	inst := startFakeTCPInstrument(t, "")
	inst.responder = func(line string) string {
		if line == "FREQuency?" {
			return "777\n"
		}
		return ""
	}
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "hioki_im3536", Addr: inst.addr, Terminator: "\n"})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer health.Close()
	svc := NewServiceWithGateway("http://unused")
	svc.ConfigureInterpreter(health.URL, "test")
	exec := NewFlowExecutor(repo, svc, map[string]*InstrumentWorker{"hioki_im3536": worker})
	exec.authorize = allowFlowAuthorization
	exec.settle = 0
	lease, err := NewSafetyService(repo).CreateLease(ctx, "hioki_im3536", creator, "persistence fail-closed test", 15*time.Minute, false, "maintainer")
	if err != nil {
		t.Fatal(err)
	}

	newRunningFlow := func(t *testing.T) *FlowSession {
		flow, err := exec.Create(ctx, "hioki_im3536", creator, creator, "req-persist", CreateFlowRequest{Objective: "1 kHz 到 2 kHz 取 2 个线性频点", LeaseID: lease.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = exec.Approve(ctx, flow.ID, approver, "maintainer"); err != nil {
			t.Fatal(err)
		}
		if err = repo.StartFlow(ctx, flow.ID); err != nil {
			t.Fatal(err)
		}
		flow, err = repo.GetFlow(ctx, flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		return flow
	}

	t.Run("requested audit failure prevents hardware send", func(t *testing.T) {
		flow := newRunningFlow(t)
		if err := worker.AcquireSession(flow.ID, flow.DeadlineAt); err != nil {
			t.Fatal(err)
		}
		defer worker.ReleaseSession(flow.ID)
		realAudit := exec.audit
		exec.audit = func(_ context.Context, action string, _ map[string]any) error {
			if action == "instrument.command.requested" {
				return errors.New("forced audit failure")
			}
			return nil
		}
		_, runErr := exec.runRaw(ctx, flow, worker, 1, "set_frequency", map[string]any{"hz": 4321.0})
		exec.audit = realAudit
		if commandErrorCode(runErr) != "audit_failed" {
			t.Fatalf("run error=%v, want audit_failed", runErr)
		}
		inst.assertNoLine(t, "FREQuency 4321")
		if err := repo.FinishFlow(ctx, flow.ID, "failed", "audit_failed", nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("step insert failure cannot complete flow", func(t *testing.T) {
		flow := newRunningFlow(t)
		if err := worker.AcquireSession(flow.ID, flow.DeadlineAt); err != nil {
			t.Fatal(err)
		}
		if err := repo.AddStep(ctx, &FlowStep{SessionID: flow.ID, StepNo: 1, Decision: "next_command", Command: "set_frequency", Params: map[string]any{"hz": 1000.0}, Status: "succeeded", WhitelistVersion: whitelistVersion}); err != nil {
			t.Fatal(err)
		}
		result, runErr := exec.runRaw(ctx, flow, worker, 1, "set_frequency", map[string]any{"hz": 1000.0})
		if runErr != nil {
			t.Fatal(runErr)
		}
		step := &FlowStep{SessionID: flow.ID, StepNo: 1, Decision: "next_command", Command: "set_frequency", Params: map[string]any{"hz": 1000.0}, Status: "succeeded", WhitelistVersion: whitelistVersion, DurationMS: int(result.Duration.Milliseconds())}
		if exec.persistStep(ctx, flow, worker, step, true) {
			t.Fatal("duplicate step unexpectedly persisted")
		}
		finished, err := repo.GetFlow(ctx, flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		if finished.Status != "failed" || finished.ErrorCode != "step_persistence_failed" {
			t.Fatalf("status=%s code=%s", finished.Status, finished.ErrorCode)
		}
		if worker.State() != WorkerStateLockedManualCheck {
			t.Fatalf("worker state=%s, want manual-check lock", worker.State())
		}
		if err := worker.ConfirmManualCheck(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("terminal audit failure cannot remain completed", func(t *testing.T) {
		flow := newRunningFlow(t)
		if err := worker.AcquireSession(flow.ID, flow.DeadlineAt); err != nil {
			t.Fatal(err)
		}
		realAudit := exec.audit
		exec.audit = func(_ context.Context, action string, _ map[string]any) error {
			if action == "instrument.flow.completed" {
				return errors.New("forced terminal audit failure")
			}
			return nil
		}
		exec.finish(ctx, flow, "completed", "", &ParsedResult{Type: "sweep_xy"})
		exec.audit = realAudit
		finished, err := repo.GetFlow(ctx, flow.ID)
		if err != nil {
			t.Fatal(err)
		}
		if finished.Status != "failed" || finished.ErrorCode != "audit_failed" || finished.Result != nil {
			t.Fatalf("status=%s code=%s result=%+v", finished.Status, finished.ErrorCode, finished.Result)
		}
		if worker.State() != WorkerStateLockedManualCheck {
			t.Fatalf("worker state=%s, want manual-check lock", worker.State())
		}
		if err := worker.ConfirmManualCheck(); err != nil {
			t.Fatal(err)
		}
	})
}
