// Command api runs the C23 Go + Gin public API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"c23desk/internal/aiclient"
	"c23desk/internal/cache"
	"c23desk/internal/config"
	"c23desk/internal/httpapi"
	"c23desk/internal/rules"
	"c23desk/internal/service"
	"c23desk/internal/store"
)

func main() {
	cfg := config.Load()
	// `api -healthcheck` is used by the container HEALTHCHECK (distroless image has no curl).
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		c := &http.Client{Timeout: 2 * time.Second}
		r, err := c.Get("http://127.0.0.1:" + cfg.Port + "/health")
		if err != nil || r.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	log.Info("starting c23 api", "rules_version", rules.Version, "config", cfg.Redacted())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("invalid database configuration", "error", err.Error())
		os.Exit(1)
	}
	defer st.Close()
	if cfg.AutoMigrate {
		if applied, err := st.Migrate(ctx); err != nil {
			log.Warn("migrations not applied (database unavailable?); API starts degraded", "error", err.Error())
		} else {
			log.Info("migrations applied", "applied", applied)
		}
	}

	var c cache.Cache = cache.Noop{}
	if cfg.RedisURL != "" {
		r, err := cache.NewRedis(cfg.RedisURL, log)
		if err != nil {
			log.Warn("invalid REDIS_URL; cache disabled", "error", err.Error())
		} else {
			c = r
			defer r.Close()
		}
	}
	svc := &service.Service{Store: st, Cache: c, AI: aiclient.New(cfg.AIServiceURL, cfg.AITimeout, log), Rules: rules.Default(),
		Log: log, RawDir: cfg.RawDataDir}

	if cfg.SeedOfficial {
		if r, err := svc.ImportOfficial(ctx, store.Actor{Name: "system-seed", RequestID: "startup"}); err != nil {
			log.Warn("official fixture not seeded (already present or DB unavailable)", "error", err.Error())
		} else {
			log.Info("official fixture seeded", "pilots", r.Imported)
		}
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: httpapi.New(svc, cfg, log), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err.Error())
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("stopped")
}
