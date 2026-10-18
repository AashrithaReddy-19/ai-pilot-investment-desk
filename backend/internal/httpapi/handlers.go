package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/aiassist"
	"c23desk/internal/config"
	"c23desk/internal/model"
)

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	pilotRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
)

type Handlers struct {
	cfg      config.Config
	store    Store
	rules    Rules
	cache    Cache
	audit    Auditor
	platform Platform
	uploads  Uploads
	notify   func()
	version  func() string
	log      *slog.Logger
	docSlots chan struct{}    // limits how many documents are read at once; nil = unlimited
	ai       *aiassist.Reader // balance-sheet AI reader; nil = off
}

type fwdOpts struct {
	cacheable   bool
	contentType string // overrides the upstream content type (exports)
	filename    string // adds Content-Disposition: attachment
}

// ---------------------------------------------------------------- ops

func (h *Handlers) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "auth_mode": h.cfg.AuthMode, "rules_version": strOrNil(h.version())})
}

// ready needs Postgres and the rules engine. Redis is optional (fail-open), so it never fails readiness.
func (h *Handlers) ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	checks := map[string]string{}
	healthy := true

	if err := h.store.Ping(ctx); err != nil {
		checks["postgres"], healthy = "down", false
	} else {
		checks["postgres"] = "ok"
	}
	if err := h.rules.Ping(ctx); err != nil {
		checks["rules_engine"], healthy = "down", false
	} else {
		checks["rules_engine"] = "ok"
	}
	switch {
	case !h.cache.Enabled():
		checks["redis"] = "disabled"
	case h.cache.Ping(ctx) != nil:
		checks["redis"] = "down (running without cache)"
	default:
		checks["redis"] = "ok"
	}
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{"ready": healthy, "checks": checks, "rules_version": strOrNil(h.version())})
}

// ---------------------------------------------------------------- helpers

// readBody enforces the size cap and valid JSON. An empty body becomes {}.
func (h *Handlers) readBody(c *gin.Context) ([]byte, bool) {
	b, ok := h.readRawBody(c)
	if !ok {
		return nil, false
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return []byte("{}"), true
	}
	if !json.Valid(b) {
		fail(c, http.StatusBadRequest, "invalid_json", "request body is not valid JSON", "")
		return nil, false
	}
	return b, true
}

// readRawBody is readBody without the JSON rule: for multipart uploads the original
// content-type must survive. The MAX_BODY_BYTES limit is enforced before any reading.
func (h *Handlers) readRawBody(c *gin.Context) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.cfg.MaxBodyBytes)
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			fail(c, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds %d bytes", h.cfg.MaxBodyBytes), "")
			return nil, false
		}
		fail(c, http.StatusBadRequest, "bad_request", "could not read request body", "")
		return nil, false
	}
	return b, true
}

// readObject is readBody plus "must be a JSON object", returned as raw fields.
func (h *Handlers) readObject(c *gin.Context) (map[string]json.RawMessage, bool) {
	b, ok := h.readBody(c)
	if !ok {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		fail(c, http.StatusBadRequest, "invalid_json", "request body must be a JSON object", "")
		return nil, false
	}
	return m, true
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// extConventions reads the multi-period conventions a request chose (extension.defaults).
// Absent stays NULL, never zero.
func extConventions(extUsed json.RawMessage) (*int, *float64) {
	var rawExt struct {
		Defaults *struct {
			HorizonMonths   *int     `json:"horizon_months"`
			DiscountRatePct *float64 `json:"discount_rate"`
		} `json:"defaults"`
	}
	_ = json.Unmarshal(extUsed, &rawExt)
	if rawExt.Defaults == nil {
		return nil, nil
	}
	return rawExt.Defaults.HorizonMonths, rawExt.Defaults.DiscountRatePct
}

// callRules sends one request to the rules engine, through the cache when allowed.
func (h *Handlers) callRules(c *gin.Context, method, path string, body []byte, cacheable bool) (*model.Upstream, bool, error) {
	rid := reqID(c)
	call := func(ctx context.Context) (*model.Upstream, error) {
		return h.rules.Do(ctx, method, path, body, rid)
	}
	if !cacheable {
		r, err := call(c.Request.Context())
		return r, false, err
	}
	return h.cache.Do(c.Request.Context(), path, body, call)
}

// upstreamOK turns every rules-engine failure into a clean error response.
// It returns true only when r is a usable 2xx answer.
func (h *Handlers) upstreamOK(c *gin.Context, r *model.Upstream, err error) bool {
	switch {
	case err != nil:
		h.log.Error("rules engine call failed", "err", err, "request_id", reqID(c))
		if isTimeout(err) {
			fail(c, http.StatusGatewayTimeout, "rules_timeout", "rules engine timed out", "")
		} else {
			fail(c, http.StatusBadGateway, "rules_unavailable", "rules engine is unreachable", "")
		}
		return false
	case r.Status >= 500:
		h.log.Error("rules engine returned an error", "status", r.Status, "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "rules_error", "rules engine failed", "")
		return false
	case r.Status >= 400:
		fail(c, r.Status, "rules_rejected", truncate(engineDetail(r.Body), 500), "")
		return false
	}
	return true
}

// post is callRules + upstreamOK for handlers that need the parsed answer.
func (h *Handlers) post(c *gin.Context, method, path string, body []byte, cacheable bool) (*model.Upstream, bool) {
	r, _, err := h.callRules(c, method, path, body, cacheable)
	if !h.upstreamOK(c, r, err) {
		return nil, false
	}
	return r, true
}

func (h *Handlers) storeErr(c *gin.Context, err error) {
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "not found", "")
		return
	}
	h.log.Error("database error", "err", err, "request_id", reqID(c))
	if isTimeout(err) {
		fail(c, http.StatusGatewayTimeout, "database_timeout", "database timed out", "")
		return
	}
	fail(c, http.StatusInternalServerError, "database_error", "database operation failed", "")
}

