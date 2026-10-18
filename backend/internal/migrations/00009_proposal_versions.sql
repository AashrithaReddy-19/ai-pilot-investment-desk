-- +goose Up

-- Versioned proposal inputs: version starts at 1 and goes up by one each time the stored inputs change
-- (a rename or a notes edit keeps the version).
ALTER TABLE proposals ADD COLUMN version INT NOT NULL DEFAULT 1 CHECK (version >= 1);

-- An evaluation can name the proposal it was run for and the inputs version in force at that moment.
-- NULL means the evaluation is not tied to a proposal. Deleting the proposal keeps the evaluation.
ALTER TABLE evaluations ADD COLUMN proposal_id UUID REFERENCES proposals(id) ON DELETE SET NULL;
ALTER TABLE evaluations ADD COLUMN proposal_version INT CHECK (proposal_version IS NULL OR proposal_version >= 1);
CREATE INDEX idx_evaluations_proposal ON evaluations (proposal_id, created_at DESC) WHERE proposal_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_evaluations_proposal;
ALTER TABLE evaluations DROP COLUMN IF EXISTS proposal_version;
ALTER TABLE evaluations DROP COLUMN IF EXISTS proposal_id;
ALTER TABLE proposals DROP COLUMN IF EXISTS version;
