// Package config reads configuration from environment variables. No secrets are hard-coded.
package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds runtime settings.
type Config struct {
	Port         string
	DatabaseURL  string
	RedisURL     string // empty = cache disabled
	AIServiceURL string // empty = AI disabled
	AITimeout    time.Duration
	CORSOrigins  []string
	RawDataDir   string
	MaxBodyBytes int64
	AutoMigrate  bool
	SeedOfficial bool
	APIKey       string // optional; when set, mutating requests need X-API-Key
	LogLevel     string
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(k), 10, 64); err == nil && v > 0 {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

// Load reads the environment.
func Load() Config {
	var origins []string
	for _, o := range strings.Split(env("CORS_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" && o != "*" { // wildcard deliberately not supported
			origins = append(origins, o)
		}
	}
	return Config{
		Port:         env("PORT", "8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://c23:c23@localhost:5432/c23?sslmode=disable"),
		RedisURL:     env("REDIS_URL", ""),
		AIServiceURL: strings.TrimRight(env("AI_SERVICE_URL", ""), "/"),
		AITimeout:    time.Duration(envInt("AI_TIMEOUT_MS", 1500)) * time.Millisecond,
		CORSOrigins:  origins,
		RawDataDir:   env("RAW_DATA_DIR", "../decision-engine/data/raw"),
		MaxBodyBytes: envInt("MAX_BODY_BYTES", 1<<20),
		AutoMigrate:  envBool("AUTO_MIGRATE", true),
		SeedOfficial: envBool("SEED_OFFICIAL", false),
		APIKey:       env("API_KEY", ""),
		LogLevel:     env("LOG_LEVEL", "info"),
	}
}

// Redacted returns a log-safe view (passwords and keys removed).
func (c Config) Redacted() map[string]any {
	return map[string]any{"port": c.Port, "database_url": RedactURL(c.DatabaseURL), "redis_url": RedactURL(c.RedisURL),
		"ai_service_url": c.AIServiceURL, "ai_timeout": c.AITimeout.String(), "cors_origins": c.CORSOrigins, "raw_data_dir": c.RawDataDir,
		"max_body_bytes": c.MaxBodyBytes, "auto_migrate": c.AutoMigrate, "seed_official": c.SeedOfficial, "api_key_set": c.APIKey != ""}
}

// RedactURL hides the password in a connection URL.
func RedactURL(s string) string {
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil {
		return "(unparseable)"
	}
	if u.User != nil {
		if _, ok := u.User.Password(); ok {
			u.User = url.UserPassword(u.User.Username(), "REDACTED")
		}
	}
	return u.String()
}
