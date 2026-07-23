IMPLEMENT Step 2 of the NL execute feature: NLExecute handler + route registration.

## Context

Working directory: `/tmp/hiaf-lab-system`

Just completed Step 1: migration `022_instrument_results`, `InstrumentResult` model, `Point` struct, `results_repo.go` with `InsertResult` and `ListResults`.

## Step 2a: Add NLExecute handler to handler.go

Read `go-server/instruments/handler.go` first. Add a new method `NLExecute` after `InterpretCommand`.

The flow: translate NL → submit command to worker → wait for result → parse scan data → store result → return.

```go
// NLExecute translates natural language, executes the command, and returns the result.
func (h *Handler) NLExecute(w http.ResponseWriter, r *http.Request) {
    if !requireIdempotencyKey(w, r) {
        return
    }
    id := chi.URLParam(r, "id")
    worker, ok := h.workers[id]
    if !ok {
        common.WriteError(w, r, http.StatusNotFound, "instrument_not_found", "仪器不存在", nil)
        return
    }

    var req NLCommandRequest
    decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
    decoder.DisallowUnknownFields()
    if err := decoder.Decode(&req); err != nil || strings.TrimSpace(req.Input) == "" || len(req.Input) > 1000 || len(req.History) > 10 {
        common.WriteError(w, r, http.StatusBadRequest, "bad_request", "自然语言请求格式无效", nil)
        return
    }
    for _, item := range req.History {
        if (item.Role != "user" && item.Role != "assistant") || len(item.Content) > 1000 {
            common.WriteError(w, r, http.StatusBadRequest, "bad_request", "对话历史格式无效", nil)
            return
        }
    }

    claims := middleware.GetUserClaims(r.Context())
    if claims == nil {
        common.WriteError(w, r, http.StatusUnauthorized, "unauthorized", "未登录", nil)
        return
    }

    // Role check: maintainer or admin (same as /commands)
    role := middleware.GetUserRole(r.Context())
    if role != "maintainer" && role != "admin" {
        common.WriteError(w, r, http.StatusForbidden, "forbidden", "需要维护者或管理员权限", nil)
        return
    }

    // Rate limit for NL
    if !h.allowNL(claims.UserID) {
        common.WriteError(w, r, http.StatusTooManyRequests, "rate_limited", "AI 翻译请求过于频繁", nil)
        return
    }

    middleware.SetAuditAction(r.Context(), "instrument.nl.executed")

    // 1. Interpret NL input via py-agent
    candidate, err := h.svc.Interpret(r.Context(), id, req)
    if err != nil {
        slog.Error("instrument interpretation failed", "error", err, "instrument_id", id, "request_id", common.GetRequestID(r.Context()))
        common.WriteError(w, r, http.StatusBadGateway, "agent_unavailable", "AI 翻译服务不可用", nil)
        return
    }

    // 2. Only execute if status is "ok" (has valid command)
    if candidate.Status != "ok" {
        // Return clarification/rejection without executing
        common.WriteSuccess(w, r, candidate)
        return
    }

    // 3. Build QueueCommand and submit to worker
    cmd := &QueueCommand{
        Name:       candidate.Command,
        Params:     candidate.Params,
        Risk:       candidate.Risk,
        ResponseCh: make(chan CommandResult, 1),
    }

    if err := worker.Submit(cmd); err != nil {
        slog.Error("instrument command submit failed", "error", err, "instrument_id", id)
        common.WriteError(w, r, http.StatusServiceUnavailable, "instrument_unavailable", err.Error(), nil)
        return
    }

    // 4. Wait for result with 30s timeout
    var result CommandResult
    select {
    case result = <-cmd.ResponseCh:
    case <-time.After(30 * time.Second):
        common.WriteError(w, r, http.StatusGatewayTimeout, "timeout", "命令执行超时 (30s)", nil)
        return
    }

    // 5. Parse scan data if applicable
    var points []Point
    var plotType string
    var parsedValue *float64

    def, defErr := GetCommand(id, candidate.Command)
    if defErr == nil && def.Returns != nil {
        if returnsStr, ok := def.Returns.(string); ok && returnsStr == "array" && result.Response != "" {
            points, plotType = parseScanData(result.Response)
        }
    }
    if points == nil && result.Response != "" {
        if v, err := strconv.ParseFloat(strings.TrimSpace(result.Response), 64); err == nil {
            parsedValue = &v
        }
    }

    // 6. Store result
    requestID := common.GetRequestID(r.Context())
    errCode := ""
    if result.Error != nil {
        errCode = result.Error.Error()
    }
    storeErr := InsertResult(h.db, &InstrumentResult{
        ID:           uuid.New().String(),
        InstrumentID: id,
        CommandName:  candidate.Command,
        SCPI:         candidate.SCPI,
        RawResponse:  result.Response,
        ParsedValue:  parsedValue,
        ParsedPoints: points,
        PlotType:     plotType,
        ErrorCode:    errCode,
        DurationMS:   int(result.Duration.Milliseconds()),
        UserID:       claims.UserID,
        RequestID:    requestID,
    })
    if storeErr != nil {
        slog.Error("store instrument result failed", "error", storeErr)
        // Don't fail the request — still return the result
    }

    // 7. Return combined response
    common.WriteSuccess(w, r, map[string]any{
        "status":        "ok",
        "command":       candidate.Command,
        "scpi":          candidate.SCPI,
        "explanation":   candidate.Explanation,
        "response":      result.Response,
        "parsed_value":  parsedValue,
        "parsed_points": points,
        "plot_type":     plotType,
        "duration_ms":   int(result.Duration.Milliseconds()),
        "error":         errCode,
    })
}

// parseScanData parses a CSV-like instrument response into (x,y) points.
// Returns points and plot_type ("line") or nil if not recognized as scan data.
func parseScanData(raw string) ([]Point, string) {
    trimmed := strings.TrimSpace(raw)
    if trimmed == "" {
        return nil, ""
    }
    parts := strings.Split(trimmed, ",")
    if len(parts) < 4 || len(parts)%2 != 0 {
        return nil, ""
    }
    points := make([]Point, 0, len(parts)/2)
    for i := 0; i+1 < len(parts); i += 2 {
        x, err1 := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
        y, err2 := strconv.ParseFloat(strings.TrimSpace(parts[i+1]), 64)
        if err1 != nil || err2 != nil {
            return nil, ""
        }
        points = append(points, Point{X: x, Y: y})
    }
    return points, "line"
}
```

## Step 2b: Modify NewHandler signature

Change NewHandler to accept a *sql.DB:

```go
func NewHandler(svc *Service, db *sql.DB, workerMaps ...map[string]*InstrumentWorker) *Handler {
    workers := map[string]*InstrumentWorker{}
    for _, m := range workerMaps {
        for k, v := range m {
            workers[k] = v
        }
    }
    return &Handler{
        svc:     svc,
        db:      db,
        workers: workers,
        nlCalls: make(map[string][]time.Time),
    }
}
```

And add `db *sql.DB` field to the Handler struct.

## Step 2c: Register route in main.go

In `go-server/main.go`, find the existing instrument routes and add:

```go
r.Post("/api/v1/instruments/{id}/nl-execute", instrumentsH.NLExecute)
```

## Step 2d: Add necessary imports

Make sure handler.go imports:
```go
"database/sql"
"strconv"
"strings"
"time"
"github.com/google/uuid"
```

## Step 2e: Verify

```
cd go-server && go build ./... && go vet ./...
```

Fix any compilation errors.
