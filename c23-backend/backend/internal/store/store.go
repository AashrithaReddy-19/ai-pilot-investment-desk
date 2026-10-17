// Package store is the PostgreSQL persistence layer (pgx). PostgreSQL is the only source of truth.
//
// Every mutation writes its audit_logs row in the SAME transaction, so a change without an audit
// record (or an audit record without a change) cannot exist. All SQL uses bind parameters.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("already exists")
	ErrVersionConflict = errors.New("version conflict: the record was changed by someone else")
	ErrInUse           = errors.New("record is referenced by another record")
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps a pgx pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects (lazily) to PostgreSQL. A down database does not prevent API startup; /ready reports it.
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.ConnectTimeout = 3 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Ping checks connectivity.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.pool.Ping(ctx)
}

// Migrate applies embedded migrations in order, each in its own transaction, recording them in schema_migrations.
func (s *Store) Migrate(ctx context.Context) ([]string, error) {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var applied []string
	for _, n := range names {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, n).Scan(&exists); err != nil {
			return applied, err
		}
		if exists {
			continue
		}
		sqlText, _ := migrationsFS.ReadFile("migrations/" + n)
		err := s.tx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sqlText)); err != nil {
				return fmt.Errorf("migration %s: %w", n, err)
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, n)
			return err
		})
		if err != nil {
			return applied, err
		}
		applied = append(applied, n)
	}
	return applied, nil
}

// Reset truncates all data tables (tests and demo reset only).
func (s *Store) Reset(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE pilots, pilot_financials, pilot_metrics, portfolios, portfolio_pilots, overlap_resolutions,
		decisions, scenario_runs, audit_logs, model_predictions RESTART IDENTITY CASCADE`)
	return err
}

func (s *Store) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// retryRead retries a read-only operation once on a transient connection error. Writes are never retried.
func retryRead[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	v, err := fn()
	if err != nil && isTransient(err) && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
		return fn()
	}
	return v, err
}

func isTransient(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return strings.HasPrefix(pe.Code, "08") || pe.Code == "57P01" // connection exceptions, admin shutdown
	}
	return pgconn.SafeToRetry(err)
}

func mapErr(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505":
			return ErrConflict
		case "23503":
			return ErrInUse
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Actor identifies who made a change (from request headers; auth-ready).
type Actor struct {
	Name      string
	RequestID string
}

func audit(ctx context.Context, tx pgx.Tx, a Actor, entity, id, action string, before, after any) error {
	var b, af []byte
	if before != nil {
		b, _ = json.Marshal(before)
	}
	if after != nil {
		af, _ = json.Marshal(after)
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(entity_type, entity_id, action, actor, request_id, before_state, after_state)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, entity, id, action, a.Name, a.RequestID, nullJSON(b), nullJSON(af))
	return err
}

func nullJSON(b []byte) any {
	if b == nil {
		return nil
	}
	return b
}

// ---------------------------------------------------------------- pilots

func amt(a money.Amount) any {
	if !a.Known {
		return nil
	}
	return a.Paise
}

func pct(p money.Pct) any {
	if !p.Known {
		return nil
	}
	return p.Centi
}

func toAmt(v *int64) money.Amount {
	if v == nil {
		return money.Amount{}
	}
	return money.Amount{Paise: *v, Known: true}
}

func toPct(v *int64) money.Pct {
	if v == nil {
		return money.Pct{}
	}
	return money.Pct{Centi: *v, Known: true}
}

func sourceType(p domain.Pilot) string {
	if s := p.Sources["setup_cost_inr"]; s != "" && s != domain.SourceUnknown {
		return s
	}
	return domain.SourceTeam
}

func insertPilot(ctx context.Context, tx pgx.Tx, p domain.Pilot) error {
	src, _ := json.Marshal(p.Sources)
	if _, err := tx.Exec(ctx, `INSERT INTO pilots(id, task, description, benefit_type, benefit_type_raw, overlap_group, overlap_group_raw,
		readiness_label, source_type, field_sources, version, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)`,
		p.ID, p.Task, p.Description, p.BenefitType, p.BenefitTypeRaw, p.OverlapGroup, p.OverlapGroupRaw, p.ReadinessLabel,
		sourceType(p), src, p.Version, p.CreatedAt); err != nil {
		return err
	}
	return writeChildren(ctx, tx, p)
}

func writeChildren(ctx context.Context, tx pgx.Tx, p domain.Pilot) error {
	if _, err := tx.Exec(ctx, `INSERT INTO pilot_financials(pilot_id, setup_cost_paise, annual_running_cost_paise, annual_benefit_paise,
		capacity_value_paise, realized_cash_saving_paise) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (pilot_id) DO UPDATE SET setup_cost_paise=$2, annual_running_cost_paise=$3, annual_benefit_paise=$4,
		capacity_value_paise=$5, realized_cash_saving_paise=$6`,
		p.ID, amt(p.SetupCost), amt(p.RunningCost), amt(p.AnnualBenefit), amt(p.CapacityValue), amt(p.RealizedCashSaving)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pilot_metrics(pilot_id, expected_adoption_centi, actual_adoption_centi, minimum_adoption_centi,
		expected_quality_centi, actual_quality_centi, minimum_quality_centi, data_readiness_centi, technical_readiness_centi, process_readiness_centi)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (pilot_id) DO UPDATE SET expected_adoption_centi=$2, actual_adoption_centi=$3,
		minimum_adoption_centi=$4, expected_quality_centi=$5, actual_quality_centi=$6, minimum_quality_centi=$7, data_readiness_centi=$8,
		technical_readiness_centi=$9, process_readiness_centi=$10`,
		p.ID, pct(p.ExpectedAdoption), pct(p.ActualAdoption), pct(p.MinimumAdoption), pct(p.ExpectedQuality), pct(p.ActualQuality),
		pct(p.MinimumQuality), pct(p.DataReadiness), pct(p.TechnicalReadiness), pct(p.ProcessReadiness))
	return err
}

