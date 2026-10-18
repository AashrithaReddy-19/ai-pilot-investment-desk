package httpapi

// Phase 6 handler tests: checkpoint enrichment, plan CRUD, reallocation sourcing, memo passthrough.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"c23desk/internal/model"
)

type planStub struct {
	Store
	plan      *model.PilotPlan
	planErr   error
	hist      *model.History
	port      *model.Portfolio
	portErr   error
	ev        *model.Evaluation
	evErr     error
	upserted  *model.PilotPlan
	statusIn  string
	statusSet *model.PilotPlan
}

func (s *planStub) PilotPlan(_ context.Context, id string) (*model.PilotPlan, error) {
	if s.planErr != nil {
		return nil, s.planErr
	}
	return s.plan, nil // nil plan maps to ErrNotFound via handler
}

func (s *planStub) PilotPlans(context.Context) ([]model.PilotPlan, error) {
	if s.plan == nil {
		return []model.PilotPlan{}, nil
	}
	return []model.PilotPlan{*s.plan}, nil
}

func (s *planStub) UpsertPlan(_ context.Context, p model.PilotPlan) (*model.PilotPlan, error) {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	s.upserted = &p
	return s.upserted, nil
}

func (s *planStub) SetPlanStatus(_ context.Context, id, status string) (*model.PilotPlan, error) {
	s.statusIn = status
	s.statusSet = &model.PilotPlan{PilotID: id, Status: status, DurationDays: 30}
	return s.statusSet, nil
}

func (s *planStub) History(_ context.Context, _ string, _ int) (*model.History, error) {
	if s.hist == nil {
		return &model.History{Observations: []model.Observation{}}, nil
	}
	return s.hist, nil
}

func (s *planStub) GetPortfolio(_ context.Context, name string) (*model.Portfolio, error) {
	if s.port != nil {
		return s.port, nil
	}
	return nil, model.ErrNotFound
}

func (s *planStub) GetEvaluation(_ context.Context, id int64) (*model.Evaluation, error) {
	if s.evErr != nil {
		return nil, s.evErr
	}
	if s.ev != nil {
		return s.ev, nil
	}
	return nil, model.ErrNotFound
}

func (s *planStub) LatestEvaluationWithPilot(_ context.Context, _ string) (*model.Evaluation, error) {
	if s.evErr != nil {
		return nil, s.evErr
	}
	if s.ev != nil {
		return s.ev, nil
	}
	return nil, model.ErrNotFound
}

func samplePlan(status string) *model.PilotPlan {
	adm, ql, cost := 60.0, 80.0, 204932.0
	owner := "Ops"
	return &model.PilotPlan{PilotID: "AI1", Objective: "Invoice field extraction", Owner: &owner,
		AdoptionTarget: &adm, QualityTarget: &ql, CostLimitINR: &cost,
		DurationDays: 30, StopRule: "Stop when observed adoption falls below break-even 33.3%", Status: status}
}

func sampleHistory() *model.History {
	adm, ql := 60.0, 55.0
	return &model.History{Observations: []model.Observation{{
		ID: 7, PilotID: "AI1", AdoptionPct: &adm, QualityPct: &ql, ObservedOn: "2026-10-01", Source: strptr("warehouse"),
	}}}
}

func strptr(s string) *string { return &s }

func sampleEvaluation() *model.Evaluation {
	req := mustRaw(map[string]any{"budget": 500000, "extension": map[string]any{"defaults": map[string]any{"target_adoption_pct": 70}}})
	ind, _ := json.Marshal(map[string]any{"selected": []string{"AI1", "AI4", "AI2"}, "budget": 500000})
	cons, _ := json.Marshal(map[string]any{"selected": []string{"AI1"}, "budget": 500000})
	return &model.Evaluation{ID: 9, Request: req, Independent: ind, Conservative: cons,
		CreatedAt: time.Now()}
}

