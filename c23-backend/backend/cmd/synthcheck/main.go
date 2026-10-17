// Command synthcheck runs the production c23-rules-v1 engine over the synthetic comparison dataset and writes
// its outputs for the Python validation pipeline (decision-engine/synthetic), which checks its own independent
// re-implementation against them:
//
//	../decision-engine/reports/synthetic/go_engine_outputs.json
//
// It reads data only; the synthetic and official source files are never modified. No engine logic lives here.
//
// Usage (from backend/): go run ./cmd/synthcheck
package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
)

// schemaFields are the synthetic columns that exist in the pilot schema; derived and label columns
// (net, payback, roi, decision, ...) are deliberately not passed in: the engine recomputes everything.
var schemaFields = []string{
	"task", "setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "benefit_type", "overlap_group",
	"expected_adoption_pct", "actual_adoption_pct", "minimum_adoption_pct",
	"expected_quality_pct", "actual_quality_pct", "minimum_quality_pct",
	"data_readiness_pct", "technical_readiness_pct", "process_readiness_pct",
	"capacity_value_inr", "realized_cash_saving_inr",
}

// Budgets (INR) used for the optimizer cross-checks; they mirror decision-engine/synthetic/portfolio.py.
var budgets = []int64{200000, 400000, 600000, 800000}

// optimizerSubset is the size of the synthetic slice given to the production optimizer (its exact search is
// capped at portfolio.MaxCandidates eligible pilots, so the full 120-row set cannot be passed in).
const optimizerSubset = 20

type pilotOut struct {
	ID                   string   `json:"record_id"`
	Decision             string   `json:"decision"`
	PrimaryReason        string   `json:"primary_reason"`
	ReasonCodes          []string `json:"reason_codes"`
	CanonicalBenefitType string   `json:"canonical_benefit_type"`
	StatedNetAnnual      *string  `json:"stated_net_annual_benefit_inr"`
	StatedPaybackMonths  *string  `json:"stated_payback_months"`
	StatedROIPct         *string  `json:"stated_roi_pct"`
	CashBenefit          *string  `json:"cash_benefit_inr"`
	CashNetAnnual        *string  `json:"cash_net_annual_benefit_inr"`
	CashPaybackMonths    *string  `json:"cash_payback_months"`
	CashPaybackStatus    string   `json:"cash_payback_status"`
	CashROIPct           *string  `json:"cash_roi_pct"`
	BreakevenAdoptionPct *string  `json:"cash_breakeven_adoption_pct"`
	OverallReadinessPct  *string  `json:"overall_readiness_pct"`
	ReadinessLevel       string   `json:"readiness_level"`
}

type optOut struct {
	Dataset   string                `json:"dataset"`
	BudgetInr int64                 `json:"budget_inr"`
	Basis     string                `json:"basis"`
	Selected  []string              `json:"selected"`
	NetAnnual *string               `json:"net_annual_benefit_inr"`
	Setup     string                `json:"total_setup_cost_inr"`
	Status    string                `json:"assessment_status"`
	Excluded  []portfolio.Exclusion `json:"excluded"`
	Error     string                `json:"error,omitempty"`
}

func main() {
	synth := env("SYNTHETIC_JSON", filepath.Join("..", "decision-engine", "data", "C23", "C23_complete_valid_synthetic_dataset.json"))
	raw := env("RAW_DATA_DIR", filepath.Join("..", "decision-engine", "data", "raw"))
	out := env("SYNTHETIC_REPORT_DIR", filepath.Join("..", "decision-engine", "reports", "synthetic"))
	th := rules.Default()

	ps, err := loadSynthetic(synth)
	must(err)
	evals := evaluate(ps, th)
	pilots := make([]pilotOut, 0, len(ps))
	for _, p := range ps {
		pilots = append(pilots, describe(evals[p.ID]))
	}

	off, err := fixture.LoadOfficial(raw)
	must(err)
	offEvals := evaluate(off.Pilots, th)
	official := make([]pilotOut, 0, len(off.Pilots))
	for _, p := range off.Pilots {
		official = append(official, describe(offEvals[p.ID]))
	}

	var opts []optOut
	sub := ps[:optimizerSubset]
	subEvals := evaluate(sub, th)
	for _, b := range budgets {
		for _, basis := range []string{portfolio.BasisCash, portfolio.BasisStated} {
			opts = append(opts, optimize("official_fixture", off.Pilots, offEvals, b, basis, th))
			opts = append(opts, optimize(fmt.Sprintf("synthetic_first_%d", optimizerSubset), sub, subEvals, b, basis, th))
		}
	}
	full := optimize("synthetic_all_120", ps, evals, 800000, portfolio.BasisStated, th)

	doc := map[string]any{
		"generated_by":         "backend/cmd/synthcheck",
		"rules_version":        rules.Version,
		"note":                 "Production engine outputs on SYNTHETIC test data. Not real-world evidence.",
		"thresholds":           th,
		"synthetic_pilots":     pilots,
		"official_pilots":      official,
		"optimizer":            opts,
		"optimizer_full_check": full,
	}
	must(os.MkdirAll(out, 0o755))
	b, err := json.MarshalIndent(doc, "", "  ")
	must(err)
	path := filepath.Join(out, "go_engine_outputs.json")
	must(os.WriteFile(path, append(b, '\n'), 0o644))
	fmt.Printf("synthcheck: %d synthetic + %d official pilots evaluated with %s -> %s\n", len(pilots), len(official), rules.Version, path)
}