const pilotSelect = `SELECT p.id, p.task, p.description, p.benefit_type, p.benefit_type_raw, p.overlap_group, p.overlap_group_raw,
	p.readiness_label, p.field_sources, p.version, p.created_at, p.updated_at,
	f.setup_cost_paise, f.annual_running_cost_paise, f.annual_benefit_paise, f.capacity_value_paise, f.realized_cash_saving_paise,
	m.expected_adoption_centi, m.actual_adoption_centi, m.minimum_adoption_centi, m.expected_quality_centi, m.actual_quality_centi,
	m.minimum_quality_centi, m.data_readiness_centi, m.technical_readiness_centi, m.process_readiness_centi
	FROM pilots p JOIN pilot_financials f ON f.pilot_id = p.id JOIN pilot_metrics m ON m.pilot_id = p.id`

func scanPilot(row pgx.Row) (domain.Pilot, error) {
	var p domain.Pilot
	var src []byte
	var f [5]*int64
	var m [9]*int64
	err := row.Scan(&p.ID, &p.Task, &p.Description, &p.BenefitType, &p.BenefitTypeRaw, &p.OverlapGroup, &p.OverlapGroupRaw,
		&p.ReadinessLabel, &src, &p.Version, &p.CreatedAt, &p.UpdatedAt, &f[0], &f[1], &f[2], &f[3], &f[4],
		&m[0], &m[1], &m[2], &m[3], &m[4], &m[5], &m[6], &m[7], &m[8])
	if err != nil {
		return p, mapErr(err)
	}
	_ = json.Unmarshal(src, &p.Sources)
	p.SetupCost, p.RunningCost, p.AnnualBenefit, p.CapacityValue, p.RealizedCashSaving = toAmt(f[0]), toAmt(f[1]), toAmt(f[2]), toAmt(f[3]), toAmt(f[4])
	p.ExpectedAdoption, p.ActualAdoption, p.MinimumAdoption = toPct(m[0]), toPct(m[1]), toPct(m[2])
	p.ExpectedQuality, p.ActualQuality, p.MinimumQuality = toPct(m[3]), toPct(m[4]), toPct(m[5])
	p.DataReadiness, p.TechnicalReadiness, p.ProcessReadiness = toPct(m[6]), toPct(m[7]), toPct(m[8])
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, nil
}