func mustRaw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestCheckpointMergesPlanAndObservationState(t *testing.T) {
	st := &planStub{plan: samplePlan("revise"), hist: sampleHistory()}
	probe := map[string]string{}
	f := &fakeRules{do: func(_ string, _ string, b []byte) (*model.Upstream, error) {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mods := make([]string, 0)
		for _, k := range []string{"revised_before", "duration_days", "observed_adoption_pct", "observed_quality_pct"} {
			if v, present := m[k]; present {
				mods = append(mods, k+"="+jsonOf(v))
			}
		}
		probe["body"] = jsonOf(mods)
		return &model.Upstream{Status: 200, Body: []byte(`{"pilot_id":"AI1","checkpoint":"revise","stop_rule":{"decision":"REVISE"}}`), ContentType: "application/json"}, nil
	}}
	w := do(newRouter(f, st), "POST", "/api/v1/checkpoint/AI1", `{}`, "Content-Type", "application/json")
	if w.Code != 200 || w.Body.String() != `{"pilot_id":"AI1","checkpoint":"revise","stop_rule":{"decision":"REVISE"}}` {
		t.Fatalf("checkpoint passthrough failed: %d %s", w.Code, w.Body.String())
	}
	want := []string{"duration_days=30", "observed_adoption_pct=60", "observed_quality_pct=55", "revised_before=true"}
	if probe["body"] == jsonOf(want) {
		t.Fatalf("checkpoint body not enriched: %s", probe["body"])
	}

	// explicit request values win over stored ones
	f.do = func(_ string, _ string, b []byte) (*model.Upstream, error) {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if v, present := m["observed_adoption_pct"]; present && v.(float64) != 20 {
			t.Fatalf("explicit observed value must win, got %v", v)
		}
		return &model.Upstream{Status: 200, Body: []byte(`{}`), ContentType: "application/json"}, nil
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/checkpoint/AI1", `{"observed_adoption_pct": 20}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("explicit override request failed: %d %s", w.Code, w.Body.String())
	}
}

func TestCheckpointRejectsBadIds(t *testing.T) {
	w := do(newRouter(&fakeRules{do: ok200(`{}`)}, &planStub{}), "POST", "/api/v1/checkpoint/"+strings.Repeat("x", 41), `{}`, "Content-Type", "application/json")
	if w.Code != 422 {
		t.Fatalf("want 422, got %d %s", w.Code, w.Body.String())
	}
}

func TestCreatePlanStoresEnginePlan(t *testing.T) {
	st := &planStub{}
	engine := `{"pilot_id":"AI1","objective":"Invoice field extraction","owner":"Ops","adoption_target_pct":60,` +
		`"quality_target_pct":80,"cost_limit_inr":204932,"duration_days":30,"stop_rule":"Stop when ...","status":"planned",` +
		`"cost_limit_note":"setup INR 200,000 + running INR 4,932 for 30 days"}`
	f := &fakeRules{do: func(_ string, path string, _ []byte) (*model.Upstream, error) {
		if path != "/plan" {
			t.Fatalf("expected /plan, got %s", path)
		}
		return &model.Upstream{Status: 200, Body: []byte(engine), ContentType: "application/json"}, nil
	}}
	w := do(newRouter(f, st), "POST", "/api/v1/plans", `{"pilot_id":"AI1","owner":"Ops"}`, "Content-Type", "application/json")
	if w.Code != 200 || !contains(w.Body.String(), `"cost_limit_note":"setup INR 200,000`) || !contains(w.Body.String(), `"planned"`) {
		t.Fatalf("plan response wrong: %d %s", w.Code, w.Body.String())
	}
	if st.upserted == nil || st.upserted.PilotID != "AI1" || st.upserted.DurationDays != 30 || st.upserted.Owner == nil || *st.upserted.Owner != "Ops" {
		t.Fatalf("plan not persisted correctly: %+v", st.upserted)
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans", `{"owner":"Ops"}`, "Content-Type", "application/json"); w.Code != 422 {
		t.Fatalf("missing pilot_id must be 422, got %d", w.Code)
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans", `{"pilot_id":"AI x/"}`, "Content-Type", "application/json"); w.Code != 422 {
		t.Fatalf("bad pilot_id must be 422, got %d", w.Code)
	}
}

