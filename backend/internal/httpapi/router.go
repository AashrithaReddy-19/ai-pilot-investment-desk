// Package httpapi is the Gin layer: routing, validation, and glue between
// the rules engine (FastAPI), the cache (Redis) and the store (Postgres).
package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

type Store interface {
	Ping(ctx context.Context) error
	SavePortfolio(ctx context.Context, in model.PortfolioIn) (*model.Portfolio, error)
	GetPortfolio(ctx context.Context, name string) (*model.Portfolio, error)
	ListPortfolios(ctx context.Context) ([]model.PortfolioSummary, error)
	DeletePortfolio(ctx context.Context, name string) error
	AddObservation(ctx context.Context, o model.ObservationIn, d model.DecisionIn) (*model.Observation, *model.Decision, error)
	History(ctx context.Context, pilotID string, limit int) (*model.History, error)
	SaveEvaluation(ctx context.Context, e model.EvaluationIn) (int64, error)
	AddInterview(ctx context.Context, in model.InterviewIn) (*model.Interview, error)
	ListInterviews(ctx context.Context) ([]model.Interview, error)
}

type Rules interface {
	Do(ctx context.Context, method, path string, body []byte, reqID string) (*model.Upstream, error)
	Ping(ctx context.Context) error
}

type Cache interface {
	Do(ctx context.Context, endpoint string, body []byte, fn func(context.Context) (*model.Upstream, error)) (*model.Upstream, bool, error)
	Ping(ctx context.Context) error
	Enabled() bool
}

type Deps struct {
	Cfg   config.Config
	Store Store
	Rules Rules
	Cache Cache
	Log   *slog.Logger
}

func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(requestID(), accessLog(d.Log), gin.CustomRecovery(recoverJSON(d.Log)), cors(d.Cfg.AllowedOrigins), timeout(d.Cfg.RequestTimeout))

	h := &Handlers{cfg: d.Cfg, store: d.Store, rules: d.Rules, cache: d.Cache, log: d.Log}

	r.GET("/health", h.health)
	r.GET("/ready", h.ready)

	api := r.Group("/api/v1")
	api.GET("/defaults", h.defaults)

	// Pass-through to the rules engine (cached: answers are deterministic)
	api.POST("/pilots", h.forward("/pilots", fwdOpts{cacheable: true}))
	api.POST("/portfolio", h.forward("/portfolio", fwdOpts{cacheable: true}))
	api.POST("/portfolio/check", h.forward("/portfolio/check", fwdOpts{cacheable: true}))
	api.POST("/stoprule/:id", h.stopRulePreview)
	api.POST("/export/csv", h.forward("/export/csv", fwdOpts{contentType: "text/csv; charset=utf-8", filename: "pilots.csv"}))
	api.POST("/export/evidence", h.forward("/export/evidence", fwdOpts{contentType: "text/markdown; charset=utf-8", filename: "evidence.md"}))

	// Both modes side by side, remembered in the evaluations table
	api.POST("/evaluate", h.evaluate)

	// Saved portfolios (starting state + negotiated scope + final evidence)
	api.POST("/portfolios", h.savePortfolio)
	api.GET("/portfolios", h.listPortfolios)
	api.GET("/portfolios/:name", h.getPortfolio)
	api.DELETE("/portfolios/:name", h.deletePortfolio)

	// Measured results and the stop-rule decisions they produced
	api.POST("/pilots/:id/observations", h.addObservation)
	api.GET("/pilots/:id/history", h.history)

	// MBA evidence
	api.POST("/interviews", h.addInterview)
	api.GET("/interviews", h.listInterviews)

	r.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, "not_found", "no such route", "")
	})
	return r
}
