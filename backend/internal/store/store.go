// Package store is the PostgreSQL layer (pgx). All queries are parameterized.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"c23desk/internal/model"
)

type Store struct{ pool *pgxpool.Pool }

func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// ---------- portfolios ----------

// SavePortfolio upserts by name and replaces the per-pilot rows, all in one transaction.
func (s *Store) SavePortfolio(ctx context.Context, in model.PortfolioIn) (*model.Portfolio, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO portfolios (name, mode, budget_inr, starting_state, negotiated_scope, final_evidence)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (name) DO UPDATE SET
			mode = EXCLUDED.mode,
			budget_inr = EXCLUDED.budget_inr,
			starting_state = EXCLUDED.starting_state,
			negotiated_scope = EXCLUDED.negotiated_scope,
			final_evidence = EXCLUDED.final_evidence
		RETURNING id::text`,
		in.Name, in.Mode, in.BudgetINR, []byte(in.StartingState), []byte(in.NegotiatedScope), []byte(in.FinalEvidence),
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("upsert portfolio: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM portfolio_pilots WHERE portfolio_id = $1`, id); err != nil {
		return nil, fmt.Errorf("clear pilots: %w", err)
	}
	batch := &pgx.Batch{}
	for _, p := range in.Pilots {
		batch.Queue(`
			INSERT INTO portfolio_pilots
				(portfolio_id, pilot_id, selected, reason, benefit_group, setup_inr, run_inr, run_assumed, net_inr, payback_months)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, p.PilotID, p.Selected, p.Reason, p.BenefitGroup, p.SetupINR, p.RunINR, p.RunAssumed, p.NetINR, p.PaybackMonths)
	}
	br := tx.SendBatch(ctx, batch)
	for range in.Pilots {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return nil, fmt.Errorf("insert pilot row: %w", err)
		}
	}
	if err := br.Close(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetPortfolio(ctx, in.Name)
}

func (s *Store) GetPortfolio(ctx context.Context, name string) (*model.Portfolio, error) {
	var p model.Portfolio
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, mode, budget_inr, starting_state, negotiated_scope, final_evidence, created_at, updated_at
		FROM portfolios WHERE name = $1`, name,
	).Scan(&p.ID, &p.Name, &p.Mode, &p.BudgetINR, &p.StartingState, &p.NegotiatedScope, &p.FinalEvidence, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pilot_id, selected, reason, benefit_group, setup_inr, run_inr, run_assumed, net_inr, payback_months
		FROM portfolio_pilots WHERE portfolio_id = $1 ORDER BY selected DESC, pilot_id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p.Pilots = []model.PortfolioPilot{}
	for rows.Next() {
		var r model.PortfolioPilot
		if err := rows.Scan(&r.PilotID, &r.Selected, &r.Reason, &r.BenefitGroup, &r.SetupINR, &r.RunINR, &r.RunAssumed, &r.NetINR, &r.PaybackMonths); err != nil {
			return nil, err
		}
		p.Pilots = append(p.Pilots, r)
	}
	return &p, rows.Err()
}

func (s *Store) ListPortfolios(ctx context.Context) ([]model.PortfolioSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id::text, p.name, p.mode, p.budget_inr,
		       (SELECT count(*) FROM portfolio_pilots pp WHERE pp.portfolio_id = p.id AND pp.selected),
		       p.created_at, p.updated_at
		FROM portfolios p ORDER BY p.updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PortfolioSummary{}
	for rows.Next() {
		var r model.PortfolioSummary
		if err := rows.Scan(&r.ID, &r.Name, &r.Mode, &r.BudgetINR, &r.SelectedCount, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeletePortfolio(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM portfolios WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ---------- observations and decisions ----------

// AddObservation stores a measurement and the stop-rule decision it produced, atomically.
func (s *Store) AddObservation(ctx context.Context, o model.ObservationIn, d model.DecisionIn) (*model.Observation, *model.Decision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var obs model.Observation
	err = tx.QueryRow(ctx, `
		INSERT INTO observations (pilot_id, adoption_pct, quality_pct, source)
		VALUES ($1, $2, $3, $4)
		RETURNING id, pilot_id, adoption_pct, quality_pct, to_char(observed_on, 'YYYY-MM-DD'), source, created_at`,
		o.PilotID, o.AdoptionPct, o.QualityPct, o.Source,
	).Scan(&obs.ID, &obs.PilotID, &obs.AdoptionPct, &obs.QualityPct, &obs.ObservedOn, &obs.Source, &obs.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert observation: %w", err)
	}
	var dec model.Decision
	err = tx.QueryRow(ctx, `
		INSERT INTO decisions (pilot_id, decision, metric, threshold, actual, reason, observation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, pilot_id, decision, metric, threshold, actual, reason, observation_id, created_at`,
		d.PilotID, d.Decision, d.Metric, d.Threshold, d.Actual, d.Reason, obs.ID,
	).Scan(&dec.ID, &dec.PilotID, &dec.Decision, &dec.Metric, &dec.Threshold, &dec.Actual, &dec.Reason, &dec.ObservationID, &dec.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert decision: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return &obs, &dec, nil
}

func (s *Store) History(ctx context.Context, pilotID string, limit int) (*model.History, error) {
	h := &model.History{Observations: []model.Observation{}, Decisions: []model.Decision{}}

	rows, err := s.pool.Query(ctx, `
		SELECT id, pilot_id, adoption_pct, quality_pct, to_char(observed_on, 'YYYY-MM-DD'), source, created_at
		FROM observations WHERE pilot_id = $1 ORDER BY observed_on DESC, id DESC LIMIT $2`, pilotID, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o model.Observation
		if err := rows.Scan(&o.ID, &o.PilotID, &o.AdoptionPct, &o.QualityPct, &o.ObservedOn, &o.Source, &o.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		h.Observations = append(h.Observations, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.pool.Query(ctx, `
		SELECT id, pilot_id, decision, metric, threshold, actual, reason, observation_id, created_at
		FROM decisions WHERE pilot_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, pilotID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d model.Decision
		if err := rows.Scan(&d.ID, &d.PilotID, &d.Decision, &d.Metric, &d.Threshold, &d.Actual, &d.Reason, &d.ObservationID, &d.CreatedAt); err != nil {
			return nil, err
		}
		h.Decisions = append(h.Decisions, d)
	}
	return h, rows.Err()
}

// ---------- evaluations (history / dual answer) ----------

func (s *Store) SaveEvaluation(ctx context.Context, e model.EvaluationIn) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO evaluations (request, independent, conservative) VALUES ($1, $2, $3) RETURNING id`,
		[]byte(e.Request), []byte(e.Independent), []byte(e.Conservative)).Scan(&id)
	return id, err
}

// ---------- interviews ----------

func (s *Store) AddInterview(ctx context.Context, in model.InterviewIn) (*model.Interview, error) {
	var r model.Interview
	err := s.pool.QueryRow(ctx, `
		INSERT INTO interviews (interviewee_role, task, minutes_per_task, useful_result, quote, interviewed_on)
		VALUES ($1, $2, $3, $4, $5, $6::text::date)
		RETURNING id, interviewee_role, task, minutes_per_task, useful_result, quote, to_char(interviewed_on, 'YYYY-MM-DD'), created_at`,
		in.IntervieweeRole, in.Task, in.MinutesPerTask, in.UsefulResult, in.Quote, in.InterviewedOn,
	).Scan(&r.ID, &r.IntervieweeRole, &r.Task, &r.MinutesPerTask, &r.UsefulResult, &r.Quote, &r.InterviewedOn, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListInterviews(ctx context.Context) ([]model.Interview, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, interviewee_role, task, minutes_per_task, useful_result, quote, to_char(interviewed_on, 'YYYY-MM-DD'), created_at
		FROM interviews ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Interview{}
	for rows.Next() {
		var r model.Interview
		if err := rows.Scan(&r.ID, &r.IntervieweeRole, &r.Task, &r.MinutesPerTask, &r.UsefulResult, &r.Quote, &r.InterviewedOn, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
