// Package cache is an OPTIONAL cache-aside layer. Redis is never a source of truth.
//
// Keys are content-addressed: they embed a fingerprint of every input (pilot version, peers, budget,
// rules version...). A changed input therefore produces a different key, so a stale result cannot be
// served even if an invalidation is lost. Explicit invalidation on writes is extra hygiene.
// Every Redis error is logged and treated as a cache miss: correctness never depends on Redis.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// TTLs (cache-aside, short-lived).
const (
	TTLEvaluation   = 5 * time.Minute
	TTLPortfolio    = 2 * time.Minute
	TTLOptimization = 2 * time.Minute
	TTLSemantic     = 5 * time.Minute
)

// Cache is the minimal interface the service needs.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration)
	InvalidatePrefix(ctx context.Context, prefix string)
	Status(ctx context.Context) string // "disabled" | "ok" | "degraded"
}

// Noop is used when REDIS_URL is empty.
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, bool)         { return nil, false }
func (Noop) Set(context.Context, string, []byte, time.Duration) {}
func (Noop) InvalidatePrefix(context.Context, string)           {}
func (Noop) Status(context.Context) string                      { return "disabled" }

// breakerOpenFor is how long Redis is bypassed after a failure (circuit breaker).
// Without it a dead Redis costs every request several dial attempts.
const breakerOpenFor = 5 * time.Second

// Redis implements Cache with tight timeouts and a circuit breaker so a slow/dead Redis cannot stall requests.
type Redis struct {
	c         *redis.Client
	log       *slog.Logger
	failures  atomic.Int64
	openUntil atomic.Int64 // unix nanos; while in the future, Redis is skipped entirely
}

type quietLogger struct{}

func (quietLogger) Printf(context.Context, string, ...interface{}) {}

func init() { redis.SetLogger(quietLogger{}) } // errors are reported through slog by warn()

func (r *Redis) open() bool { return time.Now().UnixNano() < r.openUntil.Load() }

// NewRedis builds a client. It does not fail if Redis is down.
func NewRedis(url string, log *slog.Logger) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout = 200*time.Millisecond, 200*time.Millisecond, 200*time.Millisecond
	opt.MaxRetries = 0
	opt.PoolSize = 20
	return &Redis{c: redis.NewClient(opt), log: log}, nil
}

func (r *Redis) warn(op, key string, err error) {
	r.failures.Add(1)
	r.openUntil.Store(time.Now().Add(breakerOpenFor).UnixNano())
	r.log.Warn("redis degraded; continuing without cache", "op", op, "key", key, "error", err.Error(), "bypass_for", breakerOpenFor.String())
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool) {
	if r.open() {
		return nil, false
	}
	b, err := r.c.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		r.warn("get", key, err)
		return nil, false
	}
	return b, true
}

func (r *Redis) Set(ctx context.Context, key string, val []byte, ttl time.Duration) {
	if r.open() {
		return
	}
	if err := r.c.Set(ctx, key, val, ttl).Err(); err != nil {
		r.warn("set", key, err)
	}
}

// InvalidatePrefix deletes keys with the prefix (SCAN, bounded; fine for this data size).
// While the breaker is open nothing can be deleted; content-addressed keys keep this safe (a changed input
// never maps to an old key) and TTLs bound leftover entries.
func (r *Redis) InvalidatePrefix(ctx context.Context, prefix string) {
	if r.open() {
		return
	}
	iter := r.c.Scan(ctx, 0, prefix+"*", 200).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		r.warn("scan", prefix, err)
		return
	}
	if len(keys) > 0 {
		if err := r.c.Del(ctx, keys...).Err(); err != nil {
			r.warn("del", prefix, err)
		}
	}
}

func (r *Redis) Status(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := r.c.Ping(ctx).Err(); err != nil {
		return "degraded"
	}
	r.openUntil.Store(0) // healthy again: close the breaker
	return "ok"
}

// Close releases the client.
func (r *Redis) Close() error { return r.c.Close() }

// Fingerprint hashes any JSON-serialisable inputs into a short, stable key component.
func Fingerprint(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// Key builders (documented key space).
func PilotEvaluationKey(id, fp string) string  { return "pilot:" + id + ":evaluation:v1:" + fp }
func PilotPrefix(id string) string             { return "pilot:" + id + ":" }
func PortfolioSummaryKey(id, fp string) string { return "portfolio:" + id + ":summary:v1:" + fp }
func PortfolioPrefix(id string) string         { return "portfolio:" + id + ":" }
func OptimizationKey(fp string) string         { return "optimization:v1:" + fp }
func SemanticKey(fp string) string             { return "semantic:" + fp + ":v1" }