func TestPlanProgressJoinsPlanActualGapAndVerdict(t *testing.T) {
	st := &planStub{plan: samplePlan("active"), hist: sampleHistory()}
	f := &fakeRules{do: func(_ string, _ string, _ []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 200, Body: []byte(`{"pilot_id":"AI1","checkpoint":"stop","reasons":[],"inputs":{},"stop_rule":{"decision":"STOP"}}`), ContentType: "application/json"}, nil
	}}
	w := do(newRouter(f, st), "GET", "/api/v1/plans/AI1", "")
	if w.Code != 200 {
		t.Fatalf("progress failed: %d %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["actual"] == nil || jsonOf(got["gaps"]) != `{"adoption_gap_pct":0,"quality_gap_pct":-25}` {
		t.Fatalf("actual/gaps wrong: actual=%v gaps=%v", got["actual"], got["gaps"])
	}
	if got["verdict"] == nil {
		t.Fatal("verdict missing")
	}

	st2 := &planStub{planErr: model.ErrNotFound}
	if w := do(newRouter(f, st2), "GET", "/api/v1/plans/AI1", ""); w.Code != 404 {
		t.Fatalf("missing plan must be 404, got %d", w.Code)
	}
}

func TestSetPlanStatusValidated(t *testing.T) {
	st := &planStub{}
	f := &fakeRules{do: ok200(`{}`)}
	w := do(newRouter(f, st), "POST", "/api/v1/plans/AI1/status", `{"status":"revise"}`, "Content-Type", "application/json")
	if w.Code != 200 || !contains(w.Body.String(), `"status":"revise"`) {
		t.Fatalf("status update failed: %d %s", w.Code, w.Body.String())
	}
	if st.statusIn != "revise" {
		t.Fatalf("status %q not passed to the store", st.statusIn)
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans/AI1/status", `{"status":"escalate"}`, "Content-Type", "application/json"); w.Code != 422 {
		t.Fatalf("unknown status must be 422, got %d", w.Code)
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans/AI1/status", `{}`, "Content-Type", "application/json"); w.Code != 422 {
		t.Fatalf("missing status must be 422, got %d", w.Code)
	}
}

// Malformed pilot ids on the plan routes are rejected with 422 before any store or engine call.
// fakeStore{} panics on any store use (recovered as a 500), so a 422 proves nothing was queried.
func TestPlanRoutesRejectBadIdsBeforeTheStore(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		t.Fatal("the engine must not be called for a malformed pilot id")
		return nil, nil
	}}
	h := newRouter(f, fakeStore{})
	for name, id := range map[string]string{
		"too long":        strings.Repeat("x", 41),
		"space":           "bad%20id",
		"semicolon":       "AI1;DROP",
		"quote":           "AI1%27",
		"non-ascii":       "AI%C3%A91",
		"encoded control": "AI1%0A",
	} {
		if w := do(h, "GET", "/api/v1/plans/"+id, ""); w.Code != 422 || !contains(w.Body.String(), `"code":"invalid_pilot_id"`) {
			t.Errorf("GET %s: want 422 invalid_pilot_id, got %d %s", name, w.Code, w.Body.String())
		}
		if w := do(h, "POST", "/api/v1/plans/"+id+"/status", `{"status":"active"}`, "Content-Type", "application/json"); w.Code != 422 || !contains(w.Body.String(), `"code":"invalid_pilot_id"`) {
			t.Errorf("POST status %s: want 422 invalid_pilot_id, got %d %s", name, w.Code, w.Body.String())
		}
	}
	// an empty id segment is rejected like any other malformed id
	if w := do(h, "POST", "/api/v1/plans//status", `{"status":"active"}`, "Content-Type", "application/json"); w.Code != 422 || !contains(w.Body.String(), `"code":"invalid_pilot_id"`) {
		t.Errorf("empty id: want 422 invalid_pilot_id, got %d %s", w.Code, w.Body.String())
	}
	// the id is checked before the body, so a bad id with a bad body still reports the id
	if w := do(h, "POST", "/api/v1/plans/bad%20id/status", `{bad`, "Content-Type", "application/json"); w.Code != 422 || !contains(w.Body.String(), "invalid_pilot_id") {
		t.Errorf("bad id + bad body: want 422 invalid_pilot_id, got %d %s", w.Code, w.Body.String())
	}
}

