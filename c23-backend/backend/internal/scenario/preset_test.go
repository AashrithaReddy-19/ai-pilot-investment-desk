package scenario_test

import (
	"encoding/json"
	"strings"
	"testing"

	"c23desk/internal/rules"
	"c23desk/internal/scenario"
	"c23desk/internal/testutil"
)

func preset(t *testing.T, id, pilot string) (scenario.Result, string) {
	t.Helper()
	base := testutil.ByID(testutil.Official(t).Pilots)[pilot]
	r, errs := scenario.Run(base, scenario.Request{Preset: id}, rules.Default(), nil)
	if errs != nil {
		return r, errs[0].Code
	}
	return r, ""
}

func TestPresetsUseTheNormalEngine(t *testing.T) {
	r, _ := preset(t, "running_cost_up_25", "AI1") // 60000 -> 75000: net 105000, payback 22.86
	if js(r.Scenario.Metrics.Stated.NetAnnual) != "105000" || js(r.Scenario.Metrics.Stated.Months) != "22.86" || js(r.Changes[0].After) != "75000" {
		t.Fatal(js(r.Scenario.Metrics.Stated), js(r.Changes))
	}
	r, _ = preset(t, "benefit_down_20", "AI1") // 180000 -> 144000: net 84000
	if js(r.Scenario.Metrics.Stated.NetAnnual) != "84000" || r.Assumptions[0].Rule != "annual_benefit_inr -20%" {
		t.Fatal(js(r.Scenario.Metrics.Stated), r.Assumptions)
	}
	r, _ = preset(t, "adoption_down_30", "AI1") // expected 70 -> actual 49 (labelled assumption): above break-even 33.33, below 60
	if js(r.Scenario.InputSnapshot.ActualAdoption) != "49" || r.Scenario.Decision != rules.Revise ||
		!strings.Contains(strings.Join(r.Scenario.ReasonCodes, ","), rules.ReasonLowAdoption) || !strings.Contains(r.Assumptions[0].Rule, "not measured yet") {
		t.Fatal(r.Scenario.ReasonCodes, r.Assumptions)
	}
	r, _ = preset(t, "quality_down_10", "AI1") // 80 * 0.9 = 72 < 80 -> STOP
	if js(r.Scenario.InputSnapshot.ActualQuality) != "72" || r.Scenario.Decision != rules.Stop || r.Scenario.PrimaryReason != rules.ReasonQualityFailed {
		t.Fatal(r.Scenario.ReasonCodes)
	}
	r, _ = preset(t, "missing_running_cost", "AI1")
	if r.Scenario.Decision != rules.Revise || r.Scenario.Metrics.Stated.Months.Known() {
		t.Fatal(r.Scenario.ReasonCodes)
	}
	if js(r.Baseline.Metrics.Stated.Months) != "20" {
		t.Fatal("baseline untouched")
	}
}

func TestPresetErrorsAndAdjustRules(t *testing.T) {
	if _, code := preset(t, "magic", "AI1"); code != "INVALID_ENUM" {
		t.Fatal(code)
	}
	base := testutil.ByID(testutil.Official(t).Pilots)["AI1"]
	run := func(req scenario.Request) string {
		_, errs := scenario.Run(base, req, rules.Default(), nil)
		if errs != nil {
			return errs[0].Code
		}
		return ""
	}
	if c := run(scenario.Request{Preset: "benefit_down_20", AdjustPct: map[string]int64{"setup_cost_inr": 1}}); c != "PRESET_EXCLUSIVE" {
		t.Fatal(c)
	}
	if c := run(scenario.Request{AdjustPct: map[string]int64{"actual_quality_pct": -10}}); c != "ADJUST_UNKNOWN_BASE" {
		t.Fatal("unknown cannot be adjusted:", c)
	}
	if c := run(scenario.Request{Overrides: json.RawMessage(`{"annual_running_cost_inr":null}`), AdjustPct: map[string]int64{"annual_running_cost_inr": 10}}); c != "ADJUST_UNKNOWN_BASE" {
		t.Fatal(c)
	}
	if c := run(scenario.Request{AdjustPct: map[string]int64{"setup_cost_inr": -101}}); c != "OUT_OF_RANGE" {
		t.Fatal(c)
	}
	if c := run(scenario.Request{AdjustPct: map[string]int64{"id": 5}}); c != "UNKNOWN_FIELD" {
		t.Fatal(c)
	}
	r, _ := scenario.Run(base, scenario.Request{AdjustPct: map[string]int64{"expected_adoption_pct": 100}}, rules.Default(), nil)
	if js(r.Scenario.InputSnapshot.ExpectedAdoption) != "100" || !strings.Contains(r.Assumptions[0].Rule, "capped") {
		t.Fatal("percent capped at 100", r.Assumptions)
	}
	r, _ = scenario.Run(base, scenario.Request{AdjustPct: map[string]int64{"annual_running_cost_inr": 100}}, rules.Default(), nil)
	if js(r.Scenario.Metrics.Stated.Months) != "40" {
		t.Fatal("+100% running cost = 120000 -> 40 months")
	}
	if len(scenario.Presets) != 5 {
		t.Fatal("presets list")
	}
}