// ---------------------------------------------------------------- pass-through

func (h *Handlers) forward(path string, o fwdOpts) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, ok := h.readBody(c)
		if !ok {
			return
		}
		r, hit, err := h.callRules(c, http.MethodPost, path, body, o.cacheable)
		if !h.upstreamOK(c, r, err) {
			return
		}
		ct := r.ContentType
		if o.contentType != "" {
			ct = o.contentType
		}
		if ct == "" {
			ct = "application/json"
		}
		if o.cacheable {
			c.Header("X-Cache", map[bool]string{true: "HIT", false: "MISS"}[hit])
		}
		if o.filename != "" {
			c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, o.filename))
			h.record(c, "export.download", "export", o.filename, nil)
		}
		c.Data(http.StatusOK, ct, r.Body)
	}
}

// uploadForward sends the raw multipart body to the engine untouched (uploads skip the cache)
// and returns the engine's JSON answer.
func (h *Handlers) uploadForward(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, ok := h.readRawBody(c)
		if !ok {
			return
		}
		ct := c.Request.Header.Get("Content-Type")
		if ct == "" {
			ct = "multipart/form-data"
		}
		r, err := h.rules.DoUpload(c.Request.Context(), path, body, ct, reqID(c))
		if !h.upstreamOK(c, r, err) {
			return
		}
		c.Data(http.StatusOK, "application/json", r.Body)
	}
}

func (h *Handlers) defaults(c *gin.Context) {
	r, ok := h.post(c, http.MethodGet, "/defaults", nil, false)
	if !ok {
		return
	}
	c.Data(http.StatusOK, "application/json", r.Body)
}

func (h *Handlers) stopRulePreview(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . or -", "id")
		return
	}
	body, ok := h.readBody(c)
	if !ok {
		return
	}
	r, ok := h.post(c, http.MethodPost, "/stoprule/"+url.PathEscape(id), body, false)
	if !ok {
		return
	}
	c.Data(http.StatusOK, "application/json", r.Body)
}

// ---------------------------------------------------------------- evaluate (dual answer)

func (h *Handlers) evaluate(c *gin.Context) {
	scen, ok := h.readObject(c)
	if !ok {
		return
	}
	// Optional link to a stored proposal: recorded with its current inputs version, never sent to the engine.
	var proposalID *string
	var proposalVersion *int
	if raw, present := scen["proposal_id"]; present {
		if !isNull(raw) {
			var pid string
			if json.Unmarshal(raw, &pid) != nil || !validUUID(pid) {
				fail(c, http.StatusUnprocessableEntity, "invalid_value", "proposal_id must be a UUID", "proposal_id")
				return
			}
			p, err := h.store.GetProposal(c.Request.Context(), pid)
			if errors.Is(err, model.ErrNotFound) {
				fail(c, http.StatusUnprocessableEntity, "unknown_proposal", "proposal_id does not match a stored proposal", "proposal_id")
				return
			}
			if err != nil {
				h.storeErr(c, err)
				return
			}
			proposalID, proposalVersion = &p.ID, &p.Version
		}
		delete(scen, "proposal_id")
	}
	delete(scen, "mode") // this endpoint always computes both modes
	reqBody, _ := json.Marshal(scen)

	pilots, ok := h.post(c, http.MethodPost, "/pilots", reqBody, true)
	if !ok {
		return
	}
	answers := map[string]*model.Upstream{}
	for _, mode := range []string{"independent", "conservative"} {
		scen["mode"] = json.RawMessage(strconv.Quote(mode))
		b, _ := json.Marshal(scen)
		r, ok := h.post(c, http.MethodPost, "/portfolio", b, true)
		if !ok {
			return
		}
		answers[mode] = r
	}
	// History is secondary: a failed write is logged, never shown as a failed evaluation
	// (evaluation_id is then null).
	var evaluationID *int64
	if id, err := h.store.SaveEvaluation(c.Request.Context(), model.EvaluationIn{
		Request: reqBody, Independent: answers["independent"].Body, Conservative: answers["conservative"].Body,
		ProposalID: proposalID, ProposalVersion: proposalVersion, RulesVersion: strOrNil(h.version()),
	}); err != nil {
		h.log.Warn("could not save evaluation history", "err", err, "request_id", reqID(c))
	} else {
		evaluationID = &id
		h.record(c, "evaluation.create", "evaluation", strconv.FormatInt(id, 10),
			gin.H{"proposal_id": proposalID, "proposal_version": proposalVersion})
	}
	c.JSON(http.StatusOK, gin.H{
		"pilots":           json.RawMessage(pilots.Body),
		"independent":      json.RawMessage(answers["independent"].Body),
		"conservative":     json.RawMessage(answers["conservative"].Body),
		"evaluation_id":    evaluationID,
		"proposal_id":      proposalID,
		"proposal_version": proposalVersion,
	})
}

