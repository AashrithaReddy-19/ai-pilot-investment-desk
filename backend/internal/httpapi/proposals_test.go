package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"c23desk/internal/model"
)

// fakeProposalStore extends fakeStore with the proposal/evidence methods the routes need.
type fakeProposalStore struct {
	fakeStore
	created   model.Proposal
	duplicate bool
	missing   bool
	lastObs   model.ObservationIn
}

const testUUID = "11111111-2222-3333-4444-555555555555"

func (f *fakeProposalStore) CreateProposal(_ context.Context, in model.ProposalIn) (*model.Proposal, error) {
	if f.duplicate {
		return nil, errors.New("upsert: ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)")
	}
	f.created = model.Proposal{ID: testUUID, Name: in.Name, Status: "active", Inputs: in.Inputs, Notes: in.Notes}
	return &f.created, nil
}

func (f *fakeProposalStore) GetProposal(_ context.Context, id string) (*model.Proposal, error) {
	if f.missing {
		return nil, model.ErrNotFound
	}
	return &model.Proposal{ID: id, Name: "test", Status: "active", Inputs: json.RawMessage(`{}`)}, nil
}

func (f *fakeProposalStore) ListProposals(_ context.Context, includeArchived bool) ([]model.ProposalSummary, error) {
	out := []model.ProposalSummary{{ID: testUUID, Name: "test", Status: "active"}}
	if includeArchived {
		out = append(out, model.ProposalSummary{ID: testUUID, Name: "old", Status: "archived"})
	}
	return out, nil
}

func (f *fakeProposalStore) UpdateProposal(_ context.Context, id string, in model.ProposalIn) (*model.Proposal, error) {
	if f.missing {
		return nil, model.ErrNotFound
	}
	name := in.Name
	if name == "" {
		name = "test"
	}
	return &model.Proposal{ID: id, Name: name, Status: "active", Inputs: in.Inputs, Notes: in.Notes}, nil
}

func (f *fakeProposalStore) ArchiveProposal(_ context.Context, id string, restore bool) (*model.Proposal, error) {
	if f.missing {
		return nil, model.ErrNotFound
	}
	status := "archived"
	if restore {
		status = "active"
	}
	return &model.Proposal{ID: id, Name: "test", Status: status, Inputs: json.RawMessage(`{}`)}, nil
}

func (f *fakeProposalStore) DeleteProposal(_ context.Context, _ string) error {
	if f.missing {
		return model.ErrNotFound
	}
	return nil
}

func (f *fakeProposalStore) AddEvidence(_ context.Context, in model.EvidenceIn) (*model.Evidence, error) {
	return &model.Evidence{ID: 7, ProposalID: in.ProposalID, Kind: in.Kind, Source: in.Source, Quality: in.Quality, Note: in.Note}, nil
}

func (f *fakeProposalStore) ListEvidence(_ context.Context, proposalID string) ([]model.Evidence, error) {
	return []model.Evidence{{ID: 7, ProposalID: proposalID, Kind: "interview", Source: "ops"}}, nil
}

func (f *fakeProposalStore) AddObservation(_ context.Context, o model.ObservationIn, _ model.DecisionIn) (*model.Observation, *model.Decision, error) {
	f.lastObs = o
	return &model.Observation{ID: 1, PilotID: o.PilotID}, &model.Decision{ID: 1, PilotID: o.PilotID}, nil
}

