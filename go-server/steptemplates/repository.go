package steptemplates

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const templateColumns = `id, name, kind, description, source_prompt, ai_generated, created_by, created_at, updated_at`
const itemColumns = `id, template_id, name, description, step_order, depends_on_order, meta, created_at, updated_at`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(template *StepTemplate, items []StepTemplateItem) (*StepTemplate, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin create template: %w", err)
	}
	defer tx.Rollback()

	var t StepTemplate
	err = tx.QueryRow(
		`INSERT INTO step_templates (name, kind, description, source_prompt, ai_generated, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 RETURNING `+templateColumns,
		template.Name, template.Kind, nullText(template.Description),
		nullText(template.SourcePrompt), template.AIGenerated, template.CreatedBy,
	).Scan(&t.ID, &t.Name, &t.Kind, &t.Description, &t.SourcePrompt, &t.AIGenerated,
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert template: %w", err)
	}

	for _, item := range items {
		var it StepTemplateItem
		metaJSON, err := json.Marshal(itemMeta(item.Meta))
		if err != nil {
			return nil, fmt.Errorf("marshal item meta: %w", err)
		}
		err = tx.QueryRow(
			`INSERT INTO step_template_items (template_id, name, description, step_order, depends_on_order, meta)
			 VALUES ($1,$2,$3,$4,$5,$6)
			 RETURNING `+itemColumns,
			t.ID, item.Name, nullText(item.Description), item.StepOrder, item.DependsOnOrder, metaJSON,
		).Scan(&it.ID, &it.TemplateID, &it.Name, &it.Description, &it.StepOrder,
			&it.DependsOnOrder, &it.Meta, &it.CreatedAt, &it.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert template item: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create template: %w", err)
	}

	t.Items, _ = r.getItems(t.ID)
	return &t, nil
}

