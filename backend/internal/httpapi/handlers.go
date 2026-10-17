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

	"c23desk/internal/config"
	"c23desk/internal/model"
)

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	pilotRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)
)

type Handlers struct {
	cfg   config.Config
	store Store
	rules Rules
	cache Cache
	log   *slog.Logger
}

type fwdOpts struct {
	cacheable   bool
	contentType string // overrides the upstream content type (exports)
	filename    string // adds Content-Disposition: attachment
}

// ---------------------------------------------------------------- ops

func (h *Handlers) health(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }

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
	c.JSON(code, gin.H{"ready": healthy, "checks": checks})
}

// ---------------------------------------------------------------- helpers

// readBody enforces the size cap and valid JSON. An empty body becomes {}.
func (h *Handlers) readBody(c *gin.Context) ([]byte, bool) {
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
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return []byte("{}"), true
	}
	if !json.Valid(b) {
		fail(c, http.StatusBadRequest, "invalid_json", "request body is not valid JSON", "")
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
		fail(c, r.Status, "rules_rejected", truncate(string(r.Body), 500), "")
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
		}
		c.Data(http.StatusOK, ct, r.Body)
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
	// History is secondary: a failed write is logged, never shown as a failed evaluation.
	if _, err := h.store.SaveEvaluation(c.Request.Context(), model.EvaluationIn{
		Request: reqBody, Independent: answers["independent"].Body, Conservative: answers["conservative"].Body,
	}); err != nil {
		h.log.Warn("could not save evaluation history", "err", err, "request_id", reqID(c))
	}
	c.JSON(http.StatusOK, gin.H{
		"pilots":       json.RawMessage(pilots.Body),
		"independent":  json.RawMessage(answers["independent"].Body),
		"conservative": json.RawMessage(answers["conservative"].Body),
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
	evidence, _ := json.Marshal(map[string]any{
		"portfolio": json.RawMessage(portR.Body),
		"pilots":    json.RawMessage(pilotsR.Body),
	})

	saved, err := h.store.SavePortfolio(c.Request.Context(), model.PortfolioIn{
		Name: name, Mode: mode, BudgetINR: budget,
		StartingState: defs.StartingState, NegotiatedScope: scope, FinalEvidence: evidence,
		Pilots: rows,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
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
	Net           float64  `json:"net"`
	PaybackMonths *float64 `json:"payback_months"`
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
		model.ObservationIn{PilotID: id, AdoptionPct: adoption, QualityPct: quality, Source: source},
		model.DecisionIn{PilotID: id, Decision: sr.Decision, Metric: sr.Metric, Threshold: sr.Threshold, Actual: sr.Actual,
			Reason: strings.Join(sr.Reasons, "; ")})
	if err != nil {
		h.storeErr(c, err)
		return
	}
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
