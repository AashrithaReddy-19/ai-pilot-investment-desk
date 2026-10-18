-- +goose Up
CREATE TABLE proposal_extractions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_id   UUID NOT NULL UNIQUE REFERENCES uploads(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('processing', 'review', 'confirmed', 'failed')),
    fields      JSONB NOT NULL DEFAULT '{}',
    warnings    JSONB NOT NULL DEFAULT '[]',
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_proposal_extractions_workspace ON proposal_extractions(workspace_id, updated_at DESC);

CREATE TRIGGER trg_proposal_extractions_touch BEFORE UPDATE ON proposal_extractions
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_proposal_extractions_touch ON proposal_extractions;
DROP TABLE IF EXISTS proposal_extractions;
