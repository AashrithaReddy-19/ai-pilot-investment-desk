package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

// recordingAuditor keeps every event the handlers write; err makes every write fail.
type recordingAuditor struct {
	events  []model.AuditEventIn
	err     error
	filter  model.AuditFilter
	listErr error
}

func (a *recordingAuditor) AddAuditEvent(_ context.Context, e model.AuditEventIn) error {
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, e)
	return nil
}

func (a *recordingAuditor) ListAuditEvents(_ context.Context, f model.AuditFilter) ([]model.AuditEvent, error) {
	a.filter = f
	if a.listErr != nil {
		return nil, a.listErr
	}
	id := "p1"
	return []model.AuditEvent{{ID: 1, Action: "proposal.create", EntityType: "proposal", EntityID: &id, Details: json.RawMessage(`{}`)}}, nil
}

func (a *recordingAuditor) actions() []string {
	out := []string{}
	for _, e := range a.events {
		id := ""
		if e.EntityID != nil {
			id = *e.EntityID
		}
		out = append(out, e.Action+" "+e.EntityType+":"+id)
	}
	return out
}

func newAuditRouter(f *fakeRules, st Store, a Auditor) http.Handler {
	return NewRouter(Deps{
		Cfg:   config.Config{AllowedOrigins: []string{"http://localhost:5173"}, RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024},
		Store: st, Rules: f, Cache: fakeCache{}, Audit: a,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestAuditRecordsSuccessfulEditsAndExports(t *testing.T) {
	a := &recordingAuditor{}
	ps := &fakeProposalStore{}
	h := newAuditRouter(&fakeRules{do: ok200(`{}`)}, ps, a)
	for _, step := range []struct{ method, path, body string }{
		{"POST", "/api/v1/proposals", `{"name":"Invoice extraction","inputs":{"task":"x"}}`},
		{"PATCH", "/api/v1/proposals/" + testUUID, `{"notes":"n","inputs":{"task":"y"}}`},
		{"POST", "/api/v1/proposals/" + testUUID + "/archive", ``},
		{"POST", "/api/v1/proposals/" + testUUID + "/restore", ``},
		{"POST", "/api/v1/proposals/" + testUUID + "/evidence", `{"kind":"interview","source":"ops lead"}`},
		{"DELETE", "/api/v1/proposals/" + testUUID, ``},
		{"POST", "/api/v1/export/csv", `{}`},
	} {
		if w := do(h, step.method, step.path, step.body, "Content-Type", "application/json"); w.Code >= 300 {
			t.Fatalf("%s %s failed: %d %s", step.method, step.path, w.Code, w.Body.String())
		}
	}
	want := []string{
		"proposal.create proposal:" + testUUID,
		"proposal.update proposal:" + testUUID,
		"proposal.archive proposal:" + testUUID,
		"proposal.restore proposal:" + testUUID,
		"evidence.add evidence:7",
		"proposal.delete proposal:" + testUUID,
		"export.download export:pilots.csv",
	}
	if jsonOf(a.actions()) != jsonOf(want) {
		t.Fatalf("audit trail:\n got %v\nwant %v", a.actions(), want)
	}
	if string(a.events[1].Details) != `{"fields":["inputs","notes"],"version":0}` {
		t.Fatalf("update details must list the changed fields only: %s", a.events[1].Details)
	}
	for _, e := range a.events {
		if e.RequestID == "" || e.Actor != nil {
			t.Fatalf("every event carries the request id and no actor until auth exists: %+v", e)
		}
	}
}

func TestAuditCoversPlansAndMemo(t *testing.T) {
	a := &recordingAuditor{}
	st := &planStub{plan: samplePlan("active")}
	h := newAuditRouter(&fakeRules{do: ok200(`{}`)}, st, a)
	if w := do(h, "POST", "/api/v1/plans/AI1/status", `{"status":"stopped"}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/v1/export/memo", `{"pilot_id":"AI1"}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("memo: %d %s", w.Code, w.Body.String())
	}
	if w := do(h, "POST", "/api/v1/export/memo", `{"pilot_id":"bad id"}`, "Content-Type", "application/json"); w.Code != 200 {
		t.Fatalf("memo with an unusable pilot id still exports: %d %s", w.Code, w.Body.String())
	}
	if len(a.events) != 3 {
		t.Fatalf("want 3 events, got %v", a.actions())
	}
	if a.events[0].Action != "plan.status" || string(a.events[0].Details) != `{"status":"stopped"}` {
		t.Fatalf("plan event wrong: %+v %s", a.events[0], a.events[0].Details)
	}
	if string(a.events[1].Details) != `{"pilot_id":"AI1"}` || string(a.events[2].Details) != `{}` {
		t.Fatalf("memo events must record only a valid pilot id: %s / %s", a.events[1].Details, a.events[2].Details)
	}
}

func TestAuditSkipsFailedRequests(t *testing.T) {
	a := &recordingAuditor{}
	down := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) { return nil, errors.New("down") }}
	h := newAuditRouter(down, &fakeProposalStore{missing: true}, a)
	for _, step := range []struct{ method, path, body string }{
		{"POST", "/api/v1/export/csv", `{}`},                                    // engine down
		{"PATCH", "/api/v1/proposals/" + testUUID, `{"notes":"x"}`},             // not found
		{"DELETE", "/api/v1/proposals/" + testUUID, ``},                         // not found
		{"POST", "/api/v1/proposals", `{"name":"bad/name"}`},                    // validation
		{"POST", "/api/v1/proposals/" + testUUID + "/evidence", `{"kind":"x"}`}, // validation
	} {
		if w := do(h, step.method, step.path, step.body, "Content-Type", "application/json"); w.Code < 400 {
			t.Fatalf("%s %s should fail, got %d", step.method, step.path, w.Code)
		}
	}
	if len(a.events) != 0 {
		t.Fatalf("failed requests must not be audited: %v", a.actions())
	}
}

func TestAuditWriteFailureDoesNotFailTheRequest(t *testing.T) {
	a := &recordingAuditor{err: errors.New("audit table unavailable")}
	h := newAuditRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{}, a)
	w := do(h, "POST", "/api/v1/proposals", `{"name":"Still saved"}`, "Content-Type", "application/json")
	if w.Code != 201 {
		t.Fatalf("a failed audit write must not fail the change: %d %s", w.Code, w.Body.String())
	}
}

