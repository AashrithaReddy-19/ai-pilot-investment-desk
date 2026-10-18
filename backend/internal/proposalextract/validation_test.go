package proposalextract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateMockResponseRejectsInvalidEnumAndMissingEvidence(t *testing.T) {
	fields := make(map[string]Field, len(FieldNames))
	for _, name := range FieldNames {
		fields[name] = Field{Value: nil, Status: "missing"}
	}
	fields["benefit_type"] = Field{Value: "invented", Status: "explicit", Evidence: []Evidence{{Source: "page 1"}}}
	raw, _ := json.Marshal(Result{Fields: fields})
	if _, err := ValidateMockResponse(raw); err == nil || !strings.Contains(err.Error(), "benefit_type") {
		t.Fatalf("expected enum validation error, got %v", err)
	}
	fields["benefit_type"] = Field{Value: "cashable", Status: "explicit"}
	raw, _ = json.Marshal(Result{Fields: fields})
	if _, err := ValidateMockResponse(raw); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("expected evidence validation error, got %v", err)
	}
}

func TestValidateMockResponseAcceptsValidatedElevenFieldEnvelope(t *testing.T) {
	values := map[string]any{
		"id": "P-1", "task": "Invoice automation", "setup_cost_inr": float64(1000),
		"annual_benefit_inr": float64(2000), "annual_running_cost_inr": float64(100),
		"benefit_type": "cashable", "overlap_group": "finance", "adoption_pct": float64(75),
		"readiness": "medium", "ramp_months": float64(3), "delay_months": float64(1),
	}
	fields := make(map[string]Field, len(FieldNames))
	for _, name := range FieldNames {
		fields[name] = Field{Value: values[name], Status: "explicit", Evidence: []Evidence{{Source: "page 1", Excerpt: name, Original: name}}}
	}
	raw, _ := json.Marshal(Result{Fields: fields})
	if _, err := ValidateMockResponse(raw); err != nil {
		t.Fatalf("valid response was rejected: %v", err)
	}
}

func TestValidateForConfirmationAcceptsReviewedOptionalNulls(t *testing.T) {
	fields := make(map[string]Field, len(FieldNames))
	for _, name := range FieldNames {
		fields[name] = Field{Value: nil, Status: "explicit"}
	}
	set := func(name string, value any) {
		field := fields[name]
		field.Value = value
		fields[name] = field
	}
	set("id", "P-1")
	set("task", "Invoice automation")
	set("setup_cost_inr", float64(1000))
	set("annual_benefit_inr", float64(2000))
	if err := ValidateForConfirmation(fields); err != nil {
		t.Fatalf("optional nulls should be valid after explicit review: %v", err)
	}
	set("adoption_pct", float64(101))
	if err := ValidateForConfirmation(fields); err == nil || !strings.Contains(err.Error(), "adoption_pct") {
		t.Fatalf("expected percentage range error, got %v", err)
	}
}
