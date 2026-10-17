// Package httpapi is the public REST API (Gin). Handlers are thin: parse -> service -> respond.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/aiclient"
	"c23desk/internal/config"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
	"c23desk/internal/scenario"
	"c23desk/internal/service"
)

var groupRE = regexp.MustCompile(`^[a-z0-9][a-z0-9 _.&-]{0,63}$`)

// API holds dependencies.
type API struct {
	svc *service.Service
	log *slog.Logger
}

// New builds the router.
func New(svc *service.Service, cfg config.Config, log *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	a := &API{svc: svc, log: log}
	r.Use(requestContext(log), recovery(log), cors(cfg.CORSOrigins), bodyLimit(cfg.MaxBodyBytes), apiKey(cfg.APIKey))
	r.NoRoute(func(c *gin.Context) {
		abort(c, &service.Error{Status: http.StatusNotFound, Code: "ROUTE_NOT_FOUND", Message: "no such endpoint"})
	})
	r.NoMethod(func(c *gin.Context) {
		abort(c, &service.Error{Status: http.StatusMethodNotAllowed, Code: "METHOD_NOT_ALLOWED", Message: "method not allowed"})
	})

	r.GET("/health", a.health)
	r.GET("/ready", a.ready)
	v1 := r.Group("/api/v1")
	v1.GET("/rules", a.rules)
	v1.GET("/defaults", func(c *gin.Context) { c.JSON(http.StatusOK, svc.Defaults()) })
	v1.GET("/scenario-presets", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"presets": scenario.Presets}) })
	v1.GET("/dashboard", a.dashboard)
	v1.GET("/activity", a.activity)
	v1.POST("/preview/portfolio", a.previewPortfolio)
	v1.POST("/preview/optimize", a.previewOptimize)
	v1.POST("/evaluate", a.evaluateAdhoc)
	v1.POST("/optimize", a.optimizeAdhoc)
	v1.GET("/pilots", a.listPilots)
	v1.POST("/pilots", a.createPilot)
	v1.GET("/pilots/:id", pilotID, a.getPilot)
	v1.PUT("/pilots/:id", pilotID, a.updatePilot)
	v1.POST("/pilots/:id/evaluate", pilotID, a.evaluatePilot)
	v1.GET("/pilots/:id/decisions", pilotID, a.decisions)
	v1.POST("/pilots/:id/scenarios", pilotID, a.runScenario)
	v1.GET("/pilots/:id/scenarios", pilotID, a.listScenarios)
	v1.GET("/portfolios", a.listPortfolios)
	v1.POST("/portfolios", a.createPortfolio)
	v1.GET("/portfolios/:id", portfolioID, a.getPortfolio)
	v1.PUT("/portfolios/:id", portfolioID, a.updatePortfolio)
	v1.POST("/portfolios/:id/optimize", portfolioID, a.optimizePortfolio)
	v1.POST("/portfolios/:id/approve", portfolioID, a.approvePortfolio)
	v1.GET("/portfolios/:id/overlaps", portfolioID, a.overlaps)
	v1.POST("/portfolios/:id/overlaps/:overlapId/resolve", portfolioID, a.resolveOverlap)
	v1.POST("/import", a.importData)
	v1.POST("/import/official", a.importOfficial)
	v1.GET("/export/:portfolioId", a.export)
	v1.GET("/audit/:entityType/:id", a.audit)
	return r
}

// ---------------------------------------------------------------- helpers

type errorBody struct {
	Error struct {
		*service.Error
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func abort(c *gin.Context, e *service.Error) {
	var b errorBody
	b.Error.Error, b.Error.RequestID = e, c.GetString("request_id")
	c.AbortWithStatusJSON(e.Status, b)
}

func (a *API) fail(c *gin.Context, err error) {
	var se *service.Error
	if !errors.As(err, &se) {
		se = &service.Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "internal error", Cause: err}
	}
	if se.Status >= 500 {
		cause := ""
		if se.Cause != nil {
			cause = se.Cause.Error()
		}
		a.log.Error("request failed", "request_id", c.GetString("request_id"), "code", se.Code, "cause", cause)
	} else if se.Code == "VALIDATION_FAILED" {
		a.log.Warn("validation failed", "request_id", c.GetString("request_id"), "fields", len(se.Fields))
	}
	abort(c, se)
}

func actor(c *gin.Context) service.Actor {
	return service.Actor{Name: c.GetString("actor"), RequestID: c.GetString("request_id")}
}

// body reads the (size-limited) body and checks it is syntactically valid JSON.
func body(c *gin.Context) (json.RawMessage, *service.Error) {
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, &service.Error{Status: http.StatusRequestEntityTooLarge, Code: "PAYLOAD_TOO_LARGE", Message: "request body too large"}
		}
		return nil, &service.Error{Status: http.StatusBadRequest, Code: "BAD_BODY", Message: "could not read body"}
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		b = []byte("{}")
	}
	if !json.Valid(b) {
		return nil, &service.Error{Status: http.StatusBadRequest, Code: "MALFORMED_JSON", Message: "request body is not valid JSON (note: NaN and Infinity are not valid JSON)"}
	}
	return b, nil
}

