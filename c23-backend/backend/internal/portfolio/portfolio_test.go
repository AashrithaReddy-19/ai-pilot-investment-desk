package portfolio_test

import (
	"encoding/json"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
	"c23desk/internal/testutil"
)

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func official(t *testing.T) ([]domain.Pilot, map[string]decision.Evaluation) {
	ps := testutil.Official(t).Pilots
	return ps, testutil.EvaluateAll(ps)
}

func pick(ps []domain.Pilot, ids ...string) []domain.Pilot {
	m := testutil.ByID(ps)
	var out []domain.Pilot
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}

// supportPair returns AI4 + AI5 with AI5's readiness revised to medium (test assumption), so overlap
// mechanics are tested in isolation from AI5's LOW_READINESS revise rule.
func supportPair(t *testing.T) ([]domain.Pilot, map[string]decision.Evaluation) {
	ps := testutil.Official(t).Pilots
	m := testutil.ByID(ps)
	ai5 := m["AI5"].Clone()
	ai5.ReadinessLabel = domain.Str("medium")
	sel := []domain.Pilot{m["AI4"], ai5}
	return sel, testutil.EvaluateAll(sel)
}

func codes(is []portfolio.Issue) string {
	var s []string
	for _, i := range is {
		s = append(s, i.Code)
	}
	return strings.Join(s, ",")
}

func assess(ps []domain.Pilot, ev map[string]decision.Evaluation, budget int64, res ...portfolio.Resolution) portfolio.Assessment {
	return portfolio.Assess(portfolio.Input{Budget: money.Rupees(budget), Pilots: ps, Evaluations: ev, Resolutions: res})
}

func TestValidPortfolioTotals(t *testing.T) {
	ps, ev := official(t)
	a := assess(pick(ps, "AI1", "AI2", "AI4"), ev, 600000)
	if a.Status != portfolio.StatusValid || !a.ApprovalAllowed || len(a.Blockers) != 0 {
		t.Fatal(a.Status, codes(a.Blockers))
	}
	if js(a.TotalSetup) != "600000" || js(a.TotalRecognized) != "540000" || js(a.NetAnnual) != "360000" ||
		js(a.Payback.Months) != "20" || js(a.BudgetRemaining) != "0" {
		t.Fatal(js(a))
	}
}

// Judge CASE C: budget 600000, selected setup 800000
func TestCaseC_BudgetExceededRejected(t *testing.T) {
	ps, ev := official(t)
	a := assess(pick(ps, "AI1", "AI2", "AI3", "AI4"), ev, 600000)
	if a.Status != portfolio.StatusRejected || a.ApprovalAllowed || !strings.Contains(codes(a.Blockers), "BUDGET_EXCEEDED") ||
		js(a.BudgetRemaining) != "-200000" {
		t.Fatal(a.Status, codes(a.Blockers))
	}
}

// Judge CASE D / DEMO 3: AI4 + AI5 share "support"
func TestCaseD_AI4AI5DoubleCountRisk(t *testing.T) {
	ps, ev := official(t)
	a := assess(pick(ps, "AI4", "AI5"), ev, 600000)
	if a.Status != portfolio.StatusNeedsReview || a.ApprovalAllowed {
		t.Fatal(a.Status)
	}
	if len(a.Overlaps) != 1 || a.Overlaps[0].Code != "DOUBLE_COUNT_RISK" || a.Overlaps[0].Group != "support" || a.Overlaps[0].Status != "UNRESOLVED" {
		t.Fatal(js(a.Overlaps))
	}
	// conservative: counted once (180000), never 360000
	if js(a.TotalRecognized) != "180000" || js(a.Overlaps[0].ClaimedTotal) != "360000" || js(a.Overlaps[0].PoolInr) != "180000" {
		t.Fatal(js(a.Overlaps))
	}
	if !strings.Contains(codes(a.Blockers), "OVERLAP_UNRESOLVED") {
		t.Fatal(codes(a.Blockers))
	}
}

func TestReviseBlocksApproval(t *testing.T) {
	ps, ev := official(t)
	a := assess(pick(ps, "AI4", "AI5"), ev, 600000, portfolio.Resolution{Group: "support", Type: portfolio.ExcludeSecond, PrimaryPilotID: "AI4"})
	if a.Status != portfolio.StatusNeedsReview || a.ApprovalAllowed || codes(a.Blockers) != "PILOT_NEEDS_REVISION" {
		t.Fatal(a.Status, codes(a.Blockers)) // overlap resolved, but AI5 (LOW_READINESS) must be revised first
	}
}

