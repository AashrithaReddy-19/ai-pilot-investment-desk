-- C23 schema v1. Money is BIGINT paise (1 INR = 100 paise); NULL means UNKNOWN, never zero.
-- Percentages are INTEGER hundredths of a percent (0..10000).

CREATE TABLE pilots (
    id                 TEXT PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    task               TEXT NOT NULL CHECK (length(btrim(task)) > 0),
    description        TEXT,
    benefit_type       TEXT NOT NULL CHECK (benefit_type IN ('cashable','time_freed','other','unknown')),
    benefit_type_raw   TEXT,
    overlap_group      TEXT,            -- NULL = OVERLAP_UNKNOWN (never "no overlap")
    overlap_group_raw  TEXT,
    readiness_label    TEXT CHECK (readiness_label IN ('low','medium','high')),
    source_type        TEXT NOT NULL,
    field_sources      JSONB NOT NULL DEFAULT '{}'::jsonb,
    version            INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX pilots_id_lower_uq ON pilots (lower(id));
CREATE INDEX pilots_overlap_group_idx ON pilots (overlap_group);

CREATE TABLE pilot_financials (
    pilot_id                   TEXT PRIMARY KEY REFERENCES pilots(id) ON DELETE CASCADE,
    setup_cost_paise           BIGINT CHECK (setup_cost_paise >= 0),
    annual_running_cost_paise  BIGINT CHECK (annual_running_cost_paise >= 0),
    annual_benefit_paise       BIGINT CHECK (annual_benefit_paise >= 0),
    capacity_value_paise       BIGINT CHECK (capacity_value_paise >= 0),
    realized_cash_saving_paise BIGINT CHECK (realized_cash_saving_paise >= 0)
);

CREATE TABLE pilot_metrics (
    pilot_id                 TEXT PRIMARY KEY REFERENCES pilots(id) ON DELETE CASCADE,
    expected_adoption_centi  INTEGER CHECK (expected_adoption_centi BETWEEN 0 AND 10000),
    actual_adoption_centi    INTEGER CHECK (actual_adoption_centi BETWEEN 0 AND 10000),
    minimum_adoption_centi   INTEGER CHECK (minimum_adoption_centi BETWEEN 0 AND 10000),
    expected_quality_centi   INTEGER CHECK (expected_quality_centi BETWEEN 0 AND 10000),
    actual_quality_centi     INTEGER CHECK (actual_quality_centi BETWEEN 0 AND 10000),
    minimum_quality_centi    INTEGER CHECK (minimum_quality_centi BETWEEN 0 AND 10000),
    data_readiness_centi     INTEGER CHECK (data_readiness_centi BETWEEN 0 AND 10000),
    technical_readiness_centi INTEGER CHECK (technical_readiness_centi BETWEEN 0 AND 10000),
    process_readiness_centi  INTEGER CHECK (process_readiness_centi BETWEEN 0 AND 10000)
);

CREATE TABLE portfolios (
    id                 UUID PRIMARY KEY,
    name               TEXT NOT NULL CHECK (name ~ '^[A-Za-z0-9 _.-]{1,80}$'),
    budget_paise       BIGINT CHECK (budget_paise >= 0),
    basis              TEXT NOT NULL CHECK (basis IN ('cash','stated')),
    status             TEXT NOT NULL CHECK (status IN ('DRAFT','NEEDS_REVIEW','VALID','APPROVED','REJECTED')),
    rules_version      TEXT NOT NULL,
    assessment         JSONB NOT NULL,
    approved_snapshot  JSONB,
    approved_at        TIMESTAMPTZ,
    version            INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX portfolios_name_uq ON portfolios (lower(name));

-- Benefit attribution ledger: one row per selected pilot.
CREATE TABLE portfolio_pilots (
    portfolio_id            UUID NOT NULL REFERENCES portfolios(id) ON DELETE CASCADE,
    pilot_id                TEXT NOT NULL REFERENCES pilots(id) ON DELETE RESTRICT,
    benefit_group           TEXT,
    claimed_benefit_paise   BIGINT,
    basis_benefit_paise     BIGINT,
    recognized_benefit_paise BIGINT NOT NULL CHECK (recognized_benefit_paise >= 0),
    source                  TEXT,
    resolution              TEXT NOT NULL,
    review_status           TEXT NOT NULL,
    review_note             TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (portfolio_id, pilot_id),
    -- double-count guard at the storage layer: never recognise more than the pilot's own basis benefit
    CHECK (recognized_benefit_paise <= COALESCE(basis_benefit_paise, 0))
);
CREATE INDEX portfolio_pilots_pilot_idx ON portfolio_pilots (pilot_id);

CREATE TABLE overlap_resolutions (
    id               BIGSERIAL PRIMARY KEY,
    portfolio_id     UUID NOT NULL REFERENCES portfolios(id) ON DELETE CASCADE,
    overlap_group    TEXT NOT NULL,
    resolution_type  TEXT NOT NULL CHECK (resolution_type IN ('REMOVE_ONE','EXCLUDE_SECOND','SPLIT_BENEFIT','CUSTOM_ALLOCATION')),
    resolution       JSONB NOT NULL,
    review_note      TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (portfolio_id, overlap_group)
);

-- Deterministic decisions (never mixed with model predictions).
CREATE TABLE decisions (
    id                   BIGSERIAL PRIMARY KEY,
    pilot_id             TEXT NOT NULL REFERENCES pilots(id) ON DELETE CASCADE,
    pilot_version        INTEGER NOT NULL,
    decision             TEXT NOT NULL CHECK (decision IN ('PROCEED','REVISE','STOP','REVIEW')),
    primary_reason       TEXT NOT NULL,
    reason_codes         JSONB NOT NULL,
    rules_version        TEXT NOT NULL,
    input_snapshot       JSONB NOT NULL,
    calculation_snapshot JSONB NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX decisions_pilot_idx ON decisions (pilot_id, created_at DESC);

CREATE TABLE scenario_runs (
    id            BIGSERIAL PRIMARY KEY,
    pilot_id      TEXT NOT NULL REFERENCES pilots(id) ON DELETE CASCADE,
    name          TEXT NOT NULL DEFAULT '',
    overrides     JSONB NOT NULL,
    decision      TEXT NOT NULL,
    rules_version TEXT NOT NULL,
    result        JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX scenario_runs_pilot_idx ON scenario_runs (pilot_id, created_at DESC);

-- Append-only audit trail. Written in the SAME transaction as the change it records.
CREATE TABLE audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    entity_type  TEXT NOT NULL CHECK (entity_type IN ('pilot','portfolio')),
    entity_id    TEXT NOT NULL,
    action       TEXT NOT NULL,
    actor        TEXT NOT NULL,
    request_id   TEXT NOT NULL DEFAULT '',
    before_state JSONB,
    after_state  JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_entity_idx ON audit_logs (entity_type, entity_id, id);

-- Advisory AI outputs. Stored separately; never read by the decision engine.
CREATE TABLE model_predictions (
    id              BIGSERIAL PRIMARY KEY,
    pilot_id        TEXT REFERENCES pilots(id) ON DELETE CASCADE,
    related_pilot_id TEXT REFERENCES pilots(id) ON DELETE CASCADE,
    model_name      TEXT NOT NULL,
    model_version   TEXT NOT NULL,
    prediction      TEXT NOT NULL,
    probability     DOUBLE PRECISION CHECK (probability BETWEEN 0 AND 1),
    input_snapshot  JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX model_predictions_pilot_idx ON model_predictions (pilot_id);
