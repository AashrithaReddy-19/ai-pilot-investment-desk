package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go"

	"c23desk/internal/proposalextract"
)

const (
	maxModelResponseBytes = 64 * 1024
	maxModelInputBytes    = 120 * 1024
)

type ConverseClient interface {
	Converse(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type Extractor struct {
	client  ConverseClient
	modelID string
	timeout time.Duration
}

func NewExtractor(ctx context.Context, cfg Config) (*Extractor, error) {
	if err := RequireConfigured(cfg); err != nil {
		return nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(options *retry.StandardOptions) {
				options.MaxAttempts = 3
			})
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration for Bedrock: %w", err)
	}
	return &Extractor{
		client:  bedrockruntime.NewFromConfig(awsCfg),
		modelID: cfg.ModelID,
		timeout: 45 * time.Second,
	}, nil
}

func newExtractor(client ConverseClient, modelID string) *Extractor {
	return &Extractor{client: client, modelID: modelID, timeout: 45 * time.Second}
}

func (e *Extractor) Extract(ctx context.Context, filename string, content []byte) (proposalextract.Result, error) {
	if e == nil || e.client == nil || strings.TrimSpace(e.modelID) == "" {
		return proposalextract.Result{}, errors.New("Bedrock extraction is not configured")
	}
	sources, err := proposalextract.SourceText(ctx, filename, content)
	if err != nil {
		return proposalextract.Result{}, fmt.Errorf("prepare document text for Bedrock: %w", err)
	}
	sourceJSON, err := json.Marshal(sources)
	if err != nil {
		return proposalextract.Result{}, fmt.Errorf("encode document sources: %w", err)
	}
	if len(sourceJSON) > maxModelInputBytes {
		return proposalextract.Result{}, fmt.Errorf("document text exceeds the Bedrock prompt limit of %d bytes; use deterministic extraction or a smaller document", maxModelInputBytes)
	}
	prompt := extractionPrompt(string(sourceJSON))
	callCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	maxTokens := int32(4096)
	out, err := e.client.Converse(callCtx, &bedrockruntime.ConverseInput{
		ModelId: &e.modelID,
		Messages: []types.Message{{
			Role:    types.ConversationRoleUser,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: prompt}},
		}},
		InferenceConfig: &types.InferenceConfiguration{MaxTokens: &maxTokens, Temperature: float32Ptr(0)},
	})
	if err != nil {
		return proposalextract.Result{}, classifyError(err)
	}
	raw, err := responseText(out)
	if err != nil {
		return proposalextract.Result{}, err
	}
	if len(raw) > maxModelResponseBytes {
		return proposalextract.Result{}, fmt.Errorf("Bedrock extraction response exceeds %d bytes", maxModelResponseBytes)
	}
	result, err := proposalextract.ValidateMockResponse([]byte(raw))
	if err != nil {
		return proposalextract.Result{}, fmt.Errorf("Bedrock returned an invalid extraction response: %w", err)
	}
	return result, nil
}

func extractionPrompt(sourceJSON string) string {
	return `Extract proposal data from the supplied document sources. Return ONLY one JSON object matching this exact shape:
{"fields":{"id":{"value":null,"status":"missing","explanation":"","evidence":[]},"task":{"value":null,"status":"missing","explanation":"","evidence":[]},"setup_cost_inr":{"value":null,"status":"missing","explanation":"","evidence":[]},"annual_benefit_inr":{"value":null,"status":"missing","explanation":"","evidence":[]},"annual_running_cost_inr":{"value":null,"status":"missing","explanation":"","evidence":[]},"benefit_type":{"value":null,"status":"missing","explanation":"","evidence":[]},"overlap_group":{"value":null,"status":"missing","explanation":"","evidence":[]},"adoption_pct":{"value":null,"status":"missing","explanation":"","evidence":[]},"readiness":{"value":null,"status":"missing","explanation":"","evidence":[]},"ramp_months":{"value":null,"status":"missing","explanation":"","evidence":[]},"delay_months":{"value":null,"status":"missing","explanation":"","evidence":[]}},"warnings":[]}
Use only these statuses: explicit, derived, missing, ambiguous. Explicit and derived values require evidence entries copied from the supplied source references, with source and excerpt. Missing and ambiguous values MUST have value null. Never calculate annual benefits from time saved, coverage, or productivity claims. Never convert productivity into cash savings. Never guess unsupported values, enums, readiness, ramp, delay, or overlap. Use null and missing/ambiguous when evidence is insufficient. Accepted benefit_type values are cashable, cashable assumption, time freed, non-cashable, unknown. Accepted readiness values are low, medium, high. Amounts must be numeric INR values without currency symbols. Preserve the document's stated period and units in evidence fields. The output must contain exactly the 11 fields and no additional fields.

Document sources:
` + sourceJSON
}

func responseText(out *bedrockruntime.ConverseOutput) (string, error) {
	if out == nil || out.Output == nil {
		return "", errors.New("Bedrock returned no response content")
	}
	message, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", errors.New("Bedrock returned an unsupported response type")
	}
	var text strings.Builder
	for _, block := range message.Value.Content {
		if item, ok := block.(*types.ContentBlockMemberText); ok {
			text.WriteString(item.Value)
		}
	}
	raw := strings.TrimSpace(text.String())
	if raw == "" {
		return "", errors.New("Bedrock returned an empty response")
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "```json"))
		raw = strings.TrimSpace(strings.TrimSuffix(raw, "```"))
	}
	return raw, nil
}

func classifyError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("Bedrock extraction timed out; retry the review or check model latency and network access")
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "AccessDeniedException", "UnauthorizedException":
			return errors.New("Bedrock access was denied; grant bedrock:InvokeModel for the configured model")
		case "ThrottlingException", "TooManyRequestsException":
			return errors.New("Bedrock is throttling requests; retry after a short delay")
		case "ResourceNotFoundException", "ValidationException":
			return fmt.Errorf("Bedrock model configuration is invalid or unsupported: %s", apiErr.ErrorMessage())
		}
		return fmt.Errorf("Bedrock request failed: %s", apiErr.ErrorMessage())
	}
	return fmt.Errorf("Bedrock request failed: %w", err)
}

func float32Ptr(v float32) *float32 { return &v }
