// Command api is the Gin backend: HTTP API, Postgres, Redis cache, and a client for the FastAPI rules engine.
//
//	go run ./cmd/api                  start the server
//	go run ./cmd/api migrate          apply pending migrations   (also: migrate down | migrate status)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"c23desk/internal/cache"
	"c23desk/internal/config"
	"c23desk/internal/httpapi"
	"c23desk/internal/rules"
	"c23desk/internal/store"
)

func main() {
	_ = godotenv.Load() // optional .env for local development; real env vars win
	if m := os.Getenv("GIN_MODE"); m != "" {
		gin.SetMode(m)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log, os.Args[1:]); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) > 0 {
		if args[0] != "migrate" {
			return fmt.Errorf("unknown command %q (use: migrate [up|down|status])", args[0])
		}
		action := "up"
		if len(args) > 1 {
			action = args[1]
		}
		return store.Migrate(ctx, cfg.DatabaseURL, action, os.Stdout)
	}

	if cfg.AutoMigrate {
		if err := store.Migrate(ctx, cfg.DatabaseURL, "up", os.Stdout); err != nil {
			return err
		}
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	cch, err := cache.New(cfg.RedisURL, cfg.CacheTTL, cfg.RulesVersion, log)
	if err != nil {
		return err
	}
	defer func() { _ = cch.Close() }()

	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:   cfg,
		Store: st,
		Rules: rules.New(cfg.RulesEngineURL, cfg.RequestTimeout),
		Cache: cch,
		Log:   log,
	})
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 10*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "rules_engine", cfg.RulesEngineURL, "cache", cch.Enabled())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down, finishing in-flight requests")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}
