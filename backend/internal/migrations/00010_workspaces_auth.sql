-- +goose Up

-- Workspaces isolate organisations: every stored row belongs to exactly one workspace.
-- Existing rows move into the fixed default workspace, which is also what AUTH_MODE=off uses.
CREATE TABLE workspaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    settings    JSONB NOT NULL DEFAULT '{}',   -- budget, budget_set, mode, engine defaults, ranking weights
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO workspaces (id, name) VALUES ('00000000-0000-0000-0000-000000000001', 'Default workspace');
CREATE TRIGGER trg_workspaces_touch BEFORE UPDATE ON workspaces
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- People who can sign in. Tokens are the only credential; the gateway stores their SHA-256, never the token.
CREATE TABLE users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email        TEXT NOT NULL CHECK (email ~ '^[^@\s]+@[^@\s]+$' AND char_length(email) <= 200),
    display_name TEXT CHECK (display_name IS NULL OR char_length(display_name) <= 80),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_users_email ON users (lower(email));

-- Role per workspace: viewer reads and runs calculations, analyst also edits, admin also approves,
-- deletes, changes settings and manages members.
CREATE TABLE memberships (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         TEXT NOT NULL CHECK (role IN ('viewer', 'analyst', 'admin')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX idx_memberships_user ON memberships (user_id);

CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_sha256 TEXT NOT NULL UNIQUE,
    label        TEXT NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_api_tokens_user ON api_tokens (user_id, created_at DESC);

-- Every stored record gets its workspace. The default keeps existing rows (and AUTH_MODE=off) working.
ALTER TABLE portfolios   ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE proposals    ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE evaluations  ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE observations ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE decisions    ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE interviews   ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE pilot_plans  ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;
ALTER TABLE audit_events ADD COLUMN workspace_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001' REFERENCES workspaces(id) ON DELETE CASCADE;

-- Names are unique per workspace, not globally: two organisations may both call a portfolio "q4".
ALTER TABLE portfolios DROP CONSTRAINT portfolios_name_key;
ALTER TABLE portfolios ADD CONSTRAINT portfolios_workspace_name_key UNIQUE (workspace_id, name);
ALTER TABLE proposals  DROP CONSTRAINT proposals_name_key;
ALTER TABLE proposals  ADD CONSTRAINT proposals_workspace_name_key UNIQUE (workspace_id, name);
ALTER TABLE pilot_plans DROP CONSTRAINT pilot_plans_pkey;
ALTER TABLE pilot_plans ADD PRIMARY KEY (workspace_id, pilot_id);

CREATE INDEX idx_evaluations_workspace  ON evaluations  (workspace_id, created_at DESC);
CREATE INDEX idx_observations_workspace ON observations (workspace_id, pilot_id, observed_on DESC);
CREATE INDEX idx_decisions_workspace    ON decisions    (workspace_id, created_at DESC);
CREATE INDEX idx_audit_events_workspace ON audit_events (workspace_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_events_workspace;
DROP INDEX IF EXISTS idx_decisions_workspace;
DROP INDEX IF EXISTS idx_observations_workspace;
DROP INDEX IF EXISTS idx_evaluations_workspace;
ALTER TABLE pilot_plans DROP CONSTRAINT pilot_plans_pkey;
ALTER TABLE pilot_plans ADD PRIMARY KEY (pilot_id);
ALTER TABLE proposals  DROP CONSTRAINT proposals_workspace_name_key;
ALTER TABLE proposals  ADD CONSTRAINT proposals_name_key UNIQUE (name);
ALTER TABLE portfolios DROP CONSTRAINT portfolios_workspace_name_key;
ALTER TABLE portfolios ADD CONSTRAINT portfolios_name_key UNIQUE (name);
ALTER TABLE audit_events DROP COLUMN workspace_id;
ALTER TABLE pilot_plans  DROP COLUMN workspace_id;
ALTER TABLE interviews   DROP COLUMN workspace_id;
ALTER TABLE decisions    DROP COLUMN workspace_id;
ALTER TABLE observations DROP COLUMN workspace_id;
ALTER TABLE evaluations  DROP COLUMN workspace_id;
ALTER TABLE proposals    DROP COLUMN workspace_id;
ALTER TABLE portfolios   DROP COLUMN workspace_id;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TRIGGER IF EXISTS trg_workspaces_touch ON workspaces;
DROP TABLE IF EXISTS workspaces;
