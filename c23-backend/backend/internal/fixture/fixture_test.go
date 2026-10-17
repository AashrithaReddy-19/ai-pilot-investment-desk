package fixture_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/testutil"
)

func TestOfficialFixtureLoadsAndIsVerified(t *testing.T) {
	r := testutil.Official(t)
	if len(r.Pilots) != 5 || len(r.Rejected) != 0 || len(r.Warnings) != 0 {
		t.Fatalf("%+v", r)
	}
	if !r.BudgetOK || r.Budget.String() != "600000" || r.Meta["synthetic"] != true {
		t.Fatal("budget/meta")
	}
	ai1 := testutil.ByID(r.Pilots)["AI1"]
	if ai1.Sources["setup_cost_inr"] != domain.SourceOfficial || ai1.Sources["expected_adoption_pct"] != domain.SourceTeam ||
		ai1.Sources["readiness_label"] != domain.SourceTeam || ai1.Sources["actual_adoption_pct"] != domain.SourceUnknown {
		t.Fatalf("provenance %v", ai1.Sources)
	}
	if ai1.ExpectedAdoption.String() != "70" || *ai1.ReadinessLabel != "high" || ai1.ActualAdoption.Known {
		t.Fatal("extension merge")
	}
}

func TestTamperedRawFileRejected(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"MANIFEST.json", "official_fixture.json", "starting_state.json", "extension.json"} {
		b, _ := os.ReadFile(filepath.Join(testutil.RawDir(), f))
		if f == "official_fixture.json" {
			b = []byte(strings.Replace(string(b), "200000", "100000", 1))
		}
		_ = os.WriteFile(filepath.Join(dir, f), b, 0o600)
	}
	if _, err := fixture.LoadOfficial(dir); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("tampered fixture must fail integrity check, got %v", err)
	}
}

func row(id, extra string) string {
	return `{"id":"` + id + `","task":"t","setup_cost_inr":1,"annual_running_cost_inr":1,"annual_benefit_inr":2,"benefit_type":"cash","overlap_group":"g` + id + `"` + extra + `}`
}

func TestDuplicateIDsRejectAllOccurrencesOrderIndependent(t *testing.T) {
	for _, body := range []string{"[" + row("A1", "") + "," + row("a1", "") + "," + row("B", "") + "]",
		"[" + row("B", "") + "," + row("a1", "") + "," + row("A1", "") + "]"} {
		r, err := fixture.Load([]byte(body), nil, domain.SourceTeam)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Pilots) != 1 || r.Pilots[0].ID != "B" || len(r.Rejected) != 2 || r.Rejected[0].Errors[0].Code != domain.CodeDuplicateID {
			t.Fatalf("%+v", r)
		}
	}
}

func TestMalformedAndShapes(t *testing.T) {
	for _, s := range []string{"{bad", "", "42", `{"records":5}`, `{"x":1}`} {
		if _, err := fixture.Load([]byte(s), nil, domain.SourceTeam); !errors.Is(err, fixture.ErrMalformed) {
			t.Errorf("%q: %v", s, err)
		}
	}
	r, err := fixture.Load([]byte(`[1, "x", null]`), nil, domain.SourceTeam)
	if err != nil || len(r.Rejected) != 3 {
		t.Fatalf("%v %+v", err, r)
	}
	r, _ = fixture.Load([]byte(`[]`), nil, domain.SourceTeam)
	if len(r.Warnings) != 1 || !strings.HasPrefix(r.Warnings[0], "EMPTY_LIST") {
		t.Fatal(r.Warnings)
	}
}

func TestExtensionRules(t *testing.T) {
	fx := []byte("[" + row("A", "") + "," + row("B", "") + "]")
	ext, _ := json.Marshal(map[string]any{"pilot_budget_inr": -1, "pilots": []any{
		map[string]any{"id": "A", "adoption_pct": 150},
		map[string]any{"id": "B", "observed_adoption_pct": 40},
		map[string]any{"id": "Z", "adoption_pct": 10},
	}})
	r, err := fixture.Load(fx, ext, domain.SourceTeam)
	if err != nil {
		t.Fatal(err)
	}
	if r.BudgetOK || len(r.Errors) != 1 || r.Errors[0].Code != "NEGATIVE_VALUE" {
		t.Fatal("negative budget must be rejected")
	}
	if len(r.Rejected) != 1 || r.Rejected[0].ID != "A" || r.Rejected[0].Errors[0].Code != "OUT_OF_RANGE" {
		t.Fatalf("adoption 150 must reject pilot loudly: %+v", r.Rejected)
	}
	b := testutil.ByID(r.Pilots)["B"]
	if b.Sources["actual_adoption_pct"] != domain.SourceObserved || b.ActualAdoption.String() != "40" {
		t.Fatal(b.Sources)
	}
	if !strings.Contains(strings.Join(r.Warnings, ";"), "ORPHAN_EXTENSION_ROW") {
		t.Fatal(r.Warnings)
	}
}