func TestProposalCreateValidation(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{})
	for name, body := range map[string]string{
		"missing name":   `{}`,
		"empty name":     `{"name":"   "}`,
		"name too long":  `{"name":"` + strings.Repeat("a", 81) + `"}`,
		"name bad chars": `{"name":"bad name!"}`,
		"inputs array":   `{"name":"ok","inputs":[]}`,
	} {
		if w := do(h, "POST", "/api/v1/proposals", body); w.Code != 422 {
			t.Errorf("%s: want 422, got %d %s", name, w.Code, w.Body)
		}
	}
	// A 2001-char note also trips the router's 1024-byte body cap first; either rejection is fine.
	if w := do(h, "POST", "/api/v1/proposals", `{"name":"ok","notes":"`+strings.Repeat("n", 2001)+`"}`); w.Code != 413 && w.Code != 422 {
		t.Errorf("notes too long: want 413 or 422, got %d", w.Code)
	}
	w := do(h, "POST", "/api/v1/proposals", `{"name":"Invoice extraction","inputs":{"task":"extract"},"notes":"draft"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), testUUID) {
		t.Fatalf("want 201 with the id, got %d %s", w.Code, w.Body)
	}
}

func TestProposalCreateDuplicateIs422(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{duplicate: true})
	w := do(h, "POST", "/api/v1/proposals", `{"name":"dup"}`)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "duplicate_name") {
		t.Fatalf("want 422 duplicate_name, got %d %s", w.Code, w.Body)
	}
}

func TestProposalIDPathValidationAnd404(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{missing: true})
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		if w := do(h, method, "/api/v1/proposals/not-a-uuid", `{}`); w.Code != 422 {
			t.Errorf("%s bad id: want 422, got %d", method, w.Code)
		}
	}
	if w := do(h, "GET", "/api/v1/proposals/"+testUUID, ""); w.Code != 404 {
		t.Errorf("missing proposal: want 404, got %d", w.Code)
	}
	if w := do(h, "DELETE", "/api/v1/proposals/"+testUUID, ""); w.Code != 404 {
		t.Errorf("missing delete: want 404, got %d", w.Code)
	}
}

func TestProposalUpdatePatchKeepsOmittedFields(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{})
	w := do(h, "PATCH", "/api/v1/proposals/"+testUUID, `{"notes":"only notes"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"test"`) || !strings.Contains(w.Body.String(), "only notes") {
		t.Fatalf("want 200 keeping stored name, got %d %s", w.Code, w.Body)
	}
}

func TestProposalArchiveAndRestore(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{})
	if w := do(h, "POST", "/api/v1/proposals/"+testUUID+"/archive", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"archived"`) {
		t.Fatalf("archive: got %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/v1/proposals/"+testUUID+"/restore", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"active"`) {
		t.Fatalf("restore: got %d %s", w.Code, w.Body)
	}
}

func TestProposalListIncludeArchived(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{})
	if w := do(h, "GET", "/api/v1/proposals", ""); w.Code != 200 || strings.Contains(w.Body.String(), `"old"`) {
		t.Fatalf("default list must hide archived: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/proposals?include_archived=true", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"old"`) {
		t.Fatalf("include_archived must show archived: %d %s", w.Code, w.Body)
	}
}

func TestEvidenceValidationAndCreate(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeProposalStore{})
	for name, body := range map[string]string{
		"bad kind":       `{"kind":"rumour","source":"ops"}`,
		"missing source": `{"kind":"interview"}`,
		"bad quality":    `{"kind":"interview","source":"ops","quality":"vibes"}`,
	} {
		if w := do(h, "POST", "/api/v1/proposals/"+testUUID+"/evidence", body); w.Code != 422 {
			t.Errorf("%s: want 422, got %d %s", name, w.Code, w.Body)
		}
	}
	// A giant note trips the router's body cap before the 2000-char field cap; either rejection is fine.
	if w := do(h, "POST", "/api/v1/proposals/"+testUUID+"/evidence", `{"kind":"document","source":"ops","note":"`+strings.Repeat("n", 2001)+`"}`); w.Code != 413 && w.Code != 422 {
		t.Errorf("note too long: want 413 or 422, got %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/proposals/nope/evidence", `{"kind":"document","source":"ops"}`); w.Code != 422 {
		t.Errorf("bad uuid: want 422, got %d", w.Code)
	}
	w := do(h, "POST", "/api/v1/proposals/"+testUUID+"/evidence", `{"kind":"interview","source":"finance clerk","quality":"observed","note":"12 min per invoice"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"id":7`) || !strings.Contains(w.Body.String(), testUUID) {
		t.Fatalf("want 201, got %d %s", w.Code, w.Body)
	}
}

func TestObservationAcceptsProposalLink(t *testing.T) {
	f := &fakeRules{do: ok200(`{"decision":"GO","reasons":["ok"],"metric":"payback","threshold":24,"actual":20}`)}
	st := &fakeProposalStore{}
	h := newRouter(f, st)

	// proposal_id must reach the store but NOT be forwarded to the rules engine.
	w := do(h, "POST", "/api/v1/pilots/AI1/observations", `{"observed_adoption_pct":50,"proposal_id":"`+testUUID+`"}`)
	if w.Code != 201 {
		t.Fatalf("want 201, got %d %s", w.Code, w.Body)
	}
	if st.lastObs.ProposalID == nil || *st.lastObs.ProposalID != testUUID {
		t.Fatalf("proposal link lost: %v", st.lastObs.ProposalID)
	}
	if strings.Contains(f.lastBody, testUUID) {
		t.Fatal("proposal_id must not be forwarded to the rules engine")
	}

	// An invalid proposal_id is rejected before the engine is called.
	if w := do(h, "POST", "/api/v1/pilots/AI1/observations", `{"observed_adoption_pct":50,"proposal_id":"nope"}`); w.Code != 422 {
		t.Fatalf("bad proposal_id: want 422, got %d", w.Code)
	}
	// A null proposal_id is allowed.
	if w := do(h, "POST", "/api/v1/pilots/AI1/observations", `{"observed_adoption_pct":50,"proposal_id":null}`); w.Code != 201 {
		t.Fatalf("null proposal_id: want 201, got %d %s", w.Code, w.Body)
	}
}

func TestObservationUnknownProposalIs422(t *testing.T) {
	// A real FK violation from postgres must surface as 422 unknown_proposal, not 500.
	fkErr := errors.New(`insert observation: ERROR: violates foreign key constraint "observations_proposal_id_fkey" (SQLSTATE 23503)`)
	fk := &fakeProposalStore{fakeStore: fakeStore{}}
	h := newRouter(&fakeRules{do: ok200(`{"decision":"GO","reasons":["ok"]}`)}, fkWithFK{fk, fkErr})
	w := do(h, "POST", "/api/v1/pilots/AI1/observations", `{"observed_adoption_pct":50,"proposal_id":"`+testUUID+`"}`)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "unknown_proposal") {
		t.Fatalf("want 422 unknown_proposal, got %d %s", w.Code, w.Body)
	}
}

