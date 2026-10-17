package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/store"
	"c23desk/internal/store/pgtest"
	"c23desk/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Stop()
	os.Exit(code)
}

var actor = store.Actor{Name: "tester", RequestID: "req-1"}

func seed(t *testing.T) (*store.Store, []domain.Pilot) {
	s := pgtest.Fresh(t)
	ps := testutil.Official(t).Pilots
	if err := s.CreatePilots(context.Background(), ps, actor, "import"); err != nil {
		t.Fatal(err)
	}
	return s, ps
}

func js(v any) string { b, _ := json.Marshal(v); return string(b) }

// field extracts a top-level JSON field as compact JSON text (JSONB re-formats whitespace).
func field(raw json.RawMessage, key string) string {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	return string(m[key])
}

func TestMigrateIdempotent(t *testing.T) {
	s := pgtest.Fresh(t)
	applied, err := s.Migrate(context.Background())
	if err != nil || len(applied) != 0 {
		t.Fatal(applied, err)
	}
}

func TestPilotRoundTripPreservesNullsAndProvenance(t *testing.T) {
	s, ps := seed(t)
	ctx := context.Background()
	got, err := s.ListPilots(ctx)
	if err != nil || len(got) != 5 {
		t.Fatal(err, len(got))
	}
	a := got[0]
	if a.ID != "AI1" || a.SetupCost.String() != "200000" || a.ActualAdoption.Known || a.Sources["setup_cost_inr"] != domain.SourceOfficial ||
		*a.ReadinessLabel != "high" || a.Version != 1 {
		t.Fatalf("%+v", a)
	}
	// unknown stays unknown through the database (never stored as 0)
	p := ps[0].Clone()
	p.ID, p.RunningCost, p.OverlapGroup = "X1", money.Unknown(), nil
	if err := s.CreatePilots(ctx, []domain.Pilot{p}, actor, "create"); err != nil {
		t.Fatal(err)
	}
	x, _ := s.GetPilot(ctx, "X1")
	if x.RunningCost.Known || x.OverlapGroup != nil {
		t.Fatal("NULL must round-trip as unknown")
	}
	if _, err := s.GetPilot(ctx, "NOPE"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDuplicateRejectedAndImportAtomic(t *testing.T) {
	s, ps := seed(t)
	ctx := context.Background()
	fresh := ps[0].Clone()
	fresh.ID = "NEW1"
	dup := ps[1].Clone()
	dup.ID = "ai2" // case-insensitive duplicate of AI2
	err := s.CreatePilots(ctx, []domain.Pilot{fresh, dup}, actor, "import")
	if !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.GetPilot(ctx, "NEW1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("transaction must roll back the whole import")
	}
	if a, _ := s.ListAudit(ctx, "pilot", "NEW1"); len(a) != 0 {
		t.Fatal("no audit row for a rolled-back change")
	}
}

func TestCheckConstraintsBackstopValidation(t *testing.T) {
	s, ps := seed(t)
	p := ps[0].Clone()
	p.ID = "NEG"
	p.SetupCost = money.Amount{Paise: -1, Known: true} // bypasses API validation on purpose
	if err := s.CreatePilots(context.Background(), []domain.Pilot{p}, actor, "create"); err == nil {
		t.Fatal("DB CHECK must reject negative money")
	}
	p.ID, p.SetupCost = "BAD/ID", money.Rupees(1)
	if err := s.CreatePilots(context.Background(), []domain.Pilot{p}, actor, "create"); err == nil {
		t.Fatal("DB CHECK must reject unsafe ids")
	}
}

func TestUpdateVersioningAndAudit(t *testing.T) {
	s, _ := seed(t)
	ctx := context.Background()
	p, _ := s.GetPilot(ctx, "AI1")
	p.RunningCost = money.Rupees(120000)
	p.Sources["annual_running_cost_inr"] = domain.SourceTeam
	up, err := s.UpdatePilot(ctx, p, 1, actor)
	if err != nil || up.Version != 2 {
		t.Fatal(err, up.Version)
	}
	if _, err := s.UpdatePilot(ctx, p, 1, actor); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatal("stale version must conflict", err)
	}
	log, _ := s.ListAudit(ctx, "pilot", "AI1")
	if len(log) != 2 || log[0].Action != "import" || log[1].Action != "update" || field(log[1].Before, "annual_running_cost_inr") != "60000" ||
		field(log[1].After, "annual_running_cost_inr") != "120000" || log[1].Actor != "tester" || log[1].RequestID != "req-1" {
		t.Fatalf("%+v", log)
	}
}

func TestConcurrentUpdatesOnlyOneWins(t *testing.T) {
	s, _ := seed(t)
	ctx := context.Background()
	p, _ := s.GetPilot(ctx, "AI2")
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, conflict := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			q := p.Clone()
			q.Task = "task " + string(rune('a'+i))
			_, err := s.UpdatePilot(ctx, q, 1, actor)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				ok++
			} else if errors.Is(err, store.ErrVersionConflict) {
				conflict++
			}
		}(i)
	}
	wg.Wait()
	if ok != 1 || conflict != 7 {
		t.Fatalf("ok=%d conflict=%d", ok, conflict)
	}
}

