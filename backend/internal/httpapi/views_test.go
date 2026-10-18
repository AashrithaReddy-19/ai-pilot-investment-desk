package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

func (f *fakePlatform) GetSettings(ctx context.Context) (json.RawMessage, error) {
	f.sawWorkspace = append(f.sawWorkspace, model.WorkspaceID(ctx))
	if f.settings == nil {
		return json.RawMessage(`{}`), nil
	}
	return f.settings, nil
}
func (f *fakePlatform) PutSettings(_ context.Context, s json.RawMessage) (json.RawMessage, error) {
	f.settings = s
	return s, nil
}
func (f *fakePlatform) ProposalsForEngine(_ context.Context, ids []string) ([]model.Proposal, error) {
	if len(ids) == 0 {
		return f.proposals, nil
	}
	out := []model.Proposal{}
	for _, id := range ids {
		found := false
		for _, p := range f.proposals {
			if p.ID == id {
				out, found = append(out, p), true
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", model.ErrUnknownProposal, id)
		}
	}
	return out, nil
}
func (f *fakePlatform) LatestProposalObservation(_ context.Context, id string) (*model.Observation, error) {
	if id == pA {
		a, q := 72.0, 90.0
		return &model.Observation{AdoptionPct: &a, QualityPct: &q}, nil
	}
	return nil, model.ErrNotFound
}
func (f *fakePlatform) SaveSnapshot(_ context.Context, in model.SnapshotIn) (*model.Snapshot, error) {
	if f.snapshots == nil {
		f.snapshots = map[string]*model.Snapshot{}
	}
	s := &model.Snapshot{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", len(f.snapshots)+1), Kind: in.Kind,
		Request: in.Request, Response: in.Response, Proposals: in.Proposals, RulesVersion: in.RulesVersion}
	f.snapshots[s.ID] = s
	return s, nil
}
func (f *fakePlatform) GetSnapshot(_ context.Context, id string) (*model.Snapshot, error) {
	if s, ok := f.snapshots[id]; ok {
		return s, nil
	}
	return nil, model.ErrNotFound
}
func (f *fakePlatform) ProposalVersions(_ context.Context, ids []string) (map[string]int, error) {
	out := map[string]int{}
	for _, id := range ids {
		if v, ok := f.versions[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}
func (f *fakePlatform) AddApproval(_ context.Context, in model.ApprovalIn) (*model.Approval, error) {
	if in.ProposalID != pA {
		return nil, model.ErrNotFound
	}
	f.approvals = append(f.approvals, in)
	return &model.Approval{ID: 1, ProposalID: in.ProposalID, ProposalVersion: 2, Decision: in.Decision, EngineOutcome: in.EngineOutcome}, nil
}

const (
	pA = "aaaaaaaa-1111-1111-1111-000000000001"
	pB = "bbbbbbbb-1111-1111-1111-000000000002"
)

func sampleProposals() []model.Proposal {
	return []model.Proposal{
		{ID: pA, Name: "Invoice extraction", Version: 2,
			Inputs: json.RawMessage(`{"setup_cost_inr":200000,"annual_benefit_inr":180000,"annual_running_cost_inr":60000,"readiness":"high","notes_only":"x"}`)},
		{ID: pB, Name: "Ticket triage", Version: 1,
			Inputs: json.RawMessage(`{"setup_cost_inr":500000,"annual_benefit_inr":400000,"ai_running_cost_inr":150000}`)},
	}
}

func newViewsRouter(f *fakeRules, p *fakePlatform) (*Handlers, func(method, path, body string) (int, string)) {
	r := NewRouter(Deps{Cfg: config.Config{RequestTimeout: 2e9, MaxBodyBytes: 1 << 16}, Store: fakeStore{}, Rules: f,
		Cache: fakeCache{}, Platform: p, Version: func() string { return "8" }, Log: discardLog()})
	return nil, func(method, path, body string) (int, string) {
		w := do(r, method, path, body, "Content-Type", "application/json")
		return w.Code, w.Body.String()
	}
}

func TestSettingsValidation(t *testing.T) {
	p := &fakePlatform{}
	_, call := newViewsRouter(&fakeRules{do: ok200(`{}`)}, p)
	for body, want := range map[string]string{
		`{"budget":-1}`:                       "budget must be",
		`{"mode":"wild"}`:                     "mode must be",
		`{"budget_set":{"total":1}}`:          "budget_set.total is not a known setting",
		`{"defaults":{"horizon_months":"x"}}`: "must be a number",
		`{"colour":"red"}`:                    "colour is not a known setting",
	} {
		if code, out := call("PUT", "/api/v1/settings", body); code != 422 || !contains(out, want) {
			t.Errorf("%s: want 422 %q, got %d %s", body, want, code, out)
		}
	}
	code, out := call("PUT", "/api/v1/settings", `{"budget":700000,"mode":"conservative","budget_set":{"total_inr":1500000,"contingency_pct":null},"defaults":{"discount_rate":0}}`)
	if code != 200 || !contains(out, `"budget":700000`) || contains(out, "contingency_pct") {
		t.Fatalf("valid settings (null withdraws a value): %d %s", code, out)
	}
	if code, out := call("GET", "/api/v1/settings", ""); code != 200 || !contains(out, `"total_inr":1500000`) {
		t.Fatalf("settings round trip: %d %s", code, out)
	}
}

func TestEngineProposalMapping(t *testing.T) {
	ps := sampleProposals()
	a, q := 72.0, 90.0
	got, err := engineProposal(ps[0], &model.Observation{AdoptionPct: &a, QualityPct: &q})
	if err != nil {
		t.Fatal(err)
	}
	s := string(mustJSON(got))
	for _, want := range []string{`"name":"Invoice extraction"`, `"ai_running_cost_inr":60000`, `"observed_adoption_pct":72`, `"observed_quality_pct":90`} {
		if !contains(s, want) {
			t.Errorf("mapping lost %s: %s", want, s)
		}
	}
	if contains(s, "notes_only") || contains(s, "annual_running_cost_inr") {
		t.Errorf("unknown keys must not reach the engine: %s", s)
	}
	// stated observed values win over the linked observation
	ps[0].Inputs = json.RawMessage(`{"setup_cost_inr":1,"annual_benefit_inr":2,"observed_adoption_pct":10}`)
	got, _ = engineProposal(ps[0], &model.Observation{AdoptionPct: &a})
	if string(got["observed_adoption_pct"]) != "10" {
		t.Errorf("stated observed value must win: %s", got["observed_adoption_pct"])
	}
	if _, err := engineProposal(model.Proposal{Name: "Empty", Inputs: json.RawMessage(`{}`)}, nil); err == nil || !contains(err.Error(), "setup_cost_inr") {
		t.Errorf("missing setup must be an error naming the field: %v", err)
	}
}

func TestWorkspaceDashboardBuildsFromStoredProposalsAndSnapshots(t *testing.T) {
	var sent map[string]any
	f := &fakeRules{do: func(_, path string, body []byte) (*model.Upstream, error) {
		_ = json.Unmarshal(body, &sent)
		return &model.Upstream{Status: 200, Body: []byte(`{"header":{"contract_version":1},"proposals":[{"id":"Invoice extraction","outcome":"invest"}]}`)}, nil
	}}
	p := &fakePlatform{proposals: sampleProposals(), settings: json.RawMessage(`{"budget":700000,"budget_set":{"total_inr":1500000},"defaults":{"discount_rate":0}}`),
		versions: map[string]int{pA: 2, pB: 1}}
	_, call := newViewsRouter(f, p)
	code, out := call("POST", "/api/v1/workspace/dashboard", `{"ai_burn_to_date":1550000}`)
	if code != 200 || !contains(out, `"snapshot"`) || !contains(out, `"result"`) {
		t.Fatalf("dashboard: %d %s", code, out)
	}
	if f.lastPath != "/views/dashboard" || sent["budget"] != 700000.0 || sent["ai_burn_to_date"] != 1550000.0 {
		t.Fatalf("engine request wrong: %s %v", f.lastPath, sent)
	}
	if len(sent["proposals"].([]any)) != 2 || sent["extension"] == nil || sent["budget_set"] == nil {
		t.Fatalf("engine request must carry proposals, defaults and budget set: %v", sent)
	}
	var resp struct {
		Snapshot model.Snapshot `json:"snapshot"`
	}
	_ = json.Unmarshal([]byte(out), &resp)
	if len(resp.Snapshot.Proposals) != 2 || resp.Snapshot.Proposals[0].Version != 2 || *resp.Snapshot.RulesVersion != "8" {
		t.Fatalf("snapshot must record proposal versions and the rules version: %+v", resp.Snapshot)
	}

	// fresh, then stale after an inputs edit, then reproducible on replay
	code, out = call("GET", "/api/v1/snapshots/"+resp.Snapshot.ID, "")
	if code != 200 || !contains(out, `"stale":false`) {
		t.Fatalf("fresh snapshot: %d %s", code, out)
	}
	p.versions[pA] = 3
	code, out = call("GET", "/api/v1/snapshots/"+resp.Snapshot.ID, "")
	if !contains(out, `"stale":true`) || !contains(out, `inputs changed (v2 → v3)`) {
		t.Fatalf("stale snapshot: %d %s", code, out)
	}
	code, out = call("POST", "/api/v1/snapshots/"+resp.Snapshot.ID+"/replay", "")
	if code != 200 || !contains(out, `"matches":true`) {
		t.Fatalf("replay: %d %s", code, out)
	}

	// approval records the engine outcome the approver saw
	code, out = call("POST", "/api/v1/proposals/"+pA+"/approvals", fmt.Sprintf(`{"decision":"approved","snapshot_id":"%s"}`, resp.Snapshot.ID))
	if code != 201 || p.approvals[0].EngineOutcome == nil || *p.approvals[0].EngineOutcome != "invest" {
		t.Fatalf("approval: %d %s %+v", code, out, p.approvals)
	}
	if code, _ := call("POST", "/api/v1/proposals/"+pA+"/approvals", `{"decision":"maybe"}`); code != 422 {
		t.Fatalf("bad decision: %d", code)
	}
	if code, _ := call("POST", "/api/v1/proposals/"+pB+"/approvals", `{"decision":"approved"}`); code != 404 {
		t.Fatalf("unknown proposal approval: %d", code)
	}
}

func TestWorkspaceViewErrors(t *testing.T) {
	p := &fakePlatform{}
	_, call := newViewsRouter(&fakeRules{do: ok200(`{}`)}, p)
	if code, out := call("POST", "/api/v1/workspace/dashboard", `{}`); code != 422 || !contains(out, "no_proposals") {
		t.Fatalf("no proposals: %d %s", code, out)
	}
	if code, _ := call("POST", "/api/v1/workspace/dashboard", `{"use_fixture":true}`); code != 200 {
		t.Fatalf("fixture fallback: %d", code)
	}
	p.proposals = sampleProposals()
	if code, out := call("POST", "/api/v1/workspace/decision", `{"proposal_ids":["cccccccc-1111-1111-1111-000000000003"]}`); code != 422 || !contains(out, "unknown_proposal") {
		t.Fatalf("unknown id: %d %s", code, out)
	}
	p.proposals[1].Inputs = json.RawMessage(`{"setup_cost_inr":1}`)
	if code, out := call("POST", "/api/v1/workspace/decision", `{}`); code != 422 || !contains(out, "incomplete_proposal") {
		t.Fatalf("incomplete proposal: %d %s", code, out)
	}
	if code, _ := call("POST", "/api/v1/workspace/comparison", `{"use_fixture":true}`); code != 422 {
		t.Fatalf("comparison needs stored proposals: %d", code)
	}
}

func TestWorkspaceFeaturesWithoutPlatformAre503(t *testing.T) {
	r := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{})
	if w := do(r, "GET", "/api/v1/settings", ""); w.Code != 503 || !contains(w.Body.String(), "not_configured") {
		t.Fatalf("no platform: %d %s", w.Code, w.Body.String())
	}
}

func TestEngineV7RoutesAreForwardedAndCached(t *testing.T) {
	for _, path := range []string{"/economics", "/decision", "/views/dashboard", "/views/yearly-savings", "/pricing/quote",
		"/pricing/optimise", "/forecast/usage"} {
		f := &fakeRules{do: ok200(`{"ok":1}`)}
		w := do(newRouter(f, fakeStore{}), "POST", "/api/v1"+path, `{}`)
		if w.Code != 200 || f.lastPath != path || w.Header().Get("X-Cache") == "" {
			t.Errorf("%s: %d path=%s cache=%q", path, w.Code, f.lastPath, w.Header().Get("X-Cache"))
		}
	}
	f := &fakeRules{do: ok200("# report")}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/export/decision-report", `{}`)
	if w.Code != 200 || !contains(w.Header().Get("Content-Disposition"), "decision-report.md") {
		t.Fatalf("decision report download: %d %v", w.Code, w.Header())
	}
}

var _ = errors.New
