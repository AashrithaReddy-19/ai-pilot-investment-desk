package domain

import (
	"encoding/json"
	"testing"
)

const base = `"id":"AI1","task":"Invoice field extraction","setup_cost_inr":200000,"annual_running_cost_inr":60000,` +
	`"annual_benefit_inr":180000,"benefit_type":"cashable assumption","overlap_group":"finance"`

// parse merges `extra` (",key:value,...") over the base pilot. Keys are replaced, never duplicated
// (duplicate keys are themselves rejected: see TestDuplicateKeysRejected).
func parse(t *testing.T, extra string) (Pilot, []FieldError) {
	t.Helper()
	var obj, over map[string]json.RawMessage
	_ = json.Unmarshal([]byte("{"+base+"}"), &obj)
	if extra != "" {
		if err := json.Unmarshal([]byte("{"+extra[1:]+"}"), &over); err != nil {
			t.Fatalf("bad test input %q: %v", extra, err)
		}
	}
	for k, v := range over {
		obj[k] = v
	}
	b, _ := json.Marshal(obj)
	return ParsePilot(b, SourceTeam)
}

func TestDuplicateKeysRejected(t *testing.T) {
	for _, s := range []string{`{"id":"A","task":"t","setup_cost_inr":-1,"setup_cost_inr":5}`, `{"id":"A","id":"B","task":"t"}`} {
		_, errs := ParsePilot(json.RawMessage(s), SourceTeam)
		if len(errs) != 1 || errs[0].Code != CodeDuplicateKey {
			t.Errorf("%s: %v", s, errs)
		}
	}
	if _, errs := ParsePilot(json.RawMessage(`{"id":"A","task":"t","x":{"k":1,"k":2}}`), SourceTeam); errs[0].Code != CodeUnknownField {
		t.Fatal("nested objects are not part of the schema; top-level check suffices", errs)
	}
}

func codes(errs []FieldError) map[string]string {
	m := map[string]string{}
	for _, e := range errs {
		m[e.Field] = e.Code
	}
	return m
}

func TestValidPilot(t *testing.T) {
	p, errs := parse(t, "")
	if errs != nil {
		t.Fatal(errs)
	}
	if p.SetupCost.Paise != 20000000 || p.BenefitType != BenefitCashable || *p.OverlapGroup != "finance" {
		t.Fatalf("%+v", p)
	}
	if p.Sources["setup_cost_inr"] != SourceTeam || p.Sources["actual_adoption_pct"] != SourceUnknown {
		t.Fatal(p.Sources)
	}
	if p.ActualAdoption.Known || p.ActualQuality.Known {
		t.Fatal("absent actuals must be NOT_MEASURED (unknown), never 0")
	}
}

func TestNegativeInputs(t *testing.T) {
	cases := map[string][2]string{
		`"setup_cost_inr":-5`:            {"setup_cost_inr", "NEGATIVE_VALUE"},
		`"annual_running_cost_inr":-1`:   {"annual_running_cost_inr", "NEGATIVE_VALUE"},
		`"annual_benefit_inr":-0.01`:     {"annual_benefit_inr", "NEGATIVE_VALUE"},
		`"setup_cost_inr":"2 lakh"`:      {"setup_cost_inr", "NOT_NUMERIC"},
		`"setup_cost_inr":"200000"`:      {"setup_cost_inr", "NOT_NUMERIC"},
		`"annual_benefit_inr":true`:      {"annual_benefit_inr", "NOT_NUMERIC"},
		`"annual_benefit_inr":[1]`:       {"annual_benefit_inr", "NOT_NUMERIC"},
		`"setup_cost_inr":0.001`:         {"setup_cost_inr", "TOO_PRECISE"},
		`"setup_cost_inr":1e30`:          {"setup_cost_inr", "TOO_LARGE"},
		`"expected_adoption_pct":101`:    {"expected_adoption_pct", "OUT_OF_RANGE"},
		`"actual_quality_pct":100.5`:     {"actual_quality_pct", "OUT_OF_RANGE"},
		`"data_readiness_pct":150`:       {"data_readiness_pct", "OUT_OF_RANGE"},
		`"actual_adoption_pct":-1`:       {"actual_adoption_pct", "OUT_OF_RANGE"},
		`"task":""`:                      {"task", "EMPTY"},
		`"task":42`:                      {"task", "NOT_TEXT"},
		`"overlap_group":"../etc"`:       {"overlap_group", "INVALID_LABEL"},
		`"overlap_group":7`:              {"overlap_group", "NOT_TEXT"},
		`"readiness_label":"very high"`:  {"readiness_label", "INVALID_ENUM"},
		`"id":"../AI1"`:                  {"id", "INVALID_ID"},
		`"id":"AI1'; DROP TABLE pilots"`: {"id", "INVALID_ID"},
		`"id":null`:                      {"id", "REQUIRED"},
		`"task":null`:                    {"task", "REQUIRED"},
		`"is_admin":true`:                {"is_admin", "UNKNOWN_FIELD"},
	}
	for extra, want := range cases {
		// later duplicate keys override earlier ones in encoding/json
		_, errs := parse(t, ","+extra)
		if codes(errs)[want[0]] != want[1] {
			t.Errorf("%s: want %s=%s, got %v", extra, want[0], want[1], errs)
		}
	}
}