func TestOverlapResolutions(t *testing.T) {
	sel, ev := supportPair(t)
	// spec example: AI4 120000 + AI5 60000 = 180000, NOT 360000
	a := assess(sel, ev, 600000, portfolio.Resolution{Group: "support", Type: portfolio.CustomAllocation,
		Allocations: map[string]money.Amount{"AI4": money.Rupees(120000), "AI5": money.Rupees(60000)}, Note: "reviewed with support lead"})
	if a.Status != portfolio.StatusValid || js(a.TotalRecognized) != "180000" || a.Overlaps[0].Status != "RESOLVED" {
		t.Fatal(a.Status, codes(a.Blockers), js(a.TotalRecognized))
	}
	l := map[string]portfolio.LedgerEntry{}
	for _, e := range a.Ledger {
		l[e.PilotID] = e
	}
	if js(l["AI4"].RecognizedBenefit) != "120000" || js(l["AI5"].ClaimedBenefit) != "180000" || l["AI5"].Resolution != "CUSTOM_ALLOCATION" ||
		l["AI5"].ReviewStatus != "RESOLVED" || l["AI4"].Source != domain.SourceOfficial {
		t.Fatal(js(a.Ledger))
	}
	// both running costs still count: net = 180000 - 120000
	if js(a.NetAnnual) != "60000" {
		t.Fatal(js(a.NetAnnual))
	}

	a = assess(sel, ev, 600000, portfolio.Resolution{Group: "support", Type: portfolio.ExcludeSecond, PrimaryPilotID: "AI4"})
	if js(a.TotalRecognized) != "180000" || a.Status != portfolio.StatusValid {
		t.Fatal(js(a.TotalRecognized))
	}
	a = assess(sel, ev, 600000, portfolio.Resolution{Group: "support", Type: portfolio.SplitBenefit})
	if js(a.TotalRecognized) != "180000" || js(a.Ledger[0].RecognizedBenefit) != "90000" {
		t.Fatal(js(a.Ledger))
	}
	a = assess(sel, ev, 600000, portfolio.Resolution{Group: "support", Type: portfolio.SplitBenefit, SharesPct: map[string]int64{"AI4": 70, "AI5": 30}})
	if js(a.Ledger[0].RecognizedBenefit) != "126000" || js(a.Ledger[1].RecognizedBenefit) != "54000" {
		t.Fatal(js(a.Ledger))
	}
}

func TestInvalidResolutionsNeverDoubleCount(t *testing.T) {
	sel, ev := supportPair(t)
	bad := []portfolio.Resolution{
		{Group: "support", Type: portfolio.CustomAllocation, Allocations: map[string]money.Amount{"AI4": money.Rupees(180000), "AI5": money.Rupees(180000)}},
		{Group: "support", Type: portfolio.CustomAllocation, Allocations: map[string]money.Amount{"AI4": money.Rupees(200000), "AI5": money.Rupees(0)}},
		{Group: "support", Type: portfolio.CustomAllocation, Allocations: map[string]money.Amount{"AI4": money.Rupees(1)}},
		{Group: "support", Type: portfolio.ExcludeSecond, PrimaryPilotID: "AI1"},
		{Group: "support", Type: portfolio.SplitBenefit, SharesPct: map[string]int64{"AI4": 80, "AI5": 80}},
		{Group: "support", Type: portfolio.RemoveOne, PrimaryPilotID: "AI4"},
		{Group: "support", Type: "MAGIC"},
	}
	for _, r := range bad {
		a := assess(sel, ev, 600000, r)
		if a.Status == portfolio.StatusValid || js(a.TotalRecognized) != "180000" {
			t.Errorf("%+v: status %s recognised %s", r, a.Status, js(a.TotalRecognized))
		}
	}
}

func TestStopAndIncompleteRejected(t *testing.T) {
	ps, ev := official(t)
	p := testutil.ByID(ps)["AI1"]
	p.RunningCost = money.Unknown()
	q := testutil.ByID(ps)["AI2"]
	q.RunningCost = money.Rupees(200000)
	ev2 := testutil.EvaluateAll([]domain.Pilot{p, q})
	a := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{p, q}, Evaluations: ev2})
	if a.Status != portfolio.StatusRejected || !strings.Contains(codes(a.Blockers), "FINANCE_INCOMPLETE") ||
		!strings.Contains(codes(a.Blockers), "STOP_PILOT_SELECTED") || a.TotalRunning.Known || a.NetAnnual.Known() {
		t.Fatal(codes(a.Blockers), js(a.TotalRunning))
	}
	_ = ev
}

