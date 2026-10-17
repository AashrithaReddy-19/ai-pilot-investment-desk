-- +goose Up
CREATE TABLE portfolios (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL CHECK (name ~ '^[A-Za-z0-9_-]{1,40}$'),
    mode             TEXT NOT NULL CHECK (mode IN ('independent', 'conservative')),
    budget_inr       NUMERIC(14,2) CHECK (budget_inr IS NULL OR budget_inr >= 0),
    starting_state   JSONB NOT NULL,   -- original fixture, untouched
    negotiated_scope JSONB NOT NULL,   -- fixture_as_used + extension + mode
    final_evidence   JSONB NOT NULL,   -- portfolio result, pilots view, stop rules
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);
CREATE INDEX idx_portfolios_created ON portfolios (created_at DESC);

CREATE TABLE portfolio_pilots (
    portfolio_id   UUID NOT NULL REFERENCES portfolios(id) ON DELETE CASCADE,
    pilot_id       TEXT NOT NULL,
    selected       BOOLEAN NOT NULL,
    reason         TEXT,
    benefit_group  TEXT,
    setup_inr      NUMERIC(14,2),
    run_inr        NUMERIC(14,2),
    run_assumed    BOOLEAN NOT NULL DEFAULT FALSE,
    net_inr        NUMERIC(14,2),
    payback_months NUMERIC(8,1),   -- NULL = never pays back
    PRIMARY KEY (portfolio_id, pilot_id)
);

-- +goose StatementBegin
CREATE FUNCTION touch_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trg_portfolios_touch BEFORE UPDATE ON portfolios
    FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_portfolios_touch ON portfolios;
DROP FUNCTION IF EXISTS touch_updated_at();
DROP TABLE IF EXISTS portfolio_pilots;
DROP TABLE IF EXISTS portfolios;
