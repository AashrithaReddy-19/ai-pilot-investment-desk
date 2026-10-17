package decision_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/rules"
	"c23desk/internal/testutil"
)

func ai1(t *testing.T) domain.Pilot {
	return testutil.ByID(testutil.Official(t).Pilots)["AI1"]
}

func eval(p domain.Pilot) decision.Evaluation {
	return decision.Evaluate(p, rules.Default(), decision.Context{})
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func measured(p domain.Pilot, adoption, quality int64) domain.Pilot {
	p.ActualAdoption, p.ActualQuality = money.Percent(adoption), money.Percent(quality)
	return p
}

func TestOfficialPilotPrePilotIsReview(t *testing.T) {
	e := eval(ai1(t))
	if e.Decision != rules.Review || e.PrimaryReason != rules.ReasonInsufficientEvidence || e.RulesVersion != "c23-rules-v1" {
		t.Fatalf("%s %s", e.Decision, e.PrimaryReason)
	}
	if js(e.Metrics.Stated.NetAnnual) != "120000" || js(e.Metrics.Stated.MonthlyNet) != "10000" || js(e.Metrics.Stated.Months) != "20" {
		t.Fatal("reference metrics")
	}
	if e.Metrics.Stage != "PRE_PILOT" || e.Metrics.AdoptionStatus != "NOT_MEASURED" || e.Metrics.QualityStatus != "NOT_MEASURED" {
		t.Fatal("stage")
	}
	if e.InputSnapshot.ID != "AI1" || !strings.Contains(e.Explanation, "20 months") {
		t.Fatal(e.Explanation)
	}
}

func TestMeasuredGoodPilotProceeds(t *testing.T) {
	e := eval(measured(ai1(t), 70, 90))
	if e.Decision != rules.Proceed || len(e.ReasonCodes) != 0 || e.PrimaryReason != "ALL_RULES_PASSED" {
		t.Fatalf("%+v", e.ReasonCodes)
	}
}

// Judge CASE B / DEMO 6: unknown running cost
func TestCaseB_NullRunningCost(t *testing.T) {
	p := ai1(t)
	p.RunningCost = money.Unknown()
	e := eval(measured(p, 70, 90))
	if e.Decision != rules.Revise || e.PrimaryReason != rules.ReasonMissingRunning || e.Metrics.Stated.Status != "NOT_CALCULABLE" {
		t.Fatalf("%s %s %s", e.Decision, e.PrimaryReason, e.Metrics.Stated.Status)
	}
	if e.Metrics.Stated.Months.Known() || js(e.Metrics.Stated.Months) != "null" || e.Metrics.BreakevenAdoptionPct.Known() {
		t.Fatal("payback must be null, never computed from 0")
	}
}

func TestMissingSetupAndBenefit(t *testing.T) {
	p := ai1(t)
	p.SetupCost, p.AnnualBenefit = money.Unknown(), money.Unknown()
	e := eval(p)
	if e.Decision != rules.Revise || !has(e.ReasonCodes, rules.ReasonMissingSetup) || !has(e.ReasonCodes, rules.ReasonMissingBenefit) {
		t.Fatal(e.ReasonCodes)
	}
}

// Judge CASE G: running 200000 > benefit 180000
func TestCaseG_NeverPaysBack(t *testing.T) {
	p := ai1(t)
	p.RunningCost = money.Rupees(200000)
	e := eval(measured(p, 90, 95))
	if e.Decision != rules.Stop || e.PrimaryReason != rules.ReasonNeverPaysBack || js(e.Metrics.Stated.NetAnnual) != "-20000" ||
		e.Metrics.Stated.Status != "NEVER_PAYS_BACK" || e.Metrics.Stated.Months.Known() {
		t.Fatalf("%s %s %s", e.Decision, e.PrimaryReason, js(e.Metrics.Stated))
	}
}

// Judge CASE E: adoption 70 -> 20 (break-even 33.33%)
func TestCaseE_AdoptionBelowBreakevenStops(t *testing.T) {
	e := eval(measured(ai1(t), 20, 90))
	if e.Decision != rules.Stop || e.PrimaryReason != rules.ReasonAdoptionBelowBreak || !has(e.ReasonCodes, rules.ReasonLowAdoption) {
		t.Fatalf("%s %v", e.Decision, e.ReasonCodes)
	}
	f := e.FailedRules[0]
	if f.Threshold != "33.33" || f.Actual != "20" {
		t.Fatalf("%+v", f)
	}
}

// Phase 10 example: actual 31, minimum 50 -> LOW_ADOPTION
func TestLowAdoptionAboveBreakevenRevises(t *testing.T) {
	p := ai1(t)
	p.MinimumAdoption = money.Percent(50)
	e := eval(measured(p, 45, 90))
	if e.Decision != rules.Revise || e.PrimaryReason != rules.ReasonLowAdoption || e.Metrics.MinimumAdoption.Source != "pilot" {
		t.Fatalf("%s %v", e.Decision, e.ReasonCodes)
	}
	e = eval(measured(p, 31, 90)) // 31 < break-even 33.33 too -> STOP
	if e.Decision != rules.Stop || !has(e.ReasonCodes, rules.ReasonLowAdoption) {
		t.Fatal(e.ReasonCodes)
	}
}

// Judge CASE F / DEMO 5: quality 90 -> 60; quality STOP overrides good finance
func TestCaseF_QualityFailureStops(t *testing.T) {
	e := eval(measured(ai1(t), 95, 60))
	if e.Decision != rules.Stop || e.PrimaryReason != rules.ReasonQualityFailed {
		t.Fatalf("%s %s", e.Decision, e.PrimaryReason)
	}
	if js(e.Metrics.Stated.Months) != "20" {
		t.Fatal("finance is still reported, but cannot rescue the pilot")
	}
	p := ai1(t)
	p.MinimumQuality = money.Percent(68)
	if eval(measured(p, 95, 68)).Decision != rules.Proceed {
		t.Fatal("boundary: quality equal to minimum passes")
	}
	if eval(measured(p, 95, 67)).Decision != rules.Stop {
		t.Fatal("67 < 68 must stop")
	}
}

func TestReadiness(t *testing.T) {
	p := measured(ai1(t), 70, 90)
	p.ReadinessLabel = domain.Str("low")
	if e := eval(p); e.Decision != rules.Revise || e.PrimaryReason != rules.ReasonLowReadiness {
		t.Fatal(e.ReasonCodes)
	}
	p.DataReadiness, p.TechnicalReadiness = money.Percent(40), money.Percent(50) // numeric wins over label: mean 45 < 50
	e := eval(p)
	if e.Metrics.ReadinessLevel != "low" || js(e.Metrics.OverallReadinessPct) != "45" || !strings.Contains(e.Metrics.ReadinessBasis, "mean of 2") {
		t.Fatal(js(e.Metrics))
	}
	p.ProcessReadiness = money.Percent(100) // mean 63.33 -> medium
	if e := eval(p); e.Decision != rules.Proceed || e.Metrics.ReadinessLevel != "medium" {
		t.Fatal(e.ReasonCodes)
	}
	p.ReadinessLabel, p.DataReadiness, p.TechnicalReadiness, p.ProcessReadiness = nil, money.Pct{}, money.Pct{}, money.Pct{}
	if e := eval(p); !has(e.Warnings, rules.WarnReadinessUnknown) || e.Decision != rules.Proceed {
		t.Fatal("unknown readiness is a warning, not a fabricated level")
	}
	p.DataReadiness, p.TechnicalReadiness, p.ProcessReadiness = money.Percent(0), money.Percent(0), money.Percent(0)
	if eval(p).Metrics.ReadinessLevel != "low" {
		t.Fatal("all-zero readiness")
	}
}

func TestTimeFreedIsNotCash(t *testing.T) {
	p := measured(ai1(t), 70, 90)
	p.BenefitType = domain.BenefitTimeFreed
	e := eval(p)
	if e.Decision != rules.Review || e.PrimaryReason != rules.ReasonTimeNotCash {
		t.Fatal(e.ReasonCodes)
	}
	if e.Metrics.Cash.NetAnnual.Known() || js(e.Metrics.Stated.Months) != "20" {
		t.Fatal("cash basis must be unknown, stated basis unchanged")
	}
	p.RealizedCashSaving = money.Rupees(90000)
	e = eval(p)
	if e.Decision != rules.Proceed || js(e.Metrics.Cash.NetAnnual) != "30000" || js(e.Metrics.Cash.Months) != "80" {
		t.Fatal(js(e.Metrics.Cash))
	}
}

func TestOverlapUnknownAndSharedGroup(t *testing.T) {
	p := measured(ai1(t), 70, 90)
	p.OverlapGroup = nil
	if e := eval(p); e.Decision != rules.Review || e.PrimaryReason != rules.ReasonOverlapUnknown {
		t.Fatal(e.ReasonCodes)
	}
	r := testutil.EvaluateAll(testutil.Official(t).Pilots)
	if !strings.Contains(strings.Join(r["AI4"].Warnings, ";"), "DOUBLE_COUNT_RISK: shares benefit group 'support' with AI5") {
		t.Fatal(r["AI4"].Warnings)
	}
	if strings.Contains(strings.Join(r["AI1"].Warnings, ";"), "DOUBLE_COUNT_RISK") {
		t.Fatal("AI1 has no overlap")
	}
}

func TestOfficialReadinessDrivesAI5Revise(t *testing.T) {
	r := testutil.EvaluateAll(testutil.Official(t).Pilots)
	want := map[string]string{"AI1": rules.Review, "AI2": rules.Review, "AI3": rules.Review, "AI4": rules.Review, "AI5": rules.Revise}
	for id, d := range want {
		if r[id].Decision != d {
			t.Errorf("%s: %s (%v)", id, r[id].Decision, r[id].ReasonCodes)
		}
	}
}

func TestMostSevereWinsAndAllReasonsReported(t *testing.T) {
	p := ai1(t)
	p.RunningCost = money.Unknown()
	p.ReadinessLabel = domain.Str("low")
	e := eval(measured(p, 70, 50)) // quality fail (STOP) + missing run cost (REVISE) + low readiness (REVISE)
	if e.Decision != rules.Stop || e.PrimaryReason != rules.ReasonQualityFailed {
		t.Fatal(e.Decision, e.PrimaryReason)
	}
	if strings.Join(e.ReasonCodes, ",") != "MISSING_RUNNING_COST,QUALITY_THRESHOLD_FAILED,LOW_READINESS" {
		t.Fatal(e.ReasonCodes)
	}
}

func TestDeterministic(t *testing.T) {
	p := measured(ai1(t), 45, 85)
	if js(eval(p)) != js(eval(p)) {
		t.Fatal("same input must give byte-identical output")
	}
}

func TestZeroSetupWarning(t *testing.T) {
	p := measured(ai1(t), 70, 90)
	p.SetupCost = money.Rupees(0)
	e := eval(p)
	if !has(e.Warnings, rules.WarnZeroSetup) || e.Metrics.Stated.Status != "IMMEDIATE" {
		t.Fatal(e.Warnings)
	}
}

// Rule defaults must match the team assumptions file so the two never drift.
func TestDefaultsMatchExtensionFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(testutil.RawDir(), "extension.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ext struct {
		Defaults map[string]int64 `json:"defaults"`
	}
	_ = json.Unmarshal(b, &ext)
	d := rules.Default()
	if ext.Defaults["target_adoption_pct"] != d.MinimumAdoptionPct || ext.Defaults["quality_threshold_pct"] != d.MinimumQualityPct ||
		ext.Defaults["assumed_run_cost_pct_of_setup"] != d.AssumedRunCostPct {
		t.Fatalf("rules defaults drifted from extension.json: %v vs %+v", ext.Defaults, d)
	}
}
