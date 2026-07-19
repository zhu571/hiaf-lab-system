package experiences

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(authorID string, req CreateExperienceRequest) (*Experience, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin create experience: %w", err)
	}
	defer rollback(tx)

	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		return nil, fmt.Errorf("marshal experience tags: %w", err)
	}

	var out Experience
	err = scanExperience(tx.QueryRow(
		`INSERT INTO experiences (project_id, title, content, tags_json, author_id)
		 VALUES ($1, $2, $3, $4::jsonb, $5)
		 RETURNING id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		           published_at, created_at, updated_at`,
		nullableStringPtr(req.ProjectID), req.Title, req.Content, string(tagsJSON), authorID,
	), &out)
	if err != nil {
		return nil, fmt.Errorf("create experience: %w", err)
	}

	if err := replaceLinks(tx, out.ID, req.LinkedProjects); err != nil {
		return nil, err
	}
	out.LinkedProjects = append([]ExperienceProjectLink(nil), req.LinkedProjects...)

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create experience: %w", err)
	}
	return &out, nil
}

func (r *Repository) GetByID(id string) (*Experience, error) {
	var out Experience
	err := scanExperience(r.db.QueryRow(
		`SELECT id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		        published_at, created_at, updated_at
		 FROM experiences
		 WHERE id = $1`,
		id,
	), &out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get experience: %w", err)
	}
	links, err := r.getLinks(id)
	if err != nil {
		return nil, err
	}
	out.LinkedProjects = links
	return &out, nil
}

