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
	ListEvaluations(ctx context.Context, limit int) ([]model.EvaluationSummary, error)
	GetEvaluation(ctx context.Context, id int64) (*model.Evaluation, error)
	HistoryTimeline(ctx context.Context, limit int) ([]model.HistoryEvent, error)
	AddInterview(ctx context.Context, in model.InterviewIn) (*model.Interview, error)
	ListInterviews(ctx context.Context) ([]model.Interview, error)
	CreateProposal(ctx context.Context, in model.ProposalIn) (*model.Proposal, error)
	GetProposal(ctx context.Context, id string) (*model.Proposal, error)
	ListProposals(ctx context.Context, includeArchived bool) ([]model.ProposalSummary, error)
	UpdateProposal(ctx context.Context, id string, in model.ProposalIn) (*model.Proposal, error)
	ArchiveProposal(ctx context.Context, id string, restore bool) (*model.Proposal, error)
	DeleteProposal(ctx context.Context, id string) error
	AddEvidence(ctx context.Context, in model.EvidenceIn) (*model.Evidence, error)
	ListEvidence(ctx context.Context, proposalID string) ([]model.Evidence, error)
	UpsertPlan(ctx context.Context, p model.PilotPlan) (*model.PilotPlan, error)
	PilotPlan(ctx context.Context, pilotID string) (*model.PilotPlan, error)
	PilotPlans(ctx context.Context) ([]model.PilotPlan, error)
	SetPlanStatus(ctx context.Context, pilotID, status string) (*model.PilotPlan, error)
	LatestEvaluationWithPilot(ctx context.Context, pilotID string) (*model.Evaluation, error)
}

type Rules interface {
	Do(ctx context.Context, method, path string, body []byte, reqID string) (*model.Upstream, error)
	DoUpload(ctx context.Context, path string, body []byte, contentType, reqID string) (*model.Upstream, error)
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
	api.POST("/whatif", h.forward("/whatif", fwdOpts{cacheable: true}))
	api.POST("/sweep", h.forward("/sweep", fwdOpts{cacheable: true}))
	api.POST("/compare", h.forward("/compare", fwdOpts{cacheable: true}))

	// Multipart uploads: nothing stored, never cached, MAX_BODY_BYTES enforced
	api.POST("/balance-sheet/parse", h.uploadForward("/balance-sheet/parse"))
	api.POST("/balance-sheet/confirm", h.uploadForward("/balance-sheet/confirm"))
	api.POST("/pilots/import/parse", h.uploadForward("/pilots/import/parse"))
	api.POST("/pilots/import/confirm", h.uploadForward("/pilots/import/confirm"))
	api.POST("/export/csv", h.forward("/export/csv", fwdOpts{contentType: "text/csv; charset=utf-8", filename: "pilots.csv"}))
	api.POST("/export/evidence", h.forward("/export/evidence", fwdOpts{contentType: "text/markdown; charset=utf-8", filename: "evidence.md"}))

	// Both modes side by side, remembered in the evaluations table
	api.POST("/evaluate", h.evaluate)
	api.GET("/evaluations", h.listEvaluations)
	api.GET("/evaluations/:id", h.getEvaluation)
	api.GET("/history", h.timeline) // merged evaluation + decision timeline

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

	// Phase 6 lifecycle: checkpoint preview, 30-day plans, budget reallocation, memo
	api.POST("/checkpoint/:id", h.checkpointPreview)
	api.POST("/plans", h.createPlan)
	api.GET("/plans", h.listPlans)
	api.GET("/plans/:id", h.planProgress)
	api.POST("/plans/:id/status", h.setPlanStatus)
	api.POST("/reallocation", h.realloc)
	api.POST("/export/memo", h.memoForward)

	// Stored proposals (persistent pilot candidates) with their evidence
	api.POST("/proposals", h.createProposal)
	api.GET("/proposals", h.listProposals)
	api.GET("/proposals/:id", h.getProposal)
	api.PATCH("/proposals/:id", h.updateProposal)
	api.POST("/proposals/:id/archive", h.archiveProposal)
	api.POST("/proposals/:id/restore", h.archiveProposal)
	api.DELETE("/proposals/:id", h.deleteProposal)
	api.POST("/proposals/:id/evidence", h.addEvidence)
	api.GET("/proposals/:id/evidence", h.listEvidence)

	r.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, "not_found", "no such route", "")
	})
	return r
}