func TestUnknownOverlapAndBudgetMissingAndEmpty(t *testing.T) {
	ps, ev := official(t)
	p := testutil.ByID(ps)["AI5"]
	p.OverlapGroup = nil
	a := assess([]domain.Pilot{p}, ev, 600000)
	if a.Status != portfolio.StatusNeedsReview || !strings.Contains(codes(a.Blockers), "OVERLAP_UNKNOWN") {
		t.Fatal(codes(a.Blockers))
	}
	a = portfolio.Assess(portfolio.Input{Budget: money.Unknown(), Pilots: pick(ps, "AI1"), Evaluations: ev})
	if a.Status != portfolio.StatusRejected || !strings.Contains(codes(a.Blockers), "BUDGET_MISSING") {
		t.Fatal(codes(a.Blockers))
	}
	if a := assess(nil, ev, 600000); a.Status != portfolio.StatusDraft || a.ApprovalAllowed {
		t.Fatal(a.Status)
	}
}

func TestTimeFreedCashBasis(t *testing.T) {
	ps, _ := official(t)
	p := testutil.ByID(ps)["AI1"]
	p.BenefitType = domain.BenefitTimeFreed
	ev := testutil.EvaluateAll([]domain.Pilot{p})
	a := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{p}, Evaluations: ev})
	if js(a.TotalRecognized) != "0" || a.Ledger[0].ReviewStatus != "NOT_RECOGNIZED" || !strings.Contains(codes(a.Warnings), "TIME_NOT_CASH") {
		t.Fatal(js(a.Ledger))
	}
	s := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{p}, Evaluations: ev, Basis: portfolio.BasisStated})
	if js(s.TotalRecognized) != "180000" {
		t.Fatal("stated basis")
	}
}

// ---------------- optimizer ----------------

func optimize(t *testing.T, ps []domain.Pilot, budget money.Amount) portfolio.OptimizeResult {
	t.Helper()
	r, err := portfolio.Optimize(ps, testutil.EvaluateAll(ps), budget, "", rules.Default())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOptimizerOfficial(t *testing.T) {
	ps, _ := official(t)
	r := optimize(t, ps, money.Rupees(600000))
	if strings.Join(r.Selected, ",") != "AI1,AI2,AI4" || r.Assessment.Status != portfolio.StatusValid || r.CombinationsEvaluated != 16 {
		t.Fatalf("%v %s %d", r.Selected, r.Assessment.Status, r.CombinationsEvaluated)
	}
	ex := map[string]string{}
	for _, e := range r.Excluded {
		ex[e.PilotID] = e.Code
	}
	if ex["AI5"] != "PILOT_NEEDS_REVISION" || ex["AI3"] != "NOT_FUNDED" {
		t.Fatal(ex)
	}
	// with AI5's readiness revised, the overlap rule is what keeps it out
	sel, _ := supportPair(t)
	rev := append(pick(ps, "AI1", "AI2", "AI3"), sel...)
	r = optimize(t, rev, money.Rupees(600000))
	for _, e := range r.Excluded {
		ex[e.PilotID] = e.Code
	}
	if strings.Join(r.Selected, ",") != "AI1,AI2,AI4" || ex["AI5"] != "DOUBLE_COUNT_RISK" {
		t.Fatal(r.Selected, ex)
	}
	big := optimize(t, ps, money.Rupees(5000000))
	if strings.Join(big.Selected, ",") != "AI1,AI2,AI3,AI4" || js(big.Assessment.TotalSetup) != "800000" {
		t.Fatal(big.Selected)
	}
}

func TestOptimizerBudgetEdges(t *testing.T) {
	ps, _ := official(t)
	for _, b := range []int64{100000, 0, 199999} {
		r := optimize(t, ps, money.Rupees(b))
		if len(r.Selected) != 0 || r.Assessment.Status != portfolio.StatusDraft || !strings.HasPrefix(r.Message, "Empty portfolio") {
			t.Fatal(b, r.Selected, r.Message)
		}
	}
	r := optimize(t, ps, money.Unknown())
	if len(r.Selected) != 0 || !strings.Contains(r.Message, "unknown") {
		t.Fatal(r.Message)
	}
}

func TestOptimizerOrderIndependent(t *testing.T) {
	ps, _ := official(t)
	want := js(optimize(t, ps, money.Rupees(600000)))
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		sh := append([]domain.Pilot(nil), ps...)
		rng.Shuffle(len(sh), func(a, b int) { sh[a], sh[b] = sh[b], sh[a] })
		if got := js(optimize(t, sh, money.Rupees(600000))); got != want {
			t.Fatal("optimizer output depends on input order")
		}
	}
}