// ---------------------------------------------------------------- saved portfolios

func (h *Handlers) savePortfolio(c *gin.Context) {
	scen, ok := h.readObject(c)
	if !ok {
		return
	}
	var name string
	if err := json.Unmarshal(scen["name"], &name); err != nil || !nameRe.MatchString(name) {
		fail(c, http.StatusUnprocessableEntity, "invalid_name", "name must be 1-40 chars: letters, digits, _ or -", "name")
		return
	}
	delete(scen, "name")

	mode := "independent"
	if raw, present := scen["mode"]; present {
		var m string
		if json.Unmarshal(raw, &m) != nil || (m != "independent" && m != "conservative") {
			fail(c, http.StatusUnprocessableEntity, "invalid_mode", "mode must be independent or conservative", "mode")
			return
		}
		mode = m
	}
	scenBody, _ := json.Marshal(scen)

	defs, ok := h.fetchDefaults(c)
	if !ok {
		return
	}
	pilotsR, ok := h.post(c, http.MethodPost, "/pilots", scenBody, true)
	if !ok {
		return
	}
	portR, ok := h.post(c, http.MethodPost, "/portfolio", scenBody, true)
	if !ok {
		return
	}

	rows, budget, err := buildRows(pilotsR.Body, portR.Body, mode)
	if err != nil {
		h.log.Error("unexpected rules engine response shape", "err", err, "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "rules_error", "rules engine returned an unexpected response", "")
		return
	}
	// A budget that was sent but is not a number is stored as NULL (unknown), never zero.
	if raw, present := scen["budget"]; present {
		var f *float64
		if json.Unmarshal(raw, &f) == nil && f != nil {
			budget = f
		} else {
			budget = nil
		}
	}

	fixtureUsed, extUsed := defs.StartingState, defs.Extension
	if raw, present := scen["fixture"]; present && !isNull(raw) {
		fixtureUsed = raw
	}
	if raw, present := scen["extension"]; present && !isNull(raw) {
		extUsed = raw
	}
	scope, _ := json.Marshal(map[string]any{"fixture_as_used": fixtureUsed, "extension": extUsed, "mode": mode})

	// The saved summary records the multi-period conventions in force (NULL when the engine did not send them).
	horiz, disc := extConventions(extUsed)

	evidence, _ := json.Marshal(map[string]any{
		"portfolio": json.RawMessage(portR.Body),
		"pilots":    json.RawMessage(pilotsR.Body),
	})

	// starting_state is the original fixture the user supplied, not always the shipped default.
	saved, err := h.store.SavePortfolio(c.Request.Context(), model.PortfolioIn{
		Name: name, Mode: mode, BudgetINR: budget, HorizonMonths: horiz, DiscountRatePct: disc,
		StartingState: fixtureUsed, NegotiatedScope: scope, FinalEvidence: evidence,
		Pilots: rows,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "portfolio.save", "portfolio", saved.Name, gin.H{"id": saved.ID, "mode": mode})
	c.JSON(http.StatusOK, saved)
}

type defaultsDoc struct {
	StartingState json.RawMessage `json:"starting_state"`
	Extension     json.RawMessage `json:"extension"`
}

func (h *Handlers) fetchDefaults(c *gin.Context) (defaultsDoc, bool) {
	r, ok := h.post(c, http.MethodGet, "/defaults", nil, false)
	if !ok {
		return defaultsDoc{}, false
	}
	var d defaultsDoc
	if err := json.Unmarshal(r.Body, &d); err != nil || len(d.StartingState) == 0 {
		h.log.Error("rules engine /defaults has an unexpected shape", "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "rules_error", "rules engine /defaults is missing starting_state", "")
		return defaultsDoc{}, false
	}
	return d, true
}

type pilotsView struct {
	Pilots []struct {
		ID    string  `json:"id"`
		Group string  `json:"group"`
		Setup float64 `json:"setup"`
		Run   float64 `json:"run"`
		Flags []struct {
			Code string `json:"code"`
		} `json:"flags"`
		Independent  netView `json:"independent"`
		Conservative netView `json:"conservative"`
	} `json:"pilots"`
}

type netView struct {
	Net           float64          `json:"net"`
	PaybackMonths *float64         `json:"payback_months"`
	MultiPeriod   *multiPeriodView `json:"multi_period"`
}

type multiPeriodView struct {
	ProfitINR  *float64 `json:"profit_inr"`
	ROIPct     *float64 `json:"roi_pct"`
	BreakMonth *int     `json:"breakeven_month"`
}

type portfolioView struct {
	Budget   *float64 `json:"budget"`
	Selected []string `json:"selected"`
	Rejected []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"rejected"`
}

// buildRows flattens the two rules-engine answers into one row per valid pilot.
func buildRows(pilotsBody, portfolioBody []byte, mode string) ([]model.PortfolioPilot, *float64, error) {
	var pv pilotsView
	if err := json.Unmarshal(pilotsBody, &pv); err != nil {
		return nil, nil, fmt.Errorf("pilots: %w", err)
	}
	var pf portfolioView
	if err := json.Unmarshal(portfolioBody, &pf); err != nil {
		return nil, nil, fmt.Errorf("portfolio: %w", err)
	}
	selected := map[string]bool{}
	for _, id := range pf.Selected {
		selected[id] = true
	}
	reasons := map[string]string{}
	for _, r := range pf.Rejected {
		reasons[r.ID] = r.Reason
	}
	rows := make([]model.PortfolioPilot, 0, len(pv.Pilots))
	for _, p := range pv.Pilots {
		v := p.Independent
		if mode == "conservative" {
			v = p.Conservative
		}
		row := model.PortfolioPilot{
			PilotID: p.ID, Selected: selected[p.ID],
			BenefitGroup: strPtr(p.Group), SetupINR: floatPtr(p.Setup), RunINR: floatPtr(p.Run),
			NetINR: floatPtr(v.Net), PaybackMonths: v.PaybackMonths,
		}
		if v.MultiPeriod != nil { // additive: the multi-period view carried by newer engine responses
			row.ProfitINR = v.MultiPeriod.ProfitINR
			row.ROIPct = v.MultiPeriod.ROIPct
		}
		for _, f := range p.Flags {
			if f.Code == "RUN_COST_ASSUMED" {
				row.RunAssumed = true
			}
		}
		if !row.Selected {
			reason := reasons[p.ID]
			if reason == "" {
				reason = "Not selected: no portfolio was computed for this scenario."
			}
			row.Reason = &reason
		}
		rows = append(rows, row)
	}
	return rows, pf.Budget, nil
}

func strPtr(s string) *string     { return &s }
func floatPtr(f float64) *float64 { return &f }

func (h *Handlers) listPortfolios(c *gin.Context) {
	list, err := h.store.ListPortfolios(c.Request.Context())
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"portfolios": list})
}

func (h *Handlers) getPortfolio(c *gin.Context) {
	name := c.Param("name")
	if !nameRe.MatchString(name) {
		fail(c, http.StatusUnprocessableEntity, "invalid_name", "name must be 1-40 chars: letters, digits, _ or -", "name")
		return
	}
	p, err := h.store.GetPortfolio(c.Request.Context(), name)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handlers) deletePortfolio(c *gin.Context) {
	name := c.Param("name")
	if !nameRe.MatchString(name) {
		fail(c, http.StatusUnprocessableEntity, "invalid_name", "name must be 1-40 chars: letters, digits, _ or -", "name")
		return
	}
	if err := h.store.DeletePortfolio(c.Request.Context(), name); err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "portfolio.delete", "portfolio", name, nil)
	c.Status(http.StatusNoContent)
}

// ---------------------------------------------------------------- observations and history

func optPct(body map[string]json.RawMessage, field string) (*float64, error) {
	raw, present := body[field]
	if !present || isNull(raw) {
		return nil, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s must be a number", field)
	}
	if f < 0 || f > 100 {
		return nil, fmt.Errorf("%s must be between 0 and 100", field)
	}
	return &f, nil
}

func (h *Handlers) addObservation(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . or -", "id")
		return
	}
	body, ok := h.readObject(c)
	if !ok {
		return
	}
	adoption, err := optPct(body, "observed_adoption_pct")
	if err != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "observed_adoption_pct")
		return
	}
	quality, err := optPct(body, "observed_quality_pct")
	if err != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "observed_quality_pct")
		return
	}
	if adoption == nil && quality == nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "send observed_adoption_pct and/or observed_quality_pct", "")
		return
	}
	var source *string
	if raw, present := body["source"]; present && !isNull(raw) {
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) > 100 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "source must be a string of at most 100 characters", "source")
			return
		}
		source = &s
	}
	delete(body, "source")

	// Optional link to a stored proposal: keeps observation IDs from colliding across scenarios.
	var proposalID *string
	if raw, present := body["proposal_id"]; present && !isNull(raw) {
		var pid string
		if json.Unmarshal(raw, &pid) != nil || !validUUID(pid) {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "proposal_id must be a UUID", "proposal_id")
			return
		}
		proposalID = &pid
	}
	delete(body, "proposal_id")
	rulesBody, _ := json.Marshal(body)

	r, ok := h.post(c, http.MethodPost, "/stoprule/"+url.PathEscape(id), rulesBody, false)
	if !ok {
		return
	}
	var sr struct {
		Decision  string   `json:"decision"`
		Reasons   []string `json:"reasons"`
		Metric    string   `json:"metric"`
		Threshold *float64 `json:"threshold"`
		Actual    *float64 `json:"actual"`
	}
	if err := json.Unmarshal(r.Body, &sr); err != nil || sr.Decision == "" {
		fail(c, http.StatusBadGateway, "rules_error", "rules engine returned an unexpected stop-rule response", "")
		return
	}
	obs, dec, err := h.store.AddObservation(c.Request.Context(),
		model.ObservationIn{PilotID: id, ProposalID: proposalID, AdoptionPct: adoption, QualityPct: quality, Source: source},
		model.DecisionIn{PilotID: id, ProposalID: proposalID, Decision: sr.Decision, Metric: sr.Metric, Threshold: sr.Threshold, Actual: sr.Actual,
			Reason: strings.Join(sr.Reasons, "; ")})
	if err != nil {
		if isFKViolation(err) {
			fail(c, http.StatusUnprocessableEntity, "unknown_proposal", "proposal_id does not match a stored proposal", "proposal_id")
			return
		}
		h.storeErr(c, err)
		return
	}
	h.record(c, "observation.create", "observation", strconv.FormatInt(obs.ID, 10),
		gin.H{"pilot_id": id, "decision": dec.Decision, "proposal_id": proposalID})
	c.JSON(http.StatusCreated, gin.H{"observation": obs, "decision": dec, "stop_rule": json.RawMessage(r.Body)})
}

func (h *Handlers) history(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . or -", "id")
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "limit must be an integer from 1 to 200", "limit")
			return
		}
		limit = n
	}
	hist, err := h.store.History(c.Request.Context(), id, limit)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, hist)
}

// ---------------------------------------------------------------- interviews

func (h *Handlers) addInterview(c *gin.Context) {
	b, ok := h.readBody(c)
	if !ok {
		return
	}
	var in model.InterviewIn
	if err := json.Unmarshal(b, &in); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "body does not match the interview shape", "")
		return
	}
	in.IntervieweeRole, in.Task = strings.TrimSpace(in.IntervieweeRole), strings.TrimSpace(in.Task)
	switch {
	case in.IntervieweeRole == "" || len(in.IntervieweeRole) > 100:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "interviewee_role is required (max 100 chars; use a role, not a name)", "interviewee_role")
		return
	case in.Task == "" || len(in.Task) > 200:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "task is required (max 200 chars)", "task")
		return
	case in.MinutesPerTask != nil && (*in.MinutesPerTask < 0 || *in.MinutesPerTask > 10000):
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "minutes_per_task must be between 0 and 10000", "minutes_per_task")
		return
	}
	if in.InterviewedOn != nil {
		if _, err := time.Parse("2006-01-02", *in.InterviewedOn); err != nil {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "interviewed_on must be YYYY-MM-DD", "interviewed_on")
			return
		}
	}
	iv, err := h.store.AddInterview(c.Request.Context(), in)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "interview.create", "interview", strconv.Itoa(iv.ID), nil) // no quotes or roles in the audit row
	c.JSON(http.StatusCreated, iv)
}

func (h *Handlers) listInterviews(c *gin.Context) {
	list, err := h.store.ListInterviews(c.Request.Context())
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"interviews": list})
}

// ---------------------------------------------------------------- Phase 6: lifecycle (checkpoint, plans, reallocation, memo)

var planStatusRe = regexp.MustCompile(`^(planned|active|revise|stopped|completed)$`)

// enrichEvidence fills a checkpoint-style request body with the tracker's stored state:
// the plan's revise flag and duration, plus actuals from the latest linked observation.
// Explicit request values win; the engine never writes anything — previews only.
// Returns false when a store lookup fails (response already written).
func (h *Handlers) enrichEvidence(c *gin.Context, id string, body map[string]json.RawMessage) bool {
	plan, err := h.store.PilotPlan(c.Request.Context(), id)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		h.storeErr(c, err)
		return false
	}
	if plan != nil {
		if plan.Status == "revise" {
			if _, present := body["revised_before"]; !present {
				body["revised_before"] = json.RawMessage("true")
			}
		}
		if _, present := body["duration_days"]; !present {
			raw, _ := json.Marshal(plan.DurationDays)
			body["duration_days"] = raw
		}
	}
	hist, err := h.store.History(c.Request.Context(), id, 1)
	if err != nil {
		h.storeErr(c, err)
		return false
	}
	if len(hist.Observations) > 0 {
		o := hist.Observations[0]
		if _, present := body["observed_adoption_pct"]; !present && o.AdoptionPct != nil {
			raw, _ := json.Marshal(o.AdoptionPct)
			body["observed_adoption_pct"] = raw
		}
		if _, present := body["observed_quality_pct"]; !present && o.QualityPct != nil {
			raw, _ := json.Marshal(o.QualityPct)
			body["observed_quality_pct"] = raw
		}
	}
	return true
}

// runCheckpoint — same body, then the engine checkpoint preview.
func (h *Handlers) runCheckpoint(c *gin.Context, id string, body map[string]json.RawMessage) (*model.Upstream, bool) {
	if !h.enrichEvidence(c, id, body) {
		return nil, false
	}
	b, _ := json.Marshal(body)
	return h.post(c, http.MethodPost, "/checkpoint/"+url.PathEscape(id), b, false)
}

// checkpointPreview — POST /api/v1/checkpoint/:id.
// Keeps the stop-rule (GO/REVISE/STOP) in the answer; the checkpoint is an additional lens.
func (h *Handlers) checkpointPreview(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . -", "pilot_id")
		return
	}
	body, ok := h.readObject(c)
	if !ok {
		return
	}
	r, ok := h.runCheckpoint(c, id, body)
	if !ok {
		return
	}
	c.Data(http.StatusOK, "application/json", r.Body)
}

type enginePlan struct {
	PilotID        string   `json:"pilot_id"`
	Objective      string   `json:"objective"`
	Owner          *string  `json:"owner"`
	AdoptionTarget *float64 `json:"adoption_target_pct"`
	QualityTarget  *float64 `json:"quality_target_pct"`
	CostLimitINR   *float64 `json:"cost_limit_inr"`
	DurationDays   int      `json:"duration_days"`
	StopRule       string   `json:"stop_rule"`
	Status         string   `json:"status"`
	CostLimitNote  string   `json:"cost_limit_note"`
}

// createPlan — POST /api/v1/plans: generate the 30-day plan from the engine and record it.
// Regenerating replaces the current plan (one plan per pilot); status starts at planned.
func (h *Handlers) createPlan(c *gin.Context) {
	scen, ok := h.readObject(c)
	if !ok {
		return
	}
	var pid string
	if raw, present := scen["pilot_id"]; present {
		if json.Unmarshal(raw, &pid) != nil || !pilotRe.MatchString(pid) {
			fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot_id must be 1-40 chars: letters, digits, _ . -", "pilot_id")
			return
		}
	} else {
		fail(c, http.StatusUnprocessableEntity, "missing_pilot_id", "pilot_id is required", "pilot_id")
		return
	}
	b, _ := json.Marshal(scen)
	r, ok := h.post(c, http.MethodPost, "/plan", b, true)
	if !ok {
		return
	}
	var pl enginePlan
	if err := json.Unmarshal(r.Body, &pl); err != nil || pl.PilotID == "" {
		h.log.Error("unexpected engine /plan response shape", "err", err, "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "rules_error", "rules engine returned an unexpected plan", "")
		return
	}
	if pl.Status == "" {
		pl.Status = "planned"
	}
	saved, err := h.store.UpsertPlan(c.Request.Context(), model.PilotPlan{
		PilotID: pl.PilotID, Objective: pl.Objective, Owner: pl.Owner,
		AdoptionTarget: pl.AdoptionTarget, QualityTarget: pl.QualityTarget,
		CostLimitINR: pl.CostLimitINR, DurationDays: pl.DurationDays,
		StopRule: pl.StopRule, Status: pl.Status,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "plan.create", "plan", saved.PilotID, gin.H{"status": saved.Status})
	c.JSON(http.StatusOK, gin.H{"plan": saved, "cost_limit_note": pl.CostLimitNote, "source": "engine plan generator"})
}

func (h *Handlers) listPlans(c *gin.Context) {
	plans, err := h.store.PilotPlans(c.Request.Context())
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// planProgress — GET /api/v1/plans/:id: expected from the plan, actual from the latest
// linked observation, the gap spelled out, and the checkpoint verdict re-run with actuals.
// GET has no scenario body, so the verdict runs against the shipped default scenario; when the
// pilot is not in it, plan and actuals are still returned with verdict: null.
func (h *Handlers) planProgress(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . or -", "id")
		return
	}
	plan, err := h.store.PilotPlan(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	hist, err := h.store.History(c.Request.Context(), id, 1)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	out := gin.H{"pilot_id": id, "plan": plan, "actual": nil, "gaps": gin.H{}}
	if len(hist.Observations) > 0 {
		o := hist.Observations[0]
		out["actual"] = gin.H{"adoption_pct": o.AdoptionPct, "quality_pct": o.QualityPct,
			"observed_on": o.ObservedOn, "source": o.Source}
		gaps := gin.H{}
		if o.AdoptionPct != nil && plan.AdoptionTarget != nil {
			gaps["adoption_gap_pct"] = *o.AdoptionPct - *plan.AdoptionTarget
		}
		if o.QualityPct != nil && plan.QualityTarget != nil {
			gaps["quality_gap_pct"] = *o.QualityPct - *plan.QualityTarget
		}
		out["gaps"] = gaps
	}
	r, ok := h.runCheckpoint(c, id, map[string]json.RawMessage{})
	if !ok {
		return
	}
	var verdict map[string]json.RawMessage
	if json.Unmarshal(r.Body, &verdict) != nil || verdict == nil {
		out["verdict"] = nil
	} else {
		out["verdict"] = verdict
	}
	c.JSON(http.StatusOK, out)
}

// setPlanStatus — POST /api/v1/plans/:id/status. Status drives the checkpoint's
// revised-before flag (status revise ⇒ one gate failure already accepted).
func (h *Handlers) setPlanStatus(c *gin.Context) {
	id := c.Param("id")
	if !pilotRe.MatchString(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot id must be 1-40 chars: letters, digits, _ . or -", "id")
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	var status string
	if raw, present := in["status"]; present {
		if json.Unmarshal(raw, &status) != nil || !planStatusRe.MatchString(status) {
			fail(c, http.StatusUnprocessableEntity, "invalid_status", "status must be one of: planned, active, revise, stopped, completed", "status")
			return
		}
	} else {
		fail(c, http.StatusUnprocessableEntity, "missing_status", "status is required", "status")
		return
	}
	saved, err := h.store.SetPlanStatus(c.Request.Context(), id, status)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "plan.status", "plan", id, gin.H{"status": status})
	c.JSON(http.StatusOK, saved)
}

// realloc — POST /api/v1/reallocation: on stop, free the stopped pilot's reservation, keep the
// funded pilots locked, and compute the next best use of the freed budget. A recommendation
// only; nothing is written. Scenario comes from a saved portfolio (by name) or an evaluation
// (by id, else the newest one that funded this pilot).
func (h *Handlers) realloc(c *gin.Context) {
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	var pid string
	if raw, present := in["pilot_id"]; present {
		if json.Unmarshal(raw, &pid) != nil || !pilotRe.MatchString(pid) {
			fail(c, http.StatusUnprocessableEntity, "invalid_pilot_id", "pilot_id must be 1-40 chars: letters, digits, _ . -", "pilot_id")
			return
		}
	} else {
		fail(c, http.StatusUnprocessableEntity, "missing_pilot_id", "pilot_id is required", "pilot_id")
		return
	}
	var portfolioName, evaluationID string
	if raw, present := in["objective"]; present && !isNull(raw) {
		var obj string
		if json.Unmarshal(raw, &obj) == nil && obj != "net" {
			fail(c, http.StatusUnprocessableEntity, "objective_not_accepted", "reallocation ranks by net only; leave objective absent or \"net\"", "objective")
			return
		}
	}
	if raw, present := in["portfolio_name"]; present {
		_ = json.Unmarshal(raw, &portfolioName)
	}
	if raw, present := in["evaluation_id"]; present {
		_ = json.Unmarshal(raw, &evaluationID)
	}

	body := map[string]json.RawMessage{"stopped_ids": json.RawMessage(`["` + pid + `"]`), "objective": json.RawMessage(`"net"`)}
	var src gin.H
	if portfolioName != "" {
		p, err := h.store.GetPortfolio(c.Request.Context(), portfolioName)
		if err != nil {
			h.storeErr(c, err)
			return
		}
		var funded []string
		selected := false
		for _, pp := range p.Pilots {
			if pp.Selected {
				funded = append(funded, pp.PilotID)
				if pp.PilotID == pid {
					selected = true
				}
			}
		}
		if !selected {
			fail(c, http.StatusUnprocessableEntity, "pilot_not_funded", "pilot is not funded (selected) in portfolio "+portfolioName, "pilot_id")
			return
		}
		var scope struct {
			Fixture   *json.RawMessage `json:"fixture_as_used"`
			Extension *json.RawMessage `json:"extension"`
			Mode      string           `json:"mode"`
		}
		_ = json.Unmarshal(p.NegotiatedScope, &scope)
		if scope.Fixture != nil && !isNull(*scope.Fixture) {
			body["fixture"] = *scope.Fixture
		}
		if scope.Extension != nil && !isNull(*scope.Extension) {
			body["extension"] = *scope.Extension
		}
		body["mode"] = json.RawMessage(`"` + modeOr(scope.Mode, "independent") + `"`)
		if p.BudgetINR != nil {
			raw, _ := json.Marshal(*p.BudgetINR)
			body["budget"] = raw
		}
		rawFunded, _ := json.Marshal(funded)
		body["funded_ids"] = rawFunded
		src = gin.H{"type": "portfolio", "name": p.Name, "id": p.ID}
	} else { // evaluation source by id, else the newest evaluation that funded this pilot
		var ev *model.Evaluation
		var err error
		if evaluationID != "" {
			var idNum int64
			idNum, err = strconv.ParseInt(evaluationID, 10, 64)
			if err != nil {
				fail(c, http.StatusUnprocessableEntity, "invalid_evaluation_id", "evaluation_id must be a number", "evaluation_id")
				return
			}
			ev, err = h.store.GetEvaluation(c.Request.Context(), idNum)
		} else {
			ev, err = h.store.LatestEvaluationWithPilot(c.Request.Context(), pid)
		}
		if err != nil {
			h.storeErr(c, err)
			return
		}
		var ind struct {
			Selected []string `json:"selected"`
			Budget   *float64 `json:"budget"`
		}
		if err := json.Unmarshal(ev.Independent, &ind); err != nil {
			fail(c, http.StatusBadGateway, "evaluation_unreadable", "stored evaluation could not be read", "")
			return
		}
		picked := false
		for _, s := range ind.Selected {
			if s == pid {
				picked = true
			}
		}
		if !picked {
			fail(c, http.StatusUnprocessableEntity, "pilot_not_funded", "pilot is not in the selected portfolio of evaluation "+evaluationID, "pilot_id")
			return
		}
		var req map[string]json.RawMessage
		_ = json.Unmarshal(ev.Request, &req)
		for _, k := range []string{"fixture", "extension"} {
			if raw, present := req[k]; present && !isNull(raw) {
				body[k] = raw
			}
		}
		mode := "independent"
		if raw, present := req["mode"]; present {
			var m string
			if json.Unmarshal(raw, &m) == nil && (m == "independent" || m == "conservative") {
				mode = m
			}
		}
		body["mode"] = json.RawMessage(`"` + mode + `"`)
		if b := budgetFrom(req["budget"], ind.Budget); b != nil {
			raw, _ := json.Marshal(*b)
			body["budget"] = raw
		}
		rawFunded, _ := json.Marshal(ind.Selected)
		body["funded_ids"] = rawFunded
		src = gin.H{"type": "evaluation", "id": ev.ID}
	}
	b, _ := json.Marshal(body)
	r, ok := h.post(c, http.MethodPost, "/reallocation", b, false)
	if !ok {
		return
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(r.Body, &result) != nil || result == nil {
		h.log.Error("unexpected engine /reallocation response shape", "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "rules_error", "rules engine returned an unexpected response", "")
		return
	}
	result["source"] = mustJSON(src)
	result["accepted"] = json.RawMessage("false")
	result["note"] = mustJSON(gin.H{"text": "Recommendation only: nothing changes until you save the updated portfolio.", "accept_via": "POST /api/v1/portfolios"})
	c.JSON(http.StatusOK, result)
}

func modeOr(m, def string) string {
	if m == "independent" || m == "conservative" {
		return m
	}
	return def
}

// budgetFrom prefers an explicit request budget (numeric) and falls back to the stored one.
func budgetFrom(raw json.RawMessage, fallback *float64) *float64 {
	if raw != nil {
		var f *float64
		if json.Unmarshal(raw, &f) == nil && f != nil {
			return f
		}
	}
	return fallback
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// memoForward — POST /api/v1/export/memo: one-page decision memo. The stored 30-day plan
// (if any) and the latest observation actuals are embedded so the memo carries the plan
// section and real evidence without extra calls.
func (h *Handlers) memoForward(c *gin.Context) {
	scen, ok := h.readObject(c)
	if !ok {
		return
	}
	var pilot gin.H // audit detail: only a valid pilot id is recorded
	if raw, present := scen["pilot_id"]; present {
		var pid string
		if json.Unmarshal(raw, &pid) == nil && pilotRe.MatchString(pid) {
			pilot = gin.H{"pilot_id": pid}
			if plan, err := h.store.PilotPlan(c.Request.Context(), pid); err == nil {
				scen["plan"] = mustJSON(plan)
			}
			if !h.enrichEvidence(c, pid, scen) {
				return
			}
		}
	}
	b, _ := json.Marshal(scen)
	r, hit, err := h.callRules(c, http.MethodPost, "/export/memo", b, true)
	if !h.upstreamOK(c, r, err) {
		return
	}
	c.Header("X-Cache", map[bool]string{true: "HIT", false: "MISS"}[hit])
	c.Header("Content-Disposition", `attachment; filename="memo.md"`)
	h.record(c, "export.download", "export", "memo.md", pilot)
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", r.Body)
}
