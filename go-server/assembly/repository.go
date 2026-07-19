package assembly

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const stepColumns = `id, project_id, name, description, depends_on, status, assigned_to,
step_order, started_at, completed_at, created_by, created_at, updated_at`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(step *AssemblyStep) error {
	if step.StepOrder <= 0 {
		max, err := r.MaxStepOrder(step.ProjectID)
		if err != nil {
			return err
		}
		step.StepOrder = max + 1
	}
	err := scanStep(r.db.QueryRow(`INSERT INTO assembly_steps
		(project_id, name, description, depends_on, status, assigned_to, step_order, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+stepColumns,
		step.ProjectID, step.Name, nullText(step.Description), step.DependsOn, step.Status,
		step.AssignedTo, step.StepOrder, step.CreatedBy), step)
	if err != nil {
		return fmt.Errorf("create assembly step: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(id string) (*AssemblyStep, error) {
	var step AssemblyStep
	err := scanStep(r.db.QueryRow(`SELECT `+stepColumns+` FROM assembly_steps WHERE id=$1 AND deleted_at IS NULL`, id), &step)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get assembly step: %w", err)
	}
	return &step, nil
}

func (r *Repository) GetByProject(projectID string) ([]AssemblyStep, error) {
	rows, err := r.db.Query(`SELECT `+stepColumns+` FROM assembly_steps
		WHERE project_id=$1 AND deleted_at IS NULL ORDER BY step_order`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list assembly steps: %w", err)
	}
	defer rows.Close()
	items := []AssemblyStep{}
	for rows.Next() {
		var step AssemblyStep
		if err := scanStep(rows, &step); err != nil {
			return nil, fmt.Errorf("scan assembly step: %w", err)
		}
		items = append(items, step)
	}
	return items, rows.Err()
}

func (r *Repository) Update(id string, req UpdateStepRequest) error {
	result, err := r.db.Exec(`UPDATE assembly_steps SET
		name=COALESCE($2,name), description=COALESCE($3,description),
		assigned_to=COALESCE($4,assigned_to), updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL`, id, req.Name, req.Description, req.AssignedTo)
	if err != nil {
		return fmt.Errorf("update assembly step: %w", err)
	}
	return requireAffected(result, ErrStepNotFound)
}

func (r *Repository) UpdateStatus(id, fromStatus, toStatus string, startedAt, completedAt *time.Time) error {
	result, err := r.db.Exec(`UPDATE assembly_steps SET status=$3, started_at=$4, completed_at=$5, updated_at=now()
		WHERE id=$1 AND status=$2 AND deleted_at IS NULL`, id, fromStatus, toStatus, startedAt, completedAt)
	if err != nil {
		return fmt.Errorf("transition assembly step: %w", err)
	}
	return requireAffected(result, ErrStepConflict)
}

func (r *Repository) SoftDelete(id string) error {
	result, err := r.db.Exec(`UPDATE assembly_steps SET deleted_at=now(), updated_at=now()
		WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("delete assembly step: %w", err)
	}
	return requireAffected(result, ErrStepNotFound)
}

func (r *Repository) Reorder(projectID string, items []ReorderItem) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin assembly reorder: %w", err)
	}
	defer tx.Rollback()
	for _, item := range items {
		result, err := tx.Exec(`UPDATE assembly_steps SET step_order=$3, updated_at=now()
			WHERE id=$1 AND project_id=$2 AND deleted_at IS NULL`, item.ID, projectID, -(item.StepOrder + 10000))
		if err != nil {
			return fmt.Errorf("stage assembly reorder: %w", err)
		}
		if err := requireAffected(result, ErrStepNotFound); err != nil {
			return err
		}
	}
	for _, item := range items {
		if _, err := tx.Exec(`UPDATE assembly_steps SET step_order=$3, updated_at=now()
			WHERE id=$1 AND project_id=$2 AND deleted_at IS NULL`, item.ID, projectID, item.StepOrder); err != nil {
			return fmt.Errorf("finish assembly reorder: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit assembly reorder: %w", err)
	}
	return nil
}

func (r *Repository) GetDependencyChain(id string) ([]string, error) {
	seen, chain, current := map[string]bool{}, []string{}, id
	for {
		var dependency sql.NullString
		err := r.db.QueryRow(`SELECT depends_on FROM assembly_steps WHERE id=$1 AND deleted_at IS NULL`, current).Scan(&dependency)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStepNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("get assembly dependency: %w", err)
		}
		if !dependency.Valid {
			return chain, nil
		}
		if seen[dependency.String] || dependency.String == id {
			return nil, ErrDependencyCycle
		}
		seen[dependency.String] = true
		chain = append(chain, dependency.String)
		current = dependency.String
	}
}

func (r *Repository) MaxStepOrder(projectID string) (int, error) {
	var max int
	if err := r.db.QueryRow(`SELECT COALESCE(MAX(step_order),0) FROM assembly_steps
		WHERE project_id=$1 AND deleted_at IS NULL`, projectID).Scan(&max); err != nil {
		return 0, fmt.Errorf("get max assembly step order: %w", err)
	}
	return max, nil
}

type rowScanner interface{ Scan(...any) error }

func scanStep(row rowScanner, step *AssemblyStep) error {
	var description, dependsOn, assignedTo, createdBy sql.NullString
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&step.ID, &step.ProjectID, &step.Name, &description, &dependsOn, &step.Status,
		&assignedTo, &step.StepOrder, &startedAt, &completedAt, &createdBy, &step.CreatedAt, &step.UpdatedAt); err != nil {
		return err
	}
	step.Description = description.String
	step.DependsOn = stringPtr(dependsOn)
	step.AssignedTo = stringPtr(assignedTo)
	step.CreatedBy = stringPtr(createdBy)
	step.StartedAt = timePtr(startedAt)
	step.CompletedAt = timePtr(completedAt)
	return nil
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func nullText(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func requireAffected(result sql.Result, zero error) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return zero
	}
	return nil
}
