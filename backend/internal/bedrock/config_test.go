package bedrock

import (
	"strings"
	"testing"
)

func TestDisabledBedrockIsExplicit(t *testing.T) {
	t.Setenv("BEDROCK_ENABLED", "")
	t.Setenv("BEDROCK_REGION", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("BEDROCK_MODEL_ID", "")
	cfg, err := LoadFromEnv()
	if err != nil || cfg.Enabled {
		t.Fatalf("expected disabled config, got %+v, %v", cfg, err)
	}
	if err := RequireConfigured(cfg); err == nil || !strings.Contains(err.Error(), "BEDROCK_ENABLED=true") {
		t.Fatalf("expected actionable disabled error, got %v", err)
	}
}

func TestEnabledBedrockRequiresRegionAndModel(t *testing.T) {
	t.Setenv("BEDROCK_ENABLED", "true")
	t.Setenv("AWS_REGION", "")
	t.Setenv("BEDROCK_REGION", "")
	t.Setenv("BEDROCK_MODEL_ID", "")
	if _, err := LoadFromEnv(); err == nil || !strings.Contains(err.Error(), "BEDROCK_MODEL_ID") {
		t.Fatalf("expected missing model configuration error, got %v", err)
	}
}

func TestEnabledBedrockUsesExplicitSettings(t *testing.T) {
	t.Setenv("BEDROCK_ENABLED", "true")
	t.Setenv("BEDROCK_REGION", "example-region")
	t.Setenv("AWS_REGION", "")
	t.Setenv("BEDROCK_MODEL_ID", "model-enabled-in-account")
	cfg, err := LoadFromEnv()
	if err != nil || cfg.Region != "example-region" || cfg.ModelID != "model-enabled-in-account" {
		t.Fatalf("unexpected config: %+v, %v", cfg, err)
	}
}
