package cache

import (
	"context"
	"testing"
	"time"

	"c23desk/internal/model"
)

func TestKeyIgnoresFieldOrderAndWhitespace(t *testing.T) {
	c := withVersion("1")
	a, _ := c.key("/portfolio", []byte(`{"budget":600000,"mode":"independent"}`))
	b, _ := c.key("/portfolio", []byte(`{ "mode" : "independent",  "budget" : 600000 }`))
	if a != b {
		t.Fatalf("same content must give the same key:\n%s\n%s", a, b)
	}
}

func TestKeyChangesWithContentEndpointAndVersion(t *testing.T) {
	c1, c2 := withVersion("1"), withVersion("2")
	base, _ := c1.key("/portfolio", []byte(`{"budget":600000}`))
	for name, other := range map[string]func() (string, error){
		"budget":   func() (string, error) { return c1.key("/portfolio", []byte(`{"budget":100000}`)) },
		"endpoint": func() (string, error) { return c1.key("/pilots", []byte(`{"budget":600000}`)) },
		"version":  func() (string, error) { return c2.key("/portfolio", []byte(`{"budget":600000}`)) },
	} {
		k, err := other()
		if err != nil || k == base {
			t.Errorf("%s change must change the key (err=%v)", name, err)
		}
	}
}

func TestKeyRejectsInvalidJSON(t *testing.T) {
	if _, err := withVersion("1").key("/x", []byte(`{bad`)); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestDisabledCacheCallsThrough(t *testing.T) {
	c, err := New("", time.Minute, "1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled() {
		t.Fatal("cache without a URL must be disabled")
	}
	calls := 0
	for i := 0; i < 2; i++ {
		r, hit, err := c.Do(context.Background(), "/x", []byte(`{}`), func(context.Context) (*model.Upstream, error) {
			calls++
			return &model.Upstream{Status: 200, Body: []byte(`{}`)}, nil
		})
		if err != nil || hit || r.Status != 200 {
			t.Fatalf("unexpected result: %+v hit=%v err=%v", r, hit, err)
		}
	}
	if calls != 2 {
		t.Fatalf("a disabled cache must call through every time, got %d calls", calls)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal("ping on a disabled cache must not fail")
	}
}

func withVersion(v string) *Cache {
	c := &Cache{}
	c.version.Store(v)
	return c
}

func TestSetVersionChangesTheKeyNamespace(t *testing.T) {
	c := withVersion("fallback")
	before, _ := c.key("/pilots", []byte(`{}`))
	c.SetVersion("7")
	after, _ := c.key("/pilots", []byte(`{}`))
	if before == after || c.Version() != "7" {
		t.Fatalf("version switch did not change the key: %s vs %s", before, after)
	}
	c.SetVersion("") // an engine that reports nothing keeps the last known version
	if c.Version() != "7" {
		t.Fatalf("empty version must be ignored, got %q", c.Version())
	}
}