// ListPilots returns all pilots ordered by id.
func (s *Store) ListPilots(ctx context.Context) ([]domain.Pilot, error) {
	return retryRead(ctx, func() ([]domain.Pilot, error) {
		rows, err := s.pool.Query(ctx, pilotSelect+` ORDER BY p.id`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []domain.Pilot{}
		for rows.Next() {
			p, err := scanPilot(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, p)
		}
		return out, rows.Err()
	})
}

// GetPilot returns one pilot or ErrNotFound.
func (s *Store) GetPilot(ctx context.Context, id string) (domain.Pilot, error) {
	return retryRead(ctx, func() (domain.Pilot, error) {
		return scanPilot(s.pool.QueryRow(ctx, pilotSelect+` WHERE p.id = $1`, id))
	})
}

// CreatePilots inserts pilots atomically (all or nothing), each with an audit record.
func (s *Store) CreatePilots(ctx context.Context, ps []domain.Pilot, a Actor, action string) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return mapErr(s.tx(ctx, func(tx pgx.Tx) error {
		for i := range ps {
			ps[i].Version, ps[i].CreatedAt, ps[i].UpdatedAt = 1, now, now
			if err := insertPilot(ctx, tx, ps[i]); err != nil {
				return err
			}
			if err := audit(ctx, tx, a, "pilot", ps[i].ID, action, nil, ps[i]); err != nil {
				return err
			}
		}
		return nil
	}))
}