func (r *Repository) GetByID(id string) (*StepTemplate, error) {
	var t StepTemplate
	var description, sourcePrompt sql.NullString
	var createdBy sql.NullString
	err := r.db.QueryRow(
		`SELECT `+templateColumns+` FROM step_templates WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&t.ID, &t.Name, &t.Kind, &description, &sourcePrompt, &t.AIGenerated,
		&createdBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	t.Description = description.String
	t.SourcePrompt = sourcePrompt.String
	if createdBy.Valid {
		t.CreatedBy = &createdBy.String
	}
	items, err := r.getItems(t.ID)
	if err != nil {
		return nil, err
	}
	t.Items = items
	return &t, nil
}

func (r *Repository) GetTemplateWithItems(id string) (*StepTemplate, []StepTemplateItem, error) {
	var t StepTemplate
	var description, sourcePrompt sql.NullString
	var createdBy sql.NullString
	err := r.db.QueryRow(
		`SELECT `+templateColumns+` FROM step_templates WHERE id=$1 AND deleted_at IS NULL`, id,
	).Scan(&t.ID, &t.Name, &t.Kind, &description, &sourcePrompt, &t.AIGenerated,
		&createdBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get template with items: %w", err)
	}
	t.Description = description.String
	t.SourcePrompt = sourcePrompt.String
	if createdBy.Valid {
		t.CreatedBy = &createdBy.String
	}
	items, err := r.getItemModels(t.ID)
	if err != nil {
		return nil, nil, err
	}
	return &t, items, nil
}

func (r *Repository) List(kind, query string, page, perPage int) ([]StepTemplate, int, error) {
	var conditions []string
	var args []any
	argIdx := 1

	conditions = append(conditions, "deleted_at IS NULL")
	if kind != "" {
		conditions = append(conditions, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, kind)
		argIdx++
	}
	if query != "" {
		conditions = append(conditions, fmt.Sprintf("(name ILIKE $%d OR description ILIKE $%d)", argIdx, argIdx+1))
		like := "%" + query + "%"
		args = append(args, like, like)
		argIdx += 2
	}

	where := strings.Join(conditions, " AND ")

	var total int
	err := r.db.QueryRow(
		`SELECT COUNT(*) FROM step_templates WHERE `+where, args...,
	).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count templates: %w", err)
	}

	offset := (page - 1) * perPage
	args = append(args, perPage, offset)
	rows, err := r.db.Query(
		`SELECT `+templateColumns+` FROM step_templates WHERE `+where+
			` ORDER BY created_at DESC LIMIT $`+itoa(argIdx)+` OFFSET $`+itoa(argIdx+1),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	templates := []StepTemplate{}
	for rows.Next() {
		var t StepTemplate
		var description, sourcePrompt sql.NullString
		var createdBy sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Kind, &description, &sourcePrompt,
			&t.AIGenerated, &createdBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan template: %w", err)
		}
		t.Description = description.String
		t.SourcePrompt = sourcePrompt.String
		if createdBy.Valid {
			t.CreatedBy = &createdBy.String
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate templates: %w", err)
	}
	return templates, total, nil
}

func (r *Repository) Update(id string, req UpdateTemplateRequest) (*StepTemplate, error) {
	var t StepTemplate
	var description, sourcePrompt sql.NullString
	var createdBy sql.NullString
	err := r.db.QueryRow(
		`UPDATE step_templates SET
		 name=COALESCE($2,name), description=COALESCE($3,description), updated_at=now()
		 WHERE id=$1 AND deleted_at IS NULL
		 RETURNING `+templateColumns,
		id, req.Name, req.Description,
	).Scan(&t.ID, &t.Name, &t.Kind, &description, &sourcePrompt, &t.AIGenerated,
		&createdBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	t.Description = description.String
	t.SourcePrompt = sourcePrompt.String
	if createdBy.Valid {
		t.CreatedBy = &createdBy.String
	}
	items, err := r.getItems(t.ID)
	if err != nil {
		return nil, err
	}
	t.Items = items
	return &t, nil
}

func (r *Repository) ReplaceItems(templateID string, items []StepTemplateItem) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin replace items: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE step_template_items SET deleted_at=now(), updated_at=now()
		 WHERE template_id=$1 AND deleted_at IS NULL`, templateID,
	); err != nil {
		return fmt.Errorf("soft delete old items: %w", err)
	}

	for _, item := range items {
		metaJSON, err := json.Marshal(itemMeta(item.Meta))
		if err != nil {
			return fmt.Errorf("marshal item meta: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO step_template_items (template_id, name, description, step_order, depends_on_order, meta)
			 VALUES ($1,$2,$3,$4,$5,$6)`,
			templateID, item.Name, nullText(item.Description), item.StepOrder, item.DependsOnOrder, metaJSON,
		); err != nil {
			return fmt.Errorf("insert new item: %w", err)
		}
	}

	return tx.Commit()
}

func (r *Repository) SoftDelete(id string) error {
	result, err := r.db.Exec(
		`UPDATE step_templates SET deleted_at=now(), updated_at=now()
		 WHERE id=$1 AND deleted_at IS NULL`, id,
	)
	if err != nil {
		return fmt.Errorf("soft delete template: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("template not found")
	}
	return nil
}

func (r *Repository) getItems(templateID string) ([]StepTemplateItem, error) {
	rows, err := r.db.Query(
		`SELECT `+itemColumns+` FROM step_template_items
		 WHERE template_id=$1 AND deleted_at IS NULL ORDER BY step_order`,
		templateID,
	)
	if err != nil {
		return nil, fmt.Errorf("list template items: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

func (r *Repository) getItemModels(templateID string) ([]StepTemplateItem, error) {
	rows, err := r.db.Query(
		`SELECT `+itemColumns+` FROM step_template_items
		 WHERE template_id=$1 AND deleted_at IS NULL ORDER BY step_order`,
		templateID,
	)
	if err != nil {
		return nil, fmt.Errorf("list template item models: %w", err)
	}
	defer rows.Close()
	var items []StepTemplateItem
	for rows.Next() {
		var it StepTemplateItem
		var description sql.NullString
		if err := rows.Scan(&it.ID, &it.TemplateID, &it.Name, &description, &it.StepOrder,
			&it.DependsOnOrder, &it.Meta, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.Description = description.String
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}
	return items, nil
}

func scanItems(rows *sql.Rows) ([]StepTemplateItem, error) {
	var items []StepTemplateItem
	for rows.Next() {
		var it StepTemplateItem
		var description sql.NullString
		if err := rows.Scan(&it.ID, &it.TemplateID, &it.Name, &description, &it.StepOrder,
			&it.DependsOnOrder, &it.Meta, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		it.Description = description.String
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate items: %w", err)
	}
	return items, nil
}

func itemMeta(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}

func nullText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