// decodeStrict decodes an envelope rejecting unknown fields.
func decodeStrict(raw json.RawMessage, dst any) *service.Error {
	if t := bytes.TrimSpace(raw); len(t) == 0 || t[0] != '{' {
		return &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "body must be a JSON object",
			Fields: []domain.FieldError{{Code: domain.CodeNotObject, Message: "body must be a JSON object"}}}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid request body",
			Fields: []domain.FieldError{{Code: "INVALID_FIELD", Message: sanitizeDecodeErr(err)}}}
	}
	return nil
}

func sanitizeDecodeErr(err error) string {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return "field " + ute.Field + " has the wrong type (" + ute.Value + ")"
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func pilotID(c *gin.Context) {
	if !domain.ValidID(c.Param("id")) {
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "pilot id must be 1-64 characters: letters, digits, _ or -"})
	}
}

func portfolioID(c *gin.Context) {
	if !service.ValidUUID(c.Param("id")) {
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "portfolio id must be a lowercase UUID"})
	}
}

// ---------------------------------------------------------------- health

func (a *API) health(c *gin.Context) {
	ctx := c.Request.Context()
	db := "ok"
	if err := a.svc.Store.Ping(ctx); err != nil {
		db = "unavailable"
	}
	ai := a.svc.AI.Health(ctx)
	if !a.svc.AI.Enabled() {
		ai = aiclient.StatusDisabled
	}
	core := "ok"
	if db != "ok" {
		core = "degraded" // stateless endpoints still work
	}
	c.JSON(http.StatusOK, gin.H{"status": core, "rules_version": rules.Version, "time": time.Now().UTC(),
		"components": gin.H{"api": "ok", "database": db, "redis": a.svc.Cache.Status(ctx), "ai_service": ai},
		"note":       "redis and ai_service are optional; their failure never changes deterministic results"})
}

// ready is for orchestration: 200 only when the database (the source of truth) is reachable.
func (a *API) ready(c *gin.Context) {
	if err := a.svc.Store.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ready": false, "database": "unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ready": true, "database": "ok"})
}

func (a *API) rules(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"rules_version": rules.Version, "thresholds": a.svc.Rules, "decision_severity": rules.Severity,
		"reason_priority": rules.Priority, "reason_forces_decision": rules.DecisionFor, "tie_break_order": portfolio.TieBreakOrder,
		"resolution_types": []string{portfolio.RemoveOne, portfolio.ExcludeSecond, portfolio.SplitBenefit, portfolio.CustomAllocation},
		"disclaimer":       service.Disclaimer})
}

// ---------------------------------------------------------------- client views (read-only)

func (a *API) dashboard(c *gin.Context) {
	id := c.Query("portfolio_id")
	if id != "" && !service.ValidUUID(id) {
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "portfolio_id must be a lowercase UUID"})
		return
	}
	d, err := a.svc.Dashboard(c.Request.Context(), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (a *API) activity(c *gin.Context) {
	n := 50
	if v := c.Query("limit"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
			n = 50
		}
	}
	r, err := a.svc.RecentActivity(c.Request.Context(), n)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": r})
}

// budgetBasis parses budget_inr (required, known) and basis from an envelope.
func budgetBasis(c *gin.Context, raw json.RawMessage, basis string) (money.Amount, bool) {
	budget, code := money.ParseAmount(raw)
	if code == "" && !budget.Known {
		code = domain.CodeRequired
	}
	if code != "" {
		abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid budget",
			Fields: []domain.FieldError{{Field: "budget_inr", Code: code, Message: "budget_inr must be a known, non-negative INR number"}}})
		return budget, false
	}
	if basis != "" && basis != portfolio.BasisCash && basis != portfolio.BasisStated {
		abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid basis",
			Fields: []domain.FieldError{{Field: "basis", Code: domain.CodeInvalidEnum, Message: "basis must be cash or stated"}}})
		return budget, false
	}
	return budget, true
}

