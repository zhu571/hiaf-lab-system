package ask

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// historyColumns 列表/明细共用列（不含 rows 大字段；明细另行取 rows）。
const historyColumns = `id, user_id, request_id, question, answer, sql_text, table_name,
	columns, row_count, duration_ms, model, created_at`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

// SaveAsk 插入一条问答历史（rows 快照 ≤256KB 由 service 层封顶）。
func (r *Repository) SaveAsk(h *AskHistory) error {
	columnsJSON, err := json.Marshal(h.Columns)
	if err != nil {
		return fmt.Errorf("marshal columns: %w", err)
	}
	rowsJSON, err := json.Marshal(h.Rows)
	if err != nil {
		return fmt.Errorf("marshal rows: %w", err)
	}
	err = r.db.QueryRow(
		`INSERT INTO ask_history (user_id, request_id, question, answer, sql_text, table_name,
		                         columns, rows, row_count, duration_ms, model)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, created_at`,
		h.UserID, h.RequestID, h.Question, h.Answer, h.SQLText, h.TableName,
		columnsJSON, rowsJSON, h.RowCount, h.DurationMS, h.Model,
	).Scan(&h.ID, &h.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert ask_history: %w", err)
	}
	return nil
}

// ListHistory 我的问答历史列表（不含 rows 大字段），按时间倒序分页。
func (r *Repository) ListHistory(userID string, limit, offset int) ([]AskHistory, int, error) {
	var total int
	if err := r.db.QueryRow(`SELECT count(*) FROM ask_history WHERE user_id=$1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count ask_history: %w", err)
	}
	rows, err := r.db.Query(
		`SELECT `+historyColumns+` FROM ask_history
		 WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list ask_history: %w", err)
	}
	defer rows.Close()
	items, err := scanHistory(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetHistory 按 id 取完整记录（含 rows 快照），供历史详情还原表格。
func (r *Repository) GetHistory(id string) (*AskHistory, error) {
	return r.getHistoryWhere(`WHERE id=$1`, id)
}

// GetHistoryByUser 按 id + 归属校验取完整记录（仅本人可见）。
func (r *Repository) GetHistoryByUser(id, userID string) (*AskHistory, error) {
	return r.getHistoryWhere(`WHERE id=$1 AND user_id=$2`, id, userID)
}

func (r *Repository) getHistoryWhere(where string, args ...any) (*AskHistory, error) {
	row := r.db.QueryRow(`SELECT `+historyColumns+`, rows FROM ask_history `+where, args...)
	h := &AskHistory{}
	var columns, rows []byte
	if err := row.Scan(&h.ID, &h.UserID, &h.RequestID, &h.Question, &h.Answer, &h.SQLText,
		&h.TableName, &columns, &h.RowCount, &h.DurationMS, &h.Model, &h.CreatedAt, &rows); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get ask_history: %w", err)
	}
	if err := json.Unmarshal(columns, &h.Columns); err != nil {
		return nil, fmt.Errorf("unmarshal columns: %w", err)
	}
	// 034 保留策略将 90 天前快照置 NULL：NULL 列扫描得到 nil []byte，
	// json.Unmarshal(nil, ...) 会报 unexpected end of JSON input，必须跳过。
	if len(rows) > 0 {
		if err := json.Unmarshal(rows, &h.Rows); err != nil {
			return nil, fmt.Errorf("unmarshal rows: %w", err)
		}
	}
	return h, nil
}

// contextDailyReport 是 AI-3 上下文用的日报摘要行（只读单表 SELECT，ask 全库只读例外）。
type contextDailyReport struct {
	ReportDate string
	Summary    string
	RawText    string
}

// RecentDailyReports 最近 days 天的日报（AI-3 上下文数据源，只读单表 SELECT，
// LIMIT 封顶防 token 浪费；日报内容可能为空串，由 service 层回退/裁剪）。
func (r *Repository) RecentDailyReports(ctx context.Context, days, limit int) ([]contextDailyReport, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT report_date, summary, raw_text FROM daily_reports
		 WHERE report_date >= CURRENT_DATE - $1::int
		 ORDER BY report_date DESC, created_at DESC
		 LIMIT $2`, days, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent daily_reports: %w", err)
	}
	defer rows.Close()
	var items []contextDailyReport
	for rows.Next() {
		var it contextDailyReport
		if err := rows.Scan(&it.ReportDate, &it.Summary, &it.RawText); err != nil {
			return nil, fmt.Errorf("scan daily_report: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily_reports: %w", err)
	}
	return items, nil
}

// contextProject 是 AI-3 上下文用的项目行（只读单表 SELECT）。
type contextProject struct {
	Code   string
	Name   string
	Status string
}

// RecentProjects 最近创建的项目（AI-3 上下文数据源，前 limit 个，只读单表 SELECT）。
func (r *Repository) RecentProjects(ctx context.Context, limit int) ([]contextProject, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT code, name, status FROM projects
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent projects: %w", err)
	}
	defer rows.Close()
	var items []contextProject
	for rows.Next() {
		var it contextProject
		if err := rows.Scan(&it.Code, &it.Name, &it.Status); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return items, nil
}

// NullifyOldSnapshots 将 cutoff 之前且快照非空的行置 NULL（保留策略，P2-3）。
// 返回受影响行数；置 NULL 后空间由 autovacuum 自然回收，无需手动 VACUUM。
func (r *Repository) NullifyOldSnapshots(cutoff time.Time) (int64, error) {
	res, err := r.db.Exec(
		`UPDATE ask_history SET rows = NULL WHERE created_at < $1 AND rows IS NOT NULL`, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("nullify ask_history snapshots: %w", err)
	}
	return res.RowsAffected()
}

func scanHistory(rows *sql.Rows) ([]AskHistory, error) {
	var items []AskHistory
	for rows.Next() {
		h := AskHistory{}
		var columns []byte
		if err := rows.Scan(&h.ID, &h.UserID, &h.RequestID, &h.Question, &h.Answer, &h.SQLText,
			&h.TableName, &columns, &h.RowCount, &h.DurationMS, &h.Model, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ask_history: %w", err)
		}
		if err := json.Unmarshal(columns, &h.Columns); err != nil {
			return nil, fmt.Errorf("unmarshal columns: %w", err)
		}
		items = append(items, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ask_history: %w", err)
	}
	return items, nil
}