// UpdatePilot replaces a pilot if its version matches (optimistic locking). Audit has before and after.
func (s *Store) UpdatePilot(ctx context.Context, p domain.Pilot, expectedVersion int, a Actor) (domain.Pilot, error) {
	var out domain.Pilot
	err := s.tx(ctx, func(tx pgx.Tx) error {
		before, err := scanPilot(tx.QueryRow(ctx, pilotSelect+` WHERE p.id = $1 FOR UPDATE OF p`, p.ID))
		if err != nil {
			return err
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		src, _ := json.Marshal(p.Sources)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := tx.Exec(ctx, `UPDATE pilots SET task=$2, description=$3, benefit_type=$4, benefit_type_raw=$5, overlap_group=$6,
			overlap_group_raw=$7, readiness_label=$8, field_sources=$9, version=version+1, updated_at=$10 WHERE id=$1`,
			p.ID, p.Task, p.Description, p.BenefitType, p.BenefitTypeRaw, p.OverlapGroup, p.OverlapGroupRaw, p.ReadinessLabel, src, now); err != nil {
			return err
		}
		if err := writeChildren(ctx, tx, p); err != nil {
			return err
		}
		p.Version, p.CreatedAt, p.UpdatedAt = before.Version+1, before.CreatedAt, now
		out = p
		return audit(ctx, tx, a, "pilot", p.ID, "update", before, p)
	})
	return out, mapErr(err)
}

// ---------------------------------------------------------------- decisions, scenarios, predictions

// DecisionRecord is a persisted evaluation.
type DecisionRecord struct {
	ID            int64           `json:"id"`
	PilotID       string          `json:"pilot_id"`
	PilotVersion  int             `json:"pilot_version"`
	Decision      string          `json:"decision"`
	PrimaryReason string          `json:"primary_reason"`
	ReasonCodes   json.RawMessage `json:"reason_codes"`
	RulesVersion  string          `json:"rules_version"`
	InputSnapshot json.RawMessage `json:"input_snapshot"`
	Calculation   json.RawMessage `json:"calculation_snapshot"`
	CreatedAt     time.Time       `json:"created_at"`
}

// SaveDecision persists a full evaluation snapshot so past decisions are reproducible without recomputation.
func (s *Store) SaveDecision(ctx context.Context, e decision.Evaluation) (DecisionRecord, error) {
	rc, _ := json.Marshal(e.ReasonCodes)
	in, _ := json.Marshal(e.InputSnapshot)
	calc, _ := json.Marshal(e)
	r := DecisionRecord{PilotID: e.PilotID, PilotVersion: e.InputSnapshot.Version, Decision: e.Decision, PrimaryReason: e.PrimaryReason,
		ReasonCodes: rc, RulesVersion: e.RulesVersion, InputSnapshot: in, Calculation: calc}
	err := s.pool.QueryRow(ctx, `INSERT INTO decisions(pilot_id, pilot_version, decision, primary_reason, reason_codes, rules_version,
		input_snapshot, calculation_snapshot) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`,
		r.PilotID, r.PilotVersion, r.Decision, r.PrimaryReason, rc, r.RulesVersion, in, calc).Scan(&r.ID, &r.CreatedAt)
	return r, mapErr(err)
}

// ListDecisions returns the decision history of a pilot (newest first).
func (s *Store) ListDecisions(ctx context.Context, pilotID string) ([]DecisionRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, pilot_id, pilot_version, decision, primary_reason, reason_codes, rules_version, input_snapshot,
		calculation_snapshot, created_at FROM decisions WHERE pilot_id=$1 ORDER BY id DESC LIMIT 200`, pilotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DecisionRecord{}
	for rows.Next() {
		var r DecisionRecord
		if err := rows.Scan(&r.ID, &r.PilotID, &r.PilotVersion, &r.Decision, &r.PrimaryReason, &r.ReasonCodes, &r.RulesVersion,
			&r.InputSnapshot, &r.Calculation, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ScenarioRecord is a persisted scenario run.
type ScenarioRecord struct {
	ID           int64           `json:"id"`
	PilotID      string          `json:"base_pilot_id"`
	Name         string          `json:"name"`
	Overrides    json.RawMessage `json:"overrides"`
	Decision     string          `json:"decision"`
	RulesVersion string          `json:"rules_version"`
	Result       json.RawMessage `json:"result"`
	CreatedAt    time.Time       `json:"created_at"`
}

// SaveScenario persists a scenario run (kept separate from official pilot data).
func (s *Store) SaveScenario(ctx context.Context, r ScenarioRecord) (ScenarioRecord, error) {
	err := s.pool.QueryRow(ctx, `INSERT INTO scenario_runs(pilot_id, name, overrides, decision, rules_version, result)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`, r.PilotID, r.Name, []byte(r.Overrides), r.Decision, r.RulesVersion,
		[]byte(r.Result)).Scan(&r.ID, &r.CreatedAt)
	return r, mapErr(err)
}

// ListScenarios lists scenario runs for a pilot (newest first).
func (s *Store) ListScenarios(ctx context.Context, pilotID string) ([]ScenarioRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, pilot_id, name, overrides, decision, rules_version, result, created_at
		FROM scenario_runs WHERE pilot_id=$1 ORDER BY id DESC LIMIT 200`, pilotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScenarioRecord{}
	for rows.Next() {
		var r ScenarioRecord
		if err := rows.Scan(&r.ID, &r.PilotID, &r.Name, &r.Overrides, &r.Decision, &r.RulesVersion, &r.Result, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Prediction is an advisory AI output (never an input to decisions).
type Prediction struct {
	PilotID        string          `json:"pilot_id"`
	RelatedPilotID string          `json:"related_pilot_id"`
	ModelName      string          `json:"model_name"`
	ModelVersion   string          `json:"model_version"`
	Prediction     string          `json:"prediction"`
	Probability    float64         `json:"probability"`
	Input          json.RawMessage `json:"input_snapshot"`
}

// SavePredictions stores advisory predictions in one transaction.
func (s *Store) SavePredictions(ctx context.Context, ps []Prediction) error {
	return mapErr(s.tx(ctx, func(tx pgx.Tx) error {
		for _, p := range ps {
			if _, err := tx.Exec(ctx, `INSERT INTO model_predictions(pilot_id, related_pilot_id, model_name, model_version, prediction, probability,
				input_snapshot) VALUES ($1,$2,$3,$4,$5,$6,$7)`, p.PilotID, p.RelatedPilotID, p.ModelName, p.ModelVersion, p.Prediction,
				p.Probability, []byte(p.Input)); err != nil {
				return err
			}
		}
		return nil
	}))
}

// CountPredictions is used by tests and readiness diagnostics.
func (s *Store) CountPredictions(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM model_predictions`).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------- portfolios

// Portfolio is a saved selection with its last assessment snapshot.
type Portfolio struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Budget           money.Amount           `json:"budget_inr"`
	Basis            string                 `json:"basis"`
	Status           string                 `json:"status"`
	RulesVersion     string                 `json:"rules_version"`
	PilotIDs         []string               `json:"pilot_ids"`
	Resolutions      []portfolio.Resolution `json:"overlap_resolutions"`
	Assessment       portfolio.Assessment   `json:"assessment"`
	ApprovedSnapshot json.RawMessage        `json:"approved_snapshot,omitempty"`
	ApprovedAt       *time.Time             `json:"approved_at,omitempty"`
	Version          int                    `json:"version"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// portfolioAudit is the audit representation (assessment summary only, keeps audit rows small).
func portfolioAudit(p Portfolio) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "budget_inr": p.Budget, "basis": p.Basis, "status": p.Status,
		"pilot_ids": p.PilotIDs, "overlap_resolutions": p.Resolutions, "version": p.Version,
		"total_setup_cost_inr": p.Assessment.TotalSetup, "total_recognized_benefit_inr": p.Assessment.TotalRecognized,
		"blockers": p.Assessment.Blockers}
}

func writePortfolioChildren(ctx context.Context, tx pgx.Tx, p Portfolio) error {
	if _, err := tx.Exec(ctx, `DELETE FROM portfolio_pilots WHERE portfolio_id=$1`, p.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM overlap_resolutions WHERE portfolio_id=$1`, p.ID); err != nil {
		return err
	}
	for _, l := range p.Assessment.Ledger {
		if _, err := tx.Exec(ctx, `INSERT INTO portfolio_pilots(portfolio_id, pilot_id, benefit_group, claimed_benefit_paise, basis_benefit_paise,
			recognized_benefit_paise, source, resolution, review_status, review_note) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			p.ID, l.PilotID, l.BenefitGroup, amt(l.ClaimedBenefit), amt(l.BasisBenefit), l.RecognizedBenefit.Paise, l.Source, l.Resolution,
			l.ReviewStatus, l.ReviewNote); err != nil {
			return err
		}
	}
	for _, r := range p.Resolutions {
		rj, _ := json.Marshal(r)
		if _, err := tx.Exec(ctx, `INSERT INTO overlap_resolutions(portfolio_id, overlap_group, resolution_type, resolution, review_note)
			VALUES ($1,$2,$3,$4,$5)`, p.ID, r.Group, r.Type, rj, r.Note); err != nil {
			return err
		}
	}
	return nil
}

// CreatePortfolio saves portfolio + ledger + resolutions + audit atomically.
func (s *Store) CreatePortfolio(ctx context.Context, p Portfolio, a Actor) (Portfolio, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	p.Version, p.CreatedAt, p.UpdatedAt = 1, now, now
	err := s.tx(ctx, func(tx pgx.Tx) error {
		as, _ := json.Marshal(p.Assessment)
		if _, err := tx.Exec(ctx, `INSERT INTO portfolios(id, name, budget_paise, basis, status, rules_version, assessment, version, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$8)`, p.ID, p.Name, amt(p.Budget), p.Basis, p.Status, p.RulesVersion, as, now); err != nil {
			return err
		}
		if err := writePortfolioChildren(ctx, tx, p); err != nil {
			return err
		}
		return audit(ctx, tx, a, "portfolio", p.ID, "create", nil, portfolioAudit(p))
	})
	return p, mapErr(err)
}

// UpdatePortfolio replaces a portfolio if the version matches. `action` names the change in the audit log.
func (s *Store) UpdatePortfolio(ctx context.Context, p Portfolio, expectedVersion int, a Actor, action string) (Portfolio, error) {
	err := s.tx(ctx, func(tx pgx.Tx) error {
		before, err := getPortfolio(ctx, tx, p.ID, true)
		if err != nil {
			return err
		}
		if before.Version != expectedVersion {
			return ErrVersionConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		as, _ := json.Marshal(p.Assessment)
		if _, err := tx.Exec(ctx, `UPDATE portfolios SET name=$2, budget_paise=$3, basis=$4, status=$5, rules_version=$6, assessment=$7,
			approved_snapshot=$8, approved_at=$9, version=version+1, updated_at=$10 WHERE id=$1`, p.ID, p.Name, amt(p.Budget), p.Basis, p.Status,
			p.RulesVersion, as, nullJSON(p.ApprovedSnapshot), p.ApprovedAt, now); err != nil {
			return err
		}
		if err := writePortfolioChildren(ctx, tx, p); err != nil {
			return err
		}
		p.Version, p.CreatedAt, p.UpdatedAt = before.Version+1, before.CreatedAt, now
		return audit(ctx, tx, a, "portfolio", p.ID, action, portfolioAudit(before), portfolioAudit(p))
	})
	return p, mapErr(err)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func getPortfolio(ctx context.Context, q querier, id string, lock bool) (Portfolio, error) {
	var p Portfolio
	var budget *int64
	var as, snap []byte
	sql := `SELECT id::text, name, budget_paise, basis, status, rules_version, assessment, approved_snapshot, approved_at, version, created_at, updated_at
		FROM portfolios WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	if err := q.QueryRow(ctx, sql, id).Scan(&p.ID, &p.Name, &budget, &p.Basis, &p.Status, &p.RulesVersion, &as, &snap, &p.ApprovedAt,
		&p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, mapErr(err)
	}
	p.Budget = toAmt(budget)
	_ = json.Unmarshal(as, &p.Assessment)
	if snap != nil {
		p.ApprovedSnapshot = snap
	}
	p.PilotIDs = append([]string{}, p.Assessment.PilotIDs...)
	rows, err := q.Query(ctx, `SELECT resolution FROM overlap_resolutions WHERE portfolio_id=$1 ORDER BY overlap_group`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	p.Resolutions = []portfolio.Resolution{}
	for rows.Next() {
		var b []byte
		var r portfolio.Resolution
		if err := rows.Scan(&b); err != nil {
			return p, err
		}
		_ = json.Unmarshal(b, &r)
		p.Resolutions = append(p.Resolutions, r)
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, rows.Err()
}

// GetPortfolio loads one portfolio or ErrNotFound.
func (s *Store) GetPortfolio(ctx context.Context, id string) (Portfolio, error) {
	return retryRead(ctx, func() (Portfolio, error) { return getPortfolio(ctx, s.pool, id, false) })
}

// ListPortfolios lists portfolios (summary fields from the stored snapshot), newest first.
func (s *Store) ListPortfolios(ctx context.Context) ([]Portfolio, error) {
	rows, err := s.pool.Query(ctx, `SELECT id::text FROM portfolios ORDER BY created_at DESC, id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Portfolio{}
	for _, id := range ids {
		p, err := s.GetPortfolio(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// PortfolioIDsUsingPilot returns portfolios that include a pilot (for cache invalidation).
func (s *Store) PortfolioIDsUsingPilot(ctx context.Context, pilotID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT portfolio_id::text FROM portfolio_pilots WHERE pilot_id=$1`, pilotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- audit

// AuditEntry is one audit log row.
type AuditEntry struct {
	ID         int64           `json:"id"`
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Action     string          `json:"action"`
	Actor      string          `json:"actor"`
	RequestID  string          `json:"request_id"`
	Before     json.RawMessage `json:"before_state"`
	After      json.RawMessage `json:"after_state"`
	CreatedAt  time.Time       `json:"created_at"`
}

// ListRecentAudit returns the most recent audit entries across all entities (newest first).
func (s *Store) ListRecentAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, entity_type, entity_id, action, actor, request_id, before_state, after_state, created_at
		FROM audit_logs ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.Action, &e.Actor, &e.RequestID, &e.Before, &e.After, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAudit returns the audit trail of an entity in order.
func (s *Store) ListAudit(ctx context.Context, entityType, id string) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, entity_type, entity_id, action, actor, request_id, before_state, after_state, created_at
		FROM audit_logs WHERE entity_type=$1 AND entity_id=$2 ORDER BY id LIMIT 500`, entityType, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.Action, &e.Actor, &e.RequestID, &e.Before, &e.After, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
