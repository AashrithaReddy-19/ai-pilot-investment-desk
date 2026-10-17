package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndEnv(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("AI_SERVICE_URL", "http://ai:8001/")
	t.Setenv("AI_TIMEOUT_MS", "1200")
	t.Setenv("CORS_ORIGINS", "http://a.example, *, ,http://b.example")
	t.Setenv("AUTO_MIGRATE", "false")
	t.Setenv("MAX_BODY_BYTES", "-5")
	c := Load()
	if c.Port != "9000" || c.AIServiceURL != "http://ai:8001" || c.AITimeout != 1200*time.Millisecond || c.AutoMigrate ||
		c.MaxBodyBytes != 1<<20 || strings.Join(c.CORSOrigins, ",") != "http://a.example,http://b.example" {
		t.Fatalf("%+v", c)
	}
}

func TestRedactionNeverLeaksSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:SuperSecret@db:5432/c23")
	t.Setenv("REDIS_URL", "redis://:RedisPass@cache:6379/0")
	t.Setenv("API_KEY", "key-123")
	r := Load().Redacted()
	s := strings.Join([]string{r["database_url"].(string), r["redis_url"].(string)}, " ")
	if strings.Contains(s, "SuperSecret") || strings.Contains(s, "RedisPass") || !strings.Contains(s, "REDACTED") {
		t.Fatal(s)
	}
	for _, v := range r {
		if str, ok := v.(string); ok && strings.Contains(str, "key-123") {
			t.Fatal("api key leaked")
		}
	}
	if r["api_key_set"] != true || RedactURL("") != "" || RedactURL("http://x/") != "http://x/" || RedactURL("::") != "(unparseable)" {
		t.Fatal("redact edge cases")
	}
}
