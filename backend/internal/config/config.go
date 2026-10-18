// Package config loads all settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port           string
	DatabaseURL    string
	RedisURL       string // empty = cache disabled
	RulesEngineURL string
	RulesVersion   string // fallback cache-key version, used only while the engine has not reported its own
	AllowedOrigins []string
	RequestTimeout time.Duration
	CacheTTL       time.Duration // 0 = cache disabled
	MaxBodyBytes   int64
	AutoMigrate    bool

	AuthMode        string // off (single default workspace, no sign-in) | required (bearer tokens + workspaces)
	RateLimitPerMin int    // requests per minute per client IP; 0 disables
	RateLimitBurst  int    // bucket size: short bursts above the per-minute rate
	UploadMaxBytes  int64  // largest stored upload (the engine parses at most 2 MB)
	WorkerEnabled   bool   // run the upload processing worker in this process
	WorkerPoll      time.Duration
	VersionPoll     time.Duration // how often the engine's rules_version is re-read

	// Balance-sheet documents (PDF, XLSX, XLS) are read in the gateway and handed to the engine as CSV.
	DocumentMaxBytes    int64         // largest document accepted on /balance-sheet/parse and /confirm
	DocumentMaxPages    int           // most PDF pages read in one request
	DocumentTimeout     time.Duration // deadline for receiving, reading and parsing one document
	DocumentConcurrency int           // documents read at the same time; more get a 429
	AWSRegion           string
	S3DocumentsBucket   string
	PresignedURLExpiry  time.Duration
}

// Load reads and validates the environment. All problems are reported together.
func Load() (Config, error) {
	cfg := Config{
		Port:              env("PORT", "8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisURL:          os.Getenv("REDIS_URL"),
		RulesEngineURL:    strings.TrimRight(env("RULES_ENGINE_URL", "http://localhost:8000"), "/"),
		RulesVersion:      env("RULES_VERSION", "fallback"),
		AllowedOrigins:    splitCSV(env("ALLOWED_ORIGINS", "http://localhost:5173")),
		AuthMode:          strings.ToLower(env("AUTH_MODE", "off")),
		WorkerEnabled:     true,
		AWSRegion:         env("AWS_REGION", ""),
		S3DocumentsBucket: env("S3_DOCUMENTS_BUCKET", ""),
	}
	var errs []error
	if cfg.AuthMode != "off" && cfg.AuthMode != "required" {
		errs = append(errs, fmt.Errorf("AUTH_MODE must be off or required, got %q", cfg.AuthMode))
	}
	if n, err := intEnv("RATE_LIMIT_PER_MINUTE", 600, 0, 1_000_000); err != nil {
		errs = append(errs, err)
	} else {
		cfg.RateLimitPerMin = n
	}
	if n, err := intEnv("RATE_LIMIT_BURST", 120, 1, 1_000_000); err != nil {
		errs = append(errs, err)
	} else {
		cfg.RateLimitBurst = n
	}
	if n, err := intEnv("UPLOAD_MAX_BYTES", 2_000_000, 1024, 2_000_000); err != nil {
		errs = append(errs, err)
	} else {
		cfg.UploadMaxBytes = int64(n)
	}
	if n, err := intEnv("DOCUMENT_MAX_BYTES", 25<<20, 1<<20, 64<<20); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DocumentMaxBytes = int64(n)
	}
	if n, err := intEnv("DOCUMENT_MAX_PAGES", 500, 1, 2000); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DocumentMaxPages = n
	}
	if secs, err := intEnv("DOCUMENT_TIMEOUT_SECONDS", 60, 5, 600); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DocumentTimeout = time.Duration(secs) * time.Second
	}
	if n, err := intEnv("DOCUMENT_CONCURRENCY", 2, 1, 16); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DocumentConcurrency = n
	}
	if secs, err := intEnv("PRESIGNED_URL_EXPIRY_SECONDS", 600, 60, 3600); err != nil {
		errs = append(errs, err)
	} else {
		cfg.PresignedURLExpiry = time.Duration(secs) * time.Second
	}
	if n, err := intEnv("WORKER_POLL_MS", 1000, 100, 60_000); err != nil {
		errs = append(errs, err)
	} else {
		cfg.WorkerPoll = time.Duration(n) * time.Millisecond
	}
	if n, err := intEnv("RULES_VERSION_POLL_SECONDS", 30, 5, 3600); err != nil {
		errs = append(errs, err)
	} else {
		cfg.VersionPoll = time.Duration(n) * time.Second
	}
	if v := os.Getenv("WORKER_ENABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("WORKER_ENABLED must be true or false, got %q", v))
		}
		cfg.WorkerEnabled = b
	}
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if (cfg.AWSRegion == "") != (cfg.S3DocumentsBucket == "") {
		errs = append(errs, errors.New("AWS_REGION and S3_DOCUMENTS_BUCKET must be set together"))
	}
	if secs, err := intEnv("REQUEST_TIMEOUT_SECONDS", 5, 1, 120); err != nil {
		errs = append(errs, err)
	} else {
		cfg.RequestTimeout = time.Duration(secs) * time.Second
	}
	if secs, err := intEnv("CACHE_TTL_SECONDS", 1800, 0, 86400); err != nil {
		errs = append(errs, err)
	} else {
		cfg.CacheTTL = time.Duration(secs) * time.Second
	}
	if n, err := intEnv("MAX_BODY_BYTES", 1<<20, 1024, 64<<20); err != nil {
		errs = append(errs, err)
	} else {
		cfg.MaxBodyBytes = int64(n)
	}
	if v := os.Getenv("AUTO_MIGRATE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("AUTO_MIGRATE must be true or false, got %q", v))
		}
		cfg.AutoMigrate = b
	}
	return cfg, errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnv(key string, def, min, max int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d, got %q", key, min, max, v)
	}
	return n, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