func TestOptimizerExcludesIneligible(t *testing.T) {
	ps, _ := official(t)
	m := testutil.ByID(ps)
	a, b, c, d := m["AI1"], m["AI2"], m["AI3"], m["AI4"]
	a.RunningCost = money.Unknown()                                          // finance incomplete
	b.RunningCost = money.Rupees(200000)                                     // never pays back -> STOP
	c.OverlapGroup = nil                                                     // overlap unknown
	d.ActualQuality, d.ActualAdoption = money.Percent(10), money.Percent(90) // quality STOP
	e := m["AI5"].Clone()
	e.ID = "AI6" // a REVISE pilot (LOW_READINESS) is excluded too
	sel, _ := supportPair(t)
	r := optimize(t, []domain.Pilot{a, b, c, d, sel[1], e}, money.Rupees(5000000))
	if strings.Join(r.Selected, ",") != "AI5" {
		t.Fatal(r.Selected, js(r.Excluded))
	}
	ex := map[string]string{}
	for _, e := range r.Excluded {
		ex[e.PilotID] = e.Code
	}
	if ex["AI1"] != "FINANCE_INCOMPLETE" || ex["AI2"] != "STOP_PILOT_SELECTED" || ex["AI3"] != "OVERLAP_UNKNOWN" || ex["AI4"] != "STOP_PILOT_SELECTED" ||
		ex["AI6"] != "PILOT_NEEDS_REVISION" {
		t.Fatal(ex)
	}
}

func TestOptimizerTimeFreedExcludedOnCashBasis(t *testing.T) {
	ps, _ := official(t)
	m := testutil.ByID(ps)
	a := m["AI1"]
	a.BenefitType = domain.BenefitTimeFreed
	r := optimize(t, []domain.Pilot{a, m["AI2"]}, money.Rupees(5000000))
	if strings.Join(r.Selected, ",") != "AI2" {
		t.Fatal(r.Selected)
	}
}

func TestOptimizerTooManyCandidates(t *testing.T) {
	var ps []domain.Pilot
	base := testutil.ByID(testutil.Official(t).Pilots)["AI1"]
	for i := 0; i < portfolio.MaxCandidates+1; i++ {
		p := base.Clone()
		p.ID = "P" + string(rune('a'+i))
		g := "g" + p.ID
		p.OverlapGroup = &g
		ps = append(ps, p)
	}
	if _, err := portfolio.Optimize(ps, testutil.EvaluateAll(ps), money.Rupees(1), "", rules.Default()); err == nil {
		t.Fatal("expected ErrTooManyCandidates")
	}
}

// Invariants over many random portfolios: budget never exceeded by optimizer; recognised <= claimed;
// recognised overlap total never exceeds the pool; net = recognised - running.
func TestInvariantsRandomised(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	groups := []string{"a", "b", "c"}
	for iter := 0; iter < 300; iter++ {
		var ps []domain.Pilot
		n := 1 + rng.Intn(8)
		for i := 0; i < n; i++ {
			g := groups[rng.Intn(len(groups))]
			p := domain.Pilot{ID: "P" + string(rune('A'+i)), Task: "t", SetupCost: money.Rupees(int64(rng.Intn(300000))),
				RunningCost: money.Rupees(int64(rng.Intn(150000))), AnnualBenefit: money.Rupees(int64(rng.Intn(250000))),
				BenefitType: domain.BenefitCashable, OverlapGroup: &g, Sources: map[string]string{}}
			ps = append(ps, p)
		}
		budget := money.Rupees(int64(rng.Intn(900000)))
		ev := testutil.EvaluateAll(ps)
		r, err := portfolio.Optimize(ps, ev, budget, "", rules.Default())
		if err != nil {
			t.Fatal(err)
		}
		a := r.Assessment
		if a.TotalSetup.Known && a.TotalSetup.Paise > budget.Paise {
			t.Fatal("optimizer exceeded budget")
		}
		seen := map[string]bool{}
		for _, id := range r.Selected {
			p := testutil.ByID(ps)[id]
			if ev[id].Decision == rules.Stop || seen[*p.OverlapGroup] {
				t.Fatal("STOP pilot or duplicate group selected")
			}
			seen[*p.OverlapGroup] = true
		}
		// manual assessment of everything: recognised never exceeds claimed or pools
		all := portfolio.Assess(portfolio.Input{Budget: budget, Pilots: ps, Evaluations: ev})
		if all.TotalRecognized.Paise > all.TotalClaimed.Paise {
			t.Fatal("recognised > claimed")
		}
		for _, o := range all.Overlaps {
			if o.RecognizedTotal.Paise > o.PoolInr.Paise {
				t.Fatal("overlap double counted")
			}
		}
		if all.NetAnnual.Known() && js(all.NetAnnual) != money.FormatRat(new(big.Rat).Sub(all.TotalRecognized.Rat(), all.TotalRunning.Rat()), 2) {
			t.Fatal("net != recognised - running")
		}
		if all.Payback.Months.Known() && all.NetAnnual.R.Sign() <= 0 {
			t.Fatal("payback must be null when net <= 0")
		}
	}
}
