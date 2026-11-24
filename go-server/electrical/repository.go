package electrical

import (
	"context"
	"database/sql"
)

// Repository persists the runtime on/off switches for the toggleable
// acquisition sources. The table is owned by the electrical module.
type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	if db == nil {
		return nil
	}
	return &Repository{db: db}
}

// LoadSourceStates returns the persisted switch state for every source that has
// a row. Sources without a row are intentionally absent so the caller can fill
// in its environment-derived defaults and persist them.
func (r *Repository) LoadSourceStates(ctx context.Context) (map[string]bool, error) {
	if r == nil {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT source, enabled FROM electrical_source_states`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make(map[string]bool)
	for rows.Next() {
		var source string
		var enabled bool
		if err := rows.Scan(&source, &enabled); err != nil {
			return nil, err
		}
		states[source] = enabled
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return states, nil
}

func (r *Repository) UpsertSourceState(ctx context.Context, source string, enabled bool) error {
	if r == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO electrical_source_states (source, enabled, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (source) DO UPDATE
		SET enabled = EXCLUDED.enabled, updated_at = now()`, source, enabled)
	return err
}
