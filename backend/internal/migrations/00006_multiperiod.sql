-- +goose Up
-- Phase 2 additive columns: the saved summary rows record the multi-period conventions chosen and the
-- multi-period outcomes of the funded pilots. NULL means unknown, never zero.
ALTER TABLE portfolios ADD COLUMN horizon_months INT;
ALTER TABLE portfolios ADD COLUMN discount_rate_pct NUMERIC(6,2);
ALTER TABLE portfolio_pilots ADD COLUMN profit_inr NUMERIC(16,2);
ALTER TABLE portfolio_pilots ADD COLUMN roi_pct NUMERIC(8,2);

-- +goose Down
ALTER TABLE portfolio_pilots DROP COLUMN IF EXISTS roi_pct;
ALTER TABLE portfolio_pilots DROP COLUMN IF EXISTS profit_inr;
ALTER TABLE portfolios DROP COLUMN IF EXISTS discount_rate_pct;
ALTER TABLE portfolios DROP COLUMN IF EXISTS horizon_months;
