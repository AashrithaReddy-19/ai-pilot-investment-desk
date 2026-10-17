package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/alicebob/miniredis/v2"
	embeddedpostgres "github.com/fergusstrange/embedded-postgres"

	"c23desk/internal/aiclient"
	"c23desk/internal/cache"
	"c23desk/internal/config"
	"c23desk/internal/httpapi"
	"c23desk/internal/rules"
	"c23desk/internal/service"
	"c23desk/internal/store"
)

// runInProcess benchmarks 4 configurations against one embedded PostgreSQL, serving the real router over loopback TCP.
// Redis here is miniredis (Redis protocol, in-memory): it exercises the cache path but is NOT a Redis performance test.
func runInProcess(n int, aiURL, out string) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := uint32(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	dir, _ := os.MkdirTemp("", "c23bench")
	defer os.RemoveAll(dir)
	cacheDir, _ := os.UserCacheDir()
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(port).Database("c23").Username("c23").Password("bench").
		RuntimePath(filepath.Join(dir, "rt")).DataPath(filepath.Join(dir, "data")).CachePath(filepath.Join(cacheDir, "c23desk-embedded-postgres")).Logger(nil))
	if err := pg.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "postgres:", err)
		os.Exit(1)
	}
	defer func() { _ = pg.Stop() }()
	ctx := context.Background()
	st, err := store.Open(ctx, fmt.Sprintf("postgres://c23:bench@127.0.0.1:%d/c23?sslmode=disable", port))
	if err != nil {
		panic(err)
	}
	defer st.Close()
	if _, err := st.Migrate(ctx); err != nil {
		panic(err)
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	raw := os.Getenv("RAW_DATA_DIR")
	if raw == "" {
		raw = filepath.Join("..", "decision-engine", "data", "raw")
	}
	seed := &service.Service{Store: st, Cache: cache.Noop{}, AI: aiclient.New("", time.Second, quiet), Rules: rules.Default(), Log: quiet, RawDir: raw}
	if _, err := seed.ImportOfficial(ctx, store.Actor{Name: "bench"}); err != nil {
		panic(err)
	}
	configs := []struct {
		label string
		redis bool
		ai    string
	}{
		{"redis ON, AI online", true, aiURL}, {"redis OFF, AI online", false, aiURL},
		{"redis ON, AI unavailable", true, "http://127.0.0.1:1"}, {"redis OFF, AI unavailable", false, "http://127.0.0.1:1"},
	}
	var all []result
	for _, c := range configs {
		var ch cache.Cache = cache.Noop{}
		var mr *miniredis.Miniredis
		if c.redis {
			mr = miniredis.NewMiniRedis()
			_ = mr.Start()
			ch, _ = cache.NewRedis("redis://"+mr.Addr(), quiet)
		}
		svc := &service.Service{Store: st, Cache: ch, AI: aiclient.New(c.ai, 1500*time.Millisecond, quiet), Rules: rules.Default(), Log: quiet, RawDir: raw}
		srv := httptest.NewServer(httpapi.New(svc, config.Config{MaxBodyBytes: 1 << 20}, quiet))
		all = append(all, benchURL(srv.URL, n, c.label)...)
		srv.Close()
		if mr != nil {
			mr.Close()
		}
	}
	save(out, all)
}
