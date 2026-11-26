package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Repository 只访问 assistant_* 表，不 join 其他模块业务表。
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateSession(ctx context.Context, s *Session) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO assistant_sessions (user_id, project_id, title)
		 VALUES ($1::uuid, NULLIF($2, '')::uuid, $3)
		 RETURNING id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at`,
		s.UserID, projectID(s.ProjectID), s.Title,
	).Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
		&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt)
}

func (r *Repository) ListSessions(ctx context.Context, userID, status string, limit int) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at
		 FROM assistant_sessions
		 WHERE user_id = $1::uuid AND ($2 = '' OR status = $2)
		 ORDER BY COALESCE(last_message_at, created_at) DESC
		 LIMIT $3`, userID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
			&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetSession(ctx context.Context, userID, id string) (*Session, error) {
	var s Session
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at
		 FROM assistant_sessions WHERE id = $1::uuid AND user_id = $2::uuid`, id, userID,
	).Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
		&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) UpdateSession(ctx context.Context, userID, id string, title, status *string) (*Session, error) {
	var s Session
	err := r.db.QueryRowContext(ctx,
		`UPDATE assistant_sessions
		 SET title = COALESCE($3, title), status = COALESCE($4, status), updated_at = now()
		 WHERE id = $1::uuid AND user_id = $2::uuid
		 RETURNING id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at`,
		id, userID, title, status,
	).Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
		&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) ListMessages(ctx context.Context, userID, sessionID string) ([]Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT m.id, m.session_id, m.run_id, m.role, m.content, m.content_json, m.created_at
		 FROM assistant_messages m
		 JOIN assistant_sessions s ON s.id = m.session_id
		 WHERE m.session_id = $1::uuid AND s.user_id = $2::uuid
		 ORDER BY m.created_at`, sessionID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var raw []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.RunID, &m.Role, &m.Content, &raw, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m.ContentJSON)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) AddUserMessage(ctx context.Context, userID, sessionID, content string) (*Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM assistant_sessions WHERE id = $1::uuid AND user_id = $2::uuid)`,
		sessionID, userID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	var m Message
	err = tx.QueryRowContext(ctx,
		`INSERT INTO assistant_messages (session_id, role, content)
		 VALUES ($1::uuid, 'user', $2)
		 RETURNING id, session_id, run_id, role, content, created_at`,
		sessionID, content,
	).Scan(&m.ID, &m.SessionID, &m.RunID, &m.Role, &m.Content, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE assistant_sessions
		 SET last_message_at = now(), updated_at = now(),
		     title = CASE WHEN title = '' THEN $2 ELSE title END
		 WHERE id = $1::uuid`, sessionID, truncateTitle(content)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit message: %w", err)
	}
	return &m, nil
}

func (r *Repository) ListVisibleSessions(ctx context.Context, userID, status string, limit int) ([]Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at
		 FROM assistant_sessions
		 WHERE (user_id = $1::uuid OR project_id IS NOT NULL) AND ($2 = '' OR status = $2)
		 ORDER BY COALESCE(last_message_at, created_at) DESC
		 LIMIT $3`, userID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
			&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetSessionAny(ctx context.Context, id string) (*Session, error) {
	var s Session
	err := r.db.QueryRowContext(ctx,
		`SELECT id, user_id, project_id, title, status, summary, last_message_at, created_at, updated_at
		 FROM assistant_sessions WHERE id = $1::uuid`, id,
	).Scan(&s.ID, &s.UserID, &s.ProjectID, &s.Title, &s.Status, &s.Summary,
		&s.LastMessageAt, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) ListMessagesBySession(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, run_id, role, content, content_json, created_at
		 FROM assistant_messages WHERE session_id = $1::uuid ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var raw []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.RunID, &m.Role, &m.Content, &raw, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m.ContentJSON)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) CreateRun(ctx context.Context, run *Run) error {
	return r.db.QueryRowContext(ctx,
		`INSERT INTO assistant_runs (session_id, user_id, status, model, prompt_version)
		 VALUES ($1::uuid, $2::uuid, 'running', $3, $4)
		 RETURNING id, session_id, user_id, status, model, prompt_version,
		           input_tokens, output_tokens, error, started_at, finished_at`,
		run.SessionID, run.UserID, run.Model, run.PromptVersion,
	).Scan(&run.ID, &run.SessionID, &run.UserID, &run.Status, &run.Model, &run.PromptVersion,
		&run.InputTokens, &run.OutputTokens, &run.Error, &run.StartedAt, &run.FinishedAt)
}

func (r *Repository) FinishRun(ctx context.Context, runID, status, errMsg string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE assistant_runs
		 SET status = $2, error = NULLIF($3, ''), finished_at = now()
		 WHERE id = $1::uuid`, runID, status, errMsg)
	return err
}

func (r *Repository) AddAssistantMessage(ctx context.Context, sessionID, runID, content string, contentJSON []byte) (*Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var m Message
	err = tx.QueryRowContext(ctx,
		`INSERT INTO assistant_messages (session_id, run_id, role, content, content_json)
		 VALUES ($1::uuid, $2::uuid, 'assistant', $3, $4)
		 RETURNING id, session_id, run_id, role, content, created_at`,
		sessionID, runID, content, contentJSON,
	).Scan(&m.ID, &m.SessionID, &m.RunID, &m.Role, &m.Content, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE assistant_sessions SET last_message_at = now(), updated_at = now() WHERE id = $1::uuid`,
		sessionID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit assistant message: %w", err)
	}
	_ = json.Unmarshal(contentJSON, &m.ContentJSON)
	return &m, nil
}

func (r *Repository) AddUserMessageToSession(ctx context.Context, sessionID, content string) (*Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var m Message
	err = tx.QueryRowContext(ctx,
		`INSERT INTO assistant_messages (session_id, role, content)
		 VALUES ($1::uuid, 'user', $2)
		 RETURNING id, session_id, run_id, role, content, created_at`,
		sessionID, content,
	).Scan(&m.ID, &m.SessionID, &m.RunID, &m.Role, &m.Content, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE assistant_sessions
		 SET last_message_at = now(), updated_at = now(),
		     title = CASE WHEN title = '' THEN $2 ELSE title END
		 WHERE id = $1::uuid`, sessionID, truncateTitle(content)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit message: %w", err)
	}
	return &m, nil
}

func projectID(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func truncateTitle(content string) string {
	runes := []rune(content)
	if len(runes) > 40 {
		runes = runes[:40]
	}
	return string(runes)
}
