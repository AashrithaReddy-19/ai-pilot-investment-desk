-- +goose Up

-- Uploaded documents and their processing job. The original bytes are kept (for audit and re-parsing),
-- identified by SHA-256; the parse result is what the rules engine returned. Status stages:
--   received → processing → parsed | failed (retried until attempts = max) → confirmed
CREATE TABLE uploads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('balance_sheet', 'pilots')),
    filename        TEXT NOT NULL CHECK (char_length(filename) BETWEEN 1 AND 200),
    content_type    TEXT NOT NULL,
    format          TEXT NOT NULL CHECK (format IN ('csv', 'xlsx')),
    size_bytes      INT NOT NULL CHECK (size_bytes > 0),
    sha256          TEXT NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    content         BYTEA NOT NULL,
    options         JSONB NOT NULL DEFAULT '{}',          -- unit, period, amount_column chosen at upload
    status          TEXT NOT NULL DEFAULT 'received'
                    CHECK (status IN ('received', 'processing', 'parsed', 'failed', 'confirmed')),
    attempts        INT NOT NULL DEFAULT 0,
    max_attempts    INT NOT NULL DEFAULT 3,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT,
    result          JSONB,                                 -- engine parse answer
    confirmed       JSONB,                                 -- engine confirm answer (after review)
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, kind, sha256)                    -- the same file uploaded twice is one upload
);
CREATE INDEX idx_uploads_workspace ON uploads (workspace_id, created_at DESC);
CREATE INDEX idx_uploads_queue ON uploads (next_attempt_at) WHERE status IN ('received', 'failed');
CREATE TRIGGER trg_uploads_touch BEFORE UPDATE ON uploads
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Review corrections: the original value is kept beside the corrected one, never overwritten.
-- row_ref is the balance-sheet row number or the pilot id; field names the corrected value.
CREATE TABLE upload_corrections (
    id              BIGSERIAL PRIMARY KEY,
    upload_id       UUID NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    row_ref         TEXT NOT NULL CHECK (char_length(row_ref) BETWEEN 1 AND 80),
    field           TEXT NOT NULL CHECK (char_length(field) BETWEEN 1 AND 80),
    original_value  JSONB,
    corrected_value JSONB NOT NULL,
    note            TEXT CHECK (note IS NULL OR char_length(note) <= 500),
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_upload_corrections ON upload_corrections (upload_id, id);

-- +goose Down
DROP TABLE IF EXISTS upload_corrections;
DROP TRIGGER IF EXISTS trg_uploads_touch ON uploads;
DROP TABLE IF EXISTS uploads;