func (r *Repository) List(params ExperienceListParams) ([]Experience, int, error) {
	params.Page, params.PerPage = normalizePage(params.Page, params.PerPage)
	if params.PerPage > 100 {
		params.PerPage = 100
	}
	where, args, err := buildExperienceWhere(params)
	if err != nil {
		return nil, 0, err
	}

	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM experiences `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count experiences: %w", err)
	}

	args = append(args, params.PerPage, (params.Page-1)*params.PerPage)
	rows, err := r.db.Query(
		`SELECT id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		        published_at, created_at, updated_at
		 FROM experiences `+where+fmt.Sprintf(
			` ORDER BY CASE WHEN status = 'published' THEN published_at ELSE created_at END DESC
			   LIMIT $%d OFFSET $%d`, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list experiences: %w", err)
	}
	defer rows.Close()

	items, err := scanExperiences(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) Update(id string, req UpdateExperienceRequest) (*Experience, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin update experience: %w", err)
	}
	defer rollback(tx)

	var tags any
	if req.Tags != nil {
		tagsJSON, err := json.Marshal(req.Tags)
		if err != nil {
			return nil, fmt.Errorf("marshal experience tags: %w", err)
		}
		tags = string(tagsJSON)
	}

	var out Experience
	err = scanExperience(tx.QueryRow(
		`UPDATE experiences
		 SET title = COALESCE(NULLIF($2, ''), title),
		     content = COALESCE($3, content),
		     tags_json = COALESCE($4::jsonb, tags_json),
		     updated_at = now()
		 WHERE id = $1 AND status = 'candidate'
		 RETURNING id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		           published_at, created_at, updated_at`,
		id, stringPtrValue(req.Title), req.Content, tags,
	), &out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotCandidate
		}
		return nil, fmt.Errorf("update experience: %w", err)
	}
	if req.LinkedProjects != nil {
		if err := replaceLinks(tx, out.ID, req.LinkedProjects); err != nil {
			return nil, err
		}
		out.LinkedProjects = append([]ExperienceProjectLink(nil), req.LinkedProjects...)
	} else {
		links, err := getLinks(tx, out.ID)
		if err != nil {
			return nil, err
		}
		out.LinkedProjects = links
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update experience: %w", err)
	}
	return &out, nil
}

func (r *Repository) Publish(id, reviewerID string) (*Experience, error) {
	var out Experience
	err := scanExperience(r.db.QueryRow(
		`UPDATE experiences
		 SET status = 'published',
		     reviewer_id = $2,
		     published_at = now(),
		     updated_at = now()
		 WHERE id = $1 AND status = 'candidate'
		 RETURNING id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		           published_at, created_at, updated_at`,
		id, reviewerID,
	), &out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotCandidate
		}
		return nil, fmt.Errorf("publish experience: %w", err)
	}
	links, err := r.getLinks(id)
	if err != nil {
		return nil, err
	}
	out.LinkedProjects = links
	return &out, nil
}

func (r *Repository) Archive(id string) (*Experience, error) {
	var out Experience
	err := scanExperience(r.db.QueryRow(
		`UPDATE experiences
		 SET status = 'archived',
		     updated_at = now()
		 WHERE id = $1 AND status = 'published'
		 RETURNING id, project_id, title, content, tags_json, status, author_id, reviewer_id,
		           published_at, created_at, updated_at`,
		id,
	), &out)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotPublished
		}
		return nil, fmt.Errorf("archive experience: %w", err)
	}
	links, err := r.getLinks(id)
	if err != nil {
		return nil, err
	}
	out.LinkedProjects = links
	return &out, nil
}

func (r *Repository) getLinks(experienceID string) ([]ExperienceProjectLink, error) {
	return getLinks(r.db, experienceID)
}

func buildExperienceWhere(params ExperienceListParams) (string, []any, error) {
	args := []any{}
	parts := []string{}
	if strings.TrimSpace(params.ProjectID) != "" {
		args = append(args, strings.TrimSpace(params.ProjectID))
		if params.Status == StatusCandidate && params.UserRole != "admin" {
			parts = append(parts, fmt.Sprintf("project_id = $%d", len(args)))
		} else {
			parts = append(parts, fmt.Sprintf("(project_id = $%d OR project_id IS NULL)", len(args)))
		}
	} else {
		parts = append(parts, "project_id IS NULL")
	}
	if strings.TrimSpace(params.Status) != "" {
		args = append(args, strings.TrimSpace(params.Status))
		parts = append(parts, fmt.Sprintf("status = $%d", len(args)))
	}
	if params.Status == StatusCandidate && strings.TrimSpace(params.CandidateAuthorID) != "" {
		args = append(args, strings.TrimSpace(params.CandidateAuthorID))
		parts = append(parts, fmt.Sprintf("author_id = $%d", len(args)))
	}
	if len(params.Tags) > 0 {
		raw, err := json.Marshal(params.Tags)
		if err != nil {
			return "", nil, fmt.Errorf("marshal list tags: %w", err)
		}
		args = append(args, string(raw))
		parts = append(parts, fmt.Sprintf("tags_json @> $%d::jsonb", len(args)))
	}
	if strings.TrimSpace(params.Keyword) != "" {
		args = append(args, "%"+strings.TrimSpace(params.Keyword)+"%")
		parts = append(parts, fmt.Sprintf("(title ILIKE $%d OR content ILIKE $%d)", len(args), len(args)))
	}
	return "WHERE " + strings.Join(parts, " AND "), args, nil
}

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func getLinks(q queryer, experienceID string) ([]ExperienceProjectLink, error) {
	rows, err := q.Query(
		`SELECT project_id, relation
		 FROM experience_project_links
		 WHERE experience_id = $1
		 ORDER BY relation, project_id`,
		experienceID,
	)
	if err != nil {
		return nil, fmt.Errorf("get experience links: %w", err)
	}
	defer rows.Close()
	links := []ExperienceProjectLink{}
	for rows.Next() {
		var link ExperienceProjectLink
		if err := rows.Scan(&link.ProjectID, &link.Relation); err != nil {
			return nil, fmt.Errorf("scan experience link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate experience links: %w", err)
	}
	return links, nil
}

func replaceLinks(tx *sql.Tx, experienceID string, links []ExperienceProjectLink) error {
	if _, err := tx.Exec(`DELETE FROM experience_project_links WHERE experience_id = $1`, experienceID); err != nil {
		return fmt.Errorf("delete experience links: %w", err)
	}
	for _, link := range links {
		if _, err := tx.Exec(
			`INSERT INTO experience_project_links (experience_id, project_id, relation)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (experience_id, project_id) DO UPDATE SET relation = EXCLUDED.relation`,
			experienceID, link.ProjectID, link.Relation,
		); err != nil {
			return fmt.Errorf("insert experience link: %w", err)
		}
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExperience(row rowScanner, item *Experience) error {
	var projectID, reviewerID sql.NullString
	var publishedAt sql.NullTime
	var tagsJSON []byte
	if err := row.Scan(
		&item.ID, &projectID, &item.Title, &item.Content, &tagsJSON, &item.Status,
		&item.AuthorID, &reviewerID, &publishedAt, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return err
	}
	if projectID.Valid {
		item.ProjectID = &projectID.String
	}
	if reviewerID.Valid {
		item.ReviewerID = &reviewerID.String
	}
	if publishedAt.Valid {
		item.PublishedAt = &publishedAt.Time
	}
	if len(tagsJSON) > 0 {
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return fmt.Errorf("unmarshal experience tags: %w", err)
		}
	}
	if item.Tags == nil {
		item.Tags = []string{}
	}
	return nil
}

func scanExperiences(rows *sql.Rows) ([]Experience, error) {
	items := []Experience{}
	for rows.Next() {
		var item Experience
		if err := scanExperience(rows, &item); err != nil {
			return nil, fmt.Errorf("scan experience: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate experiences: %w", err)
	}
	return items, nil
}

func normalizePage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	return page, perPage
}

func nullableStringPtr(s *string) sql.NullString {
	if s == nil || strings.TrimSpace(*s) == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: strings.TrimSpace(*s), Valid: true}
}

func stringPtrValue(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}
