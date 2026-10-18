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
	ListProposalEvaluations(ctx context.Context, proposalID string, limit int) ([]model.EvaluationSummary, error)
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

// Auditor records successful edits and exports and reads them back. The Postgres store implements it.
type Auditor interface {
	AddAuditEvent(ctx context.Context, e model.AuditEventIn) error
	ListAuditEvents(ctx context.Context, f model.AuditFilter) ([]model.AuditEvent, error)
}

// noAudit is used when Deps.Audit is nil (e.g. handler unit tests): nothing is recorded.
type noAudit struct{}

func (noAudit) AddAuditEvent(context.Context, model.AuditEventIn) error { return nil }
func (noAudit) ListAuditEvents(context.Context, model.AuditFilter) ([]model.AuditEvent, error) {
	return []model.AuditEvent{}, nil
}

type Deps struct {
	Cfg      config.Config
	Store    Store
	Rules    Rules
	Cache    Cache
	Audit    Auditor       // optional; nil records nothing
	Platform Platform      // workspaces, people, settings, snapshots, approvals; nil = those routes answer 503
	Uploads  Uploads       // stored uploads and review; nil = those routes answer 503
	Notify   func()        // wakes the upload worker after a new upload or a retry
	Version  func() string // the rules engine's current rules_version
	Log      *slog.Logger
}

