package cache

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRedisHitMissInvalidateAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	r, err := NewRedis("redis://"+mr.Addr(), quiet)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	k := PilotEvaluationKey("AI1", Fingerprint("v1"))
	if _, ok := r.Get(ctx, k); ok {
		t.Fatal("expected miss")
	}
	r.Set(ctx, k, []byte(`{"x":1}`), TTLEvaluation)
	if b, ok := r.Get(ctx, k); !ok || string(b) != `{"x":1}` {
		t.Fatal("expected hit")
	}
	if mr.TTL(k) != TTLEvaluation {
		t.Fatal("ttl not set")
	}
	r.Set(ctx, PortfolioSummaryKey("p1", "a"), []byte("1"), TTLPortfolio)
	r.InvalidatePrefix(ctx, PilotPrefix("AI1"))
	if _, ok := r.Get(ctx, k); ok {
		t.Fatal("invalidated key still served")
	}
	if _, ok := r.Get(ctx, PortfolioSummaryKey("p1", "a")); !ok {
		t.Fatal("unrelated key must survive")
	}
	mr.FastForward(TTLPortfolio + time.Second)
	if _, ok := r.Get(ctx, PortfolioSummaryKey("p1", "a")); ok {
		t.Fatal("expired key served")
	}
	if r.Status(ctx) != "ok" {
		t.Fatal("status")
	}
}

func TestRedisDownIsAMissNotAnError(t *testing.T) {
	mr := miniredis.RunT(t)
	r, _ := NewRedis("redis://"+mr.Addr(), quiet)
	ctx := context.Background()
	r.Set(ctx, "k", []byte("v"), time.Minute)
	mr.Close() // Redis dies
	start := time.Now()
	if _, ok := r.Get(ctx, "k"); ok {
		t.Fatal("must miss when redis is down")
	}
	r.Set(ctx, "k2", []byte("v"), time.Minute)
	r.InvalidatePrefix(ctx, "k")
	if time.Since(start) > 2*time.Second {
		t.Fatal("redis failure must fail fast")
	}
	if r.Status(ctx) != "degraded" || r.failures.Load() != 1 {
		t.Fatalf("one failure opens the breaker; later calls skip Redis (failures=%d)", r.failures.Load())
	}
	// 100 more calls while open cost nothing
	start = time.Now()
	for i := 0; i < 100; i++ {
		r.Get(ctx, "k")
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("open breaker must short-circuit")
	}
}

func TestBreakerClosesWhenRedisRecovers(t *testing.T) {
	mr := miniredis.RunT(t)
	r, _ := NewRedis("redis://"+mr.Addr(), quiet)
	ctx := context.Background()
	r.openUntil.Store(time.Now().Add(time.Hour).UnixNano())
	if _, ok := r.Get(ctx, "k"); ok {
		t.Fatal("open breaker = miss")
	}
	if r.Status(ctx) != "ok" || r.open() {
		t.Fatal("successful ping closes the breaker")
	}
	r.Set(ctx, "k", []byte("v"), time.Minute)
	if _, ok := r.Get(ctx, "k"); !ok {
		t.Fatal("cache works again")
	}
}

func TestFingerprintStableAndSensitive(t *testing.T) {
	a := Fingerprint(map[string]int{"a": 1, "b": 2}, "x")
	if a != Fingerprint(map[string]int{"b": 2, "a": 1}, "x") || a == Fingerprint(map[string]int{"a": 1, "b": 3}, "x") {
		t.Fatal("fingerprint")
	}
	if _, err := NewRedis("::bad::", quiet); err == nil {
		t.Fatal("bad url")
	}
	var n Noop
	n.Set(context.Background(), "k", nil, 0)
	if _, ok := n.Get(context.Background(), "k"); ok || n.Status(context.Background()) != "disabled" {
		t.Fatal("noop")
	}
	n.InvalidatePrefix(context.Background(), "k")
}
