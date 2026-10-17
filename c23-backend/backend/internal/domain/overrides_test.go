package domain

import (
	"encoding/json"
	"testing"
)

func TestApplyOverridesEveryField(t *testing.T) {
	base, errs := parse(t, `,"description":"d","expected_adoption_pct":70`)
	if errs != nil {
		t.Fatal(errs)
	}
	ov := `{"task":"new task","description":"nd","setup_cost_inr":1,"annual_running_cost_inr":2,"annual_benefit_inr":3,
		"capacity_value_inr":4,"realized_cash_saving_inr":5,"benefit_type":"time freed","overlap_group":"Ops","readiness_label":"LOW",
		"expected_adoption_pct":1,"actual_adoption_pct":2,"minimum_adoption_pct":3,"expected_quality_pct":4,"actual_quality_pct":5,
		"minimum_quality_pct":6,"data_readiness_pct":7,"technical_readiness_pct":8,"process_readiness_pct":9}`
	p, keys, errs := ApplyOverrides(base, json.RawMessage(ov), SourceTeam)
	if errs != nil || len(keys) != 19 {
		t.Fatal(errs, keys)
	}
	if p.Task != "new task" || *p.Description != "nd" || p.SetupCost.String() != "1" || p.RunningCost.String() != "2" ||
		p.AnnualBenefit.String() != "3" || p.CapacityValue.String() != "4" || p.RealizedCashSaving.String() != "5" ||
		p.BenefitType != BenefitTimeFreed || *p.OverlapGroup != "ops" || *p.ReadinessLabel != "low" ||
		p.ExpectedAdoption.String() != "1" || p.ActualAdoption.String() != "2" || p.MinimumAdoption.String() != "3" ||
		p.ExpectedQuality.String() != "4" || p.ActualQuality.String() != "5" || p.MinimumQuality.String() != "6" ||
		p.DataReadiness.String() != "7" || p.TechnicalReadiness.String() != "8" || p.ProcessReadiness.String() != "9" {
		t.Fatalf("%+v", p)
	}
	if p.Sources["setup_cost_inr"] != SourceTeam || p.Sources["actual_quality_pct"] != SourceObserved {
		t.Fatal(p.Sources)
	}
	if base.SetupCost.String() != "200000" || base.Task == "new task" {
		t.Fatal("base mutated")
	}
	if _, _, errs := ApplyOverrides(base, json.RawMessage(`[]`), SourceTeam); errs == nil {
		t.Fatal("non-object overrides")
	}
	if _, _, errs := ApplyOverrides(base, json.RawMessage(`{"task":""}`), SourceTeam); errs == nil || errs[0].Field != "overrides.task" {
		t.Fatal(errs)
	}
}

func TestValidIDAndLabels(t *testing.T) {
	for id, ok := range map[string]bool{"AI1": true, "a_b-C9": true, "": false, "../x": false, "a b": false, "x;": false} {
		if ValidID(id) != ok {
			t.Errorf("%q", id)
		}
	}
	if NormalizeLabel("  Customer   Support ") != "customer support" {
		t.Fatal("normalize")
	}
	long := `"` + string(make([]byte, 0)) + `"`
	_ = long
	_, errs := parse(t, `,"benefit_type":"`+string(make([]rune, 0))+repeat("x", 101)+`"`)
	if codes(errs)["benefit_type"] != CodeTooLong {
		t.Fatal(errs)
	}
	_, errs = parse(t, `,"description":"`+repeat("x", 501)+`"`)
	if codes(errs)["description"] != CodeTooLong {
		t.Fatal(errs)
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
