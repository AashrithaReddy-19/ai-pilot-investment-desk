package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"c23desk/internal/model"
)

// These tests need a REAL, EMPTY-OR-DISPOSABLE Postgres database. They are skipped unless
// TEST_DATABASE_URL is set, e.g. postgres://c23:PASSWORD@localhost:5432/c23_test?sslmode=disable
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := Migrate(ctx, dsn, "up", io.Discard); err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func f(v float64) *float64 { return &v }
func str(v string) *string { return &v }
func i(v int) *int         { return &v }

func TestPortfolioRoundTripUpsertAndDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := "store_test_run"
	_ = s.DeletePortfolio(ctx, name)

	in := model.PortfolioIn{
		Name: name, Mode: "independent", BudgetINR: f(600000), HorizonMonths: i(36), DiscountRatePct: f(10),
		StartingState: json.RawMessage(`{"synthetic":true}`), NegotiatedScope: json.RawMessage(`{"mode":"independent"}`),
		FinalEvidence: json.RawMessage(`{"portfolio":{}}`),
		Pilots: []model.PortfolioPilot{
			{PilotID: "AI1", Selected: true, SetupINR: f(200000), RunINR: f(60000), NetINR: f(120000), PaybackMonths: f(20), ProfitINR: f(160000), ROIPct: f(80)},
			{PilotID: "AI5", Selected: false, Reason: str("overlap"), SetupINR: f(200000), NetINR: f(-1), PaybackMonths: nil, ProfitINR: nil, ROIPct: nil},
		},
	}
	// first save creates, second save updates the same row
	for range 2 {
		saved, err := s.SavePortfolio(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Name != name || len(saved.Pilots) != 2 {
			t.Fatalf("unexpected save: %+v", saved)
		}
		if saved.HorizonMonths == nil || *saved.HorizonMonths != 36 || saved.DiscountRatePct == nil || *saved.DiscountRatePct != 10 {
			t.Fatalf("multi-period conventions lost: %+v", saved)
		}
		a1 := saved.Pilots[0]
		if a1.ProfitINR == nil || *a1.ProfitINR != 160000 || a1.ROIPct == nil || *a1.ROIPct != 80 {
			t.Fatalf("multi-period pilot columns lost: %+v", a1)
		}
		a5 := saved.Pilots[1]
		if a5.ProfitINR != nil || a5.ROIPct != nil || a5.PaybackMonths != nil {
			t.Fatalf("unknown must stay NULL: %+v", a5)
		}

		list, err := s.ListPortfolios(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, x := range list {
			if x.Name == name {
				found = true
				if x.HorizonMonths == nil || *x.HorizonMonths != 36 {
					t.Fatalf("summary horizon lost: %+v", x)
				}
				if x.SelectedCount != 1 {
					t.Fatalf("selected count wrong: %+v", x)
				}
			}
		}
		if !found {
			t.Fatal("saved portfolio missing from list")
		}
	}
	if err := s.DeletePortfolio(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPortfolio(ctx, name); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestObservationAndDecisionHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	obs, dec, err := s.AddObservation(ctx,
		model.ObservationIn{PilotID: "STORE_T1", AdoptionPct: f(20), Source: str("test")},
		model.DecisionIn{PilotID: "STORE_T1", Decision: "STOP", Metric: "adoption", Threshold: f(33.3), Actual: f(20), Reason: "below breakeven"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.QualityPct != nil || dec.ObservationID == nil || *dec.ObservationID != obs.ID {
		t.Fatalf("unexpected rows: %+v %+v", obs, dec)
	}
	h, err := s.History(ctx, "STORE_T1", 10)
	if err != nil || len(h.Observations) < 1 || len(h.Decisions) < 1 || h.Decisions[0].Decision != "STOP" {
		t.Fatalf("unexpected history: %+v, %v", h, err)
	}
	if _, _, err := s.AddObservation(ctx, model.ObservationIn{PilotID: "STORE_T1", AdoptionPct: f(150)},
		model.DecisionIn{PilotID: "STORE_T1", Decision: "STOP", Metric: "x", Reason: "x"}); err == nil {
		t.Fatal("database CHECK must reject adoption above 100")
	}
}

func TestInterviews(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	iv, err := s.AddInterview(ctx, model.InterviewIn{IntervieweeRole: "finance clerk", Task: "invoice entry", MinutesPerTask: f(12.5), InterviewedOn: str("2026-10-17")})
	if err != nil {
		t.Fatal(err)
	}
	if *iv.InterviewedOn != "2026-10-17" || iv.Quote != nil {
		t.Fatalf("unexpected interview: %+v", iv)
	}
}

func TestEvaluationsLog(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, err := s.SaveEvaluation(ctx, model.EvaluationIn{
		Request: json.RawMessage(`{"budget":600000}`), Independent: json.RawMessage(`{"selected":["AI1"]}`), Conservative: json.RawMessage(`{"selected":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id < 1 {
		t.Fatalf("expected a row id, got %d", id)
	}
	latest, err := s.ListEvaluations(ctx, 10)
	if err != nil || len(latest) == 0 || latest[0].ID != id {
		t.Fatalf("wanted the saved row first: %+v %v", latest, err)
	}
	full, err := s.GetEvaluation(ctx, id)
	if err != nil || full.PortfolioID != nil {
		t.Fatalf("unexpected evaluation row: %+v %v", full, err)
	}
	var req map[string]any
	if json.Unmarshal(full.Request, &req) != nil || req["budget"] != 600000.0 {
		t.Fatalf("request body did not survive the round trip: %s", full.Request)
	}
	if _, err := s.GetEvaluation(ctx, id+1000000); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound for a missing row, got %v", err)
	}
}

func TestHistoryTimeline(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// one evaluation pair: the funded set changes and the budget drops
	id1, err := s.SaveEvaluation(ctx, model.EvaluationIn{
		Request: json.RawMessage(`{"budget":600000}`), Independent: json.RawMessage(`{"selected":["AI1","AI2"]}`), Conservative: json.RawMessage(`{"selected":[]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.SaveEvaluation(ctx, model.EvaluationIn{
		Request: json.RawMessage(`{"budget":400000}`), Independent: json.RawMessage(`{"selected":["AI1","AI4","AI2"]}`), Conservative: json.RawMessage(`{"selected":["AI1"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	// an observation with a decision: REVISE now, GO earlier for the same pilot
	obs, dec, err := s.AddObservation(ctx, model.ObservationIn{
		PilotID: "HIST-A1", AdoptionPct: f(65), QualityPct: f(90), Source: str("test"),
	}, model.DecisionIn{PilotID: "HIST-A1", Decision: "GO", Metric: "adoption", Threshold: f(60), Actual: f(65),
		Reason: "Adoption and quality meet thresholds."})
	if err != nil {
		t.Fatal(err)
	}
	if obs == nil || dec == nil {
		t.Fatal("expected observation + decision rows")
	}

	events, err := s.HistoryTimeline(ctx, 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("want at least 3 timeline events (2 evaluations + 1 decision), got %d: %+v", len(events), events)
	}
	if !sort.SliceIsSorted(events, func(a, b int) bool { return events[a].At.After(events[b].At) }) {
		t.Fatal("timeline must be newest first")
	}
	// newest evaluation: funded AI4, budget 600000→400000
	var newest *model.HistoryEvent
	for k := range events {
		if events[k].Kind == "evaluation" && events[k].ID == id2 && newest == nil {
			newest = &events[k]
		}
	}
	if newest == nil || newest.Budget == nil || *newest.Budget != 400000 {
		t.Fatalf("newest evaluation event missing or budget lost: %+v", events)
	}
	if !contains("funded AI4", deref(newest.ChangeSummary)) || !contains("budget INR 600000", deref(newest.ChangeSummary)) {
		t.Fatalf("change summary must say what changed: %q", deref(newest.ChangeSummary))
	}
	// my own two evaluations are the newest two rows; only position and selection are asserted
	// (cross-test rows may precede them, so the "first recorded evaluation" wording is claimed
	// by whichever row really is first)
	var mine1 *model.HistoryEvent
	for k := range events {
		if events[k].Kind == "evaluation" && events[k].ID == id1 {
			mine1 = &events[k]
		}
	}
	if mine1 == nil || len(mine1.Selected) != 2 || mine1.Selected[0] != "AI1" {
		t.Fatalf("evaluation event lost its selected set: %+v", mine1)
	}
	// the decision event carries verdict + observed values; previous_verdict is null here
	var decEv *model.HistoryEvent
	for k := range events {
		if events[k].Kind == "decision" && events[k].PilotID != nil && *events[k].PilotID == "HIST-A1" {
			decEv = &events[k]
		}
	}
	if decEv == nil || decEv.Verdict == nil || *decEv.Verdict != "GO" {
		t.Fatalf("decision event missing: %+v", decEv)
	}
	if decEv.ObservedAdoption == nil || *decEv.ObservedAdoption != 65 || decEv.ObservedQuality == nil || *decEv.ObservedQuality != 90 {
		t.Fatalf("observed values lost: %+v", decEv)
	}
	if decEv.PrevVerdict != nil {
		t.Fatalf("no earlier decision exists for this pilot in this schema; previous_verdict must be NULL: %+v", decEv)
	}
	if _, err := s.HistoryTimeline(ctx, 100000); err != nil {
		t.Fatalf("oversized limit must clamp, not fail: %v", err)
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func contains(sub, s string) bool { return strings.Contains(s, sub) }

func TestProposalRoundTripArchiveAndUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProposal(ctx, model.ProposalIn{
		Name: "store_test_prop", Inputs: json.RawMessage(`{"task":"Invoice field extraction"}`), Notes: str("team draft"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "active" || string(p.Inputs) == "" {
		t.Fatalf("unexpected proposal: %+v", p)
	}

	// Duplicate names are rejected by the unique index.
	if _, err := s.CreateProposal(ctx, model.ProposalIn{Name: "store_test_prop", Inputs: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("duplicate name must fail")
	}

	up, err := s.UpdateProposal(ctx, p.ID, model.ProposalIn{Name: "store_test_prop_v2", Inputs: json.RawMessage(`{"task":"Renamed"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if up.Name != "store_test_prop_v2" {
		t.Fatalf("update failed: %+v", up)
	}
	// Empty name in update keeps the stored one.
	same, err := s.UpdateProposal(ctx, p.ID, model.ProposalIn{Inputs: json.RawMessage(`{"task":"Renamed again"}`)})
	if err != nil || same.Name != "store_test_prop_v2" {
		t.Fatalf("partial update failed: %+v %v", same, err)
	}

	arch, err := s.ArchiveProposal(ctx, p.ID, false)
	if err != nil || arch.Status != "archived" {
		t.Fatalf("archive failed: %+v %v", arch, err)
	}
	active, err := s.ListProposals(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range active {
		if x.ID == p.ID {
			t.Fatal("archived proposal must not be listed by default")
		}
	}
	all, err := s.ListProposals(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range all {
		if x.ID == p.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("archived proposal must be listed when requested")
	}
	back, err := s.ArchiveProposal(ctx, p.ID, true)
	if err != nil || back.Status != "active" {
		t.Fatalf("restore failed: %+v %v", back, err)
	}

	if _, err := s.UpdateProposal(ctx, "00000000-0000-0000-0000-000000000000", model.ProposalIn{}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("updating a missing proposal must be ErrNotFound, got %v", err)
	}
	if err := s.DeleteProposal(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetProposal(ctx, p.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestEvidenceAndObservationLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	p, err := s.CreateProposal(ctx, model.ProposalIn{Name: "store_test_ev", Inputs: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.AddEvidence(ctx, model.EvidenceIn{ProposalID: p.ID, Kind: "interview", Source: "finance clerk", Quality: str("observed"), Note: str("said invoices take 12 min")})
	if err != nil {
		t.Fatal(err)
	}
	if e.ProposalID != p.ID || *e.Quality != "observed" {
		t.Fatalf("unexpected evidence: %+v", e)
	}
	list, err := s.ListEvidence(ctx, p.ID)
	if err != nil || len(list) != 1 || list[0].ID != e.ID {
		t.Fatalf("unexpected evidence list: %+v %v", list, err)
	}
	if _, err := s.AddEvidence(ctx, model.EvidenceIn{ProposalID: p.ID, Kind: "rumour", Source: "x"}); err == nil {
		t.Fatal("database CHECK must reject an unknown evidence kind")
	}

	// Observations linked to a proposal keep their own pilot IDs from colliding.
	obs, dec, err := s.AddObservation(ctx,
		model.ObservationIn{PilotID: "AI1", ProposalID: str(p.ID), AdoptionPct: f(40), Source: str("test")},
		model.DecisionIn{PilotID: "AI1", ProposalID: str(p.ID), Decision: "REVISE", Metric: "adoption", Reason: "below target"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.ProposalID == nil || *obs.ProposalID != p.ID || dec.ProposalID == nil {
		t.Fatalf("proposal link lost: %+v %+v", obs, dec)
	}
	h, err := s.History(ctx, "AI1", 10)
	if err != nil || len(h.Observations) == 0 {
		t.Fatalf("unexpected history: %+v %v", h, err)
	}
	// A linked observation of a deleted proposal keeps its row (ON DELETE SET NULL).
	if err := s.DeleteProposal(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	h2, err := s.History(ctx, "AI1", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range h2.Observations {
		if o.ID == obs.ID && o.ProposalID != nil {
			t.Fatal("deleting the proposal must clear the observation link, not delete the observation")
		}
	}
}

func TestPilotPlanRoundTripAndStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pid := "plan_store_test"
	adm, ql, cost := 60.0, 80.0, 204932.0
	owner := "Ops"
	plan := model.PilotPlan{PilotID: pid, Objective: "Invoice field extraction", Owner: &owner,
		AdoptionTarget: &adm, QualityTarget: &ql, CostLimitINR: &cost, DurationDays: 30,
		StopRule: "Stop when observed adoption falls below break-even 33.3%", Status: "planned"}

	// upsert twice: replace, never duplicate (one current plan per pilot)
	for range 2 {
		saved, err := s.UpsertPlan(ctx, plan)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Status != "planned" || saved.DurationDays != 30 || saved.AdoptionTarget == nil || *saved.AdoptionTarget != 60 {
			t.Fatalf("saved plan wrong: %+v", saved)
		}
	}
	got, err := s.PilotPlan(ctx, pid)
	if err != nil || got.PilotID != pid || got.Owner == nil || *got.Owner != "Ops" {
		t.Fatalf("get plan failed: %+v err=%v", got, err)
	}
	// owner NULL survives a round trip (missing is never zero)
	noOwner := plan
	noOwner.Owner = nil
	saved, err := s.UpsertPlan(ctx, noOwner)
	if err != nil || saved.Owner != nil {
		t.Fatalf("NULL owner must survive: %+v err=%v", saved, err)
	}

	moved, err := s.SetPlanStatus(ctx, pid, "revise")
	if err != nil || moved.Status != "revise" {
		t.Fatalf("status change failed: %+v err=%v", moved, err)
	}
	if _, err := s.SetPlanStatus(ctx, pid, "escalate"); err == nil {
		t.Fatal("invalid status must violate the CHECK constraint")
	}
	if err := s.DeletePlan(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PilotPlan(ctx, pid); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleted plan must be ErrNotFound, got %v", err)
	}
}

func TestLatestEvaluationWithPilotSelectsTheNewestFundingEvaluation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pid := "plan_test_eval"
	req := json.RawMessage(`{"budget": 424242, "fixture": {"records": {"pilots": []}}}`)
	ind := json.RawMessage(`{"selected": ["plan_test_eval"], "budget": 424242}`)
	id, err := s.SaveEvaluation(ctx, model.EvaluationIn{Request: req, Independent: ind, Conservative: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // keep the shared schema clean for the other tests
		_, _ = s.pool.Exec(ctx, `DELETE FROM evaluations WHERE id = $1`, id)
	})
	ev, err := s.LatestEvaluationWithPilot(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var funded struct {
		Selected []string  `json:"selected"`
		Budget   float64   `json:"budget"`
		Fixture  struct{}  `json:"fixture"`
	}
	if err := json.Unmarshal(ev.Independent, &funded); err != nil || funded.Budget != 424242 || len(funded.Selected) != 1 {
		t.Fatalf("independent decode wrong: %v err=%v", funded, err)
	}
	var got map[string]any
	if err := json.Unmarshal(ev.Request, &got); err != nil || got["budget"].(float64) != 424242 {
		t.Fatalf("request replay wrong: %v err=%v", got, err)
	}
	if _, err := s.LatestEvaluationWithPilot(ctx, "never_funded_pilot_x"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("pilot never funded must be ErrNotFound, got %v", err)
	}
}

// uniq is a name suffix that satisfies the proposals name check (letters, digits, space, _ and -).
func uniq() string { return strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "_") }

func TestProposalVersionBumpsOnlyWhenInputsChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, err := s.CreateProposal(ctx, model.ProposalIn{
		Name: "store_test_version " + uniq(), Inputs: json.RawMessage(`{"task":"a","volume":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteProposal(context.Background(), p.ID) })
	if p.Version != 1 {
		t.Fatalf("a new proposal starts at version 1, got %d", p.Version)
	}
	for _, step := range []struct {
		name string
		in   model.ProposalIn
		want int
	}{
		{"rename keeps the version", model.ProposalIn{Name: "store_test_version renamed " + uniq()}, 1},
		{"notes edit keeps the version", model.ProposalIn{Notes: str("edited")}, 1},
		{"same inputs, other key order", model.ProposalIn{Inputs: json.RawMessage(`{ "volume": 1, "task": "a" }`)}, 1},
		{"changed inputs bump it", model.ProposalIn{Inputs: json.RawMessage(`{"task":"a","volume":2}`)}, 2},
		{"changed again", model.ProposalIn{Inputs: json.RawMessage(`{"task":"b"}`)}, 3},
	} {
		up, err := s.UpdateProposal(ctx, p.ID, step.in)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if up.Version != step.want {
			t.Fatalf("%s: version %d, want %d", step.name, up.Version, step.want)
		}
	}
	got, err := s.GetProposal(ctx, p.ID)
	if err != nil || got.Version != 3 {
		t.Fatalf("stored version: %+v %v", got, err)
	}
	list, err := s.ListProposals(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.ID == p.ID && r.Version != 3 {
			t.Fatalf("summary version %d, want 3", r.Version)
		}
	}
}

func TestEvaluationLinkedToProposalVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	p, err := s.CreateProposal(ctx, model.ProposalIn{
		Name: "store_test_eval_link " + uniq(), Inputs: json.RawMessage(`{"task":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	ver := p.Version
	linked, err := s.SaveEvaluation(ctx, model.EvaluationIn{
		Request: json.RawMessage(`{"budget":1}`), Independent: json.RawMessage(`{"selected":[]}`), Conservative: json.RawMessage(`{"selected":[]}`),
		ProposalID: &p.ID, ProposalVersion: &ver,
	})
	if err != nil {
		t.Fatal(err)
	}
	unlinked, err := s.SaveEvaluation(ctx, model.EvaluationIn{
		Request: json.RawMessage(`{}`), Independent: json.RawMessage(`{}`), Conservative: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM evaluations WHERE id IN ($1, $2)`, linked, unlinked)
	})

	full, err := s.GetEvaluation(ctx, linked)
	if err != nil || full.ProposalID == nil || *full.ProposalID != p.ID || full.ProposalVersion == nil || *full.ProposalVersion != ver {
		t.Fatalf("link not stored: %+v %v", full, err)
	}
	mine, err := s.ListProposalEvaluations(ctx, p.ID, 10)
	if err != nil || len(mine) != 1 || mine[0].ID != linked {
		t.Fatalf("proposal evaluations: %+v %v", mine, err)
	}
	all, err := s.ListEvaluations(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for _, e := range all {
		seen[e.ID] = true
	}
	if !seen[linked] || !seen[unlinked] {
		t.Fatalf("the unfiltered list must still return both rows: %+v", all)
	}

	// deleting the proposal keeps the evaluation and clears the link
	if err := s.DeleteProposal(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetEvaluation(ctx, linked)
	if err != nil || after.ProposalID != nil {
		t.Fatalf("evaluation must survive with a NULL link: %+v %v", after, err)
	}
	if after.ProposalVersion == nil || *after.ProposalVersion != ver {
		t.Fatalf("the recorded version is history and stays: %+v", after.ProposalVersion)
	}
}

func TestAuditEventsAppendAndFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tag := "store_test_audit_" + uniq()
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM audit_events WHERE entity_id LIKE $1`, tag+"%")
	})
	for n, a := range []model.AuditEventIn{
		{Action: "proposal.create", EntityType: "proposal", EntityID: str(tag + "_p"), RequestID: "req-1"},
		{Action: "proposal.update", EntityType: "proposal", EntityID: str(tag + "_p"), Details: json.RawMessage(`{"version":2}`)},
		{Action: "plan.status", EntityType: "plan", EntityID: str(tag + "_plan"), Details: json.RawMessage(`{"status":"stopped"}`)},
	} {
		if err := s.AddAuditEvent(ctx, a); err != nil {
			t.Fatalf("event %d: %v", n, err)
		}
	}
	got, err := s.ListAuditEvents(ctx, model.AuditFilter{EntityType: "proposal", EntityID: tag + "_p"})
	if err != nil || len(got) != 2 {
		t.Fatalf("filter by entity: %+v %v", got, err)
	}
	if got[0].Action != "proposal.update" || got[1].Action != "proposal.create" {
		t.Fatalf("newest first expected: %s, %s", got[0].Action, got[1].Action)
	}
	if got[1].RequestID == nil || *got[1].RequestID != "req-1" || got[0].RequestID != nil {
		t.Fatalf("request_id must be kept, and NULL when absent: %+v %+v", got[1].RequestID, got[0].RequestID)
	}
	if string(got[1].Details) != "{}" || got[0].Actor != nil {
		t.Fatalf("empty details default to {} and actor stays NULL: %s %v", got[1].Details, got[0].Actor)
	}
	plans, err := s.ListAuditEvents(ctx, model.AuditFilter{EntityType: "plan", EntityID: tag + "_plan"})
	if err != nil || len(plans) != 1 || string(plans[0].Details) != `{"status": "stopped"}` {
		t.Fatalf("plan event: %+v %v", plans, err)
	}
	one, err := s.ListAuditEvents(ctx, model.AuditFilter{Limit: 1})
	if err != nil || len(one) != 1 {
		t.Fatalf("limit 1: %d %v", len(one), err)
	}
	if capped, err := s.ListAuditEvents(ctx, model.AuditFilter{Limit: 999}); err != nil || len(capped) > 50 {
		t.Fatalf("limit must be capped at 50: %d %v", len(capped), err)
	}
}
