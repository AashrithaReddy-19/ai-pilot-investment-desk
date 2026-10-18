package bedrock

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Enabled bool
	Region  string
	ModelID string
}

// LoadFromEnv validates opt-in Bedrock settings. No AWS call is made here.
func LoadFromEnv() (Config, error) {
	enabled := false
	if raw := strings.TrimSpace(os.Getenv("BEDROCK_ENABLED")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("BEDROCK_ENABLED must be true or false, got %q", raw)
		}
		enabled = parsed
	}
	cfg := Config{
		Enabled: enabled,
		Region:  firstNonEmpty(os.Getenv("BEDROCK_REGION"), os.Getenv("AWS_REGION")),
		ModelID: strings.TrimSpace(os.Getenv("BEDROCK_MODEL_ID")),
	}
	if !enabled {
		return cfg, nil
	}
	var missing []string
	if cfg.Region == "" {
		missing = append(missing, "BEDROCK_REGION or AWS_REGION")
	}
	if cfg.ModelID == "" {
		missing = append(missing, "BEDROCK_MODEL_ID")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("Bedrock is enabled but not configured: set %s; choose a model ID enabled for your AWS account and region", strings.Join(missing, " and "))
	}
	return cfg, nil
}

func RequireConfigured(cfg Config) error {
	if !cfg.Enabled {
		return errors.New("Bedrock extraction is disabled; set BEDROCK_ENABLED=true, BEDROCK_REGION (or AWS_REGION), and BEDROCK_MODEL_ID to enable it")
	}
	if cfg.Region == "" || cfg.ModelID == "" {
		return errors.New("Bedrock extraction is not configured: set BEDROCK_REGION (or AWS_REGION) and BEDROCK_MODEL_ID")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
