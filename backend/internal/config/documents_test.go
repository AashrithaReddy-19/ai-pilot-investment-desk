package config

import (
	"strings"
	"testing"
)

func TestDocumentLimitsHaveSafeDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	for _, k := range []string{"DOCUMENT_MAX_BYTES", "DOCUMENT_MAX_PAGES", "DOCUMENT_TIMEOUT_SECONDS", "DOCUMENT_CONCURRENCY"} {
		t.Setenv(k, "")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DocumentMaxBytes != 25<<20 || cfg.DocumentMaxPages != 500 || cfg.DocumentTimeout.Seconds() != 60 || cfg.DocumentConcurrency != 2 {
		t.Fatalf("defaults: %+v", cfg)
	}
	// the ordinary body limit is untouched by the document limit
	if cfg.MaxBodyBytes != 1<<20 {
		t.Fatalf("MAX_BODY_BYTES default changed: %d", cfg.MaxBodyBytes)
	}
}

func TestDocumentLimitsAreBounded(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DOCUMENT_MAX_BYTES", "999999999") // above the 64 MiB ceiling
	t.Setenv("DOCUMENT_MAX_PAGES", "0")
	t.Setenv("DOCUMENT_TIMEOUT_SECONDS", "2")
	t.Setenv("DOCUMENT_CONCURRENCY", "abc")
	_, err := Load()
	if err == nil {
		t.Fatal("out-of-range document limits must be rejected")
	}
	for _, k := range []string{"DOCUMENT_MAX_BYTES", "DOCUMENT_MAX_PAGES", "DOCUMENT_TIMEOUT_SECONDS", "DOCUMENT_CONCURRENCY"} {
		if !strings.Contains(err.Error(), k) {
			t.Errorf("the error should name %s: %v", k, err)
		}
	}
}
