// Command devstack runs the full backend locally WITHOUT Docker: an embedded PostgreSQL server (real
// binaries), an optional in-memory Redis-protocol server (miniredis; dev/benchmark only, not production
// Redis), migrations, the verified official fixture, and the Go API.
//
//	go run ./cmd/devstack                       # API :8080, PostgreSQL :55432, no Redis
//	go run ./cmd/devstack -redis                # + Redis-protocol cache on :56379
//	AI_SERVICE_URL=http://127.0.0.1:8001 go run ./cmd/devstack
//
// Production / CI use docker compose with real PostgreSQL and Redis instead.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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

func main() {
	pgPort := flag.Uint("pg-port", 55432, "embedded PostgreSQL port")
	withRedis := flag.Bool("redis", false, "start an in-memory Redis-protocol cache (miniredis)")
	redisPort := flag.Int("redis-port", 56379, "cache port")
	fresh := flag.Bool("fresh", false, "wipe the dev database first")
	flag.Parse()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	cacheDir, _ := os.UserCacheDir()
	base := filepath.Join(cacheDir, "c23desk-devstack")
	if *fresh {
		_ = os.RemoveAll(filepath.Join(base, "data"))
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "devstack dir:", err)
		os.Exit(1)
	}
	// An explicit DATABASE_URL (any PostgreSQL, or a still-running devstack database) skips the embedded server.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(uint32(*pgPort)).Database("c23").Username("c23").
			Password("c23dev").RuntimePath(filepath.Join(base, "runtime")).DataPath(filepath.Join(base, "data")).
			CachePath(filepath.Join(cacheDir, "c23desk-embedded-postgres")).Logger(nil))
		if err := pg.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "postgres:", err, "(if a previous devstack database is still running, set DATABASE_URL to reuse it)")
			os.Exit(1)
		}
		defer func() { _ = pg.Stop() }()
		dbURL = fmt.Sprintf("postgres://c23:c23dev@127.0.0.1:%d/c23?sslmode=disable", *pgPort)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer st.Close()
	if _, err := st.Migrate(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}

	var c cache.Cache = cache.Noop{}
	if u := os.Getenv("REDIS_URL"); u != "" { // e.g. a real Redis, or a dead port to rehearse a Redis outage
		r, err := cache.NewRedis(u, log)
		if err != nil {
			fmt.Fprintln(os.Stderr, "REDIS_URL:", err)
			os.Exit(1)
		}
		c = r
	} else if *withRedis {
		mr := miniredis.NewMiniRedis()
		if err := mr.StartAddr(fmt.Sprintf("127.0.0.1:%d", *redisPort)); err != nil {
			fmt.Fprintln(os.Stderr, "cache:", err)
			os.Exit(1)
		}
		defer mr.Close()
		r, _ := cache.NewRedis("redis://"+mr.Addr(), log)
		c = r
	}
	svc := &service.Service{Store: st, Cache: c, AI: aiclient.New(cfg.AIServiceURL, cfg.AITimeout, log), Rules: rules.Default(), Log: log, RawDir: cfg.RawDataDir}
	if r, err := svc.ImportOfficial(ctx, store.Actor{Name: "devstack-seed", RequestID: "startup"}); err == nil {
		log.Info("official fixture seeded", "pilots", r.Imported)
	}
	srv := &http.Server{Addr: ":" + cfg.Port, Handler: httpapi.New(svc, cfg, log), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.ListenAndServe() }()
	log.Info("devstack ready", "api", "http://127.0.0.1:"+cfg.Port, "database", config.RedactURL(dbURL), "cache", c.Status(ctx),
		"ai_service", cfg.AIServiceURL)
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}
