// Command evidence runs the judge challenge scenarios through the real deterministic engine and writes:
//
//	reports/judge_evidence.json + .md                (backend evidence for judges)
//	../decision-engine/evals/fixtures/evaluations.json (engine outputs consumed by the Python AI evals)
//
// Usage (from backend/): go run ./cmd/evidence
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
	"c23desk/internal/scenario"
)

type check struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Pass     bool   `json:"pass"`
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

func main() {
	raw := env("RAW_DATA_DIR", filepath.Join("..", "decision-engine", "data", "raw"))
	out := env("REPORT_DIR", "reports")
	evalOut := env("EVAL_FIXTURE_DIR", filepath.Join("..", "decision-engine", "evals", "fixtures"))
	off, err := fixture.LoadOfficial(raw)
	must(err)
	th := rules.Default()
	ps := off.Pilots
	by := map[string]domain.Pilot{}
	for _, p := range ps {
		by[p.ID] = p
	}
	evals := map[string]decision.Evaluation{}
	for _, p := range ps {
		evals[p.ID] = decision.Evaluate(p, th, decision.Context{GroupPeers: peers(ps, p)})
	}
	sc := func(id, ov string) decision.Evaluation {
		var others []domain.Pilot
		for _, q := range ps {
			if q.ID != id {
				others = append(others, q)
			}
		}
		r, errs := scenario.Run(by[id], scenario.Request{Overrides: json.RawMessage(ov)}, th, func(p domain.Pilot) []string { return peers(append(others, p), p) })
		if errs != nil {
			panic(fmt.Sprint(errs))
		}
		return r.Scenario
	}
	m := func(e decision.Evaluation) finance3 {
		s := e.Metrics.Stated
		return finance3{js(s.NetAnnual), js(s.MonthlyNet), js(s.Months)}
	}
	var checks []check
	add := func(id, title, input, expected, actual string, pass bool) {
		checks = append(checks, check{id, title, input, expected, actual, pass})
	}

	r := m(evals["AI1"])
	add("DEMO1", "Official reference (20-month payback)", "AI1 setup 200000, running 60000, benefit 180000",
		"net 120000, monthly 10000, payback 20", r.String(), r == finance3{"120000", "10000", "20"})
	a := sc("AI1", `{"annual_running_cost_inr":120000}`)
	add("DEMO2/CASE_A", "Changed running cost", "running 60000 -> 120000", "net 60000, monthly 5000, payback 40", m(a).String(), m(a) == finance3{"60000", "5000", "40"})
	b := sc("AI1", `{"annual_running_cost_inr":null}`)
	add("DEMO6/CASE_B", "Unknown running cost", "running = null", "payback null (NOT_CALCULABLE), REVISE",
		fmt.Sprintf("payback %s (%s), %s %s", js(b.Metrics.Stated.Months), b.Metrics.Stated.Status, b.Decision, b.PrimaryReason),
		!b.Metrics.Stated.Months.Known() && b.Decision == rules.Revise && b.PrimaryReason == rules.ReasonMissingRunning)
	ca := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{by["AI1"], by["AI2"], by["AI3"], by["AI4"]}, Evaluations: evals})
	add("CASE_C", "Budget exceeded", "budget 600000, selected setup 800000", "REJECTED with BUDGET_EXCEEDED",
		ca.Status+" "+codes(ca.Blockers), ca.Status == portfolio.StatusRejected && strings.Contains(codes(ca.Blockers), "BUDGET_EXCEEDED"))
	d := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{by["AI4"], by["AI5"]}, Evaluations: evals})
	add("DEMO3/CASE_D", "AI4 + AI5 overlap", "select AI4 and AI5 (both overlap_group=support)", "DOUBLE_COUNT_RISK, recognised 180000 not 360000, NEEDS_REVIEW",
		fmt.Sprintf("%s, recognised %s (claimed %s), %s", d.Overlaps[0].Code, js(d.TotalRecognized), js(d.Overlaps[0].ClaimedTotal), d.Status),
		d.Overlaps[0].Code == "DOUBLE_COUNT_RISK" && js(d.TotalRecognized) == "180000" && d.Status == portfolio.StatusNeedsReview)
	res := portfolio.Resolution{Group: "support", Type: portfolio.CustomAllocation, Allocations: map[string]money.Amount{"AI4": money.Rupees(120000), "AI5": money.Rupees(60000)}}
	d2 := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{by["AI4"], by["AI5"]}, Evaluations: evals, Resolutions: []portfolio.Resolution{res}})
	add("OVERLAP_RESOLVED", "Custom allocation AI4 120000 + AI5 60000", "CUSTOM_ALLOCATION",
		"recognised 180000; still NEEDS_REVIEW because AI5 is REVISE (low readiness)",
		fmt.Sprintf("recognised %s, %s %s", js(d2.TotalRecognized), d2.Status, codes(d2.Blockers)),
		js(d2.TotalRecognized) == "180000" && d2.Status == portfolio.StatusNeedsReview && codes(d2.Blockers) == portfolio.CodeRevisePilot)
	ai5 := by["AI5"].Clone()
	ai5.ReadinessLabel = domain.Str("medium")
	revEv := map[string]decision.Evaluation{"AI4": evals["AI4"], "AI5": decision.Evaluate(ai5, th, decision.Context{GroupPeers: []string{"AI4"}})}
	d3 := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: []domain.Pilot{by["AI4"], ai5}, Evaluations: revEv, Resolutions: []portfolio.Resolution{res}})
	add("OVERLAP_RESOLVED_AND_REVISED", "Same, after AI5 readiness revised to medium", "CUSTOM_ALLOCATION + AI5 readiness medium",
		"recognised 180000, VALID (approvable)", fmt.Sprintf("recognised %s, %s", js(d3.TotalRecognized), d3.Status),
		js(d3.TotalRecognized) == "180000" && d3.Status == portfolio.StatusValid)
	e := sc("AI1", `{"actual_adoption_pct":20,"actual_quality_pct":90}`)
	add("DEMO4/CASE_E", "Adoption 70 -> 20 (break-even 33.33%)", "actual adoption 20, quality 90", "STOP (ADOPTION_BELOW_BREAKEVEN)",
		e.Decision+" "+strings.Join(e.ReasonCodes, ","), e.Decision == rules.Stop && e.PrimaryReason == rules.ReasonAdoptionBelowBreak)
	e2 := sc("AI1", `{"actual_adoption_pct":45,"actual_quality_pct":90}`)
	add("ADOPTION_REVISE", "Adoption above break-even, below minimum 60", "actual adoption 45", "REVISE (LOW_ADOPTION)",
		e2.Decision+" "+e2.PrimaryReason, e2.Decision == rules.Revise && e2.PrimaryReason == rules.ReasonLowAdoption)
	f := sc("AI1", `{"actual_adoption_pct":70,"actual_quality_pct":60}`)
	add("DEMO5/CASE_F", "Quality 90 -> 60 (minimum 80)", "actual quality 60", "STOP (QUALITY_THRESHOLD_FAILED) despite 20-month payback",
		fmt.Sprintf("%s %s, payback still %s", f.Decision, f.PrimaryReason, js(f.Metrics.Stated.Months)), f.Decision == rules.Stop && f.PrimaryReason == rules.ReasonQualityFailed)
	g := sc("AI1", `{"annual_running_cost_inr":200000}`)
	add("CASE_G", "Running 200000 > benefit 180000", "running 200000", "net -20000, NEVER_PAYS_BACK, STOP",
		fmt.Sprintf("net %s, %s, %s", js(g.Metrics.Stated.NetAnnual), g.Metrics.Stated.Status, g.Decision),
		js(g.Metrics.Stated.NetAnnual) == "-20000" && g.Decision == rules.Stop)
	tf := sc("AI1", `{"benefit_type":"time freed"}`)
	add("TIME_NOT_CASH", "Time freed is not cash", "benefit_type = time freed", "REVIEW (TIME_NOT_CASH), cash basis unknown",
		fmt.Sprintf("%s %s, cash net %s", tf.Decision, tf.PrimaryReason, js(tf.Metrics.Cash.NetAnnual)), tf.Decision == rules.Review && !tf.Metrics.Cash.NetAnnual.Known())
	ou := sc("AI5", `{"overlap_group":null}`)
	add("OVERLAP_UNKNOWN", "Unknown overlap group", "AI5 overlap_group = null", "OVERLAP_UNKNOWN (not 'no overlap')",
		strings.Join(ou.ReasonCodes, ","), strings.Contains(strings.Join(ou.ReasonCodes, ","), rules.ReasonOverlapUnknown))
	opt, _ := portfolio.Optimize(ps, evals, money.Rupees(600000), "", th)
	excl := map[string]string{}
	for _, x := range opt.Excluded {
		excl[x.PilotID] = x.Code
	}
	add("OPTIMIZER", "Optimal portfolio, budget 600000", "all 5 official pilots",
		"AI1, AI2, AI4; AI5 excluded (REVISE: low readiness), AI3 not funded",
		fmt.Sprintf("%s; AI5 %s, AI3 %s; status %s", strings.Join(opt.Selected, ","), excl["AI5"], excl["AI3"], opt.Assessment.Status),
		strings.Join(opt.Selected, ",") == "AI1,AI2,AI4" && excl["AI5"] == portfolio.CodeRevisePilot && opt.Assessment.Status == portfolio.StatusValid)
	low, _ := portfolio.Optimize(ps, evals, money.Rupees(100000), "", th)
	add("LOW_BUDGET", "Budget 600000 -> 100000", "budget 100000", "empty portfolio", fmt.Sprintf("%v %s", low.Selected, low.Message), len(low.Selected) == 0)

	pass := true
	for _, c := range checks {
		pass = pass && c.Pass
	}
	doc := map[string]any{"rules_version": rules.Version, "data": "official C23 synthetic fixture (SHA-256 verified) + team extension",
		"disclaimer": "Synthetic teaching data. Calculations on stated assumptions, not proven ROI.", "all_pass": pass, "checks": checks,
		"official_pilot_decisions": decisionsSummary(evals)}
	must(os.MkdirAll(out, 0o755))
	writeJSON(filepath.Join(out, "judge_evidence.json"), doc)
	must(os.WriteFile(filepath.Join(out, "judge_evidence.md"), []byte(markdown(checks, evals, pass)), 0o644))

	fixtures := map[string]decision.Evaluation{}
	for id, ev := range evals {
		fixtures["official_"+id] = ev
	}
	for k, v := range map[string]decision.Evaluation{"case_a_running_120000": a, "case_b_running_null": b, "case_e_adoption_20": e,
		"adoption_45": e2, "case_f_quality_60": f, "case_g_running_200000": g, "time_freed": tf, "overlap_unknown": ou} {
		fixtures[k] = v
	}
	must(os.MkdirAll(evalOut, 0o755))
	writeJSON(filepath.Join(evalOut, "evaluations.json"), map[string]any{"generated_by": "backend/cmd/evidence", "rules_version": rules.Version, "evaluations": fixtures})
	fmt.Printf("judge evidence: %d checks, all_pass=%t -> %s\n", len(checks), pass, out)
	if !pass {
		os.Exit(1)
	}
}

