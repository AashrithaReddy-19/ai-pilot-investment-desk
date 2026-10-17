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
	RulesVersion   string // part of every cache key
	AllowedOrigins []string
	RequestTimeout time.Duration
	CacheTTL       time.Duration // 0 = cache disabled
	MaxBodyBytes   int64
	AutoMigrate    bool
}

// Load reads and validates the environment. All problems are reported together.
func Load() (Config, error) {
	cfg := Config{
		Port:           env("PORT", "8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		RedisURL:       os.Getenv("REDIS_URL"),
		RulesEngineURL: strings.TrimRight(env("RULES_ENGINE_URL", "http://localhost:8000"), "/"),
		RulesVersion:   env("RULES_VERSION", "1"),
		AllowedOrigins: splitCSV(env("ALLOWED_ORIGINS", "http://localhost:5173")),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
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
