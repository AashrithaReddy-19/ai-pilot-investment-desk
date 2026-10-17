// Package cache is a fail-open Redis cache for deterministic rules-engine answers.
// If Redis is disabled, down or slow, calls simply go straight to the rules engine.
package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"

	"c23desk/internal/model"
)

const opTimeout = 150 * time.Millisecond

type Cache struct {
	rdb     *redis.Client // nil when disabled
	ttl     time.Duration
	version string
	sf      singleflight.Group
	log     *slog.Logger
}

// New returns a usable Cache even when disabled (empty URL or ttl <= 0).
func New(redisURL string, ttl time.Duration, version string, log *slog.Logger) (*Cache, error) {
	if log == nil {
		log = slog.Default()
	}
	c := &Cache{ttl: ttl, version: version, log: log}
	if redisURL == "" || ttl <= 0 {
		return c, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	opts.DialTimeout = 300 * time.Millisecond
	opts.ReadTimeout = opTimeout
	opts.WriteTimeout = opTimeout
	opts.MaxRetries = 1
	c.rdb = redis.NewClient(opts)
	return c, nil
}

func (c *Cache) Enabled() bool { return c != nil && c.rdb != nil }

func (c *Cache) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return c.rdb.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	if !c.Enabled() {
		return nil
	}
	return c.rdb.Close()
}

// Do returns a cached answer for (endpoint, body) or computes it once, even if many identical
// requests arrive together (singleflight). Only HTTP 200 answers are stored.
func (c *Cache) Do(ctx context.Context, endpoint string, body []byte,
	fn func(context.Context) (*model.Upstream, error)) (*model.Upstream, bool, error) {

	if !c.Enabled() {
		r, err := fn(ctx)
		return r, false, err
	}
	key, err := c.key(endpoint, body)
	if err != nil {
		r, err := fn(ctx)
		return r, false, err
	}
	if b, ok := c.get(ctx, key); ok {
		return &model.Upstream{Status: 200, Body: b, ContentType: "application/json"}, true, nil
	}
	v, err, _ := c.sf.Do(key, func() (any, error) {
		// WithoutCancel: one caller hanging up must not fail the callers sharing this flight.
		r, err := fn(context.WithoutCancel(ctx))
		if err == nil && r.Status == 200 {
			c.set(key, r.Body)
		}
		return r, err
	})
	if err != nil {
		return nil, false, err
	}
	return v.(*model.Upstream), false, nil
}

// key builds a stable key: the same JSON content in any field order gives the same key,
// and the rules version is part of it, so bumping RULES_VERSION retires every old answer.
func (c *Cache) key(endpoint string, body []byte) (string, error) {
	var canon []byte
	if len(bytes.TrimSpace(body)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return "", err
		}
		var err error
		if canon, err = json.Marshal(v); err != nil { // map keys are marshalled sorted
			return "", err
		}
	}
	sum := sha256.Sum256(canon)
	return fmt.Sprintf("c23:rules:v%s:%s:%x", c.version, endpoint, sum), nil
}

func (c *Cache) get(ctx context.Context, key string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			c.log.Warn("cache get failed, continuing without cache", "err", err)
		}
		return nil, false
	}
	return b, true
}

func (c *Cache) set(key string, val []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if err := c.rdb.Set(ctx, key, val, c.ttl).Err(); err != nil {
		c.log.Warn("cache set failed", "err", err)
	}
}
