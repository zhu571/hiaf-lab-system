-- 050: GC 扫描数据存档（scan-archive v0.4）。
-- 项目级原始文件档案：txt+png 元数据、标签、说明、软作废。
-- 文件本体存 gascell lab-uploads/scans（单副本），本表只存相对路径与 sha256。
CREATE TABLE scans (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    machine           VARCHAR(64) NOT NULL DEFAULT '',
    filename          VARCHAR(256) NOT NULL,
    source_tag        VARCHAR(32) NOT NULL DEFAULT '',
    tags              TEXT[] NOT NULL DEFAULT '{}'
                      CHECK (cardinality(tags) <= 10),
    scan_at           TIMESTAMPTZ,
    scan_at_source    VARCHAR(16) NOT NULL DEFAULT 'filename'
                      CHECK (scan_at_source IN ('filename', 'mtime', 'upload')),
    format            VARCHAR(16) NOT NULL DEFAULT 'unknown'
                      CHECK (format IN ('legacy3', 'v7', 'v11', 'v12', 'unknown')),
    columns           JSONB NOT NULL DEFAULT '[]'::jsonb
                      CHECK (jsonb_typeof(columns) = 'array'),
    row_count         INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    bad_rows          INTEGER NOT NULL DEFAULT 0 CHECK (bad_rows >= 0),
    parse_warning     TEXT NOT NULL DEFAULT '',
    txt_sha256        VARCHAR(64) NOT NULL,
    png_sha256        VARCHAR(64),
    txt_size          BIGINT NOT NULL CHECK (txt_size > 0),
    png_size          BIGINT CHECK (png_size IS NULL OR png_size > 0),
    txt_path          TEXT NOT NULL,
    png_path          TEXT,
    thumb_path        TEXT,
    source_path       TEXT NOT NULL DEFAULT '',
    title             VARCHAR(200) NOT NULL DEFAULT '',
    description       TEXT NOT NULL DEFAULT '',
    annotated_at      TIMESTAMPTZ,
    ingest_source     VARCHAR(16) NOT NULL DEFAULT 'watcher'
                      CHECK (ingest_source IN ('watcher', 'manual', 'backfill')),
    is_void           BOOLEAN NOT NULL DEFAULT FALSE,
    voided_at         TIMESTAMPTZ,
    voided_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    void_reason       TEXT,
    uploaded_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT scans_unique_file
        UNIQUE (project_id, machine, filename, txt_sha256)
);

CREATE INDEX idx_scans_project_time
    ON scans (project_id, scan_at DESC NULLS LAST);
CREATE INDEX idx_scans_project_created
    ON scans (project_id, created_at DESC);
CREATE INDEX idx_scans_tags
    ON scans USING GIN (tags);
CREATE INDEX idx_scans_pending
    ON scans (project_id, created_at DESC)
    WHERE annotated_at IS NULL AND is_void = FALSE;
