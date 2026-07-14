package projects

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(p *Project) (*Project, error) {
	tagsJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	var project Project
	err = scanProject(r.db.QueryRow(
		`INSERT INTO projects
		 (code, name, short_name, description, status, visibility, owner_user_id,
		  start_date, target_end_date, default_category, tags_json, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::date, NULLIF($9, '')::date, $10, $11, $12)
		 RETURNING id, code, name, short_name, description, status, visibility, owner_user_id,
		           start_date, target_end_date, completed_at, archived_at, default_category, tags_json,
		           created_by, created_at, updated_at`,
		p.Code, p.Name, p.ShortName, p.Description, p.Status, p.Visibility, p.OwnerUserID,
		stringPtrValue(p.StartDate), stringPtrValue(p.TargetEndDate), p.DefaultCategory, tagsJSON, p.CreatedBy,
	), &project)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return finishProject(&project)
}

func (r *Repository) GetByID(id string) (*Project, error) {
	return r.get(`WHERE id = $1`, id)
}

func (r *Repository) GetByCode(code string) (*Project, error) {
	return r.get(`WHERE code = $1`, code)
}

func (r *Repository) List(userID, status string, includeAll bool) ([]Project, error) {
	args := []any{}
	query := `SELECT p.id, p.code, p.name, p.short_name, p.description, p.status, p.visibility,
	                 p.owner_user_id, p.start_date, p.target_end_date, p.completed_at, p.archived_at, p.default_category,
	                 p.tags_json, p.created_by, p.created_at, p.updated_at
	          FROM projects p`
	if !includeAll {
		args = append(args, userID)
		query += ` JOIN project_members pm ON pm.project_id = p.id
		          AND pm.user_id = $1 AND pm.status = 'active'`
	}
	if status != "" {
		args = append(args, status)
		query += fmt.Sprintf(" WHERE p.status = $%d", len(args))
	}
	query += ` ORDER BY p.created_at DESC`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		project, err := finishProject(&p)
		if err != nil {
			return nil, err
		}
		out = append(out, *project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return out, nil
}

func (r *Repository) Update(p *Project) (*Project, error) {
	tagsJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}

	var project Project
	err = scanProject(r.db.QueryRow(
		`UPDATE projects
		 SET name = $2,
		     short_name = $3,
		     description = $4,
		     visibility = $5,
		     start_date = NULLIF($6, '')::date,
		     target_end_date = NULLIF($7, '')::date,
		     default_category = $8,
		     tags_json = $9,
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, code, name, short_name, description, status, visibility, owner_user_id,
		           start_date, target_end_date, completed_at, archived_at, default_category, tags_json,
		           created_by, created_at, updated_at`,
		p.ID, p.Name, p.ShortName, p.Description, p.Visibility, stringPtrValue(p.StartDate),
		stringPtrValue(p.TargetEndDate), p.DefaultCategory, tagsJSON,
	), &project)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update project: %w", err)
	}
	return finishProject(&project)
}

func (r *Repository) UpdateStatus(id, status string) (*Project, error) {
	var project Project
	err := scanProject(r.db.QueryRow(
		`UPDATE projects
		 SET status = $2,
		     completed_at = CASE WHEN $2 = 'completed' THEN COALESCE(completed_at, now()) ELSE completed_at END,
		     archived_at = CASE WHEN $2 = 'archived' THEN COALESCE(archived_at, now()) ELSE archived_at END,
		     updated_at = now()
		 WHERE id = $1
		 RETURNING id, code, name, short_name, description, status, visibility, owner_user_id,
		           start_date, target_end_date, completed_at, archived_at, default_category, tags_json,
		           created_by, created_at, updated_at`,
		id, status,
	), &project)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update project status: %w", err)
	}
	return finishProject(&project)
}

func (r *Repository) AddMember(projectID, userID, role, addedBy string) (*ProjectMember, error) {
	var m ProjectMember
	err := r.db.QueryRow(
		`INSERT INTO project_members (project_id, user_id, role, status, added_by)
		 VALUES ($1, $2, $3, 'active', $4)
		 ON CONFLICT (project_id, user_id)
		 DO UPDATE SET role = EXCLUDED.role, status = 'active', added_by = EXCLUDED.added_by
		 RETURNING project_id, user_id, role, status, joined_at, added_by`,
		projectID, userID, role, addedBy,
	).Scan(&m.ProjectID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.AddedBy)
	if err != nil {
		return nil, fmt.Errorf("add project member: %w", err)
	}
	return &m, nil
}