type finance3 struct{ net, monthly, payback string }

func (f finance3) String() string {
	return fmt.Sprintf("net %s, monthly %s, payback %s", f.net, f.monthly, f.payback)
}

func decisionsSummary(ev map[string]decision.Evaluation) map[string]any {
	out := map[string]any{}
	for id, e := range ev {
		out[id] = map[string]any{"decision": e.Decision, "primary_reason": e.PrimaryReason, "reason_codes": e.ReasonCodes, "warnings": e.Warnings}
	}
	return out
}

func markdown(cs []check, ev map[string]decision.Evaluation, pass bool) string {
	var b strings.Builder
	b.WriteString("# C23 Judge Challenge Evidence\n\n> Generated by `go run ./cmd/evidence` from the real deterministic engine (" + rules.Version +
		"). Synthetic teaching fixture + team assumptions; not proven ROI.\n\n")
	fmt.Fprintf(&b, "**All checks pass: %t**\n\n| ID | Check | Input | Expected | Actual | Result |\n|---|---|---|---|---|---|\n", pass)
	for _, c := range cs {
		r := "PASS"
		if !c.Pass {
			r = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n", c.ID, c.Title, c.Input, c.Expected, c.Actual, r)
	}
	b.WriteString("\n## Official pilots (pre-pilot: nothing measured yet)\n\n| Pilot | Decision | Primary reason | Reason codes | Warnings |\n|---|---|---|---|---|\n")
	for _, id := range []string{"AI1", "AI2", "AI3", "AI4", "AI5"} {
		e := ev[id]
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", id, e.Decision, e.PrimaryReason, strings.Join(e.ReasonCodes, ", "), strings.Join(e.Warnings, "; "))
	}
	return b.String()
}

func codes(is []portfolio.Issue) string {
	var s []string
	for _, i := range is {
		s = append(s, i.Code)
	}
	return strings.Join(s, ",")
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

func writeJSON(path string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	must(os.WriteFile(path, append(b, '\n'), 0o644))
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
