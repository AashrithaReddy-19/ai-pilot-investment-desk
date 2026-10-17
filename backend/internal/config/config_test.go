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