func (a *API) previewPortfolio(c *gin.Context) {
	b, e := body(c)
	var req struct {
		Budget      json.RawMessage        `json:"budget_inr"`
		Basis       string                 `json:"basis"`
		PilotIDs    []string               `json:"pilot_ids"`
		Resolutions []portfolio.Resolution `json:"overlap_resolutions"`
	}
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	budget, ok := budgetBasis(c, req.Budget, req.Basis)
	if !ok {
		return
	}
	for _, id := range req.PilotIDs {
		if !domain.ValidID(id) {
			abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid pilot id",
				Fields: []domain.FieldError{{Field: "pilot_ids", Code: domain.CodeBadID, Message: "invalid pilot id"}}})
			return
		}
	}
	as, ev, err := a.svc.PreviewPortfolio(c.Request.Context(), service.PreviewInput{Budget: budget, Basis: req.Basis, PilotIDs: req.PilotIDs, Resolutions: req.Resolutions})
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"assessment": as, "pilot_evaluations": ev, "persisted": false})
}

func (a *API) previewOptimize(c *gin.Context) {
	b, e := body(c)
	var req struct {
		Budget json.RawMessage `json:"budget_inr"`
		Basis  string          `json:"basis"`
	}
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	budget, ok := budgetBasis(c, req.Budget, req.Basis)
	if !ok {
		return
	}
	r, err := a.svc.PreviewOptimize(c.Request.Context(), budget, req.Basis)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"optimization": r, "persisted": false})
}

// ---------------------------------------------------------------- stateless

func (a *API) evaluateAdhoc(c *gin.Context) {
	b, e := body(c)
	if e != nil {
		abort(c, e)
		return
	}
	ev, err := a.svc.EvaluateAdhoc(b)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evaluation": ev, "persisted": false})
}

func (a *API) optimizeAdhoc(c *gin.Context) {
	b, e := body(c)
	if e != nil {
		abort(c, e)
		return
	}
	var req struct {
		Pilots json.RawMessage `json:"pilots"`
		Budget json.RawMessage `json:"budget_inr"`
		Basis  string          `json:"basis"`
	}
	if e := decodeStrict(b, &req); e != nil {
		abort(c, e)
		return
	}
	budget, code := money.ParseAmount(req.Budget)
	if code != "" || !budget.Known {
		if code == "" {
			code = domain.CodeRequired
		}
		abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid budget",
			Fields: []domain.FieldError{{Field: "budget_inr", Code: code, Message: "budget_inr must be a known, non-negative INR number"}}})
		return
	}
	if req.Basis != "" && req.Basis != portfolio.BasisCash && req.Basis != portfolio.BasisStated {
		abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "invalid basis",
			Fields: []domain.FieldError{{Field: "basis", Code: domain.CodeInvalidEnum, Message: "basis must be cash or stated"}}})
		return
	}
	res, rejected, err := a.svc.OptimizeAdhoc(req.Pilots, budget, req.Basis)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": res, "rejected_rows": rejected, "persisted": false})
}

// ---------------------------------------------------------------- pilots

func (a *API) listPilots(c *gin.Context) {
	ps, err := a.svc.ListPilots(c.Request.Context())
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pilots": ps, "count": len(ps)})
}

