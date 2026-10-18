-- +goose Up

-- Snapshots: a stored engine answer with the exact request that produced it and the engine versions,
-- so any dashboard, decision or comparison can be shown again and re-run to prove it reproduces.
CREATE TABLE snapshots (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind             TEXT NOT NULL CHECK (kind IN ('dashboard', 'decision', 'comparison', 'yearly_savings')),
    request          JSONB NOT NULL,
    response         JSONB NOT NULL,
    proposals        JSONB NOT NULL DEFAULT '[]',   -- [{id, name, version}] the request was built from
    rules_version    TEXT,
    contract_version TEXT,
    created_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_snapshots_workspace ON snapshots (workspace_id, kind, created_at DESC);

-- Approvals: a decision on one proposal version. A newer inputs version makes the approval stale.
CREATE TABLE approvals (
    id               BIGSERIAL PRIMARY KEY,
    workspace_id     UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    proposal_id      UUID NOT NULL REFERENCES proposals(id) ON DELETE CASCADE,
    proposal_version INT NOT NULL CHECK (proposal_version >= 1),
    decision         TEXT NOT NULL CHECK (decision IN ('approved', 'rejected', 'changes_requested')),
    snapshot_id      UUID REFERENCES snapshots(id) ON DELETE SET NULL,
    engine_outcome   TEXT,                           -- the engine's decision outcome at the time, when known
    comment          TEXT CHECK (comment IS NULL OR char_length(comment) <= 2000),
    decided_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_approvals_proposal ON approvals (proposal_id, created_at DESC);

-- Evaluations remember the engine version that answered them.
ALTER TABLE evaluations ADD COLUMN rules_version TEXT;

-- +goose Down
ALTER TABLE evaluations DROP COLUMN IF EXISTS rules_version;
DROP TABLE IF EXISTS approvals;
DROP TABLE IF EXISTS snapshots;
