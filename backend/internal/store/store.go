// Package store is the PostgreSQL layer (pgx). All queries are parameterized.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
		INSERT INTO portfolios (name, mode, budget_inr, horizon_months, discount_rate_pct, starting_state, negotiated_scope, final_evidence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name) DO UPDATE SET
			mode = EXCLUDED.mode,
			budget_inr = EXCLUDED.budget_inr,
			horizon_months = EXCLUDED.horizon_months,
			discount_rate_pct = EXCLUDED.discount_rate_pct,
			starting_state = EXCLUDED.starting_state,
			negotiated_scope = EXCLUDED.negotiated_scope,
			final_evidence = EXCLUDED.final_evidence
		RETURNING id::text`,
		in.Name, in.Mode, in.BudgetINR, in.HorizonMonths, in.DiscountRatePct, []byte(in.StartingState), []byte(in.NegotiatedScope), []byte(in.FinalEvidence),
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
				(portfolio_id, pilot_id, selected, reason, benefit_group, setup_inr, run_inr, run_assumed, net_inr, payback_months, profit_inr, roi_pct)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			id, p.PilotID, p.Selected, p.Reason, p.BenefitGroup, p.SetupINR, p.RunINR, p.RunAssumed, p.NetINR, p.PaybackMonths, p.ProfitINR, p.ROIPct)
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
		SELECT id::text, name, mode, budget_inr, horizon_months, discount_rate_pct, starting_state, negotiated_scope, final_evidence, created_at, updated_at
		FROM portfolios WHERE name = $1`, name,
	).Scan(&p.ID, &p.Name, &p.Mode, &p.BudgetINR, &p.HorizonMonths, &p.DiscountRatePct, &p.StartingState, &p.NegotiatedScope, &p.FinalEvidence, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT pilot_id, selected, reason, benefit_group, setup_inr, run_inr, run_assumed, net_inr, payback_months, profit_inr, roi_pct
		FROM portfolio_pilots WHERE portfolio_id = $1 ORDER BY selected DESC, pilot_id`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p.Pilots = []model.PortfolioPilot{}
	for rows.Next() {
		var r model.PortfolioPilot
		if err := rows.Scan(&r.PilotID, &r.Selected, &r.Reason, &r.BenefitGroup, &r.SetupINR, &r.RunINR, &r.RunAssumed, &r.NetINR, &r.PaybackMonths, &r.ProfitINR, &r.ROIPct); err != nil {
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
		       p.horizon_months, p.created_at, p.updated_at
		FROM portfolios p ORDER BY p.updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PortfolioSummary{}
	for rows.Next() {
		var r model.PortfolioSummary
		if err := rows.Scan(&r.ID, &r.Name, &r.Mode, &r.BudgetINR, &r.SelectedCount, &r.HorizonMonths, &r.CreatedAt, &r.UpdatedAt); err != nil {
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
// proposalID may be nil: the link is optional.
func (s *Store) AddObservation(ctx context.Context, o model.ObservationIn, d model.DecisionIn) (*model.Observation, *model.Decision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var obs model.Observation
	err = tx.QueryRow(ctx, `
		INSERT INTO observations (pilot_id, proposal_id, adoption_pct, quality_pct, source)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, pilot_id, proposal_id::text, adoption_pct, quality_pct, to_char(observed_on, 'YYYY-MM-DD'), source, created_at`,
		o.PilotID, o.ProposalID, o.AdoptionPct, o.QualityPct, o.Source,
	).Scan(&obs.ID, &obs.PilotID, &obs.ProposalID, &obs.AdoptionPct, &obs.QualityPct, &obs.ObservedOn, &obs.Source, &obs.CreatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert observation: %w", err)
	}
	var dec model.Decision
	err = tx.QueryRow(ctx, `
		INSERT INTO decisions (pilot_id, proposal_id, decision, metric, threshold, actual, reason, observation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, pilot_id, proposal_id::text, decision, metric, threshold, actual, reason, observation_id, created_at`,
		d.PilotID, d.ProposalID, d.Decision, d.Metric, d.Threshold, d.Actual, d.Reason, obs.ID,
	).Scan(&dec.ID, &dec.PilotID, &dec.ProposalID, &dec.Decision, &dec.Metric, &dec.Threshold, &dec.Actual, &dec.Reason, &dec.ObservationID, &dec.CreatedAt)
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
		SELECT id, pilot_id, proposal_id::text, adoption_pct, quality_pct, to_char(observed_on, 'YYYY-MM-DD'), source, created_at
		FROM observations WHERE pilot_id = $1 ORDER BY observed_on DESC, id DESC LIMIT $2`, pilotID, limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var o model.Observation
		if err := rows.Scan(&o.ID, &o.PilotID, &o.ProposalID, &o.AdoptionPct, &o.QualityPct, &o.ObservedOn, &o.Source, &o.CreatedAt); err != nil {
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
		SELECT id, pilot_id, proposal_id::text, decision, metric, threshold, actual, reason, observation_id, created_at
		FROM decisions WHERE pilot_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, pilotID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d model.Decision
		if err := rows.Scan(&d.ID, &d.PilotID, &d.ProposalID, &d.Decision, &d.Metric, &d.Threshold, &d.Actual, &d.Reason, &d.ObservationID, &d.CreatedAt); err != nil {
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

// ListEvaluations returns the newest evaluation audit rows (no bodies). limit is clamped to 1..50.
func (s *Store) ListEvaluations(ctx context.Context, limit int) ([]model.EvaluationSummary, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, portfolio_id::text, created_at FROM evaluations ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.EvaluationSummary{}
	for rows.Next() {
		var e model.EvaluationSummary
		if err := rows.Scan(&e.ID, &e.PortfolioID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEvaluation(ctx context.Context, id int64) (*model.Evaluation, error) {
	var e model.Evaluation
	err := s.pool.QueryRow(ctx, `
		SELECT id, portfolio_id::text, request, independent, conservative, created_at FROM evaluations WHERE id = $1`, id,
	).Scan(&e.ID, &e.PortfolioID, &e.Request, &e.Independent, &e.Conservative, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// HistoryTimeline merges the last `limit` evaluations and the last `limit` stop-rule decisions
// into one newest-first timeline. Decisions carry the previous verdict for the same pilot (LAG);
// evaluations carry a diff against the previous evaluation (oldest→newest pass before the merge).
func (s *Store) HistoryTimeline(ctx context.Context, limit int) ([]model.HistoryEvent, error) {
	if limit < 1 {
		limit = 25
	}
	if limit > 50 {
		limit = 50
	}
	drows, err := s.pool.Query(ctx, `
		SELECT d.id, d.pilot_id, d.decision, d.reason, o.adoption_pct, o.quality_pct, d.created_at, d.proposal_id::text,
		       LAG(d.decision) OVER (PARTITION BY d.pilot_id ORDER BY d.created_at, d.id)
		FROM decisions d
		LEFT JOIN observations o ON o.id = d.observation_id
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	events := []model.HistoryEvent{}
	for drows.Next() {
		var e model.HistoryEvent
		e.Kind = "decision"
		if err := drows.Scan(&e.ID, &e.PilotID, &e.Verdict, &e.Reason, &e.ObservedAdoption, &e.ObservedQuality,
			&e.At, &e.ProposalID, &e.PrevVerdict); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	if err := drows.Err(); err != nil {
		return nil, err
	}

	erows, err := s.pool.Query(ctx, `
		SELECT id, created_at, request, independent, conservative
		FROM evaluations ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	// oldest→newest so each event summaries against the evaluation before it
	ascending := []struct {
		id                    int64
		at                    time.Time
		request, indep, consv []byte
	}{}
	for erows.Next() {
		var r struct {
			id  int64
			at  time.Time
			req []byte
			ind []byte
			con []byte
		}
		if err := erows.Scan(&r.id, &r.at, &r.req, &r.ind, &r.con); err != nil {
			return nil, err
		}
		ascending = append(ascending, struct {
			id                    int64
			at                    time.Time
			request, indep, consv []byte
		}{r.id, r.at, r.req, r.ind, r.con})
	}
	if err := erows.Err(); err != nil {
		return nil, err
	}
	var prevBudget *float64
	var prevMode *string
	var prevSel []string
	for i := len(ascending) - 1; i >= 0; i-- {
		asc := ascending[i]
		var req struct {
			Budget json.RawMessage `json:"budget"`
			Mode   *string         `json:"mode"`
		}
		_ = json.Unmarshal(asc.request, &req)
		var budget *float64
		if len(req.Budget) > 0 {
			_ = json.Unmarshal(req.Budget, &budget) // non-numbers stay NULL, never zero
		}
		mode := req.Mode
		if mode == nil || (*mode != "independent" && *mode != "conservative") {
			def := "independent"
			mode = &def
		}
		var sel struct {
			Selected []string `json:"selected"`
		}
		selRaw := asc.indep
		if *mode == "conservative" {
			selRaw = asc.consv
		}
		_ = json.Unmarshal(selRaw, &sel)
		if sel.Selected == nil {
			sel.Selected = []string{}
		}
		summary := "first recorded evaluation"
		if i < len(ascending)-1 {
			summary = summariseEvalChange(prevSel, sel.Selected, prevBudget, budget, prevMode, mode)
		}
		events = append(events, model.HistoryEvent{
			ID: asc.id, Kind: "evaluation", At: asc.at, Mode: mode, Budget: budget,
			Selected: sel.Selected, ChangeSummary: &summary,
		})
		prevBudget, prevMode, prevSel = budget, mode, sel.Selected
	}

	sort.SliceStable(events, func(a, b int) bool {
		if !events[a].At.Equal(events[b].At) {
			return events[a].At.After(events[b].At)
		}
		return events[a].ID > events[b].ID
	})
	return events, nil
}

func summariseEvalChange(prevSel, sel []string, prevBudget, budget *float64, prevMode, mode *string) string {
	var parts []string
	prev := map[string]bool{}
	for _, x := range prevSel {
		prev[x] = true
	}
	now := map[string]bool{}
	for _, x := range sel {
		now[x] = true
	}
	var added, removed []string
	for _, x := range sel {
		if !prev[x] {
			added = append(added, x)
		}
	}
	for _, x := range prevSel {
		if !now[x] {
			removed = append(removed, x)
		}
	}
	if len(added) > 0 {
		parts = append(parts, "funded "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "dropped "+strings.Join(removed, ", "))
	}
	if prevMode != nil && mode != nil && *prevMode != *mode {
		parts = append(parts, fmt.Sprintf("mode %s → %s", *prevMode, *mode))
	}
	if prevBudget != nil && budget != nil && *prevBudget != *budget {
		parts = append(parts, fmt.Sprintf("budget INR %s → %s", grpINR(*prevBudget), grpINR(*budget)))
	} else if (prevBudget == nil) != (budget == nil) {
		parts = append(parts, "budget changed between known and unknown")
	}
	if len(parts) == 0 {
		return "no change in funded set, mode or budget"
	}
	return strings.Join(parts, "; ")
}

func grpINR(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
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

// ---------- proposals ----------

// CreateProposal inserts a new proposal. A name conflict is a unique violation the caller turns into 422.
func (s *Store) CreateProposal(ctx context.Context, in model.ProposalIn) (*model.Proposal, error) {
	var p model.Proposal
	err := s.pool.QueryRow(ctx, `
		INSERT INTO proposals (name, inputs, notes) VALUES ($1, $2, $3)
		RETURNING id::text, name, status, inputs, notes, created_at, updated_at`,
		in.Name, []byte(in.Inputs), in.Notes,
	).Scan(&p.ID, &p.Name, &p.Status, &p.Inputs, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) GetProposal(ctx context.Context, id string) (*model.Proposal, error) {
	var p model.Proposal
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, name, status, inputs, notes, created_at, updated_at FROM proposals WHERE id::text = $1`, id,
	).Scan(&p.ID, &p.Name, &p.Status, &p.Inputs, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListProposals returns active first (newest first), then archived.
func (s *Store) ListProposals(ctx context.Context, includeArchived bool) ([]model.ProposalSummary, error) {
	where := ` WHERE status = 'active'`
	if includeArchived {
		where = ""
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, name, status, notes, created_at, updated_at FROM proposals`+where+`
		ORDER BY (status = 'active') DESC, updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProposalSummary{}
	for rows.Next() {
		var r model.ProposalSummary
		if err := rows.Scan(&r.ID, &r.Name, &r.Status, &r.Notes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateProposal patches name / inputs / notes. Nil fields keep their stored value.
func (s *Store) UpdateProposal(ctx context.Context, id string, in model.ProposalIn) (*model.Proposal, error) {
	var p model.Proposal
	err := s.pool.QueryRow(ctx, `
		UPDATE proposals SET
			name   = COALESCE($2, name),
			inputs = COALESCE($3, inputs),
			notes  = COALESCE($4, notes)
		WHERE id::text = $1
		RETURNING id::text, name, status, inputs, notes, created_at, updated_at`,
		id, nameOrNull(in.Name), in.Inputs, in.Notes,
	).Scan(&p.ID, &p.Name, &p.Status, &p.Inputs, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func nameOrNull(name string) any {
	if name == "" {
		return nil
	}
	return name
}

// ArchiveProposal flips status to archived (or back to active with restore=true).
func (s *Store) ArchiveProposal(ctx context.Context, id string, restore bool) (*model.Proposal, error) {
	status := "archived"
	if restore {
		status = "active"
	}
	var p model.Proposal
	err := s.pool.QueryRow(ctx, `
		UPDATE proposals SET status = $2 WHERE id::text = $1
		RETURNING id::text, name, status, inputs, notes, created_at, updated_at`,
		id, status,
	).Scan(&p.ID, &p.Name, &p.Status, &p.Inputs, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) DeleteProposal(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM proposals WHERE id::text = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ---------- evidence ----------

func (s *Store) AddEvidence(ctx context.Context, in model.EvidenceIn) (*model.Evidence, error) {
	var e model.Evidence
	err := s.pool.QueryRow(ctx, `
		INSERT INTO evidence (proposal_id, kind, source, quality, note) VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id, proposal_id::text, kind, source, quality, note, created_at`,
		in.ProposalID, in.Kind, in.Source, in.Quality, in.Note,
	).Scan(&e.ID, &e.ProposalID, &e.Kind, &e.Source, &e.Quality, &e.Note, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) ListEvidence(ctx context.Context, proposalID string) ([]model.Evidence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, proposal_id::text, kind, source, quality, note, created_at
		FROM evidence WHERE proposal_id::text = $1 ORDER BY id`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Evidence{}
	for rows.Next() {
		var e model.Evidence
		if err := rows.Scan(&e.ID, &e.ProposalID, &e.Kind, &e.Source, &e.Quality, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------- pilot plans (Phase 6.2: checkpoint tracker) ----------

// UpsertPlan records (or replaces) the current 30-day plan for a pilot.
func (s *Store) UpsertPlan(ctx context.Context, p model.PilotPlan) (*model.PilotPlan, error) {
	plan := p
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pilot_plans (pilot_id, objective, owner, adoption_target_pct, quality_target_pct, cost_limit_inr, duration_days, stop_rule, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (pilot_id) DO UPDATE SET
			objective = EXCLUDED.objective, owner = EXCLUDED.owner,
			adoption_target_pct = EXCLUDED.adoption_target_pct, quality_target_pct = EXCLUDED.quality_target_pct,
			cost_limit_inr = EXCLUDED.cost_limit_inr, duration_days = EXCLUDED.duration_days,
			stop_rule = EXCLUDED.stop_rule, status = EXCLUDED.status
		RETURNING created_at, updated_at`,
		p.PilotID, p.Objective, p.Owner, p.AdoptionTarget, p.QualityTarget, p.CostLimitINR, p.DurationDays, p.StopRule, p.Status,
	).Scan(&plan.CreatedAt, &plan.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *Store) PilotPlan(ctx context.Context, pilotID string) (*model.PilotPlan, error) {
	var p model.PilotPlan
	err := s.pool.QueryRow(ctx, `
		SELECT pilot_id, objective, owner, adoption_target_pct, quality_target_pct, cost_limit_inr, duration_days, stop_rule, status, created_at, updated_at
		FROM pilot_plans WHERE pilot_id = $1`, pilotID,
	).Scan(&p.PilotID, &p.Objective, &p.Owner, &p.AdoptionTarget, &p.QualityTarget, &p.CostLimitINR, &p.DurationDays, &p.StopRule, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) PilotPlans(ctx context.Context) ([]model.PilotPlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT pilot_id, objective, owner, adoption_target_pct, quality_target_pct, cost_limit_inr, duration_days, stop_rule, status, created_at, updated_at
		FROM pilot_plans ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.PilotPlan{}
	for rows.Next() {
		var p model.PilotPlan
		if err := rows.Scan(&p.PilotID, &p.Objective, &p.Owner, &p.AdoptionTarget, &p.QualityTarget, &p.CostLimitINR, &p.DurationDays, &p.StopRule, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetPlanStatus(ctx context.Context, pilotID, status string) (*model.PilotPlan, error) {
	var p model.PilotPlan
	err := s.pool.QueryRow(ctx, `
		UPDATE pilot_plans SET status = $2 WHERE pilot_id = $1
		RETURNING pilot_id, objective, owner, adoption_target_pct, quality_target_pct, cost_limit_inr, duration_days, stop_rule, status, created_at, updated_at`,
		pilotID, status,
	).Scan(&p.PilotID, &p.Objective, &p.Owner, &p.AdoptionTarget, &p.QualityTarget, &p.CostLimitINR, &p.DurationDays, &p.StopRule, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeletePlan removes a plan row (store tests and explicit resets).
func (s *Store) DeletePlan(ctx context.Context, pilotID string) error {
	ct, err := s.pool.Exec(ctx, `DELETE FROM pilot_plans WHERE pilot_id = $1`, pilotID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return nil
}

// LatestEvaluationWithPilot returns the newest evaluation whose independent portfolio selected the pilot;
func (s *Store) LatestEvaluationWithPilot(ctx context.Context, pilotID string) (*model.Evaluation, error) {
	var e model.Evaluation
	err := s.pool.QueryRow(ctx, `
		SELECT id, portfolio_id::text, request, independent, conservative, created_at FROM evaluations
		WHERE independent->'selected' ? $1 ORDER BY created_at DESC, id DESC LIMIT 1`, pilotID,
	).Scan(&e.ID, &e.PortfolioID, &e.Request, &e.Independent, &e.Conservative, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
