-- +goose Up

-- Phase 6.2: the 30-day checkpoint plan recorded when a pilot is funded.
-- Expected values come from the engine's plan generator; actuals live in observations.
-- One current plan per pilot (regeneration replaces it); status drives the tracker.
CREATE TABLE pilot_plans (
    pilot_id            TEXT PRIMARY KEY,
    objective           TEXT NOT NULL,
    owner               TEXT,
    adoption_target_pct NUMERIC(5,1) CHECK (adoption_target_pct IS NULL OR (adoption_target_pct >= 0 AND adoption_target_pct <= 100)),
    quality_target_pct  NUMERIC(5,1) CHECK (quality_target_pct IS NULL OR (quality_target_pct >= 0 AND quality_target_pct <= 100)),
    cost_limit_inr      NUMERIC(14,2) CHECK (cost_limit_inr IS NULL OR cost_limit_inr >= 0),
    duration_days       INT NOT NULL CHECK (duration_days BETWEEN 1 AND 365),
    stop_rule           TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'active', 'revise', 'stopped', 'completed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_pilot_plans_status ON pilot_plans (status, updated_at DESC);

-- +goose StatementBegin
CREATE FUNCTION touch_pilot_plans() RETURNS trigger AS $$
BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER trg_pilot_plans_touch BEFORE UPDATE ON pilot_plans
    FOR EACH ROW EXECUTE FUNCTION touch_pilot_plans();

-- +goose Down
DROP TRIGGER IF EXISTS trg_pilot_plans_touch ON pilot_plans;
DROP FUNCTION IF EXISTS touch_pilot_plans();
DROP TABLE IF EXISTS pilot_plans;
