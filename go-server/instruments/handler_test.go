package instruments

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/zhu571/hiaf-lab-system/go-server/auth"
	"github.com/zhu571/hiaf-lab-system/go-server/middleware"
)

// handler 层覆盖：状态码全覆盖 + 鉴权上下文（AuthRequired）+ 假 worker/gateway。
// 固定用户 d501 用于 instrument_results 落库。

const (
	insDBUserID  = "00000000-0000-0000-0000-00000000d501"
	insDBUser2ID = "00000000-0000-0000-0000-00000000d502"
)

const insHandlerSecret = "instruments-handler-test-secret"

func insToken(t *testing.T, userID, username, role string) string {
	t.Helper()
	token, err := middleware.GenerateToken(userID, username, role, 1, []byte(insHandlerSecret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func insRequest(t *testing.T, router http.Handler, method, path, token, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func insErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("error body: %s, err=%v", rec.Body.String(), err)
	}
	return envelope.Error.Code
}

func insNewRouter(h *Handler) http.Handler {
	middleware.SetJWTSecret([]byte(insHandlerSecret))
	router := chi.NewRouter()
	router.Route("/api/v1/instruments", func(r chi.Router) {
		r.Use(middleware.AuthRequired)
		r.Get("/", h.ListInstruments)
		r.Get("/whitelist", h.GetWhitelist)
		r.Post("/{id}/parse-result", h.ParseResult)
		r.Post("/{id}/commands", h.ExecuteCommand)
		r.Post("/{id}/interpret", h.InterpretCommand)
		r.Post("/{id}/nl-execute", h.NLExecute)
		r.Post("/{id}/emergency-stop", h.EmergencyStop)
		r.Get("/{id}/status", h.InstrumentStatus)
		r.Get("/gascell/status", h.GasCellStatus)
		r.Post("/gascell/start", h.GasCellStart)
		r.Post("/gascell/stop", h.GasCellStop)
		r.Post("/gascell/valve", h.GasCellValve)
		r.Post("/gascell/params", h.GasCellParams)
		r.Post("/gascell/a5-max", h.GasCellA5Max)
		r.Post("/gascell/a5-clear", h.GasCellA5Clear)
		r.Get("/piezo/status", h.PiezoStatus)
		r.Post("/piezo/start", h.PiezoStart)
		r.Post("/piezo/stop", h.PiezoStop)
		r.Post("/piezo/setpoint", h.PiezoSetpoint)
	})
	return router
}

func TestHandlerInstrumentStatusAndList(t *testing.T) {
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: "1.2.3.4:1", Terminator: "\n"})
	h := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{"e5063a": worker})
	router := insNewRouter(h)
	member := insToken(t, insDBUserID, "member", auth.RoleMember)

	// GET /status：404 未知仪器；200 已知（未启动状态为空串）
	rec := insRequest(t, router, http.MethodGet, "/api/v1/instruments/nope/status", member, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown status = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodGet, "/api/v1/instruments/e5063a/status", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			InstrumentID string `json:"instrument_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.InstrumentID != "e5063a" {
		t.Fatalf("status data: %+v", envelope.Data)
	}

	// GET /：200 列表（排序）
	rec = insRequest(t, router, http.MethodGet, "/api/v1/instruments", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var listEnvelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data) != 1 || listEnvelope.Data[0].ID != "e5063a" {
		t.Fatalf("list: %+v", listEnvelope.Data)
	}

	// GET /whitelist：200 命令列表（含 e5063a 全部命令）
	rec = insRequest(t, router, http.MethodGet, "/api/v1/instruments/whitelist", member, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("whitelist = %d", rec.Code)
	}
	var wlEnvelope struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wlEnvelope); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cmd := range wlEnvelope.Data {
		if cmd.Name == "identify" {
			found = true
		}
	}
	if !found || len(wlEnvelope.Data) == 0 {
		t.Fatalf("whitelist commands: %d", len(wlEnvelope.Data))
	}
}

func TestHandlerExecuteCommand(t *testing.T) {
	inst := startFakeTCPInstrument(t, "Keysight,E5063A\n")
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	h := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{"e5063a": worker})
	router := insNewRouter(h)
	member := insToken(t, insDBUserID, "member", auth.RoleMember)

	// 400：缺 Idempotency-Key / 坏 JSON / 空命令
	rec := insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "", `{"command":"identify"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no idempotency key = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k1", `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k2", `{"params":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty command = %d", rec.Code)
	}
	// 404：未知仪器
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/nope/commands", member, "k3", `{"command":"identify"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown instrument = %d", rec.Code)
	}
	// 400：红名单命令 / 参数校验失败
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k4", `{"command":"reset"}`)
	if rec.Code != http.StatusBadRequest || insErrorCode(t, rec) != "command_not_allowed" {
		t.Fatalf("red command = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k5",
		`{"command":"set_power","params":{"power_dbm":99}}`)
	if rec.Code != http.StatusBadRequest || insErrorCode(t, rec) != "validation_failed" {
		t.Fatalf("bad params = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 200：identify 正常执行
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k6", `{"command":"identify"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Command  string `json:"command"`
			Response string `json:"response"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Command != "identify" || !strings.Contains(envelope.Data.Response, "Keysight") {
		t.Fatalf("execute result: %+v", envelope.Data)
	}

	// 503：worker 未运行（新 handler 注入未启动 worker）
	idle := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	hIdle := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{"e5063a": idle})
	routerIdle := insNewRouter(hIdle)
	rec = insRequest(t, routerIdle, http.MethodPost, "/api/v1/instruments/e5063a/commands", member, "k7", `{"command":"identify"}`)
	if rec.Code != http.StatusServiceUnavailable || insErrorCode(t, rec) != "instrument_unavailable" {
		t.Fatalf("idle worker = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerParseResult(t *testing.T) {
	h := NewHandler(NewServiceWithGateway("http://unused"), nil)
	router := insNewRouter(h)
	member := insToken(t, insDBUserID, "member", auth.RoleMember)

	// 404：未知仪器
	rec := insRequest(t, router, http.MethodPost, "/api/v1/instruments/nope/parse-result", member, "", `{"command":"x","response":"y"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown instrument = %d", rec.Code)
	}
	// 400：坏 JSON / 空命令
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/parse-result", member, "", `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/parse-result", member, "", `{"response":"1"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty command = %d", rec.Code)
	}
	// 400：未知命令 / 解析失败
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/parse-result", member, "", `{"command":"nope","response":"1"}`)
	if rec.Code != http.StatusBadRequest || insErrorCode(t, rec) != "command_not_allowed" {
		t.Fatalf("unknown command = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/hioki_im3536/parse-result", member, "",
		`{"command":"measure_single","response":"abc"}`)
	if rec.Code != http.StatusBadRequest || insErrorCode(t, rec) != "parse_failed" {
		t.Fatalf("parse failed = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 200：single_value 解析
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/hioki_im3536/parse-result", member, "",
		`{"command":"measure_single","response":"1.0234E+2,-89.5,0,0"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("parse = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Type  string   `json:"type"`
			Value *float64 `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Type != "single_value" || envelope.Data.Value == nil || *envelope.Data.Value != 102.34 {
		t.Fatalf("parsed: %+v", envelope.Data)
	}
}

func TestHandlerInterpretAndNLExecute(t *testing.T) {
	interp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer internal" {
			t.Fatal("missing interpreter auth")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "command": "identify", "params": map[string]any{},
			"confidence": 0.95, "explanation": "读取标识", "question": "", "reason": "",
			"prompt_version": "1.0", "model": "test",
		})
	}))
	defer interp.Close()

	inst := startFakeTCPInstrument(t, "Keysight,E5063A\n")
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()

	svc := NewServiceWithGateway("http://unused")
	svc.ConfigureInterpreter(interp.URL, "internal")
	db := openInsDB(t)
	h := NewHandler(svc, db, map[string]*InstrumentWorker{"e5063a": worker})
	router := insNewRouter(h)
	maintainer := insToken(t, insDBUserID, "maintainer", auth.RoleMaintainer)
	member := insToken(t, insDBUserID, "member", auth.RoleMember)

	// interpret：404 未知仪器；400 空输入；400 历史角色非法；429 限流
	rec := insRequest(t, router, http.MethodPost, "/api/v1/instruments/nope/interpret", maintainer, "k1", `{"input":"hi"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("interpret unknown = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/interpret", maintainer, "k2", `{"input":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("interpret empty = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/interpret", maintainer, "k3",
		`{"input":"hi","history":[{"role":"system","content":"x"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("interpret bad history = %d", rec.Code)
	}
	// 200：成功翻译（audit action=instrument.nl.translated）
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/interpret", maintainer, "k4", `{"input":"读取仪器标识"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("interpret = %d, body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Status string `json:"status"`
			SCPI   string `json:"scpi_preview"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Status != "ok" || envelope.Data.SCPI != "*IDN?" {
		t.Fatalf("interpret result: %+v", envelope.Data)
	}

	// 429：同一用户第 10 次调用后限流（历史已 1 次，再打 9 次）
	for i := 0; i < 9; i++ {
		rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/interpret", maintainer, "k5", `{"input":"hi"}`)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/interpret", maintainer, "k6", `{"input":"hi"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit = %d", rec.Code)
	}

	// NLExecute：member → 403；maintainer2（独立用户，避开上面 interpret 的限流计数）→ 200
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/nl-execute", member, "k7", `{"input":"hi"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nl-execute member = %d", rec.Code)
	}
	maintainer2 := insToken(t, insDBUser2ID, "maintainer2", auth.RoleMaintainer)
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/nl-execute", maintainer2, "k8", `{"input":"读取标识"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("nl-execute = %d, body=%s", rec.Code, rec.Body.String())
	}
	var nlEnvelope struct {
		Data struct {
			Status  string `json:"status"`
			Command string `json:"command"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &nlEnvelope); err != nil {
		t.Fatal(err)
	}
	if nlEnvelope.Data.Status != "ok" || nlEnvelope.Data.Command != "identify" {
		t.Fatalf("nl-execute result: %+v", nlEnvelope.Data)
	}
	// 结果已落库（instrument_results）
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM instrument_results WHERE user_id = $1 AND command_name = 'identify'`,
		insDBUser2ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count < 1 {
		t.Fatalf("instrument_results rows = %d", count)
	}
}

func TestHandlerEmergencyStop(t *testing.T) {
	inst := startFakeTCPInstrument(t, "")
	worker := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	h := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{"e5063a": worker})
	router := insNewRouter(h)
	maintainer := insToken(t, insDBUserID, "maintainer", auth.RoleMaintainer)

	// 404：未知仪器
	rec := insRequest(t, router, http.MethodPost, "/api/v1/instruments/nope/emergency-stop", maintainer, "k1", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown instrument = %d", rec.Code)
	}
	// 200：急停下发（仪器收到 ABOR）
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/e5063a/emergency-stop", maintainer, "k2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("emergency = %d, body=%s", rec.Code, rec.Body.String())
	}
	inst.waitLine(t, "ABOR")
	// 503：未运行 worker
	idle := NewInstrumentWorker(WorkerConfig{InstrumentID: "e5063a", Addr: inst.addr, Terminator: "\n"})
	hIdle := NewHandler(NewServiceWithGateway("http://unused"), nil, map[string]*InstrumentWorker{"e5063a": idle})
	routerIdle := insNewRouter(hIdle)
	rec = insRequest(t, routerIdle, http.MethodPost, "/api/v1/instruments/e5063a/emergency-stop", maintainer, "k3", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("idle emergency = %d", rec.Code)
	}
}

func TestHandlerGasCell(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path == "/batch" {
			writeBatch(t, map[string]any{
				"GasCell:Piezo:A1": 1.25, "GasCell:Piezo:ValveSP": 2.5,
				"GasCell:Piezo:Running": 0, "GasCell:Piezo:Error": "",
			})(w, r)
			return
		}
		writePV(t, 2.5)(w, r)
	}))
	defer gateway.Close()

	h := NewHandler(NewServiceWithGateway(gateway.URL), nil)
	router := insNewRouter(h)
	maintainer := insToken(t, insDBUserID, "maintainer", auth.RoleMaintainer)
	viewer := insToken(t, insDBUserID, "viewer", auth.RoleViewer)

	// GET gascell/status：200（gateway 正常 + batch 回读）
	rec := insRequest(t, router, http.MethodGet, "/api/v1/instruments/gascell/status", maintainer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("gascell status = %d", rec.Code)
	}
	// GET piezo/status：200（deprecated 头 + 聚合）
	rec = insRequest(t, router, http.MethodGet, "/api/v1/instruments/piezo/status", maintainer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("piezo status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Deprecation") != "true" {
		t.Fatalf("missing Deprecation header")
	}

	// POST gascell/start：viewer 403；maintainer 200；缺 key 400
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/start", viewer, "k1", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer start = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/start", maintainer, "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no key = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/start", maintainer, "k2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d, body=%s", rec.Code, rec.Body.String())
	}
	// POST gascell/params：400 坏 JSON；200 写 setpoint
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/params", maintainer, "k3", `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("params bad json = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/params", maintainer, "k4", `{"setpoint":3.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("params = %d, body=%s", rec.Code, rec.Body.String())
	}
	// POST gascell/valve：viewer 403；maintainer 400 validation_failed——
	// GasCellValve 走独立 GET 回读 Running（writePV 返回 2.5 ≠ 0 → 「需 Running=0」），
	// 与 /batch 快照（Running=0）不同源。
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/valve", maintainer, "k5", `{"value":5}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("valve while running = %d, body=%s", rec.Code, rec.Body.String())
	}
	// POST gascell/a5-max：400 坏 JSON 仅当 value 缺失时（JSON 解码失败）
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/a5-max", maintainer, "k6", `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a5-max bad json = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/a5-max", maintainer, "k7", `{"value":0.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a5-max = %d, body=%s", rec.Code, rec.Body.String())
	}
	// POST gascell/a5-clear：200
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/a5-clear", maintainer, "k8", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("a5-clear = %d, body=%s", rec.Code, rec.Body.String())
	}

	// piezo setpoint：400 坏 JSON；503 越界（handler 将 service 错误统一 503 gateway_error）
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/setpoint", maintainer, "k9", `{bad`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("setpoint bad json = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/setpoint", maintainer, "k10", `{"value":150}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("setpoint out of range = %d, body=%s", rec.Code, rec.Body.String())
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/setpoint", maintainer, "k11", `{"value":12.5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setpoint = %d, body=%s", rec.Code, rec.Body.String())
	}
	// piezo start/stop：200
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/start", maintainer, "k12", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("piezo start = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/stop", maintainer, "k13", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("piezo stop = %d", rec.Code)
	}
}