type fkWithFK struct {
	*fakeProposalStore
	err error
}

func (f fkWithFK) AddObservation(_ context.Context, o model.ObservationIn, _ model.DecisionIn) (*model.Observation, *model.Decision, error) {
	return nil, nil, f.err
}

// ---- evaluations audit log ----

type fakeEvalStore struct {
	fakeStore
	eval model.Evaluation
}

func (f *fakeEvalStore) ListEvaluations(_ context.Context, limit int) ([]model.EvaluationSummary, error) {
	if limit > 50 || limit < 1 {
		return nil, errors.New("limit not clamped: " + strconv.Itoa(limit))
	}
	return []model.EvaluationSummary{{ID: 9, CreatedAt: time.Unix(1697600000, 0).UTC()}}, nil
}

func (f *fakeEvalStore) GetEvaluation(_ context.Context, id int64) (*model.Evaluation, error) {
	if id != 9 {
		return nil, model.ErrNotFound
	}
	return &f.eval, nil
}

func TestEvaluationsReadPath(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, &fakeEvalStore{eval: model.Evaluation{
		ID: 9, Request: json.RawMessage(`{"budget":1}`), Independent: json.RawMessage(`{}`), Conservative: json.RawMessage(`{}`),
	}})
	w := do(h, "GET", "/api/v1/evaluations?limit=50", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"id":9`) {
		t.Fatalf("list: got %d %s", w.Code, w.Body)
	}
	w = do(h, "GET", "/api/v1/evaluations", "")
	if w.Code != 200 {
		t.Fatalf("default limit: got %d", w.Code)
	}
	w = do(h, "GET", "/api/v1/evaluations/9", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"request":{"budget":1}`) {
		t.Fatalf("get: got %d %s", w.Code, w.Body)
	}
	w = do(h, "GET", "/api/v1/evaluations/not-a-number", "")
	if w.Code != 422 {
		t.Fatalf("bad id: want 422, got %d", w.Code)
	}
	w = do(h, "GET", "/api/v1/evaluations/8", "")
	if w.Code != 404 {
		t.Fatalf("missing id: want 404, got %d", w.Code)
	}
}

// limitStore records the limit the handler passes to the store.
type limitStore struct {
	fakeStore
	got int
}

func (s *limitStore) ListEvaluations(_ context.Context, limit int) ([]model.EvaluationSummary, error) {
	s.got = limit
	return []model.EvaluationSummary{}, nil
}

// The handler clamps limit to 1–50 (default 20) itself, so the query can never exceed 50.
func TestEvaluationsLimitIsClampedInTheHandler(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 20},                            // missing
		{"?limit=", 20},                     // empty
		{"?limit=abc", 20},                  // not a number
		{"?limit=1.5", 20},                  // not an integer
		{"?limit=99999999999999999999", 20}, // overflows int
		{"?limit=0", 20},                    // below the minimum
		{"?limit=-5", 20},                   // negative
		{"?limit=1", 1},                     // minimum
		{"?limit=20", 20},                   // default value
		{"?limit=49", 49},
		{"?limit=50", 50},  // maximum
		{"?limit=51", 50},  // just above the maximum
		{"?limit=999", 50}, // far above the maximum
	} {
		st := &limitStore{}
		w := do(newRouter(&fakeRules{do: ok200(`{}`)}, st), "GET", "/api/v1/evaluations"+tc.query, "")
		if w.Code != 200 {
			t.Errorf("%q: want 200, got %d %s", tc.query, w.Code, w.Body.String())
			continue
		}
		if st.got != tc.want {
			t.Errorf("%q: store got limit %d, want %d", tc.query, st.got, tc.want)
		}
	}
}