func (r *Repository) RemoveMember(projectID, userID string) error {
	if _, err := r.db.Exec(`DELETE FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID); err != nil {
		return fmt.Errorf("remove project member: %w", err)
	}
	return nil
}

func (r *Repository) UpdateMemberRole(projectID, userID, role string) (*ProjectMember, error) {
	var m ProjectMember
	err := r.db.QueryRow(
		`UPDATE project_members SET role = $3
		 WHERE project_id = $1 AND user_id = $2
		 RETURNING project_id, user_id, role, status, joined_at, added_by`,
		projectID, userID, role,
	).Scan(&m.ProjectID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.AddedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update project member role: %w", err)
	}
	return &m, nil
}

func (r *Repository) ListMembers(projectID string) ([]ProjectMember, error) {
	rows, err := r.db.Query(
		`SELECT project_id, user_id, role, status, joined_at, added_by
		 FROM project_members
		 WHERE project_id = $1
		 ORDER BY role, joined_at`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list project members: %w", err)
	}
	defer rows.Close()

	var members []ProjectMember
	for rows.Next() {
		var m ProjectMember
		if err := rows.Scan(&m.ProjectID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.AddedBy); err != nil {
			return nil, fmt.Errorf("scan project member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project members: %w", err)
	}
	return members, nil
}

func (r *Repository) GetMember(projectID, userID string) (*ProjectMember, error) {
	var m ProjectMember
	err := r.db.QueryRow(
		`SELECT project_id, user_id, role, status, joined_at, added_by
		 FROM project_members
		 WHERE project_id = $1 AND user_id = $2`,
		projectID, userID,
	).Scan(&m.ProjectID, &m.UserID, &m.Role, &m.Status, &m.JoinedAt, &m.AddedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get project member: %w", err)
	}
	return &m, nil
}

func (r *Repository) IsCodeTaken(code string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM projects WHERE code = $1)`, code).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check project code: %w", err)
	}
	return exists, nil
}

func (r *Repository) UserExists(userID string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check user exists: %w", err)
	}
	return exists, nil
}

func (r *Repository) GetStats(projectID string) (memberCount, openIssueCount, logCount int, err error) {
	err = r.db.QueryRow(
		`SELECT COUNT(*) FROM project_members WHERE project_id = $1 AND status = 'active'`,
		projectID,
	).Scan(&memberCount)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count project members: %w", err)
	}
	// TODO: Phase 2 issues 模块实现后替换为 issues repository 调用。
	err = r.db.QueryRow(
		`SELECT COUNT(*) FROM issues WHERE project_id = $1 AND status NOT IN ('resolved', 'closed')`,
		projectID,
	).Scan(&openIssueCount)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count project issues: %w", err)
	}
	// TODO: Phase 2 logs 模块实现后替换为 logs repository 调用。
	err = r.db.QueryRow(
		`SELECT COUNT(*) FROM logs WHERE project_id = $1`,
		projectID,
	).Scan(&logCount)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("count project logs: %w", err)
	}
	return memberCount, openIssueCount, logCount, nil
}

func (r *Repository) CountOwners(projectID string) (int, error) {
	var count int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM project_members
		 WHERE project_id = $1 AND role = 'owner' AND status = 'active'`,
		projectID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count project owners: %w", err)
	}
	return count, nil
}

func (r *Repository) get(where string, arg any) (*Project, error) {
	var project Project
	err := scanProject(r.db.QueryRow(
		`SELECT id, code, name, short_name, description, status, visibility, owner_user_id,
		        start_date, target_end_date, completed_at, archived_at, default_category, tags_json,
		        created_by, created_at, updated_at
		 FROM projects `+where,
		arg,
	), &project)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	return finishProject(&project)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(row rowScanner, p *Project) error {
	var startDate, targetEndDate sql.NullTime
	var completedAt, archivedAt sql.NullTime
	err := row.Scan(
		&p.ID, &p.Code, &p.Name, &p.ShortName, &p.Description, &p.Status, &p.Visibility,
		&p.OwnerUserID, &startDate, &targetEndDate, &completedAt, &archivedAt, &p.DefaultCategory, &p.TagsJSON,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	p.StartDate = dateString(startDate)
	p.TargetEndDate = dateString(targetEndDate)
	p.CompletedAt = timePtr(completedAt)
	p.ArchivedAt = timePtr(archivedAt)
	return nil
}

func finishProject(p *Project) (*Project, error) {
	if len(p.TagsJSON) == 0 {
		p.Tags = []string{}
		return p, nil
	}
	if err := json.Unmarshal(p.TagsJSON, &p.Tags); err != nil {
		return nil, fmt.Errorf("unmarshal project tags: %w", err)
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	return p, nil
}

func stringPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func dateString(t sql.NullTime) *string {
	if !t.Valid {
		return nil
	}
	s := t.Time.Format(time.DateOnly)
	return &s
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
