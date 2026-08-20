package translations

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type row struct {
	ID, EntityType, EntityID, FieldName, TargetLocale, SourceLocale, SourceHash, Text, Status, Origin, Model, PromptVersion, ErrorCode, ClaimToken string
	Attempts                                                                                                                                       int
	UpdatedAt                                                                                                                                      time.Time
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Ensure(entityType, entityID, field, source, sourceLocale, target string, requestedBy string, force bool) error {
	hash := Hash(source)
	_, err := r.db.Exec(`INSERT INTO content_translations (entity_type,entity_id,field_name,target_locale,source_locale,source_hash,status,requested_by)
VALUES ($1,$2,$3,$4,$5,$6,'pending',NULLIF($7,'')::uuid)
ON CONFLICT (entity_type,entity_id,field_name,target_locale) DO UPDATE SET source_locale=EXCLUDED.source_locale,source_hash=EXCLUDED.source_hash,status=CASE WHEN content_translations.source_hash=EXCLUDED.source_hash AND content_translations.origin='manual' AND NOT $8 THEN 'ready' ELSE 'pending' END,translated_text=CASE WHEN content_translations.source_hash=EXCLUDED.source_hash AND content_translations.origin='manual' AND NOT $8 THEN content_translations.translated_text ELSE NULL END,origin=CASE WHEN content_translations.source_hash=EXCLUDED.source_hash AND content_translations.origin='manual' AND NOT $8 THEN 'manual' ELSE 'ai' END,attempts=0,next_attempt_at=now(),locked_until=NULL,error_code=NULL,requested_by=EXCLUDED.requested_by,updated_at=now()
WHERE content_translations.source_hash <> EXCLUDED.source_hash OR $8 OR content_translations.status='failed'`, entityType, entityID, field, target, sourceLocale, hash, requestedBy, force)
	return err
}
func (r *Repository) SaveManual(entityType, entityID, field, source, sourceLocale, target, text, userID string) error {
	_, err := r.db.Exec(`INSERT INTO content_translations (entity_type,entity_id,field_name,target_locale,source_locale,source_hash,translated_text,status,origin,requested_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,'ready','manual',NULLIF($8,'')::uuid)
ON CONFLICT (entity_type,entity_id,field_name,target_locale) DO UPDATE SET source_locale=EXCLUDED.source_locale,source_hash=EXCLUDED.source_hash,translated_text=EXCLUDED.translated_text,status='ready',origin='manual',attempts=0,error_code=NULL,updated_at=now()`, entityType, entityID, field, target, sourceLocale, Hash(source), text, userID)
	return err
}
func (r *Repository) List(ctx context.Context, entityType string, ids []string, fields []string, sources map[string]string) ([]row, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{entityType}
	in := ""
	for i, id := range ids {
		args = append(args, id)
		if i > 0 {
			in += ","
		}
		in += fmt.Sprintf("$%d", len(args))
	}
	query := `SELECT id,entity_type,entity_id,field_name,target_locale,source_locale,source_hash,COALESCE(translated_text,''),status,origin,COALESCE(model,''),COALESCE(prompt_version,''),COALESCE(error_code,''),attempts,updated_at FROM content_translations WHERE entity_type=$1 AND entity_id IN (` + in + `)`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []row{}
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.EntityType, &x.EntityID, &x.FieldName, &x.TargetLocale, &x.SourceLocale, &x.SourceHash, &x.Text, &x.Status, &x.Origin, &x.Model, &x.PromptVersion, &x.ErrorCode, &x.Attempts, &x.UpdatedAt); err != nil {
			return nil, err
		}
		if source, ok := sources[x.EntityID+":"+x.FieldName]; ok && Hash(source) != x.SourceHash {
			x.Status = StatusStale
			x.Text = ""
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *Repository) Claim(ctx context.Context) (*row, error) {
	var x row
	err := r.db.QueryRowContext(ctx, `WITH picked AS (SELECT id FROM content_translations WHERE (status='pending' OR (status='processing' AND locked_until < now())) AND next_attempt_at <= now() AND attempts < 3 ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE content_translations t SET status='processing',claim_token=gen_random_uuid()::text,locked_until=now()+interval '2 minutes',attempts=attempts+1,updated_at=now() FROM picked WHERE t.id=picked.id RETURNING t.id,t.entity_type,t.entity_id,t.field_name,t.target_locale,t.source_locale,t.source_hash,COALESCE(t.translated_text,''),t.status,t.origin,COALESCE(t.model,''),COALESCE(t.prompt_version,''),COALESCE(t.error_code,''),t.attempts,t.updated_at,t.claim_token`).Scan(&x.ID, &x.EntityType, &x.EntityID, &x.FieldName, &x.TargetLocale, &x.SourceLocale, &x.SourceHash, &x.Text, &x.Status, &x.Origin, &x.Model, &x.PromptVersion, &x.ErrorCode, &x.Attempts, &x.UpdatedAt, &x.ClaimToken)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &x, err
}
func (r *Repository) Complete(id, token, hash, text, model, prompt string) error {
	_, err := r.db.Exec(`UPDATE content_translations SET translated_text=$4,status='ready',model=$5,prompt_version=$6,locked_until=NULL,error_code=NULL,updated_at=now() WHERE id=$1 AND claim_token=$2 AND source_hash=$3 AND status='processing' AND origin='ai'`, id, token, hash, text, model, prompt)
	return err
}
func (r *Repository) Fail(id, token, code string, retry bool) error {
	status := "failed"
	if retry {
		status = "pending"
	}
	_, err := r.db.Exec(`UPDATE content_translations SET status=$3,error_code=$4,next_attempt_at=now()+CASE WHEN attempts=1 THEN interval '5 seconds' ELSE interval '30 seconds' END,locked_until=NULL,updated_at=now() WHERE id=$1 AND claim_token=$2 AND status='processing'`, id, token, status, code)
	return err
}