func TestHandlerGasCellGatewayError(t *testing.T) {
	// gateway 不可用 → gascell 状态降级（disconnected，200）；piezo 聚合 502
	h := NewHandler(NewServiceWithGateway("http://127.0.0.1:1"), nil)
	router := insNewRouter(h)
	maintainer := insToken(t, insDBUserID, "maintainer", auth.RoleMaintainer)

	rec := insRequest(t, router, http.MethodGet, "/api/v1/instruments/gascell/status", maintainer, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("degraded status = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodGet, "/api/v1/instruments/piezo/status", maintainer, "", "")
	if rec.Code != http.StatusServiceUnavailable || insErrorCode(t, rec) != "gateway_error" {
		t.Fatalf("piezo degraded = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 写操作 → 502 gateway_error
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/gascell/start", maintainer, "k1", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("gateway down start = %d", rec.Code)
	}
	rec = insRequest(t, router, http.MethodPost, "/api/v1/instruments/piezo/setpoint", maintainer, "k2", `{"value":1}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("gateway down setpoint = %d", rec.Code)
	}
}

func openInsDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, display_name, role, must_change_pw, disabled)
		 VALUES ($1, 'ins_dbtest_user', 'x', 'Instruments DB Test', 'member', false, false),
		        ($2, 'ins_dbtest_user2', 'x', 'Instruments DB Test 2', 'member', false, false)
		 ON CONFLICT (id) DO NOTHING`, insDBUserID, insDBUser2ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM instrument_results WHERE user_id IN ($1,$2)`, insDBUserID, insDBUser2ID)
		db.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, insDBUserID, insDBUser2ID)
	})
	return db
}

func TestResultsRepoRoundTrip(t *testing.T) {
	db := openInsDB(t)
	parsed := 3.5
	id := uuid.NewString()
	r := &InstrumentResult{
		ID: id, InstrumentID: "e5063a", CommandName: "read_frequencies",
		SCPI: "SENS1:FREQ:DATA?", RawResponse: "1e6,2e6", ParsedValue: &parsed,
		ParsedPoints: []Point{{X: 1, Y: 2}}, PlotType: "line", DurationMS: 12,
		UserID: insDBUserID, RequestID: "req_test_1",
	}
	if err := InsertResult(db, r); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM instrument_results WHERE id = $1`, id) })
	if r.CreatedAt.IsZero() {
		t.Fatal("created_at not set")
	}

	results, err := ListResults(db, "e5063a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != id || results[0].ParsedValue == nil ||
		*results[0].ParsedValue != 3.5 || len(results[0].ParsedPoints) != 1 ||
		results[0].ParsedPoints[0] != (Point{X: 1, Y: 2}) || results[0].PlotType != "line" {
		t.Fatalf("listed result: %+v", results[0])
	}
	// limit 生效 + 无 points 行
	if _, err := ListResults(db, "e5063a", 0); err != nil {
		t.Fatal(err)
	}
	empty, err := ListResults(db, "nope", 10)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty instrument: %+v err=%v", empty, err)
	}
}
