// Command api is the Gin backend: HTTP API, Postgres, Redis cache, and a client for the FastAPI rules engine.
//
//	go run ./cmd/api                  start the server
//	go run ./cmd/api migrate          apply pending migrations   (also: migrate down | migrate status)
//	go run ./cmd/api bootstrap -email you@example.com [-workspace "Finance"] [-role admin]
//	                                  create a user, a workspace (or join the default one) and print a sign-in token
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"c23desk/internal/bedrock"
	"c23desk/internal/cache"
	"c23desk/internal/config"
	"c23desk/internal/httpapi"
	"c23desk/internal/jobs"
	"c23desk/internal/model"
	"c23desk/internal/rules"
	"c23desk/internal/storage"
	"c23desk/internal/store"
)

// The Postgres store provides every optional surface the router can use.
var (
	_ httpapi.Platform = (*store.Store)(nil)
	_ httpapi.Uploads  = (*store.Store)(nil)
	_ jobs.Queue       = (*store.Store)(nil)
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

	if len(args) > 0 && args[0] == "bootstrap" {
		return bootstrap(ctx, cfg, args[1:])
	}
	if len(args) > 0 {
		if args[0] != "migrate" {
			return fmt.Errorf("unknown command %q (use: migrate [up|down|status] or bootstrap)", args[0])
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

	engine := rules.New(cfg.RulesEngineURL, cfg.RequestTimeout)
	var objectStore storage.Store
	var aiExtractor httpapi.ExtractionProvider
	if cfg.S3DocumentsBucket != "" {
		objectStore, err = storage.NewS3(ctx, cfg.AWSRegion, cfg.S3DocumentsBucket)
		if err != nil {
			return fmt.Errorf("document storage: %w", err)
		}
	}
	bedrockCfg, err := bedrock.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("Bedrock configuration: %w", err)
	}
	if bedrockCfg.Enabled {
		aiExtractor, err = bedrock.NewExtractor(ctx, bedrockCfg)
		if err != nil {
			return fmt.Errorf("Bedrock client: %w", err)
		}
	}

	// The cache key follows the engine's own rules_version, re-read periodically, so an engine upgrade
	// retires every cached answer without anybody editing RULES_VERSION.
	go followRulesVersion(ctx, engine, cch, cfg.VersionPoll, log)

	var notify func()
	if cfg.WorkerEnabled {
		w := (&jobs.Worker{Queue: st, Engine: engine, ParsePath: httpapi.ParsePath, Fields: httpapi.UploadFields,
			Poll: cfg.WorkerPoll, CallTimeout: 30 * time.Second, Log: log, Storage: objectStore}).Init()
		notify = w.Notify
		go w.Run(ctx)
	}

	router := httpapi.NewRouter(httpapi.Deps{
		Cfg:           cfg,
		Store:         st,
		Rules:         engine,
		Cache:         cch,
		Audit:         st,
		Platform:      st,
		Uploads:       st,
		ObjectUploads: st,
		Objects:       objectStore,
		Extractions:   st,
		AIExtractor:   aiExtractor,
		Notify:        notify,
		Version:       cch.Version,
		Log:           log,
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
		log.Info("listening", "addr", srv.Addr, "rules_engine", cfg.RulesEngineURL, "cache", cch.Enabled(),
			"auth_mode", cfg.AuthMode, "upload_worker", cfg.WorkerEnabled)
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

func followRulesVersion(ctx context.Context, engine *rules.Client, cch *cache.Cache, every time.Duration, log *slog.Logger) {
	read := func() {
		vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		v, err := engine.Version(vctx)
		if err != nil {
			log.Warn("could not read the rules engine version; keeping the current cache namespace", "err", err)
			return
		}
		cch.SetVersion(v)
	}
	read()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			read()
		}
	}
}

// bootstrap creates the first user and workspace and prints a token: the only way in when AUTH_MODE=required.
func bootstrap(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	email := fs.String("email", "", "the user's email (created when new)")
	name := fs.String("name", "", "display name (optional)")
	workspace := fs.String("workspace", "", "create a new workspace with this name; empty joins the default workspace")
	role := fs.String("role", "admin", "role in the workspace: viewer, analyst or admin")
	label := fs.String("label", "bootstrap", "label for the new token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("bootstrap needs -email")
	}
	if *role != "viewer" && *role != "analyst" && *role != "admin" {
		return errors.New("-role must be viewer, analyst or admin")
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	var dn *string
	if *name != "" {
		dn = name
	}
	u, err := st.EnsureUser(ctx, *email, dn)
	if err != nil {
		return err
	}
	wsID := model.DefaultWorkspaceID
	if *workspace != "" {
		w, err := st.CreateWorkspace(ctx, *workspace, u.ID)
		if err != nil {
			return err
		}
		wsID = w.ID
		if *role != "admin" {
			if _, err := st.SetMember(model.WithWorkspace(ctx, wsID), u.Email, *role); err != nil {
				return err
			}
		}
	} else if _, err := st.SetMember(model.WithWorkspace(ctx, wsID), u.Email, *role); err != nil {
		return err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	raw := "c23_" + base64.RawURLEncoding.EncodeToString(b)
	if _, err := st.CreateToken(ctx, u.ID, *label, raw); err != nil {
		return err
	}
	fmt.Printf("user       %s (%s)\nworkspace  %s\nrole       %s\ntoken      %s\n\n", u.Email, u.ID, wsID, *role, raw)
	fmt.Println("Store the token now: only its hash is kept. Send it as  Authorization: Bearer <token>  and the workspace as  X-Workspace-ID.")
	return nil
}
