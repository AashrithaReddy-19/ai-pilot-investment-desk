package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

type extractionFake struct {
	mu     sync.Mutex
	result *model.ProposalExtraction
	saved  int
}

func (f *extractionFake) GetProposalExtraction(_ context.Context, uploadID string) (*model.ProposalExtraction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.result == nil || f.result.UploadID != uploadID {
		return nil, model.ErrNotFound
	}
	copy := *f.result
	return &copy, nil
}

func (f *extractionFake) SaveProposalExtraction(_ context.Context, uploadID, status string, fields, warnings json.RawMessage, extractionErr *string) (*model.ProposalExtraction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved++
	f.result = &model.ProposalExtraction{ID: "eeeeeeee-0000-0000-0000-000000000001", UploadID: uploadID, Status: status, Fields: fields, Warnings: warnings, Error: extractionErr}
	return f.result, nil
}

type extractionProposalStore struct {
	fakeStore
	created *model.Proposal
}

func (f *extractionProposalStore) CreateProposal(_ context.Context, in model.ProposalIn) (*model.Proposal, error) {
	f.created = &model.Proposal{ID: "ffffffff-0000-0000-0000-000000000001", Name: in.Name, Inputs: in.Inputs, Notes: in.Notes, Version: 1}
	return f.created, nil
}

func allExplicitFields() json.RawMessage {
	values := map[string]any{
		"id": "P-1", "task": "Invoice automation", "setup_cost_inr": float64(1000),
		"annual_benefit_inr": float64(2000), "annual_running_cost_inr": float64(100),
		"benefit_type": "cashable", "overlap_group": "finance", "adoption_pct": float64(75),
		"readiness": "medium", "ramp_months": float64(3), "delay_months": float64(1),
	}
	fields := map[string]any{}
	for name, value := range values {
		fields[name] = map[string]any{"value": value, "status": "explicit", "evidence": []map[string]string{{"source": "row 1", "excerpt": name}}}
	}
	raw, _ := json.Marshal(fields)
	return raw
}

func extractionRouter(u *fakeUploads, extraction ProposalExtractions, proposals Store) http.Handler {
	return NewRouter(Deps{
		Cfg:   config.Config{RequestTimeout: 2e9, MaxBodyBytes: 1 << 16, UploadMaxBytes: 4096},
		Store: proposals, Rules: &fakeRules{do: ok200(`{}`)}, Cache: fakeCache{}, Uploads: u, Extractions: extraction, Log: discardLog(),
	})
}

func TestProposalExtractionConfirmationSubmitsExistingProposalContract(t *testing.T) {
	const id = "dddddddd-0000-0000-0000-000000000001"
	u := &fakeUploads{kind: "pilots", status: "parsed", content: map[string][]byte{}}
	extraction := &extractionFake{result: &model.ProposalExtraction{UploadID: id, Status: "review", Fields: allExplicitFields(), Warnings: []byte(`[]`)}}
	store := &extractionProposalStore{}
	w := do(extractionRouter(u, extraction, store), http.MethodPost, "/api/v1/uploads/"+id+"/proposal-extraction/confirm", "")
	if w.Code != http.StatusOK {
		t.Fatalf("confirmation failed: %d %s", w.Code, w.Body)
	}
	var response struct {
		Proposal struct {
			Name   string          `json:"name"`
			Inputs json.RawMessage `json:"inputs"`
		} `json:"proposal"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Proposal.Name != "Invoice automation" {
		t.Fatalf("existing proposal contract shape was not returned: %s", w.Body)
	}
	var inputs map[string]any
	if err := json.Unmarshal(response.Proposal.Inputs, &inputs); err != nil || inputs["ai_running_cost_inr"] != float64(100) {
		t.Fatalf("running-cost mapping was not preserved: %s", response.Proposal.Inputs)
	}
}

func TestProposalExtractionRejectsInvalidCorrectionAndUnauthorizedUpload(t *testing.T) {
	const id = "dddddddd-0000-0000-0000-000000000001"
	u := &fakeUploads{kind: "pilots", status: "parsed", content: map[string][]byte{}}
	extraction := &extractionFake{result: &model.ProposalExtraction{UploadID: id, Status: "review", Fields: allExplicitFields(), Warnings: []byte(`[]`)}}
	r := extractionRouter(u, extraction, &extractionProposalStore{})
	w := do(r, http.MethodPatch, "/api/v1/uploads/"+id+"/proposal-extraction", `{"field":"readiness","value":"invented"}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "invalid_value") {
		t.Fatalf("invalid enum was accepted: %d %s", w.Code, w.Body)
	}
	other := do(r, http.MethodGet, "/api/v1/uploads/aaaaaaaa-0000-0000-0000-000000000001/proposal-extraction", "")
	if other.Code != http.StatusNotFound || !strings.Contains(other.Body.String(), "not_found") {
		t.Fatalf("cross-upload access was not rejected: %d %s", other.Code, other.Body)
	}
}

func TestProposalExtractionRequiresAllFields(t *testing.T) {
	const id = "dddddddd-0000-0000-0000-000000000001"
	u := &fakeUploads{kind: "pilots", status: "parsed", content: map[string][]byte{}}
	fields := allExplicitFields()
	var values map[string]map[string]any
	_ = json.Unmarshal(fields, &values)
	values["annual_benefit_inr"] = map[string]any{"value": nil, "status": "missing"}
	fields, _ = json.Marshal(values)
	extraction := &extractionFake{result: &model.ProposalExtraction{UploadID: id, Status: "review", Fields: fields, Warnings: []byte(`[]`)}}
	w := do(extractionRouter(u, extraction, &extractionProposalStore{}), http.MethodPost, "/api/v1/uploads/"+id+"/proposal-extraction/confirm", "")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "annual_benefit_inr") {
		t.Fatalf("missing required field was accepted: %d %s", w.Code, w.Body)
	}
}

func TestProposalExtractionStoreFailureIsNotMasked(t *testing.T) {
	const id = "dddddddd-0000-0000-0000-000000000001"
	u := &fakeUploads{kind: "pilots", status: "parsed", content: map[string][]byte{}}
	extraction := &extractionFake{result: &model.ProposalExtraction{UploadID: id, Status: "review", Fields: allExplicitFields(), Warnings: []byte(`[]`)}}
	w := do(extractionRouter(u, &failingExtractionStore{extractionFake: extraction}, &extractionProposalStore{}), http.MethodPatch, "/api/v1/uploads/"+id+"/proposal-extraction", `{"field":"readiness","value":"high"}`)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "database password") {
		t.Fatalf("database failure was not sanitized: %d %s", w.Code, w.Body)
	}
}

type errorProposalStore struct{ fakeStore }

func (*errorProposalStore) CreateProposal(context.Context, model.ProposalIn) (*model.Proposal, error) {
	return nil, errors.New("database password leaked")
}

type failingExtractionStore struct{ *extractionFake }

func (*failingExtractionStore) SaveProposalExtraction(context.Context, string, string, json.RawMessage, json.RawMessage, *string) (*model.ProposalExtraction, error) {
	return nil, errors.New("database password leaked")
}
