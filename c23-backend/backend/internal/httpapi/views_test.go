package httpapi_test

import (
	"strings"
	"testing"
)

func TestDashboardRecommendedThenSaved(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	d := e.do("GET", "/api/v1/dashboard", "").json()
	if s(d["portfolio_source"]) != "recommended" || s(get(d, "recommended.selected")) != `["AI1","AI2","AI4"]` ||
		s(get(d, "decision_counts.REVIEW")) != "4" || s(get(d, "decision_counts.REVISE")) != "1" || s(get(d, "missing_evidence_count")) != "5" ||
		s(get(d, "defaults.pilot_budget_inr")) != "600000" || s(get(d, "shared_overlap_groups.support")) != `["AI4","AI5"]` {
		t.Fatal(d)
	}
	p := e.portfolio("dash", 600000, "AI4", "AI5")
	d = e.do("GET", "/api/v1/dashboard", "").json()
	if s(d["portfolio_source"]) != "saved" || s(get(d, "portfolio.id")) != s(p["id"]) || s(get(d, "unresolved_overlap_count")) != "1" {
		t.Fatal(d)
	}
	ok := e.portfolio("approved one", 600000, "AI1")
	if r := e.do("POST", "/api/v1/portfolios/"+s(ok["id"])+"/approve", `{"version":1}`); r.Code != 200 {
		t.Fatal(r.Code)
	}
	e.portfolio("newer draft", 600000, "AI1", "AI2", "AI3", "AI4")
	if d := e.do("GET", "/api/v1/dashboard", "").json(); s(get(d, "portfolio.id")) != s(ok["id"]) {
		t.Fatal("dashboard must prefer the newest approved portfolio over a newer rejected draft")
	}
	if r := e.do("GET", "/api/v1/dashboard?portfolio_id=bad", ""); r.Code != 400 {
		t.Fatal(r.Code)
	}
}

func TestPreviewPortfolioLiveBuilder(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	r := e.do("POST", "/api/v1/preview/portfolio", `{"budget_inr":600000,"pilot_ids":["AI1","AI2","AI3","AI4"]}`)
	m := r.json()
	if r.Code != 200 || s(get(m, "assessment.status")) != "REJECTED" || s(get(m, "assessment.budget_remaining_inr")) != "-200000" || m["persisted"] != false {
		t.Fatal(r.Code, string(r.Body))
	}
	if l := e.do("GET", "/api/v1/portfolios", "").json(); s(l["count"]) != "0" {
		t.Fatal("preview must not save")
	}
	for _, bad := range []string{`{"pilot_ids":["AI1"]}`, `{"budget_inr":-1}`, `{"budget_inr":1,"pilot_ids":["AI1","AI1"]}`,
		`{"budget_inr":1,"pilot_ids":["../x"]}`, `{"budget_inr":1,"pilot_ids":["AI99"]}`, `{"budget_inr":1,"basis":"x"}`} {
		if r := e.do("POST", "/api/v1/preview/portfolio", bad); r.Code != 422 {
			t.Errorf("%s: %d", bad, r.Code)
		}
	}
}

// DEMO 8: budget reduced -> portfolio changes dynamically (same optimizer, nothing saved)
func TestDemo8_BudgetReduced(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	sel := func(budget string) string {
		r := e.do("POST", "/api/v1/preview/optimize", `{"budget_inr":`+budget+`}`)
		if r.Code != 200 {
			t.Fatal(r.Code, string(r.Body))
		}
		return s(get(r.json(), "optimization.selected"))
	}
	if a, b, c, d := sel("600000"), sel("400000"), sel("200000"), sel("100000"); a != `["AI1","AI2","AI4"]` || b != `["AI1","AI4"]` || c != `["AI1"]` || d != "[]" {
		t.Fatal(a, b, c, d)
	}
}

func TestScenarioPresetsViaAPIAndActivity(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	ps := e.do("GET", "/api/v1/scenario-presets", "").json()
	if len(get(ps, "presets").([]any)) != 5 {
		t.Fatal(ps)
	}
	r := e.do("POST", "/api/v1/pilots/AI1/scenarios", `{"name":"shock","preset":"running_cost_up_25"}`)
	if r.Code != 201 || s(get(r.json(), "result.scenario.calculated_metrics.stated_basis.payback_months")) != "22.86" {
		t.Fatal(r.Code, string(r.Body))
	}
	if r := e.do("POST", "/api/v1/pilots/AI1/scenarios", `{"preset":"quality_down_10"}`); s(get(r.json(), "result.scenario.decision")) != "STOP" {
		t.Fatal(string(r.Body))
	}
	if r := e.do("POST", "/api/v1/pilots/AI1/scenarios", `{"preset":"nope"}`); r.Code != 422 {
		t.Fatal(r.Code)
	}
	if r := e.do("POST", "/api/v1/pilots/AI1/scenarios", `{"adjust_pct":{"annual_running_cost_inr":100}}`); s(get(r.json(), "result.scenario.calculated_metrics.stated_basis.payback_months")) != "40" {
		t.Fatal(string(r.Body))
	}
	a := e.do("GET", "/api/v1/activity?limit=3", "").json()
	if n := len(get(a, "entries").([]any)); n != 3 || !strings.Contains(string(mustJSON(a)), `"action":"import"`) {
		t.Fatal(n, a)
	}
	if d := e.do("GET", "/api/v1/defaults", "").json(); s(get(d, "pilot_budget_source")) != "TEAM_ASSUMPTION (extension.json)" {
		t.Fatal(d)
	}
}
