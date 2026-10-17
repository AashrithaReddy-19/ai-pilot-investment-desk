-- +goose Up

-- Stored proposals: each is a named AI pilot candidate with versioned inputs.
-- Inputs are stored as JSONB so the schema stays additive as Phase 2 adds fields.
CREATE TABLE proposals (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL CHECK (name ~ '^[A-Za-z0-9_\- ]{1,80}$'),
    status      TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    inputs      JSONB NOT NULL DEFAULT '{}',   -- fixture pilot row + extension overrides as one doc
    notes       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);
CREATE INDEX idx_proposals_status ON proposals (status, updated_at DESC);

-- Evidence rows linked to a proposal: one row per source document / interview / measurement.
CREATE TABLE evidence (
    id          BIGSERIAL PRIMARY KEY,
    proposal_id UUID NOT NULL REFERENCES proposals(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('interview', 'measurement', 'document', 'assumption')),
    source      TEXT NOT NULL,           -- who / what provided this
    quality     TEXT CHECK (quality IS NULL OR quality IN ('observed', 'assumed', 'calculated', 'fixture')),
    note        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_evidence_proposal ON evidence (proposal_id, created_at DESC);

-- Link observations and decisions to a proposal so IDs no longer collide across scenarios.
-- Additive nullable FK: existing rows keep working with proposal_id = NULL.
ALTER TABLE observations ADD COLUMN proposal_id UUID REFERENCES proposals(id) ON DELETE SET NULL;
ALTER TABLE decisions    ADD COLUMN proposal_id UUID REFERENCES proposals(id) ON DELETE SET NULL;
CREATE INDEX idx_observations_proposal ON observations (proposal_id, observed_on DESC) WHERE proposal_id IS NOT NULL;
CREATE INDEX idx_decisions_proposal    ON decisions    (proposal_id, created_at DESC)  WHERE proposal_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS trigger AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_proposals_touch BEFORE UPDATE ON proposals
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_proposals_touch ON proposals;
ALTER TABLE decisions    DROP COLUMN IF EXISTS proposal_id;
ALTER TABLE observations DROP COLUMN IF EXISTS proposal_id;
DROP TABLE IF EXISTS evidence;
DROP TABLE IF EXISTS proposals;
