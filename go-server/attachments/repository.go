package attachments

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrLinkExists = errors.New("附件已绑定到该对象")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(attachment *Attachment) error {
	if attachment.StorageKey == "" {
		attachment.StorageKey = newStorageKey(attachment.OriginalName)
	}
	return scanAttachment(r.db.QueryRow(
		`INSERT INTO attachments (storage_key, original_name, sha256, description, mime_type, file_size, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, storage_key, COALESCE(original_name, ''), sha256, description,
		           COALESCE(mime_type, ''), file_size, uploaded_by, created_at, updated_at`,
		attachment.StorageKey, nullableString(attachment.OriginalName), attachment.Sha256,
		attachment.Description, nullableString(attachment.MimeType), attachment.FileSize,
		nullableStringPtr(attachment.UploadedBy),
	), attachment)
}

func (r *Repository) GetByID(id string) (*Attachment, error) {
	return r.get(`id = $1`, id)
}

func (r *Repository) GetBySha256(sha256 string) (*Attachment, error) {
	return r.get(`sha256 = $1`, sha256)
}

func (r *Repository) get(predicate string, arg any) (*Attachment, error) {
	var attachment Attachment
	err := scanAttachment(r.db.QueryRow(
		`SELECT id, storage_key, COALESCE(original_name, ''), sha256, description,
		        COALESCE(mime_type, ''), file_size, uploaded_by, created_at, updated_at
		 FROM attachments WHERE `+predicate+` AND deleted_at IS NULL`, arg,
	), &attachment)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return &attachment, nil
}

func (r *Repository) AddLink(link *AttachmentLink) error {
	err := r.db.QueryRow(
		`INSERT INTO attachment_links (attachment_id, entity_type, entity_id, description, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at`,
		link.AttachmentID, link.EntityType, link.EntityID, link.Description, nullableStringPtr(link.CreatedBy),
	).Scan(&link.ID, &link.CreatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrLinkExists
	}
	if err != nil {
		return fmt.Errorf("add attachment link: %w", err)
	}
	return nil
}

func (r *Repository) RemoveLink(linkID, _ string) error {
	result, err := r.db.Exec(`DELETE FROM attachment_links WHERE id = $1`, linkID)
	if err != nil {
		return fmt.Errorf("remove attachment link: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count removed attachment links: %w", err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) GetLinks(attachmentID string) ([]AttachmentLink, error) {
	rows, err := r.db.Query(
		`SELECT l.id, l.attachment_id, l.entity_type, l.entity_id, l.description, l.created_by, l.created_at
		 FROM attachment_links l
		 WHERE l.attachment_id = $1
		   AND EXISTS (SELECT 1 FROM attachments a WHERE a.id = l.attachment_id AND a.deleted_at IS NULL)
		 ORDER BY l.created_at, l.id`, attachmentID,
	)
	if err != nil {
		return nil, fmt.Errorf("get attachment links: %w", err)
	}
	defer rows.Close()
	links := []AttachmentLink{}
	for rows.Next() {
		var link AttachmentLink
		if err := scanLink(rows, &link); err != nil {
			return nil, fmt.Errorf("scan attachment link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment links: %w", err)
	}
	return links, nil
}

func (r *Repository) GetByEntity(entityType, entityID string) ([]Attachment, error) {
	rows, err := r.db.Query(
		`SELECT a.id, a.storage_key, COALESCE(a.original_name, ''), a.sha256, a.description,
		        COALESCE(a.mime_type, ''), a.file_size, a.uploaded_by, a.created_at, a.updated_at
		 FROM attachments a
		 JOIN attachment_links l ON l.attachment_id = a.id
		 WHERE l.entity_type = $1 AND l.entity_id = $2 AND a.deleted_at IS NULL
		 ORDER BY l.created_at DESC`, entityType, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("get attachments by entity: %w", err)
	}
	defer rows.Close()
	return scanAttachments(rows)
}

func (r *Repository) ListUnlinked(userID string, page, perPage int) ([]Attachment, int, error) {
	where := `a.deleted_at IS NULL AND NOT EXISTS (SELECT 1 FROM attachment_links l WHERE l.attachment_id = a.id)`
	args := []any{}
	if userID != "" {
		args = append(args, userID)
		where += ` AND a.uploaded_by = $1`
	}
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM attachments a WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count unlinked attachments: %w", err)
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := r.db.Query(
		`SELECT a.id, a.storage_key, COALESCE(a.original_name, ''), a.sha256, a.description,
		        COALESCE(a.mime_type, ''), a.file_size, a.uploaded_by, a.created_at, a.updated_at
		 FROM attachments a WHERE `+where+fmt.Sprintf(
			` ORDER BY a.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list unlinked attachments: %w", err)
	}
	defer rows.Close()
	items, err := scanAttachments(rows)
	return items, total, err
}

func (r *Repository) SoftDelete(id string) error {
	result, err := r.db.Exec(`UPDATE attachments SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("soft delete attachment: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count deleted attachments: %w", err)
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAttachment(row rowScanner, attachment *Attachment) error {
	var uploadedBy sql.NullString
	if err := row.Scan(&attachment.ID, &attachment.StorageKey, &attachment.OriginalName,
		&attachment.Sha256, &attachment.Description, &attachment.MimeType,
		&attachment.FileSize, &uploadedBy, &attachment.CreatedAt, &attachment.UpdatedAt); err != nil {
		return err
	}
	if uploadedBy.Valid {
		attachment.UploadedBy = &uploadedBy.String
	}
	return nil
}

func scanLink(row rowScanner, link *AttachmentLink) error {
	var createdBy sql.NullString
	if err := row.Scan(&link.ID, &link.AttachmentID, &link.EntityType, &link.EntityID,
		&link.Description, &createdBy, &link.CreatedAt); err != nil {
		return err
	}
	if createdBy.Valid {
		link.CreatedBy = &createdBy.String
	}
	return nil
}

func scanAttachments(rows *sql.Rows) ([]Attachment, error) {
	items := []Attachment{}
	for rows.Next() {
		var attachment Attachment
		if err := scanAttachment(rows, &attachment); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		items = append(items, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}
	return items, nil
}

func newStorageKey(name string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(name)))
	if len(ext) > 16 {
		ext = ""
	}
	return uuid.NewString() + ext
}

func nullableString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func nullableStringPtr(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return nullableString(*value)
}