func (a *API) createPilot(c *gin.Context) {
	b, e := body(c)
	if e != nil {
		abort(c, e)
		return
	}
	p, err := a.svc.CreatePilot(c.Request.Context(), b, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (a *API) getPilot(c *gin.Context) {
	p, err := a.svc.GetPilot(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (a *API) updatePilot(c *gin.Context) {
	b, e := body(c)
	if e != nil {
		abort(c, e)
		return
	}
	p, err := a.svc.UpdatePilot(c.Request.Context(), c.Param("id"), b, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (a *API) evaluatePilot(c *gin.Context) {
	r, err := a.svc.EvaluatePilot(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.Header("X-Cache", r.Cache)
	c.JSON(http.StatusOK, r)
}

func (a *API) decisions(c *gin.Context) {
	r, err := a.svc.Decisions(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pilot_id": c.Param("id"), "decisions": r})
}

func (a *API) runScenario(c *gin.Context) {
	b, e := body(c)
	if e != nil {
		abort(c, e)
		return
	}
	var req scenario.Request
	if e := decodeStrict(b, &req); e != nil {
		abort(c, e)
		return
	}
	res, id, err := a.svc.RunScenario(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"scenario_id": id, "result": res})
}

func (a *API) listScenarios(c *gin.Context) {
	r, err := a.svc.Scenarios(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pilot_id": c.Param("id"), "scenarios": r})
}

// ---------------------------------------------------------------- portfolios

func (a *API) listPortfolios(c *gin.Context) {
	r, err := a.svc.ListPortfolios(c.Request.Context())
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"portfolios": r, "count": len(r)})
}

func (a *API) portfolioInput(c *gin.Context) (service.PortfolioInput, bool) {
	var in service.PortfolioInput
	b, e := body(c)
	if e == nil {
		e = decodeStrict(b, &in)
	}
	if e != nil {
		abort(c, e)
		return in, false
	}
	return in, true
}

func (a *API) createPortfolio(c *gin.Context) {
	in, ok := a.portfolioInput(c)
	if !ok {
		return
	}
	v, err := a.svc.CreatePortfolio(c.Request.Context(), in, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (a *API) getPortfolio(c *gin.Context) {
	v, err := a.svc.GetPortfolio(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.Header("X-Cache", v.Cache)
	c.JSON(http.StatusOK, v)
}

func (a *API) updatePortfolio(c *gin.Context) {
	in, ok := a.portfolioInput(c)
	if !ok {
		return
	}
	v, err := a.svc.UpdatePortfolio(c.Request.Context(), c.Param("id"), in, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (a *API) optimizePortfolio(c *gin.Context) {
	b, e := body(c)
	var req service.OptimizeRequest
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	res, applied, hit, err := a.svc.OptimizePortfolio(c.Request.Context(), c.Param("id"), req, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.Header("X-Cache", hit)
	c.JSON(http.StatusOK, gin.H{"optimization": res, "applied": applied != nil, "portfolio": applied})
}

func (a *API) approvePortfolio(c *gin.Context) {
	b, e := body(c)
	var req struct {
		Version int `json:"version"`
	}
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	v, err := a.svc.ApprovePortfolio(c.Request.Context(), c.Param("id"), req.Version, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (a *API) overlaps(c *gin.Context) {
	v, err := a.svc.Overlaps(c.Request.Context(), c.Param("id"))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (a *API) resolveOverlap(c *gin.Context) {
	group := c.Param("overlapId")
	if !groupRE.MatchString(group) {
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "overlapId must be a normalised overlap group"})
		return
	}
	b, e := body(c)
	var req struct {
		portfolio.Resolution
		Version int `json:"version"`
	}
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	v, err := a.svc.ResolveOverlap(c.Request.Context(), c.Param("id"), group, req.Resolution, req.Version, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

// ---------------------------------------------------------------- import / export / audit

func (a *API) importData(c *gin.Context) {
	b, e := body(c)
	var req struct {
		Fixture   json.RawMessage `json:"fixture"`
		Extension json.RawMessage `json:"extension"`
	}
	if e == nil {
		e = decodeStrict(b, &req)
	}
	if e != nil {
		abort(c, e)
		return
	}
	if len(req.Fixture) == 0 {
		abort(c, &service.Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "fixture is required",
			Fields: []domain.FieldError{{Field: "fixture", Code: domain.CodeRequired, Message: "fixture is required"}}})
		return
	}
	r, err := a.svc.Import(c.Request.Context(), req.Fixture, req.Extension, actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, r)
}

func (a *API) importOfficial(c *gin.Context) {
	r, err := a.svc.ImportOfficial(c.Request.Context(), actor(c))
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, r)
}

func (a *API) export(c *gin.Context) {
	id := c.Param("portfolioId")
	if !service.ValidUUID(id) {
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "portfolio id must be a lowercase UUID"})
		return
	}
	d, err := a.svc.Export(c.Request.Context(), id)
	if err != nil {
		a.fail(c, err)
		return
	}
	switch c.DefaultQuery("format", "json") {
	case "json":
		c.JSON(http.StatusOK, d)
	case "markdown":
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", service.EvidenceMarkdown(d))
	case "csv":
		c.Header("Content-Disposition", `attachment; filename="portfolio-ledger.csv"`)
		c.Data(http.StatusOK, "text/csv; charset=utf-8", service.LedgerCSV(d))
	default:
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_FORMAT", Message: "format must be json, markdown or csv"})
	}
}

func (a *API) audit(c *gin.Context) {
	et, id := c.Param("entityType"), c.Param("id")
	switch {
	case et == "pilot" && domain.ValidID(id), et == "portfolio" && service.ValidUUID(id):
	default:
		abort(c, &service.Error{Status: http.StatusBadRequest, Code: "INVALID_ID", Message: "entityType must be pilot or portfolio with a valid id"})
		return
	}
	r, err := a.svc.Audit(c.Request.Context(), et, id)
	if err != nil {
		a.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entity_type": et, "entity_id": id, "entries": r})
}