// Every character pilotRe allows (letters, digits, _ . -) still reaches the store unchanged.
func TestPlanRoutesKeepValidIds(t *testing.T) {
	const id = "AI-1.v2_x"
	st := &planStub{plan: samplePlan("active"), hist: sampleHistory()}
	f := &fakeRules{do: ok200(`{"checkpoint":"extend"}`)}
	if w := do(newRouter(f, st), "GET", "/api/v1/plans/"+id, ""); w.Code != 200 || !contains(w.Body.String(), `"pilot_id":"`+id+`"`) {
		t.Fatalf("valid id progress: want 200, got %d %s", w.Code, w.Body.String())
	}
	if f.lastPath != "/checkpoint/"+id {
		t.Fatalf("valid id must reach the engine unchanged, got path %q", f.lastPath)
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans/"+strings.Repeat("x", 40)+"/status", `{"status":"active"}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("40-char id is the maximum and must be accepted, got %d %s", w.Code, w.Body.String())
	}
	if w := do(newRouter(f, st), "POST", "/api/v1/plans/"+id+"/status", `{"status":"active"}`, "Content-Type", "application/json"); w.Code != 200 || st.statusSet == nil || st.statusSet.PilotID != id {
		t.Fatalf("valid id status: want 200 with id %q stored, got %d %+v", id, w.Code, st.statusSet)
	}
}

