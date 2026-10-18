package proposalextract

import (
	"context"
	"strings"
	"testing"
)

func TestExtractPreservesEvidenceAndMissingState(t *testing.T) {
	result, err := Extract(context.Background(), "proposal.csv", []byte("task: Invoice automation\nsetup cost: INR 10,000\nannual benefit: ₹25,000\nadoption: 75%\nreadiness: medium\nramp: 3 months\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields["setup_cost_inr"].Value != 10000.0 {
		t.Fatalf("unexpected setup value: %#v", result.Fields["setup_cost_inr"])
	}
	if result.Fields["setup_cost_inr"].Evidence[0].Source != "row 2" {
		t.Fatalf("missing source reference: %#v", result.Fields["setup_cost_inr"].Evidence)
	}
	if result.Fields["delay_months"].Status != "missing" {
		t.Fatalf("missing values must remain explicit: %#v", result.Fields["delay_months"])
	}
	if !strings.Contains(result.Fields["annual_benefit_inr"].Evidence[0].Excerpt, "annual benefit") {
		t.Fatal("evidence must retain the source excerpt")
	}
}

func TestExtractMapsAllElevenFields(t *testing.T) {
	result, err := Extract(context.Background(), "proposal.csv", []byte(strings.Join([]string{
		"id: P-1",
		"task: Invoice automation",
		"setup cost: INR 10,000",
		"annual benefit: INR 25,000",
		"annual running cost: INR 2,000",
		"benefit type: cashable assumption",
		"overlap group: finance-ops",
		"adoption: 75%",
		"readiness: medium",
		"ramp: 3 months",
		"delay: 1 month",
	}, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range FieldNames {
		if result.Fields[name].Status != "explicit" {
			t.Fatalf("%s was not extracted: %#v", name, result.Fields[name])
		}
	}
	if result.Fields["annual_running_cost_inr"].Value != float64(2000) ||
		result.Fields["benefit_type"].Value != "cashable assumption" ||
		result.Fields["overlap_group"].Value != "finance-ops" {
		t.Fatalf("incorrect field mapping: %#v", result.Fields)
	}
}

func TestExtractRejectsUnreadableDocument(t *testing.T) {
	if _, err := Extract(context.Background(), "proposal.csv", []byte("")); err == nil {
		t.Fatal("empty documents must not produce a successful extraction")
	}
}
