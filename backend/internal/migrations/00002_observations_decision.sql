-- +goose Up
CREATE TABLE observations (
    id           BIGSERIAL PRIMARY KEY,
    pilot_id     TEXT NOT NULL,
    adoption_pct NUMERIC(5,2) CHECK (adoption_pct IS NULL OR adoption_pct BETWEEN 0 AND 100),
    quality_pct  NUMERIC(5,2) CHECK (quality_pct  IS NULL OR quality_pct  BETWEEN 0 AND 100),
    observed_on  DATE NOT NULL DEFAULT CURRENT_DATE,
    source       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_observations_pilot ON observations (pilot_id, observed_on DESC);

CREATE TABLE decisions (
    id             BIGSERIAL PRIMARY KEY,
    pilot_id       TEXT NOT NULL,
    decision       TEXT NOT NULL CHECK (decision IN ('GO', 'REVISE', 'STOP')),
    metric         TEXT NOT NULL,
    threshold      NUMERIC(14,2),
    actual         NUMERIC(14,2),
    reason         TEXT NOT NULL,
    observation_id BIGINT REFERENCES observations(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_decisions_pilot ON decisions (pilot_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS decisions;
DROP TABLE IF EXISTS observations;