func TestAuditListValidatesAndClamps(t *testing.T) {
	a := &recordingAuditor{}
	h := newAuditRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{}, a)
	w := do(h, "GET", "/api/v1/audit?entity_type=proposal&entity_id=p1&limit=999", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"count":1`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	if a.filter != (model.AuditFilter{EntityType: "proposal", EntityID: "p1", Limit: 50}) {
		t.Fatalf("filter passed to the store: %+v", a.filter)
	}
	if do(h, "GET", "/api/v1/audit", ""); a.filter.Limit != 25 {
		t.Fatalf("default limit must be 25, got %d", a.filter.Limit)
	}
	for name, q := range map[string]string{
		"unknown type":    "?entity_type=user",
		"id without type": "?entity_id=p1",
		"id too long":     "?entity_type=proposal&entity_id=" + strings.Repeat("x", 101),
	} {
		if w := do(h, "GET", "/api/v1/audit"+q, ""); w.Code != 422 {
			t.Errorf("%s: want 422, got %d", name, w.Code)
		}
	}
	a.listErr = errors.New("db down")
	if w := do(h, "GET", "/api/v1/audit", ""); w.Code != 500 || !strings.Contains(w.Body.String(), "database_error") {
		t.Fatalf("store failure: want 500 database_error, got %d %s", w.Code, w.Body.String())
	}
}

// ---- evaluations linked to a proposal ----

type evalLinkStore struct {
	fakeProposalStore
	version  int
	saved    *model.EvaluationIn
	saveErr  error
	gotLimit int
}

func (s *evalLinkStore) GetProposal(_ context.Context, id string) (*model.Proposal, error) {
	if s.missing {
		return nil, model.ErrNotFound
	}
	return &model.Proposal{ID: id, Name: "test", Status: "active", Version: s.version, Inputs: json.RawMessage(`{}`)}, nil
}

func (s *evalLinkStore) SaveEvaluation(_ context.Context, e model.EvaluationIn) (int64, error) {
	if s.saveErr != nil {
		return 0, s.saveErr
	}
	s.saved = &e
	return 42, nil
}

func (s *evalLinkStore) ListProposalEvaluations(_ context.Context, _ string, limit int) ([]model.EvaluationSummary, error) {
	s.gotLimit = limit
	v := s.version
	id := testUUID
	return []model.EvaluationSummary{{ID: 42, ProposalID: &id, ProposalVersion: &v}}, nil
}

func engineBodies(bodies *[]string) *fakeRules {
	return &fakeRules{do: func(_ string, _ string, b []byte) (*model.Upstream, error) {
		*bodies = append(*bodies, string(b))
		return &model.Upstream{Status: 200, Body: []byte(`{"selected":[]}`), ContentType: "application/json"}, nil
	}}
}

func TestEvaluateLinksTheProposalVersionWithoutSendingIt(t *testing.T) {
	var bodies []string
	st := &evalLinkStore{version: 3}
	a := &recordingAuditor{}
	w := do(newAuditRouter(engineBodies(&bodies), st, a), "POST", "/api/v1/evaluate", `{"budget":600000,"proposal_id":"`+testUUID+`"}`)
	if w.Code != 200 {
		t.Fatalf("evaluate: %d %s", w.Code, w.Body.String())
	}
	if len(bodies) != 3 {
		t.Fatalf("want /pilots + two /portfolio calls, got %d", len(bodies))
	}
	for _, b := range bodies {
		if strings.Contains(b, "proposal_id") {
			t.Fatalf("proposal_id must not reach the engine: %s", b)
		}
	}
	if st.saved == nil || *st.saved.ProposalID != testUUID || *st.saved.ProposalVersion != 3 || strings.Contains(string(st.saved.Request), "proposal_id") {
		t.Fatalf("saved evaluation wrong: %+v", st.saved)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["evaluation_id"] != 42.0 || out["proposal_id"] != testUUID || out["proposal_version"] != 3.0 {
		t.Fatalf("response must carry the link: %s", w.Body.String())
	}
	if jsonOf(a.actions()) != jsonOf([]string{"evaluation.create evaluation:42"}) {
		t.Fatalf("audit: %v", a.actions())
	}
}

func TestEvaluateWithoutProposalStaysUnlinked(t *testing.T) {
	var bodies []string
	for _, body := range []string{`{"budget":1}`, `{"budget":1,"proposal_id":null}`} {
		st := &evalLinkStore{}
		w := do(newRouter(engineBodies(&bodies), st), "POST", "/api/v1/evaluate", body)
		if w.Code != 200 || st.saved == nil || st.saved.ProposalID != nil || st.saved.ProposalVersion != nil {
			t.Fatalf("%s: unlinked evaluation expected: %d %+v", body, w.Code, st.saved)
		}
		if !strings.Contains(w.Body.String(), `"proposal_id":null`) || !strings.Contains(w.Body.String(), `"evaluation_id":42`) {
			t.Fatalf("%s: response: %s", body, w.Body.String())
		}
	}
}

func TestEvaluateRejectsBadProposalBeforeTheEngine(t *testing.T) {
	never := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		t.Fatal("the engine must not be called for a bad proposal_id")
		return nil, nil
	}}
	for name, tc := range map[string]struct {
		st   *evalLinkStore
		body string
		code string
	}{
		"not a uuid": {&evalLinkStore{}, `{"proposal_id":"nope"}`, "invalid_value"},
		"a number":   {&evalLinkStore{}, `{"proposal_id":7}`, "invalid_value"},
		"unknown":    {&evalLinkStore{fakeProposalStore: fakeProposalStore{missing: true}}, `{"proposal_id":"` + testUUID + `"}`, "unknown_proposal"},
	} {
		w := do(newRouter(never, tc.st), "POST", "/api/v1/evaluate", tc.body)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("%s: want 422 %s, got %d %s", name, tc.code, w.Code, w.Body.String())
		}
	}
}

func TestEvaluateStillAnswersWhenHistoryCannotBeSaved(t *testing.T) {
	var bodies []string
	st := &evalLinkStore{saveErr: errors.New("db down")}
	a := &recordingAuditor{}
	w := do(newAuditRouter(engineBodies(&bodies), st, a), "POST", "/api/v1/evaluate", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"evaluation_id":null`) {
		t.Fatalf("evaluation must still answer with evaluation_id null: %d %s", w.Code, w.Body.String())
	}
	if len(a.events) != 0 {
		t.Fatalf("nothing was stored, so nothing is audited: %v", a.actions())
	}
}

func TestProposalEvaluationsEndpoint(t *testing.T) {
	st := &evalLinkStore{version: 2}
	h := newRouter(&fakeRules{do: ok200(`{}`)}, st)
	w := do(h, "GET", "/api/v1/proposals/"+testUUID+"/evaluations?limit=999", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"proposal_version":2`) || st.gotLimit != 50 {
		t.Fatalf("list: %d %s (limit %d)", w.Code, w.Body.String(), st.gotLimit)
	}
	if do(h, "GET", "/api/v1/proposals/"+testUUID+"/evaluations", ""); st.gotLimit != 20 {
		t.Fatalf("default limit must be 20, got %d", st.gotLimit)
	}
	if w := do(h, "GET", "/api/v1/proposals/not-a-uuid/evaluations", ""); w.Code != 422 {
		t.Fatalf("bad id: want 422, got %d", w.Code)
	}
	missing := &evalLinkStore{fakeProposalStore: fakeProposalStore{missing: true}}
	if w := do(newRouter(&fakeRules{do: ok200(`{}`)}, missing), "GET", "/api/v1/proposals/"+testUUID+"/evaluations", ""); w.Code != 404 {
		t.Fatalf("unknown proposal: want 404, got %d", w.Code)
	}
}