// loadSynthetic maps each synthetic record to the pilot schema and validates it exactly like API input.
func loadSynthetic(path string) ([]domain.Pilot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Records []map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	ps := make([]domain.Pilot, 0, len(doc.Records))
	for i, r := range doc.Records {
		obj := map[string]json.RawMessage{"id": r["record_id"]}
		for _, f := range schemaFields {
			if v, ok := r[f]; ok {
				obj[f] = v
			}
		}
		p, errs := domain.BuildPilot(obj, domain.SourceSynthetic)
		if errs != nil {
			return nil, fmt.Errorf("record %d rejected by the pilot schema: %+v", i, errs)
		}
		ps = append(ps, p)
	}
	return ps, nil
}

func evaluate(ps []domain.Pilot, th rules.Thresholds) map[string]decision.Evaluation {
	evals := map[string]decision.Evaluation{}
	for _, p := range ps {
		evals[p.ID] = decision.Evaluate(p, th, decision.Context{GroupPeers: peers(ps, p)})
	}
	return evals
}

func describe(e decision.Evaluation) pilotOut {
	m := e.Metrics
	return pilotOut{
		ID: e.PilotID, Decision: e.Decision, PrimaryReason: e.PrimaryReason, ReasonCodes: e.ReasonCodes,
		CanonicalBenefitType: e.InputSnapshot.BenefitType,
		StatedNetAnnual:      dec(m.Stated.NetAnnual), StatedPaybackMonths: dec(m.Stated.Months), StatedROIPct: dec(m.Stated.ROIPct),
		CashBenefit: amt(m.Cash.AnnualBenefit), CashNetAnnual: dec(m.Cash.NetAnnual),
		CashPaybackMonths: dec(m.Cash.Months), CashPaybackStatus: m.Cash.Status, CashROIPct: dec(m.Cash.ROIPct),
		BreakevenAdoptionPct: dec(m.BreakevenAdoptionPct), OverallReadinessPct: dec(m.OverallReadinessPct),
		ReadinessLevel: m.ReadinessLevel,
	}
}

func optimize(name string, ps []domain.Pilot, evals map[string]decision.Evaluation, budget int64, basis string, th rules.Thresholds) optOut {
	o := optOut{Dataset: name, BudgetInr: budget, Basis: basis, Selected: []string{}, Excluded: []portfolio.Exclusion{}}
	r, err := portfolio.Optimize(ps, evals, money.Rupees(budget), basis, th)
	if err != nil {
		o.Error = err.Error()
		return o
	}
	o.Selected, o.Excluded, o.Status = r.Selected, r.Excluded, r.Assessment.Status
	o.NetAnnual, o.Setup = dec(r.Assessment.NetAnnual), r.Assessment.TotalSetup.String()
	return o
}

func dec(d money.Dec) *string {
	if !d.Known() {
		return nil
	}
	s := money.FormatRat(d.R, 2)
	return &s
}

func amt(a money.Amount) *string {
	if !a.Known {
		return nil
	}
	s := money.FormatRat(new(big.Rat).Set(a.Rat()), 2)
	return &s
}

func peers(ps []domain.Pilot, p domain.Pilot) []string {
	var out []string
	for _, q := range ps {
		if q.ID != p.ID && p.OverlapGroup != nil && q.OverlapGroup != nil && *q.OverlapGroup == *p.OverlapGroup {
			out = append(out, q.ID)
		}
	}
	return out
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}