func TestPortfolioAtomicSaveReloadAndLedger(t *testing.T) {
	s, ps := seed(t)
	ctx := context.Background()
	ev := testutil.EvaluateAll(ps)
	res := []portfolio.Resolution{{Group: "support", Type: portfolio.CustomAllocation,
		Allocations: map[string]money.Amount{"AI4": money.Rupees(120000), "AI5": money.Rupees(60000)}, Note: "split agreed"}}
	ai5 := ps[4].Clone()
	ai5.ReadinessLabel = domain.Str("medium") // test assumption: isolates persistence from AI5's revise rule
	sel := []domain.Pilot{ps[0], ps[3], ai5}
	ev = testutil.EvaluateAll([]domain.Pilot{ps[0], ps[1], ps[2], ps[3], ai5})
	a := portfolio.Assess(portfolio.Input{Budget: money.Rupees(600000), Pilots: sel, Evaluations: ev, Resolutions: res})
	p := store.Portfolio{ID: "11111111-1111-4111-8111-111111111111", Name: "Q3 plan", Budget: money.Rupees(600000), Basis: "cash",
		Status: a.Status, RulesVersion: a.RulesVersion, PilotIDs: a.PilotIDs, Resolutions: res, Assessment: a}
	if _, err := s.CreatePortfolio(ctx, p, actor); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPortfolio(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.PilotIDs, ",") != "AI1,AI4,AI5" || got.Status != portfolio.StatusValid || len(got.Resolutions) != 1 ||
		js(got.Assessment.TotalRecognized) != "360000" || js(got.Assessment.NetAnnual) != "180000" || js(got.Assessment.Payback.Months) != "40" {
		t.Fatalf("%s", js(got))
	}
	// duplicate name -> conflict, and nothing partial left behind
	p2 := p
	p2.ID = "22222222-2222-4222-8222-222222222222"
	p2.Name = "q3 PLAN"
	if _, err := s.CreatePortfolio(ctx, p2, actor); !errors.Is(err, store.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.GetPortfolio(ctx, p2.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial portfolio saved")
	}
	ids, _ := s.PortfolioIDsUsingPilot(ctx, "AI4")
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	// ledger check constraint: recognised can never exceed the pilot's own basis benefit
	bad := p
	bad.ID, bad.Name = "33333333-3333-4333-8333-333333333333", "bad"
	bad.Assessment.Ledger = append([]portfolio.LedgerEntry(nil), a.Ledger...)
	bad.Assessment.Ledger[0].RecognizedBenefit = money.Rupees(999999)
	if _, err := s.CreatePortfolio(ctx, bad, actor); err == nil {
		t.Fatal("DB must reject over-recognised benefit")
	}
	if _, err := s.GetPortfolio(ctx, bad.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("rollback")
	}
	if _, err := s.UpdatePortfolio(ctx, got, 99, actor, "update"); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatal(err)
	}
	log, _ := s.ListAudit(ctx, "portfolio", p.ID)
	if len(log) != 1 || log[0].Action != "create" {
		t.Fatal(log)
	}
}

func TestDecisionsScenariosPredictions(t *testing.T) {
	s, ps := seed(t)
	ctx := context.Background()
	ev := testutil.EvaluateAll(ps)["AI1"]
	r, err := s.SaveDecision(ctx, ev)
	if err != nil || r.Decision != "REVIEW" || r.RulesVersion != "c23-rules-v1" {
		t.Fatal(err, r)
	}
	h, _ := s.ListDecisions(ctx, "AI1")
	var calc struct {
		M struct {
			S struct {
				Months json.RawMessage `json:"payback_months"`
			} `json:"stated_basis"`
		} `json:"calculated_metrics"`
	}
	_ = json.Unmarshal(h[0].Calculation, &calc)
	if len(h) != 1 || string(calc.M.S.Months) != "20" {
		t.Fatal(string(h[0].Calculation))
	}
	if _, err := s.SaveScenario(ctx, store.ScenarioRecord{PilotID: "AI1", Name: "x", Overrides: json.RawMessage(`{}`), Decision: "REVIEW",
		RulesVersion: "c23-rules-v1", Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListScenarios(ctx, "AI1"); len(l) != 1 {
		t.Fatal(l)
	}
	if err := s.SavePredictions(ctx, []store.Prediction{{PilotID: "AI4", RelatedPilotID: "AI5", ModelName: "lexical", ModelVersion: "1",
		Prediction: "possible_overlap", Probability: 0.5, Input: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePredictions(ctx, []store.Prediction{{PilotID: "AI4", ModelName: "x", ModelVersion: "1", Prediction: "p", Probability: 2,
		Input: json.RawMessage(`{}`)}}); err == nil {
		t.Fatal("probability must be within 0..1")
	}
	if n, _ := s.CountPredictions(ctx); n != 1 {
		t.Fatal(n)
	}
}

func TestConnectionFailure(t *testing.T) {
	s, err := store.Open(context.Background(), "postgres://x:y@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Ping(context.Background()); err == nil {
		t.Fatal("ping must fail")
	}
	if _, err := s.ListPilots(context.Background()); err == nil {
		t.Fatal("list must fail cleanly, not hang or panic")
	}
	if _, err := store.Open(context.Background(), "::not a url::"); err == nil {
		t.Fatal("bad url")
	}
}