func TestAllErrorsCollected(t *testing.T) {
	_, errs := ParsePilot(json.RawMessage(`{"id":"","task":"","setup_cost_inr":-1,"annual_benefit_inr":"x"}`), SourceTeam)
	if len(errs) < 4 {
		t.Fatalf("expected every field error, got %v", errs)
	}
}

func TestNullMeansUnknown(t *testing.T) {
	p, errs := ParsePilot(json.RawMessage(`{"id":"X","task":"t","setup_cost_inr":null,"annual_benefit_inr":null,"overlap_group":null}`), SourceTeam)
	if errs != nil {
		t.Fatal(errs)
	}
	if p.SetupCost.Known || p.AnnualBenefit.Known || p.RunningCost.Known || p.OverlapGroup != nil || p.FinanceComplete() {
		t.Fatal("null/absent must stay unknown")
	}
	if p.Sources["overlap_group"] != SourceUnknown || p.BenefitType != BenefitUnknown {
		t.Fatal(p.Sources)
	}
}

func TestNotObject(t *testing.T) {
	for _, s := range []string{`[]`, `"x"`, `5`, `{bad`, ``} {
		if _, errs := ParsePilot(json.RawMessage(s), SourceTeam); len(errs) != 1 || errs[0].Code != CodeNotObject {
			t.Errorf("%q: %v", s, errs)
		}
	}
}

func TestNormalisationKeepsRaw(t *testing.T) {
	p, errs := parse(t, `,"overlap_group":"  SUPPORT ","readiness_label":" High ","benefit_type":"Time freed"`)
	if errs != nil {
		t.Fatal(errs)
	}
	if *p.OverlapGroup != "support" || *p.OverlapGroupRaw != "  SUPPORT " || *p.ReadinessLabel != "high" || p.BenefitType != BenefitTimeFreed {
		t.Fatalf("%+v", p)
	}
	q, _ := parse(t, `,"overlap_group":"   "`)
	if q.OverlapGroup != nil {
		t.Fatal("blank overlap group must be unknown, not a group")
	}
}

func TestCanonicalBenefitType(t *testing.T) {
	for raw, want := range map[string]string{"cashable assumption": BenefitCashable, "Time freed": BenefitTimeFreed,
		"cashable time": BenefitTimeFreed, "revenue uplift": BenefitOther, " ": BenefitUnknown} {
		r := raw
		if got := CanonicalBenefitType(&r); got != want {
			t.Errorf("%q: %s", raw, got)
		}
	}
	if CanonicalBenefitType(nil) != BenefitUnknown {
		t.Fatal("nil")
	}
}

func TestMergeObjects(t *testing.T) {
	fx := map[string]json.RawMessage{"id": json.RawMessage(`"AI1"`), "expected_adoption_pct": json.RawMessage(`30`)}
	ext := map[string]json.RawMessage{"id": json.RawMessage(`"AI1"`), "adoption_pct": json.RawMessage(`70`),
		"readiness": json.RawMessage(`"high"`), "setup_cost_inr": json.RawMessage(`1`), "bogus": json.RawMessage(`1`)}
	out, warns, errs := MergeObjects(fx, ext)
	if string(out["expected_adoption_pct"]) != "30" || string(out["readiness_label"]) != `"high"` {
		t.Fatal(out)
	}
	if len(warns) != 1 || len(errs) != 2 {
		t.Fatalf("warns %v errs %v", warns, errs)
	}
}

func TestCloneIsDeep(t *testing.T) {
	p, _ := parse(t, "")
	c := p.Clone()
	*c.OverlapGroup = "x"
	c.Sources["id"] = "changed"
	if *p.OverlapGroup != "finance" || p.Sources["id"] == "changed" {
		t.Fatal("clone shares memory")
	}
}
