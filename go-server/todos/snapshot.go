package todos

import (
	"database/sql"
	"fmt"
	"time"
)

// 跨模块只读例外（已批准，集中收口于本文件）：
// scheduler 批量场景（每日为每个 active 用户聚合 issue / 推送 / 建号）逐行走 HTTP 成本不可接受，
// 因此允许直读 issues / project_members / users 三张表（只读不写，写仍走各自模块 HTTP API；
// 仓库先例：steptemplates/service.go:285 直读 project_members、logs/repository.go:80 JOIN users）。
// 禁止在本文件之外出现任何跨表 SQL；若评审不认可此例外，替换为调用各模块 API 即可。

// IssueSnapshot 是 issue 聚合所需的最小字段。
type IssueSnapshot struct {
	ID         string
	Title      string
	Severity   string
	Status     string
	AssigneeID *string
	OccurredAt time.Time
}

// UserSnapshot 是 active 用户扫描/推送标注/ntfy 建号所需字段。
type UserSnapshot struct {
	ID          string
	Username    string
	DisplayName string
	Disabled    bool
	LockedUntil *time.Time
}

// Snapshot 封装全部跨表只读查询。
type Snapshot struct {
	db *sql.DB
}

func NewSnapshot(db *sql.DB) *Snapshot {
	return &Snapshot{db: db}
}

// ActiveUsers 返回 active 用户（disabled=false 且未锁定/锁已过期），与 users 表字段对齐（001/010 迁移）。
func (s *Snapshot) ActiveUsers() ([]UserSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, username, display_name, disabled, locked_until
		 FROM users
		 WHERE disabled = false AND (locked_until IS NULL OR locked_until <= now())`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active users: %w", err)
	}
	defer rows.Close()
	out := []UserSnapshot{}
	for rows.Next() {
		var u UserSnapshot
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Disabled, &u.LockedUntil); err != nil {
			return nil, fmt.Errorf("scan active user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserByID 返回单个用户快照（不存在时返回 nil, nil）。
func (s *Snapshot) UserByID(userID string) (*UserSnapshot, error) {
	var u UserSnapshot
	err := s.db.QueryRow(
		`SELECT id, username, display_name, disabled, locked_until FROM users WHERE id = $1`,
		userID,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Disabled, &u.LockedUntil)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get user snapshot: %w", err)
	}
	return &u, nil
}

// OpenIssuesForUser 返回指定用户 assignee 的 open/in_progress issue（聚合范围，方案 §2）。
func (s *Snapshot) OpenIssuesForUser(userID string) ([]IssueSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT id, title, severity, status, assignee_id, occurred_at
		 FROM issues
		 WHERE assignee_id = $1 AND status IN ('open','in_progress')`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list open issues for user: %w", err)
	}
	defer rows.Close()
	out := []IssueSnapshot{}
	for rows.Next() {
		var i IssueSnapshot
		if err := rows.Scan(&i.ID, &i.Title, &i.Severity, &i.Status, &i.AssigneeID, &i.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan issue snapshot: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// MyProjectIDs 返回用户 status='active' 的项目成员身份对应的项目 ID（共享可见性/权限判定用，含 viewer）。
func (s *Snapshot) MyProjectIDs(userID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT project_id FROM project_members WHERE user_id = $1 AND status = 'active'`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list my project ids: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan project id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ProjectRole 返回用户在项目的 active 角色；非成员返回 ("", false, nil)。
func (s *Snapshot) ProjectRole(userID, projectID string) (string, bool, error) {
	var role string
	err := s.db.QueryRow(
		`SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2 AND status = 'active'`,
		projectID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query project role: %w", err)
	}
	return role, true, nil
}
