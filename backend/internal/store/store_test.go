package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
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

func TestPortfolioRoundTripUpsertAndDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := "store_test_run"
	_ = s.DeletePortfolio(ctx, name)

	in := model.PortfolioIn{
		Name: name, Mode: "independent", BudgetINR: f(600000),
		StartingState: json.RawMessage(`{"case_id":"C23"}`), NegotiatedScope: json.RawMessage(`{"mode":"independent"}`),
		FinalEvidence: json.RawMessage(`{"ok":true}`),
		Pilots: []model.PortfolioPilot{
			{PilotID: "AI1", Selected: true, BenefitGroup: str("finance"), SetupINR: f(200000), RunINR: f(60000), NetINR: f(120000), PaybackMonths: f(20)},
			{PilotID: "AI5", Selected: false, Reason: str("Overlap"), RunAssumed: true},
		},
	}
	saved, err := s.SavePortfolio(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Pilots) != 2 || saved.Pilots[0].PilotID != "AI1" || *saved.Pilots[0].PaybackMonths != 20 {
		t.Fatalf("unexpected pilots: %+v", saved.Pilots)
	}
	if saved.Pilots[1].PaybackMonths != nil || saved.Pilots[1].NetINR != nil {
		t.Fatal("unknown values must stay NULL, never zero")
	}

	in.BudgetINR = f(400000) // same name again = update, not duplicate
	in.Pilots = in.Pilots[:1]
	again, err := s.SavePortfolio(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != saved.ID || *again.BudgetINR != 400000 || len(again.Pilots) != 1 {
		t.Fatalf("upsert failed: %+v", again)
	}

	if err := s.DeletePortfolio(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPortfolio(ctx, name); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
	if err := s.DeletePortfolio(ctx, name); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("deleting twice must report not found, got %v", err)
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