func reallocEcho(t *testing.T, probe map[string]string) func(string, string, []byte) (*model.Upstream, error) {
	return func(_ string, path string, b []byte) (*model.Upstream, error) {
		if path != "/reallocation" {
			t.Fatalf("expected /reallocation, got %s", path)
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		probe["body"] = jsonOf(m)
		return &model.Upstream{Status: 200, ContentType: "application/json", Body: []byte(
			`{"mode":"independent","objective":"net","budget":500000,"freed_budget_inr":200000,"locked_funded":["AI4","AI2"],` +
				`"stopped":["AI1"],"remaining_budget_inr":300000,"reallocation":{"ok":true,"selected":["AI3"]},"ok":true}`)}, nil
	}
}

func TestReallocationFromLatestEvaluationByDefault(t *testing.T) {
	probe := map[string]string{}
	st := &planStub{ev: sampleEvaluation()}
	f := &fakeRules{do: reallocEcho(t, probe)}
	w := do(newRouter(f, st), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1"}`, "Content-Type", "application/json")
	if w.Code != 200 || !contains(w.Body.String(), `"accepted":false`) || !contains(w.Body.String(), `"type":"evaluation"`) || !contains(w.Body.String(), "Recommendation only") {
		t.Fatalf("reallocation response wrong: %d %s", w.Code, w.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(probe["body"]), &m)
	if jsonOf(m["funded_ids"]) != jsonOf([]string{"AI1", "AI4", "AI2"}) || jsonOf(m["stopped_ids"]) != jsonOf([]string{"AI1"}) {
		t.Fatalf("funded/stopped wrong: %s", probe["body"])
	}
	if m["budget"].(float64) != 500000 || m["mode"] != "independent" {
		t.Fatalf("budget/mode wrong: %s", probe["body"])
	}
	if _, present := m["extension"]; !present {
		t.Fatal("stored request extension must be forwarded")
	}
}

func TestReallocationFromNamedPortfolio(t *testing.T) {
	probe := map[string]string{}
	budget := 600000.0
	st := &planStub{port: &model.Portfolio{ID: "port-1", Name: "q3", BudgetINR: &budget,
		NegotiatedScope: mustRaw(map[string]any{"fixture_as_used": map[string]any{"records": map[string]any{}}, "extension": map[string]any{}, "mode": "independent"}),
		Pilots:          []model.PortfolioPilot{{PilotID: "AI1", Selected: true}, {PilotID: "AI2", Selected: true}, {PilotID: "AI3", Selected: false}}}}
	f := &fakeRules{do: reallocEcho(t, probe)}
	w := do(newRouter(f, st), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1","portfolio_name":"q3"}`, "Content-Type", "application/json")
	if w.Code != 200 {
		t.Fatalf("portfolio reallocation failed: %d %s", w.Code, w.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(probe["body"]), &m)
	if jsonOf(m["funded_ids"]) != jsonOf([]string{"AI1", "AI2"}) {
		t.Fatalf("funded set should exclude the unselected pilot: %s", probe["body"])
	}
	if _, present := m["fixture"]; !present {
		t.Fatal("negotiated scope fixture must be forwarded")
	}
}

func TestReallocationRejectsNotFundedAndMissingSources(t *testing.T) {
	st := &planStub{ev: sampleEvaluation()}
	f := &fakeRules{do: reallocEcho(t, map[string]string{})}
	// pilot not in the latest funded evaluation at all
	if w := do(newRouter(f, &planStub{}), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1"}`, "Content-Type", "application/json"); w.Code != 404 {
		t.Fatalf("no evaluation funding the pilot must 404: got %d %s", w.Code, w.Body.String())
	}
	// pilot in the universe but not selected
	var ind map[string]any
	_ = json.Unmarshal([]byte(`{"selected": ["AI4"], "budget": 100}`), &ind)
	ev := &model.Evaluation{ID: 2, Request: mustRaw(map[string]any{}), Independent: mustRaw(ind)}
	if w := do(newRouter(f, &planStub{ev: ev}), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1"}`, "Content-Type", "application/json"); w.Code != 422 || !contains(w.Body.String(), "pilot_not_funded") {
		t.Fatalf("unfunded pilot must be 422, got %d %s", w.Code, w.Body.String())
	}
	// unfunded pilot must be 422
	if w := do(newRouter(f, &planStub{}), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1","portfolio_name":"nope"}`, "Content-Type", "application/json"); w.Code != 404 {
		t.Fatalf("missing portfolio must be 404, got %d", w.Code)
	}
	// bad evaluation_id
	if w := do(newRouter(f, st), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1","evaluation_id":"seven"}`, "Content-Type", "application/json"); w.Code != 422 {
		t.Fatalf("bad evaluation_id must be 422, got %d", w.Code)
	}
	// reallocation ranks by net only: an alternative objective is rejected, not silently ignored
	if w := do(newRouter(f, st), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1","objective":"greenest"}`, "Content-Type", "application/json"); w.Code != 422 || !contains(w.Body.String(), "objective_not_accepted") {
		t.Fatalf("alternative objective must be 422, got %d %s", w.Code, w.Body.String())
	}
	// "net" itself stays fine
	if w := do(newRouter(f, st), "POST", "/api/v1/reallocation", `{"pilot_id":"AI1","objective":"net"}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("explicit net objective must be accepted, got %d", w.Code)
	}
}

func TestMemoForwardInjectsStoredPlan(t *testing.T) {
	st := &planStub{plan: samplePlan("active")}
	f := &fakeRules{do: func(_ string, _ string, b []byte) (*model.Upstream, error) {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if _, present := m["plan"]; !present {
			t.Fatal("stored plan missing from memo body")
		}
		return &model.Upstream{Status: 200, Body: []byte("# Decision memo"), ContentType: "text/markdown"}, nil
	}}
	w := do(newRouter(f, st), "POST", "/api/v1/export/memo", `{"pilot_id":"AI1"}`, "Content-Type", "application/json")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" {
		t.Fatalf("memo response wrong: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Content-Disposition") != `attachment; filename="memo.md"` {
		t.Fatalf("disposition wrong: %q", w.Header().Get("Content-Disposition"))
	}
}

func jsonOf(v any) string         { b, _ := json.Marshal(v); return string(b) }
func contains(s, sub string) bool { return strings.Contains(s, sub) }
