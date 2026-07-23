IMPLEMENT Step 1 of the NL execute feature: Migration + Model + Repository.

## Context

Working directory: `/tmp/hiaf-lab-system`
Plan: `.hermes/plans/2026-07-22_nl-execute-results.md` (read it first)

Existing code:
- `go-server/instruments/handler.go` — instrument HTTP handlers
- `go-server/instruments/model.go` — all instrument models
- `go-server/instruments/worker.go` — InstrumentWorker, CommandResult
- `go-server/instruments/service.go` — Service.Interpret()
- `migrations/021_issue_log_run_id.up.sql` — latest migration

## Step 1a: Create migration

Create `migrations/022_instrument_results.up.sql`:
```sql
CREATE TABLE instrument_results (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id   TEXT NOT NULL,
    command_name    TEXT NOT NULL,
    scpi            TEXT NOT NULL DEFAULT '',
    raw_response    TEXT NOT NULL DEFAULT '',
    parsed_value    DOUBLE PRECISION,
    parsed_points   JSONB,
    plot_type       TEXT,
    error_code      TEXT,
    duration_ms     INTEGER NOT NULL DEFAULT 0,
    user_id         UUID NOT NULL REFERENCES users(id),
    request_id      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_instrument_results_instrument_id ON instrument_results(instrument_id);
CREATE INDEX idx_instrument_results_created_at ON instrument_results(created_at DESC);
```

Create `migrations/022_instrument_results.down.sql`:
```sql
DROP TABLE IF EXISTS instrument_results;
```

## Step 1b: Add models to model.go

Add after the existing types in `go-server/instruments/model.go`:

```go
// InstrumentResult records one instrument command execution result.
type InstrumentResult struct {
    ID           string    `json:"id"`
    InstrumentID string    `json:"instrument_id"`
    CommandName  string    `json:"command_name"`
    SCPI         string    `json:"scpi"`
    RawResponse  string    `json:"raw_response"`
    ParsedValue  *float64  `json:"parsed_value,omitempty"`
    ParsedPoints []Point   `json:"parsed_points,omitempty"`
    PlotType     string    `json:"plot_type,omitempty"`
    ErrorCode    string    `json:"error_code,omitempty"`
    DurationMS   int       `json:"duration_ms"`
    UserID       string    `json:"user_id"`
    RequestID    string    `json:"request_id"`
    CreatedAt    time.Time `json:"created_at"`
}

// Point is an (x, y) data point for scan plots.
type Point struct {
    X float64 `json:"x"`
    Y float64 `json:"y"`
}
```

## Step 1c: Create results_repo.go

Create `go-server/instruments/results_repo.go`:
```go
package instruments

import (
    "database/sql"
    "time"
)

// InsertResult writes one instrument execution result.
func InsertResult(db *sql.DB, r *InstrumentResult) error {
    r.CreatedAt = time.Now()
    _, err := db.Exec(`INSERT INTO instrument_results 
        (id, instrument_id, command_name, scpi, raw_response, parsed_value, parsed_points, plot_type, error_code, duration_ms, user_id, request_id, created_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
        r.ID, r.InstrumentID, r.CommandName, r.SCPI, r.RawResponse,
        r.ParsedValue, r.ParsedPoints, r.PlotType, r.ErrorCode,
        r.DurationMS, r.UserID, r.RequestID, r.CreatedAt)
    return err
}

// ListResults returns recent results for an instrument.
func ListResults(db *sql.DB, instrumentID string, limit int) ([]InstrumentResult, error) {
    rows, err := db.Query(`SELECT id, instrument_id, command_name, scpi, raw_response, 
        parsed_value, parsed_points, plot_type, error_code, duration_ms, user_id, request_id, created_at
        FROM instrument_results WHERE instrument_id=$1 ORDER BY created_at DESC LIMIT $2`,
        instrumentID, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var results []InstrumentResult
    for rows.Next() {
        var r InstrumentResult
        if err := rows.Scan(&r.ID, &r.InstrumentID, &r.CommandName, &r.SCPI, &r.RawResponse,
            &r.ParsedValue, &r.ParsedPoints, &r.PlotType, &r.ErrorCode,
            &r.DurationMS, &r.UserID, &r.RequestID, &r.CreatedAt); err != nil {
            return nil, err
        }
        results = append(results, r)
    }
    return results, rows.Err()
}
```

## Step 1d: Verify

After all changes, run:
```
cd go-server && go build ./... && go vet ./...
```

Make sure the build passes.
