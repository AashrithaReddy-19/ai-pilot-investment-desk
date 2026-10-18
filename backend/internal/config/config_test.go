package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is missing")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "")
	t.Setenv("PORT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || cfg.RulesEngineURL != "http://localhost:8000" || cfg.RequestTimeout.Seconds() != 5 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadReportsAllProblems(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REQUEST_TIMEOUT_SECONDS", "abc")
	t.Setenv("CACHE_TTL_SECONDS", "-5")
	_, err := Load()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"DATABASE_URL", "REQUEST_TIMEOUT_SECONDS", "CACHE_TTL_SECONDS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

func TestSecurityAndWorkerDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	for _, k := range []string{"AUTH_MODE", "RATE_LIMIT_PER_MINUTE", "RATE_LIMIT_BURST", "UPLOAD_MAX_BYTES", "WORKER_ENABLED", "WORKER_POLL_MS", "RULES_VERSION"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMode != "off" || cfg.RateLimitPerMin != 600 || cfg.RateLimitBurst != 120 || cfg.UploadMaxBytes != 2_000_000 ||
		!cfg.WorkerEnabled || cfg.WorkerPoll.Milliseconds() != 1000 || cfg.RulesVersion != "fallback" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestSecuritySettingsAreValidated(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("AUTH_MODE", "maybe")
	t.Setenv("UPLOAD_MAX_BYTES", "5000000")
	t.Setenv("WORKER_ENABLED", "sometimes")
	_, err := Load()
	for _, want := range []string{"AUTH_MODE", "UPLOAD_MAX_BYTES", "WORKER_ENABLED"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
	t.Setenv("AUTH_MODE", "Required")
	t.Setenv("UPLOAD_MAX_BYTES", "")
	t.Setenv("WORKER_ENABLED", "false")
	cfg, err := Load()
	if err != nil || cfg.AuthMode != "required" || cfg.WorkerEnabled {
		t.Fatalf("valid settings: %+v %v", cfg, err)
	}
}
