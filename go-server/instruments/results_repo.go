package instruments

import (
	"database/sql"
	"encoding/json"
	"time"
)

// InsertResult writes one instrument execution result.
func InsertResult(db *sql.DB, r *InstrumentResult) error {
	r.CreatedAt = time.Now()
	var pointsJSON []byte
	if len(r.ParsedPoints) > 0 {
		var err error
		pointsJSON, err = json.Marshal(r.ParsedPoints)
		if err != nil {
			return err
		}
	}
	_, err := db.Exec(`INSERT INTO instrument_results 
		(id, instrument_id, command_name, scpi, raw_response, parsed_value, parsed_points, plot_type, error_code, duration_ms, user_id, request_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13)`,
		r.ID, r.InstrumentID, r.CommandName, r.SCPI, r.RawResponse,
		r.ParsedValue, pointsJSON, r.PlotType, r.ErrorCode,
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
		var pointsJSON []byte
		if err := rows.Scan(&r.ID, &r.InstrumentID, &r.CommandName, &r.SCPI, &r.RawResponse,
			&r.ParsedValue, &pointsJSON, &r.PlotType, &r.ErrorCode,
			&r.DurationMS, &r.UserID, &r.RequestID, &r.CreatedAt); err != nil {
			return nil, err
		}
		if len(pointsJSON) > 0 {
			if err := json.Unmarshal(pointsJSON, &r.ParsedPoints); err != nil {
				return nil, err
			}
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
