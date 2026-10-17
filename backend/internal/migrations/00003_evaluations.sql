-- +goose Up
CREATE TABLE evaluations (
    id           BIGSERIAL PRIMARY KEY,
    portfolio_id UUID REFERENCES portfolios(id) ON DELETE CASCADE,
    request      JSONB NOT NULL,
    independent  JSONB NOT NULL,
    conservative JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_evaluations_created ON evaluations (created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS evaluations;
