package scenario_test

import (
	"encoding/json"
	"strings"
	"testing"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/rules"
	"c23desk/internal/scenario"
	"c23desk/internal/testutil"
)

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func run(t *testing.T, ov string, assume bool) (domain.Pilot, scenario.Result, []domain.FieldError) {
	base := testutil.ByID(testutil.Official(t).Pilots)["AI1"]
	r, errs := scenario.Run(base, scenario.Request{Name: "judge", Overrides: json.RawMessage(ov), AssumeMissingRunCost: assume}, rules.Default(), nil)
	return base, r, errs
}

func TestPeersRecomputedAfterOverlapOverride(t *testing.T) {
	ps := testutil.Official(t).Pilots
	ai5 := testutil.ByID(ps)["AI5"]
	peers := func(p domain.Pilot) []string { return testutil.Peers(ps, p) }
	r, _ := scenario.Run(ai5, scenario.Request{Overrides: json.RawMessage(`{"overlap_group":null}`)}, rules.Default(), peers)
	if strings.Contains(strings.Join(r.Scenario.Warnings, ";"), "DOUBLE_COUNT_RISK") || !strings.Contains(strings.Join(r.Baseline.Warnings, ";"), "DOUBLE_COUNT_RISK") {
		t.Fatal(r.Scenario.Warnings, r.Baseline.Warnings)
	}
	r, _ = scenario.Run(ai5, scenario.Request{Overrides: json.RawMessage(`{"overlap_group":"finance"}`)}, rules.Default(), peers)
	if !strings.Contains(strings.Join(r.Scenario.Warnings, ";"), "with AI1") {
		t.Fatal(r.Scenario.Warnings)
	}
	_ = decision.Context{}
}

// Judge CASE A / DEMO 2
func TestCaseA_RunningCostDoubled40Months(t *testing.T) {
	base, r, errs := run(t, `{"annual_running_cost_inr":120000}`, false)
	if errs != nil {
		t.Fatal(errs)
	}
	s := r.Scenario.Metrics.Stated
	if js(s.NetAnnual) != "60000" || js(s.MonthlyNet) != "5000" || js(s.Months) != "40" {
		t.Fatal(js(s))
	}
	if js(r.Baseline.Metrics.Stated.Months) != "20" || base.RunningCost.String() != "60000" {
		t.Fatal("base pilot must be unchanged")
	}
	if r.Scenario.InputSnapshot.Sources["annual_running_cost_inr"] != domain.SourceTeam ||
		r.Baseline.InputSnapshot.Sources["annual_running_cost_inr"] != domain.SourceOfficial {
		t.Fatal("provenance")
	}
	if len(r.Changes) != 1 || js(r.Changes[0].Before) != "60000" || js(r.Changes[0].After) != "120000" {
		t.Fatal(js(r.Changes))
	}
}

func TestNullRunCostStrictVsAssumed(t *testing.T) {
	_, r, _ := run(t, `{"annual_running_cost_inr":null}`, false)
	if r.Scenario.Decision != rules.Revise || r.Scenario.Metrics.Stated.Months.Known() {
		t.Fatal(r.Scenario.Decision)
	}
	_, r, _ = run(t, `{"annual_running_cost_inr":null}`, true)
	if js(r.Scenario.Metrics.Stated.Months) != "17.14" || len(r.Assumptions) != 1 ||
		!strings.Contains(strings.Join(r.Scenario.Warnings, ";"), "RUN_COST_ASSUMED") {
		t.Fatal(js(r.Scenario.Metrics.Stated), r.Assumptions)
	}
}

func TestQualityAndAdoptionScenarios(t *testing.T) {
	_, r, _ := run(t, `{"actual_adoption_pct":70,"actual_quality_pct":60}`, false)
	if r.Scenario.Decision != rules.Stop || r.Scenario.PrimaryReason != rules.ReasonQualityFailed {
		t.Fatal(r.Scenario.ReasonCodes)
	}
	if r.Scenario.InputSnapshot.Sources["actual_quality_pct"] != domain.SourceObserved {
		t.Fatal("actual_* overrides stay OBSERVED")
	}
	_, r, _ = run(t, `{"actual_adoption_pct":20,"actual_quality_pct":90}`, false)
	if r.Scenario.Decision != rules.Stop {
		t.Fatal(r.Scenario.ReasonCodes)
	}
}

func TestInvalidOverrides(t *testing.T) {
	for _, ov := range []string{`{"annual_running_cost_inr":-1}`, `{"id":"X"}`, `{"setup_cost_inr":"2 lakh"}`, `{"nope":1}`, `[1]`, `{"actual_quality_pct":101}`} {
		if _, _, errs := run(t, ov, false); errs == nil {
			t.Errorf("%s accepted", ov)
		}
	}
	if _, r, errs := run(t, ``, false); errs != nil || len(r.Changes) != 0 {
		t.Fatal("empty overrides = baseline")
	}
}