func NewRouter(d Deps) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	var limiter *rateLimiter
	if d.Cfg.RateLimitPerMin > 0 {
		limiter = newRateLimiter(d.Cfg.RateLimitPerMin, max(d.Cfg.RateLimitBurst, 1))
	}
	r.Use(requestID(), accessLog(d.Log), gin.CustomRecovery(recoverJSON(d.Log)), cors(d.Cfg.AllowedOrigins), rateLimit(limiter), timeout(d.Cfg.RequestTimeout))

	var audit Auditor = noAudit{}
	if d.Audit != nil {
		audit = d.Audit
	}
	version := d.Version
	if version == nil {
		version = func() string { return "" }
	}
	h := &Handlers{cfg: d.Cfg, store: d.Store, rules: d.Rules, cache: d.Cache, audit: audit, platform: d.Platform,
		uploads: d.Uploads, notify: d.Notify, version: version, log: d.Log}

	r.GET("/health", h.health)
	r.GET("/ready", h.ready)

	api := r.Group("/api/v1", h.authenticate())

	// Account routes work before a workspace is chosen.
	api.GET("/me", h.me)
	api.GET("/workspaces", h.listWorkspaces)
	api.POST("/workspaces", h.needPlatform(), h.createWorkspace)
	api.GET("/tokens", h.needPlatform(), h.listTokens)
	api.POST("/tokens", h.needPlatform(), h.createToken)
	api.DELETE("/tokens/:id", h.needPlatform(), h.revokeToken)

	ws := api.Group("", requireWorkspace())
	view := ws.Group("", need("viewer"))  // read and calculate
	edit := ws.Group("", need("analyst")) // create and change records
	admin := ws.Group("", need("admin"))  // delete, approve, settings, members

	view.GET("/defaults", h.defaults)

	// Pass-through to the rules engine (cached: answers are deterministic)
	view.POST("/pilots", h.forward("/pilots", fwdOpts{cacheable: true}))
	view.POST("/portfolio", h.forward("/portfolio", fwdOpts{cacheable: true}))
	view.POST("/portfolio/check", h.forward("/portfolio/check", fwdOpts{cacheable: true}))
	view.POST("/stoprule/:id", h.stopRulePreview)
	view.POST("/whatif", h.forward("/whatif", fwdOpts{cacheable: true}))
	view.POST("/sweep", h.forward("/sweep", fwdOpts{cacheable: true}))
	view.POST("/compare", h.forward("/compare", fwdOpts{cacheable: true}))
	// Engine rules version 7: economics, decisions, dashboard and yearly views, pricing and forecasting
	view.POST("/economics", h.forward("/economics", fwdOpts{cacheable: true}))
	view.POST("/decision", h.forward("/decision", fwdOpts{cacheable: true}))
	view.POST("/views/dashboard", h.forward("/views/dashboard", fwdOpts{cacheable: true}))
	view.POST("/views/yearly-savings", h.forward("/views/yearly-savings", fwdOpts{cacheable: true}))
	view.POST("/pricing/quote", h.forward("/pricing/quote", fwdOpts{cacheable: true}))
	view.POST("/pricing/optimise", h.forward("/pricing/optimise", fwdOpts{cacheable: true}))
	view.POST("/forecast/usage", h.forward("/forecast/usage", fwdOpts{cacheable: true}))

	// Multipart uploads: nothing stored, never cached, MAX_BODY_BYTES enforced
	view.POST("/balance-sheet/parse", h.uploadForward("/balance-sheet/parse"))
	view.POST("/balance-sheet/confirm", h.uploadForward("/balance-sheet/confirm"))
	view.POST("/pilots/import/parse", h.uploadForward("/pilots/import/parse"))
	view.POST("/pilots/import/confirm", h.uploadForward("/pilots/import/confirm"))
	view.POST("/export/csv", h.forward("/export/csv", fwdOpts{contentType: "text/csv; charset=utf-8", filename: "pilots.csv"}))
	view.POST("/export/evidence", h.forward("/export/evidence", fwdOpts{contentType: "text/markdown; charset=utf-8", filename: "evidence.md"}))
	view.POST("/export/decision-report", h.forward("/export/decision-report", fwdOpts{contentType: "text/markdown; charset=utf-8", filename: "decision-report.md"}))
	view.POST("/export/yearly-csv", h.forward("/export/yearly-csv", fwdOpts{contentType: "text/csv; charset=utf-8", filename: "yearly-savings.csv"}))

	// Both modes side by side, remembered in the evaluations table
	edit.POST("/evaluate", h.evaluate)
	view.GET("/evaluations", h.listEvaluations)
	view.GET("/evaluations/:id", h.getEvaluation)
	view.GET("/history", h.timeline) // merged evaluation + decision timeline
	view.GET("/audit", h.listAudit)  // who changed or exported what

	// Saved portfolios (starting state + negotiated scope + final evidence)
	edit.POST("/portfolios", h.savePortfolio)
	view.GET("/portfolios", h.listPortfolios)
	view.GET("/portfolios/:name", h.getPortfolio)
	admin.DELETE("/portfolios/:name", h.deletePortfolio)

	// Measured results and the stop-rule decisions they produced
	edit.POST("/pilots/:id/observations", h.addObservation)
	view.GET("/pilots/:id/history", h.history)

	// MBA evidence
	edit.POST("/interviews", h.addInterview)
	view.GET("/interviews", h.listInterviews)

	// Phase 6 lifecycle: checkpoint preview, 30-day plans, budget reallocation, memo
	view.POST("/checkpoint/:id", h.checkpointPreview)
	edit.POST("/plans", h.createPlan)
	view.GET("/plans", h.listPlans)
	view.GET("/plans/:id", h.planProgress)
	edit.POST("/plans/:id/status", h.setPlanStatus)
	view.POST("/reallocation", h.realloc)
	view.POST("/export/memo", h.memoForward)

	// Stored proposals (persistent pilot candidates) with their evidence and approvals
	edit.POST("/proposals", h.createProposal)
	view.GET("/proposals", h.listProposals)
	view.GET("/proposals/:id", h.getProposal)
	edit.PATCH("/proposals/:id", h.updateProposal)
	edit.POST("/proposals/:id/archive", h.archiveProposal)
	edit.POST("/proposals/:id/restore", h.archiveProposal)
	admin.DELETE("/proposals/:id", h.deleteProposal)
	edit.POST("/proposals/:id/evidence", h.addEvidence)
	view.GET("/proposals/:id/evidence", h.listEvidence)
	view.GET("/proposals/:id/evaluations", h.listProposalEvaluations)
	view.GET("/proposals/:id/approvals", h.needPlatform(), h.listApprovals)
	admin.POST("/proposals/:id/approvals", h.needPlatform(), h.addApproval)

	// Workspace settings and members
	view.GET("/settings", h.needPlatform(), h.getSettings)
	admin.PUT("/settings", h.needPlatform(), h.putSettings)
	view.GET("/members", h.needPlatform(), h.listMembers)
	admin.POST("/members", h.needPlatform(), h.setMember)
	admin.DELETE("/members/:user_id", h.needPlatform(), h.removeMember)

	// Workspace views over the stored proposals, kept as reproducible snapshots
	view.POST("/workspace/dashboard", h.needPlatform(), h.workspaceView("dashboard"))
	view.POST("/workspace/decision", h.needPlatform(), h.workspaceView("decision"))
	view.POST("/workspace/comparison", h.needPlatform(), h.workspaceView("comparison"))
	view.POST("/workspace/yearly-savings", h.needPlatform(), h.workspaceView("yearly_savings"))
	view.POST("/workspace/export/decision-report", h.needPlatform(),
		h.workspaceExport("/export/decision-report", "text/markdown; charset=utf-8", "decision-report.md"))
	view.POST("/workspace/export/yearly-csv", h.needPlatform(),
		h.workspaceExport("/export/yearly-csv", "text/csv; charset=utf-8", "yearly-savings.csv"))
	view.GET("/snapshots", h.needPlatform(), h.listSnapshots)
	view.GET("/snapshots/:id", h.needPlatform(), h.getSnapshot)
	view.POST("/snapshots/:id/replay", h.needPlatform(), h.replaySnapshot)

	// Stored uploads: original kept, parsed by the worker, reviewed, corrected, confirmed
	edit.POST("/uploads", h.needUploads(), h.createUpload)
	view.GET("/uploads", h.needUploads(), h.listUploads)
	view.GET("/uploads/:id", h.needUploads(), h.getUpload)
	view.GET("/uploads/:id/file", h.needUploads(), h.downloadUpload)
	edit.POST("/uploads/:id/retry", h.needUploads(), h.retryUpload)
	edit.POST("/uploads/:id/corrections", h.needUploads(), h.addCorrection)
	edit.POST("/uploads/:id/confirm", h.needUploads(), h.confirmUpload)

	r.NoRoute(func(c *gin.Context) {
		fail(c, http.StatusNotFound, "not_found", "no such route", "")
	})
	return r
}

func (h *Handlers) needPlatform() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.platform == nil {
			fail(c, http.StatusServiceUnavailable, "not_configured", "workspace features are not configured on this gateway", "")
			return
		}
		c.Next()
	}
}

func (h *Handlers) needUploads() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.uploads == nil {
			fail(c, http.StatusServiceUnavailable, "not_configured", "stored uploads are not configured on this gateway", "")
			return
		}
		c.Next()
	}
}
