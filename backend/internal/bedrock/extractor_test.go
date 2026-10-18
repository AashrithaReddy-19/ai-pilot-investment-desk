package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"

	"c23desk/internal/proposalextract"
)

type converseFunc func(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)

func (f converseFunc) Converse(ctx context.Context, in *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return f(ctx, in, opts...)
}

func modelResponse(t *testing.T, mutate func(map[string]proposalextract.Field)) *bedrockruntime.ConverseOutput {
	t.Helper()
	fields := map[string]proposalextract.Field{}
	for _, name := range proposalextract.FieldNames {
		var value any = "unknown"
		status := "explicit"
		if name == "readiness" {
			value = "medium"
		}
		if name == "benefit_type" {
			value = "non-cashable"
		}
		if name == "ramp_months" || name == "delay_months" {
			value = nil
			status = "missing"
		}
		fields[name] = proposalextract.Field{
			Value: value, Status: status,
			Evidence: []proposalextract.Evidence{{Source: "page 1", Excerpt: name, Original: name}},
		}
	}
	fields["setup_cost_inr"] = proposalextract.Field{Value: float64(100), Status: "explicit", Evidence: []proposalextract.Evidence{{Source: "page 1", Excerpt: "INR 100", Original: "INR 100"}}}
	fields["annual_benefit_inr"] = proposalextract.Field{Value: nil, Status: "missing", Explanation: "No annual cash benefit is stated."}
	fields["annual_running_cost_inr"] = proposalextract.Field{Value: nil, Status: "missing"}
	fields["adoption_pct"] = proposalextract.Field{Value: float64(50), Status: "explicit", Evidence: []proposalextract.Evidence{{Source: "page 1", Excerpt: "50%", Original: "50%"}}}
	mutate(fields)
	raw, err := json.Marshal(proposalextract.Result{Fields: fields})
	if err != nil {
		t.Fatal(err)
	}
	return &bedrockruntime.ConverseOutput{Output: &types.ConverseOutputMemberMessage{
		Value: types.Message{Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: string(raw)}}},
	}}
}

func TestExtractorValidatesStructuredResponseAndPreservesEvidence(t *testing.T) {
	var prompt string
	extractor := newExtractor(converseFunc(func(ctx context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
		prompt = in.Messages[0].Content[0].(*types.ContentBlockMemberText).Value
		return modelResponse(t, func(fields map[string]proposalextract.Field) {
			fields["task"] = proposalextract.Field{Value: "Invoice automation", Status: "explicit", Evidence: []proposalextract.Evidence{{Source: "row 1", Excerpt: "task: Invoice automation"}}}
		}), nil
	}), "test-model")
	result, err := extractor.Extract(context.Background(), "proposal.csv", []byte("task: Invoice automation\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Fields["task"].Evidence[0].Source != "row 1" || !strings.Contains(prompt, "task: Invoice automation") {
		t.Fatalf("source evidence was not preserved in request/result")
	}
}

func TestExtractorRejectsInvalidJSONAndEnum(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
		want     string
	}{
		{"json", "not json", "invalid extraction response JSON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			extractor := newExtractor(converseFunc(func(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return &bedrockruntime.ConverseOutput{Output: &types.ConverseOutputMemberMessage{Value: types.Message{Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: test.response}}}}}, nil
			}), "test-model")
			_, err := extractor.Extract(context.Background(), "proposal.csv", []byte("task: x"))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
	extractor := newExtractor(converseFunc(func(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
		return modelResponse(t, func(fields map[string]proposalextract.Field) {
			fields["benefit_type"] = proposalextract.Field{Value: "guaranteed savings", Status: "explicit", Evidence: []proposalextract.Evidence{{Source: "page 1", Excerpt: "guaranteed savings"}}}
		}), nil
	}), "test-model")
	_, err := extractor.Extract(context.Background(), "proposal.csv", []byte("task: x"))
	if err == nil || !strings.Contains(err.Error(), "benefit_type") {
		t.Fatalf("expected enum validation error, got %v", err)
	}
	extractor = newExtractor(converseFunc(func(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
		return modelResponse(t, func(fields map[string]proposalextract.Field) {
			fields["annual_benefit_inr"] = proposalextract.Field{
				Value: 1000.0, Status: "derived", Explanation: "calculated from time saved",
				Evidence: []proposalextract.Evidence{{Source: "page 1", Excerpt: "time saved"}},
			}
		}), nil
	}), "test-model")
	_, err = extractor.Extract(context.Background(), "proposal.csv", []byte("task: x"))
	if err == nil || !strings.Contains(err.Error(), "cannot be derived") {
		t.Fatalf("expected unsupported financial derivation error, got %v", err)
	}
}

func TestExtractorClassifiesTimeoutThrottleAndAccessDenied(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"ThrottlingException", "throttling"},
		{"AccessDeniedException", "access was denied"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			extractor := newExtractor(converseFunc(func(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
				return nil, &smithy.GenericAPIError{Code: test.code, Message: "test"}
			}), "test-model")
			_, err := extractor.Extract(context.Background(), "proposal.csv", []byte("task: x"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
	extractor := newExtractor(converseFunc(func(ctx context.Context, _ *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), "test-model")
	extractor.timeout = 1
	_, err := extractor.Extract(context.Background(), "proposal.csv", []byte("task: x"))
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestDisabledConfigurationDoesNotCreateExtractor(t *testing.T) {
	_, err := NewExtractor(context.Background(), Config{})
	if err == nil || !strings.Contains(err.Error(), "BEDROCK_ENABLED=true") {
		t.Fatalf("expected actionable disabled error, got %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("disabled configuration must not be a context error")
	}
}
