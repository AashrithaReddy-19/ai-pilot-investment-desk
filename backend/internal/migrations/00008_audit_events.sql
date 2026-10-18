-- +goose Up

-- Audit trail of successful edits and exports. Written by the gateway after the change succeeds.
-- actor stays NULL until authentication exists; request_id ties a row to the gateway access log.
-- details carries identifiers and status values only, never document contents or interview quotes.
CREATE TABLE audit_events (
    id          BIGSERIAL PRIMARY KEY,
    action      TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id   TEXT,
    actor       TEXT,
    request_id  TEXT,
    details     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_events_created ON audit_events (created_at DESC, id DESC);
CREATE INDEX idx_audit_events_entity  ON audit_events (entity_type, entity_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
