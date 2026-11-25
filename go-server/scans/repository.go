package scans

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

const scanColumns = `id, project_id, machine, filename, source_tag, tags, scan_at, scan_at_source,
	format, columns, row_count, bad_rows, parse_warning, txt_sha256, png_sha256, txt_size, png_size,
	txt_path, png_path, thumb_path, source_path, title, description, annotated_at, ingest_source,
	is_void, voided_at, voided_by, void_reason, uploaded_by, created_at, updated_at`

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(p ListParams) ([]Scan, int, error) {
	where := []string{"project_id = $1"}
	args := []any{p.ProjectID}

	if len(p.Tags) > 0 {
		args = append(args, pq.Array(p.Tags))
		if p.TagMode == "all" {
			where = append(where, fmt.Sprintf("tags @> $%d", len(args)))
		} else {
			where = append(where, fmt.Sprintf("tags && $%d", len(args)))
		}
	}
	if p.From != "" {
		args = append(args, p.From)
		where = append(where, fmt.Sprintf("COALESCE(scan_at, created_at) >= $%d", len(args)))
	}
	if p.To != "" {
		args = append(args, p.To)
		where = append(where, fmt.Sprintf("COALESCE(scan_at, created_at) <= $%d", len(args)))
	}
	if p.Q != "" {
		args = append(args, "%"+p.Q+"%")
		where = append(where, fmt.Sprintf("(filename ILIKE $%d OR title ILIKE $%d OR description ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if p.Annotated != nil {
		if *p.Annotated {
			where = append(where, "annotated_at IS NOT NULL")
		} else {
			where = append(where, "annotated_at IS NULL")
		}
	}
	if p.Format != "" {
		args = append(args, p.Format)
		where = append(where, fmt.Sprintf("format = $%d", len(args)))
	}
	if p.HasPNG != nil {
		if *p.HasPNG {
			where = append(where, "png_path IS NOT NULL")
		} else {
			where = append(where, "png_path IS NULL")
		}
	}
	if p.IngestSource != "" {
		args = append(args, p.IngestSource)
		where = append(where, fmt.Sprintf("ingest_source = $%d", len(args)))
	}
	if !p.IncludeVoid {
		where = append(where, "is_void = FALSE")
	}

	clause := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRow(`SELECT count(*) FROM scans WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count scans: %w", err)
	}

	args = append(args, p.PerPage, (p.Page-1)*p.PerPage)
	rows, err := r.db.Query(
		`SELECT `+scanColumns+` FROM scans WHERE `+clause+
			fmt.Sprintf(` ORDER BY COALESCE(scan_at, created_at) DESC, created_at DESC LIMIT $%d OFFSET $%d`,
				len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list scans: %w", err)
	}
	defer rows.Close()

	items, err := scanRows(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *Repository) GetByID(id string) (*Scan, error) {
	row := r.db.QueryRow(`SELECT `+scanColumns+` FROM scans WHERE id = $1`, id)
	item, err := scanOne(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrScanNotFound
		}
		return nil, fmt.Errorf("get scan: %w", err)
	}
	return item, nil
}

func (r *Repository) FindByUniqueKey(projectID, machine, filename, txtSHA string) (*Scan, error) {
	row := r.db.QueryRow(`SELECT `+scanColumns+` FROM scans
		WHERE project_id = $1 AND machine = $2 AND filename = $3 AND txt_sha256 = $4`,
		projectID, machine, filename, txtSHA)
	item, err := scanOne(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find scan by unique key: %w", err)
	}
	return item, nil
}

func (r *Repository) Create(item *Scan) error {
	columns, err := json.Marshal(item.Columns)
	if err != nil {
		return fmt.Errorf("marshal scan columns: %w", err)
	}
	return r.db.QueryRow(
		`INSERT INTO scans
		 (id, project_id, machine, filename, source_tag, tags, scan_at, scan_at_source,
		  format, columns, row_count, bad_rows, parse_warning, txt_sha256, png_sha256,
		  txt_size, png_size, txt_path, png_path, thumb_path, source_path, title, description,
		  annotated_at, ingest_source, uploaded_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
		 RETURNING created_at, updated_at`,
		item.ID, item.ProjectID, item.Machine, item.Filename, item.SourceTag, pq.Array(item.Tags),
		item.ScanAt, item.ScanAtSource, item.Format, columns, item.RowCount, item.BadRows,
		item.ParseWarning, item.TXTSHA256, item.PNGSHA256, item.TXTSize, item.PNGSize,
		item.TXTPath, item.PNGPath, item.ThumbPath, item.SourcePath, item.Title, item.Description,
		item.AnnotatedAt, item.IngestSource, item.UploadedBy,
	).Scan(&item.CreatedAt, &item.UpdatedAt)
}

func (r *Repository) AttachPNG(id, pngPath, pngSHA string, pngSize int64, thumbPath *string) error {
	_, err := r.db.Exec(
		`UPDATE scans SET png_path = $2, png_sha256 = $3, png_size = $4, thumb_path = $5, updated_at = now()
		  WHERE id = $1`,
		id, pngPath, pngSHA, pngSize, thumbPath,
	)
	if err != nil {
		return fmt.Errorf("attach scan png: %w", err)
	}
	return nil
}

func (r *Repository) Tags(projectID string) ([]TagCount, error) {
	rows, err := r.db.Query(
		`SELECT tag, count(*) FROM scans, LATERAL unnest(tags) AS tag
		 WHERE project_id = $1 AND is_void = FALSE
		 GROUP BY tag ORDER BY count(*) DESC, tag ASC`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list scan tags: %w", err)
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var item TagCount
		if err := rows.Scan(&item.Tag, &item.Count); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateAnnotation(id, title, description string, tags []string, annotatedAt *time.Time) error {
	res, err := r.db.Exec(
		`UPDATE scans SET title = $2, description = $3, tags = $4, annotated_at = $5, updated_at = now()
		  WHERE id = $1`,
		id, title, description, pq.Array(tags), annotatedAt,
	)
	if err != nil {
		return fmt.Errorf("update scan annotation: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrScanNotFound
	}
	return nil
}

func (r *Repository) MarkVoid(id, userID, reason string) error {
	res, err := r.db.Exec(
		`UPDATE scans SET is_void = TRUE, voided_at = now(), voided_by = $2, void_reason = $3, updated_at = now()
		  WHERE id = $1 AND is_void = FALSE`,
		id, userID, reason,
	)
	if err != nil {
		return fmt.Errorf("void scan: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrScanNotFound
	}
	return nil
}

func scanRows(rows *sql.Rows) ([]Scan, error) {
	items := []Scan{}
	for rows.Next() {
		item, err := scanOne(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

type rowScanner interface{ Scan(dest ...any) error }

func scanOne(row rowScanner) (*Scan, error) {
	var (
		item        Scan
		tags        pq.StringArray
		columns     []byte
		scanAt      sql.NullTime
		annotatedAt sql.NullTime
		voidedAt    sql.NullTime
		pngSHA      sql.NullString
		pngPath     sql.NullString
		thumbPath   sql.NullString
		voidedBy    sql.NullString
		voidReason  sql.NullString
		uploadedBy  sql.NullString
		pngSize     sql.NullInt64
	)
	if err := row.Scan(
		&item.ID, &item.ProjectID, &item.Machine, &item.Filename, &item.SourceTag, &tags,
		&scanAt, &item.ScanAtSource, &item.Format, &columns, &item.RowCount, &item.BadRows,
		&item.ParseWarning, &item.TXTSHA256, &pngSHA, &item.TXTSize, &pngSize,
		&item.TXTPath, &pngPath, &thumbPath, &item.SourcePath, &item.Title, &item.Description,
		&annotatedAt, &item.IngestSource, &item.IsVoid, &voidedAt, &voidedBy, &voidReason,
		&uploadedBy, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	item.Tags = []string(tags)
	item.Columns = []string{}
	if len(columns) > 0 {
		_ = json.Unmarshal(columns, &item.Columns)
	}
	if scanAt.Valid {
		item.ScanAt = &scanAt.Time
	}
	if annotatedAt.Valid {
		item.AnnotatedAt = &annotatedAt.Time
	}
	if voidedAt.Valid {
		item.VoidedAt = &voidedAt.Time
	}
	item.PNGSHA256 = nullStringPtr(pngSHA)
	item.PNGPath = nullStringPtr(pngPath)
	item.ThumbPath = nullStringPtr(thumbPath)
	item.VoidedBy = nullStringPtr(voidedBy)
	item.VoidReason = nullStringPtr(voidReason)
	item.UploadedBy = nullStringPtr(uploadedBy)
	if pngSize.Valid {
		item.PNGSize = &pngSize.Int64
	}
	return &item, nil
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	value := ns.String
	return &value
}
