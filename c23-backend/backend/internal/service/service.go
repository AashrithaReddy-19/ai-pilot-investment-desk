// Package service orchestrates storage, cache, AI enrichment and the pure engines.
// Business rules live in finance/decision/portfolio/scenario; this layer only wires them together.
package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"time"

	"c23desk/internal/aiclient"
	"c23desk/internal/cache"
	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
	"c23desk/internal/scenario"
	"c23desk/internal/store"
)

// Service wires everything together.
type Service struct {
	Store  *store.Store
	Cache  cache.Cache
	AI     *aiclient.Client
	Rules  rules.Thresholds
	Log    *slog.Logger
	RawDir string
}

// Actor re-exported for handlers.
type Actor = store.Actor

func (s *Service) evaluateAll(ps []domain.Pilot) map[string]decision.Evaluation {
	out := make(map[string]decision.Evaluation, len(ps))
	for _, p := range ps {
		out[p.ID] = decision.Evaluate(p, s.Rules, decision.Context{GroupPeers: peers(ps, p)})
	}
	return out
}

func peers(ps []domain.Pilot, p domain.Pilot) []string {
	out := []string{}
	if p.OverlapGroup == nil {
		return out
	}
	for _, q := range ps {
		if q.ID != p.ID && q.OverlapGroup != nil && *q.OverlapGroup == *p.OverlapGroup {
			out = append(out, q.ID)
		}
	}
	sort.Strings(out)
	return out
}

// ------------------------------------------------------------------ pilots

// PilotView is a pilot with its current decision summary.
type PilotView struct {
	Pilot         domain.Pilot          `json:"pilot"`
	Decision      string                `json:"decision"`
	PrimaryReason string                `json:"primary_reason"`
	ReasonCodes   []string              `json:"reason_codes"`
	Warnings      []string              `json:"warnings"`
	Metrics       decision.Metrics      `json:"calculated_metrics"` // engine output; clients only format it
	FailedRules   []decision.FailedRule `json:"failed_rules"`
	RulesVersion  string                `json:"rules_version"`
}

// ListPilots returns all pilots with their current deterministic decision.
func (s *Service) ListPilots(ctx context.Context) ([]PilotView, error) {
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return nil, fromStore(err, "pilots")
	}
	ev := s.evaluateAll(ps)
	out := make([]PilotView, 0, len(ps))
	for _, p := range ps {
		e := ev[p.ID]
		out = append(out, PilotView{Pilot: p, Decision: e.Decision, PrimaryReason: e.PrimaryReason, ReasonCodes: e.ReasonCodes, Warnings: e.Warnings,
			Metrics: e.Metrics, FailedRules: e.FailedRules, RulesVersion: e.RulesVersion})
	}
	return out, nil
}

// GetPilot returns one pilot.
func (s *Service) GetPilot(ctx context.Context, id string) (domain.Pilot, error) {
	p, err := s.Store.GetPilot(ctx, id)
	return p, fromStore(err, "pilot "+id)
}

// CreatePilot validates and stores a pilot (provenance TEAM_ASSUMPTION; actual_* OBSERVED).
func (s *Service) CreatePilot(ctx context.Context, raw json.RawMessage, a Actor) (domain.Pilot, error) {
	p, errs := domain.ParsePilot(raw, domain.SourceTeam)
	if errs != nil {
		return p, validation(errs)
	}
	if err := s.Store.CreatePilots(ctx, []domain.Pilot{p}, a, "create"); err != nil {
		return p, fromStore(err, "pilot "+p.ID)
	}
	s.invalidatePilot(ctx, p.ID)
	s.Log.Info("pilot created", "pilot_id", p.ID, "actor", a.Name, "request_id", a.RequestID)
	return s.GetPilot(ctx, p.ID)
}

// UpdatePilot replaces a pilot. Body must carry the current "version" (optimistic locking).
// Unchanged fields keep their provenance; changed fields become TEAM_ASSUMPTION (actual_* OBSERVED).
func (s *Service) UpdatePilot(ctx context.Context, id string, raw json.RawMessage, a Actor) (domain.Pilot, error) {
	var env struct {
		ID      *string `json:"id"`
		Version *int    `json:"version"`
	}
	_ = json.Unmarshal(raw, &env)
	if env.Version == nil || *env.Version < 1 {
		return domain.Pilot{}, fieldErr("version", domain.CodeRequired, "version (current record version) is required for updates")
	}
	if env.ID != nil && *env.ID != id {
		return domain.Pilot{}, fieldErr("id", domain.CodeBadID, "id in body must match the URL")
	}
	obj, derrs := domain.DecodeObject(raw)
	if derrs != nil {
		return domain.Pilot{}, validation(derrs)
	}
	before, err := s.Store.GetPilot(ctx, id)
	if err != nil {
		return domain.Pilot{}, fromStore(err, "pilot "+id)
	}
	obj["id"], _ = json.Marshal(id)
	// read-only fields from a GET response are accepted and ignored, so clients can PUT back what they read
	for _, k := range []string{"version", "field_sources", "assumptions", "created_at", "updated_at", "benefit_type_raw", "overlap_group_raw"} {
		delete(obj, k)
	}
	// an unchanged canonical label echoed back keeps the original raw label (e.g. "cashable assumption")
	if before.BenefitTypeRaw != nil && string(obj["benefit_type"]) == `"`+before.BenefitType+`"` {
		obj["benefit_type"], _ = json.Marshal(*before.BenefitTypeRaw)
	}
	if before.OverlapGroup != nil && before.OverlapGroupRaw != nil && string(obj["overlap_group"]) == `"`+*before.OverlapGroup+`"` {
		obj["overlap_group"], _ = json.Marshal(*before.OverlapGroupRaw)
	}
	p, errs := domain.BuildPilot(obj, domain.SourceTeam)
	if errs != nil {
		return p, validation(errs)
	}
	bj, _ := json.Marshal(before)
	pj, _ := json.Marshal(p)
	var bm, pm map[string]json.RawMessage
	_ = json.Unmarshal(bj, &bm)
	_ = json.Unmarshal(pj, &pm)
	for k, src := range p.Sources {
		if string(bm[k]) == string(pm[k]) && before.Sources[k] != "" {
			p.Sources[k] = before.Sources[k] // unchanged value keeps its original provenance (e.g. OFFICIAL_FIXTURE)
		} else if src != domain.SourceUnknown && src != domain.SourceObserved {
			p.Sources[k] = domain.SourceTeam
		}
	}
	up, err := s.Store.UpdatePilot(ctx, p, *env.Version, a)
	if err != nil {
		return p, fromStore(err, "pilot "+id)
	}
	s.invalidatePilot(ctx, id)
	s.Log.Info("pilot updated", "pilot_id", id, "version", up.Version, "actor", a.Name, "request_id", a.RequestID)
	return up, nil
}

// invalidatePilot drops cached evaluations of the pilot and every portfolio summary (cheap at this scale).
func (s *Service) invalidatePilot(ctx context.Context, id string) {
	s.Cache.InvalidatePrefix(ctx, cache.PilotPrefix(id))
	s.Cache.InvalidatePrefix(ctx, "portfolio:")
	s.Cache.InvalidatePrefix(ctx, "optimization:")
}

// EvaluateResponse wraps a deterministic evaluation with advisory AI output.
type EvaluateResponse struct {
	Evaluation    decision.Evaluation `json:"evaluation"`
	DecisionID    int64               `json:"decision_id"`
	AIStatus      string              `json:"ai_status"`
	AIExplanation *string             `json:"ai_explanation"`
	AIModel       string              `json:"ai_model,omitempty"`
	Cache         string              `json:"cache"`
}

type cachedEval struct {
	Evaluation    decision.Evaluation `json:"evaluation"`
	AIExplanation *string             `json:"ai_explanation"`
	AIModel       string              `json:"ai_model"`
}

// EvaluatePilot evaluates one pilot, persists the decision snapshot, and adds an optional AI explanation.
func (s *Service) EvaluatePilot(ctx context.Context, id string) (EvaluateResponse, error) {
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return EvaluateResponse{}, fromStore(err, "pilots")
	}
	var p *domain.Pilot
	for i := range ps {
		if ps[i].ID == id {
			p = &ps[i]
		}
	}
	if p == nil {
		return EvaluateResponse{}, notFound("pilot " + id)
	}
	pr := peers(ps, *p)
	key := cache.PilotEvaluationKey(id, cache.Fingerprint(p, pr, s.Rules, rules.Version))
	resp := EvaluateResponse{Cache: "MISS"}
	var ce cachedEval
	if b, ok := s.Cache.Get(ctx, key); ok && json.Unmarshal(b, &ce) == nil {
		resp.Cache = "HIT"
		resp.Evaluation = ce.Evaluation
		// ai_status always reports the AI service's CURRENT state; cached AI text is only shown while it is up,
		// so a cache hit behaves exactly like a fresh request (no stale "ok" during an outage).
		resp.AIStatus = s.AI.LiveStatus(ctx)
		if resp.AIStatus == aiclient.StatusOK {
			resp.AIExplanation, resp.AIModel = ce.AIExplanation, ce.AIModel
		}
	} else {
		resp.Evaluation = decision.Evaluate(*p, s.Rules, decision.Context{GroupPeers: pr})
		ex, st := s.AI.Explain(ctx, resp.Evaluation)
		resp.AIStatus = st
		if ex != nil {
			resp.AIExplanation, resp.AIModel = &ex.Explanation, ex.ModelName+"@"+ex.ModelVersion
			// only cache complete (AI-enriched) results; a degraded result is recomputed next time
			b, _ := json.Marshal(cachedEval{resp.Evaluation, resp.AIExplanation, resp.AIModel})
			s.Cache.Set(ctx, key, b, cache.TTLEvaluation)
		} else if st == aiclient.StatusDisabled {
			b, _ := json.Marshal(cachedEval{Evaluation: resp.Evaluation})
			s.Cache.Set(ctx, key, b, cache.TTLEvaluation)
		}
	}
	rec, err := s.Store.SaveDecision(ctx, resp.Evaluation)
	if err != nil {
		return resp, fromStore(err, "decision")
	}
	resp.DecisionID = rec.ID
	s.Log.Info("pilot evaluated", "pilot_id", id, "decision", resp.Evaluation.Decision, "primary_reason", resp.Evaluation.PrimaryReason,
		"rules_version", rules.Version, "ai_status", resp.AIStatus, "cache", resp.Cache)
	return resp, nil
}

// Decisions returns the persisted decision history.
func (s *Service) Decisions(ctx context.Context, id string) ([]store.DecisionRecord, error) {
	if _, err := s.Store.GetPilot(ctx, id); err != nil {
		return nil, fromStore(err, "pilot "+id)
	}
	r, err := s.Store.ListDecisions(ctx, id)
	return r, fromStore(err, "decisions")
}

// RunScenario evaluates overrides against a copy of the pilot and stores the run.
func (s *Service) RunScenario(ctx context.Context, id string, req scenario.Request) (scenario.Result, int64, error) {
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return scenario.Result{}, 0, fromStore(err, "pilots")
	}
	var base *domain.Pilot
	for i := range ps {
		if ps[i].ID == id {
			base = &ps[i]
		}
	}
	if base == nil {
		return scenario.Result{}, 0, notFound("pilot " + id)
	}
	if len(req.Name) > 80 {
		return scenario.Result{}, 0, fieldErr("name", domain.CodeTooLong, "name must be at most 80 characters")
	}
	others := make([]domain.Pilot, 0, len(ps))
	for _, q := range ps {
		if q.ID != id {
			others = append(others, q)
		}
	}
	res, errs := scenario.Run(*base, req, s.Rules, func(p domain.Pilot) []string { return peers(append(others, p), p) })
	if errs != nil {
		return res, 0, validation(errs)
	}
	ov := req.Overrides
	if len(ov) == 0 {
		ov = json.RawMessage(`{}`)
	}
	rb, _ := json.Marshal(res)
	rec, err := s.Store.SaveScenario(ctx, store.ScenarioRecord{PilotID: id, Name: req.Name, Overrides: ov, Decision: res.Scenario.Decision,
		RulesVersion: rules.Version, Result: rb})
	if err != nil {
		return res, 0, fromStore(err, "scenario")
	}
	return res, rec.ID, nil
}

// Scenarios lists stored scenario runs.
func (s *Service) Scenarios(ctx context.Context, id string) ([]store.ScenarioRecord, error) {
	if _, err := s.Store.GetPilot(ctx, id); err != nil {
		return nil, fromStore(err, "pilot "+id)
	}
	r, err := s.Store.ListScenarios(ctx, id)
	return r, fromStore(err, "scenarios")
}

// ------------------------------------------------------------------ import

// ImportResult summarises an import.
type ImportResult struct {
	Imported []string            `json:"imported"`
	Rejected []fixture.Rejected  `json:"rejected"`
	Warnings []string            `json:"warnings"`
	Errors   []domain.FieldError `json:"file_errors"`
	Source   string              `json:"source"`
	Budget   money.Amount        `json:"pilot_budget_inr"`
}

// Import validates a fixture-shaped document (+ optional extension) and stores ALL valid rows atomically.
// If any row is invalid nothing is stored (strict, all-or-nothing). Imported values are TEAM_ASSUMPTION.
func (s *Service) Import(ctx context.Context, fx, ext json.RawMessage, a Actor) (ImportResult, error) {
	r, err := fixture.Load(fx, ext, domain.SourceTeam)
	if err != nil {
		return ImportResult{}, &Error{Status: http.StatusBadRequest, Code: "MALFORMED_INPUT", Message: err.Error()}
	}
	return s.store(ctx, r, domain.SourceTeam, a)
}

// ImportOfficial loads data/raw (SHA-256 verified) — the only path that labels values OFFICIAL_FIXTURE.
func (s *Service) ImportOfficial(ctx context.Context, a Actor) (ImportResult, error) {
	r, err := fixture.LoadOfficial(s.RawDir)
	if err != nil {
		return ImportResult{}, &Error{Status: http.StatusInternalServerError, Code: "OFFICIAL_DATA_UNAVAILABLE", Message: err.Error()}
	}
	return s.store(ctx, r, domain.SourceOfficial, a)
}

func (s *Service) store(ctx context.Context, r fixture.Result, src string, a Actor) (ImportResult, error) {
	out := ImportResult{Imported: []string{}, Rejected: r.Rejected, Warnings: r.Warnings, Errors: r.Errors, Source: src, Budget: r.Budget}
	if len(r.Rejected) > 0 || len(r.Errors) > 0 {
		e := &Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED",
			Message: fmt.Sprintf("%d invalid row(s); nothing imported (all-or-nothing)", len(r.Rejected)), Details: out}
		return out, e
	}
	if err := s.Store.CreatePilots(ctx, r.Pilots, a, "import"); err != nil {
		return out, fromStore(err, "one or more pilots")
	}
	for _, p := range r.Pilots {
		out.Imported = append(out.Imported, p.ID)
		s.invalidatePilot(ctx, p.ID)
	}
	s.Log.Info("import completed", "count", len(out.Imported), "source", src, "actor", a.Name, "request_id", a.RequestID)
	return out, nil
}

// ------------------------------------------------------------------ stateless (judge-friendly, no DB)

// EvaluateAdhoc evaluates a pilot supplied in the request (nothing stored).
func (s *Service) EvaluateAdhoc(raw json.RawMessage) (decision.Evaluation, error) {
	p, errs := domain.ParsePilot(raw, domain.SourceTeam)
	if errs != nil {
		return decision.Evaluation{}, validation(errs)
	}
	return decision.Evaluate(p, s.Rules, decision.Context{}), nil
}

// OptimizeAdhoc optimizes over pilots supplied in the request (fixture shape or array; nothing stored).
func (s *Service) OptimizeAdhoc(pilots json.RawMessage, budget money.Amount, basis string) (portfolio.OptimizeResult, []fixture.Rejected, error) {
	r, err := fixture.Load(pilots, nil, domain.SourceTeam)
	if err != nil {
		return portfolio.OptimizeResult{}, nil, &Error{Status: http.StatusBadRequest, Code: "MALFORMED_INPUT", Message: err.Error()}
	}
	res, err := portfolio.Optimize(r.Pilots, s.evaluateAll(r.Pilots), budget, basis, s.Rules)
	if err != nil {
		return res, r.Rejected, fieldErr("pilots", "TOO_MANY_CANDIDATES", err.Error())
	}
	return res, r.Rejected, nil
}

// ------------------------------------------------------------------ portfolios

var (
	nameRE = regexp.MustCompile(`^[A-Za-z0-9 _.-]{1,80}$`)
	uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ValidUUID checks a portfolio ID path parameter.
func ValidUUID(s string) bool { return uuidRE.MatchString(s) }

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// PortfolioInput is the create/update body.
type PortfolioInput struct {
	Name        string                 `json:"name"`
	Budget      json.RawMessage        `json:"budget_inr"`
	Basis       string                 `json:"basis"`
	PilotIDs    []string               `json:"pilot_ids"`
	Resolutions []portfolio.Resolution `json:"overlap_resolutions"`
	Version     int                    `json:"version"`
}

// PortfolioView is what the API returns: stored record + freshly recomputed assessment.
type PortfolioView struct {
	store.Portfolio
	ApprovalStale bool   `json:"approval_stale"`
	Cache         string `json:"cache,omitempty"`
}

func (s *Service) validateInput(in PortfolioInput) (money.Amount, []domain.FieldError) {
	var errs []domain.FieldError
	if !nameRE.MatchString(in.Name) {
		errs = append(errs, domain.FieldError{Field: "name", Code: domain.CodeBadID, Message: "name must be 1-80 characters: letters, digits, space, _ . -"})
	}
	b, code := money.ParseAmount(in.Budget)
	switch {
	case code != "":
		errs = append(errs, domain.FieldError{Field: "budget_inr", Code: code, Message: "budget_inr must be a non-negative INR number"})
	case !b.Known:
		errs = append(errs, domain.FieldError{Field: "budget_inr", Code: domain.CodeRequired, Message: "budget_inr is required (null means unknown; a portfolio needs a known budget)"})
	}
	if in.Basis != "" && in.Basis != portfolio.BasisCash && in.Basis != portfolio.BasisStated {
		errs = append(errs, domain.FieldError{Field: "basis", Code: domain.CodeInvalidEnum, Message: "basis must be cash or stated"})
	}
	seen := map[string]bool{}
	for _, id := range in.PilotIDs {
		if !domain.ValidID(id) {
			errs = append(errs, domain.FieldError{Field: "pilot_ids", Code: domain.CodeBadID, Message: "invalid pilot id"})
		} else if seen[id] {
			errs = append(errs, domain.FieldError{Field: "pilot_ids", Code: "DUPLICATE_PILOT", Message: id + " is listed more than once"})
		}
		seen[id] = true
	}
	if len(in.PilotIDs) > portfolio.MaxCandidates {
		errs = append(errs, domain.FieldError{Field: "pilot_ids", Code: domain.CodeTooLong, Message: "too many pilots"})
	}
	groups := map[string]bool{}
	for _, r := range in.Resolutions {
		if groups[r.Group] {
			errs = append(errs, domain.FieldError{Field: "overlap_resolutions", Code: "DUPLICATE_RESOLUTION", Message: "one resolution per overlap group"})
		}
		groups[r.Group] = true
		if len(r.Note) > 500 {
			errs = append(errs, domain.FieldError{Field: "overlap_resolutions.review_note", Code: domain.CodeTooLong, Message: "review_note too long"})
		}
	}
	return b, errs
}

// assess loads the pilots and computes the assessment for a selection.
func (s *Service) assess(ctx context.Context, ids []string, budget money.Amount, basis string, res []portfolio.Resolution) (portfolio.Assessment, map[string]decision.Evaluation, error) {
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return portfolio.Assessment{}, nil, fromStore(err, "pilots")
	}
	by := map[string]domain.Pilot{}
	for _, p := range ps {
		by[p.ID] = p
	}
	var sel []domain.Pilot
	var unknown []domain.FieldError
	for _, id := range ids {
		p, ok := by[id]
		if !ok {
			unknown = append(unknown, domain.FieldError{Field: "pilot_ids", Code: "UNKNOWN_PILOT", Message: id + " is not a known pilot"})
			continue
		}
		sel = append(sel, p)
	}
	if unknown != nil {
		return portfolio.Assessment{}, nil, validation(unknown)
	}
	ev := s.evaluateAll(ps)
	return portfolio.Assess(portfolio.Input{Budget: budget, Pilots: sel, Evaluations: ev, Resolutions: res, Basis: basis}), ev, nil
}

// CreatePortfolio validates, assesses and saves atomically.
func (s *Service) CreatePortfolio(ctx context.Context, in PortfolioInput, a Actor) (PortfolioView, error) {
	budget, errs := s.validateInput(in)
	if errs != nil {
		return PortfolioView{}, validation(errs)
	}
	if in.Basis == "" {
		in.Basis = portfolio.BasisCash
	}
	as, _, err := s.assess(ctx, in.PilotIDs, budget, in.Basis, in.Resolutions)
	if err != nil {
		return PortfolioView{}, err
	}
	p := store.Portfolio{ID: newUUID(), Name: in.Name, Budget: budget, Basis: in.Basis, Status: as.Status, RulesVersion: rules.Version,
		PilotIDs: as.PilotIDs, Resolutions: nonNil(in.Resolutions), Assessment: as}
	saved, err := s.Store.CreatePortfolio(ctx, p, a)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio named "+in.Name)
	}
	s.Log.Info("portfolio saved", "portfolio_id", saved.ID, "status", saved.Status, "actor", a.Name, "request_id", a.RequestID)
	return PortfolioView{Portfolio: saved}, nil
}

func nonNil(r []portfolio.Resolution) []portfolio.Resolution {
	if r == nil {
		return []portfolio.Resolution{}
	}
	return r
}

// GetPortfolio returns the stored portfolio with a freshly recomputed assessment (cache-aside, content-addressed).
func (s *Service) GetPortfolio(ctx context.Context, id string) (PortfolioView, error) {
	p, err := s.Store.GetPortfolio(ctx, id)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+id)
	}
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return PortfolioView{}, fromStore(err, "pilots")
	}
	key := cache.PortfolioSummaryKey(id, cache.Fingerprint(p.Version, p.Budget, p.Basis, p.PilotIDs, p.Resolutions, ps, rules.Version))
	if b, ok := s.Cache.Get(ctx, key); ok {
		var v PortfolioView
		if json.Unmarshal(b, &v) == nil {
			v.Cache = "HIT"
			return v, nil
		}
	}
	as, _, err := s.assess(ctx, p.PilotIDs, p.Budget, p.Basis, p.Resolutions)
	if err != nil {
		var se *Error
		if errors.As(err, &se) && se.Code == "VALIDATION_FAILED" {
			// a referenced pilot disappeared: keep the stored snapshot and report it
			return PortfolioView{Portfolio: p}, nil
		}
		return PortfolioView{}, err
	}
	v := PortfolioView{Portfolio: p}
	v.Assessment = as
	if p.Status == portfolio.StatusApproved {
		v.ApprovalStale = approvalFingerprint(as) != snapshotFingerprint(p.ApprovedSnapshot)
	} else {
		v.Status = as.Status
	}
	b, _ := json.Marshal(v)
	s.Cache.Set(ctx, key, b, cache.TTLPortfolio)
	v.Cache = "MISS"
	return v, nil
}

func approvalFingerprint(a portfolio.Assessment) string {
	return cache.Fingerprint(a.PilotIDs, a.TotalSetup, a.TotalRecognized, a.TotalRunning, a.Budget, a.Ledger, a.RulesVersion)
}

func snapshotFingerprint(raw json.RawMessage) string {
	var s struct {
		Fingerprint string `json:"fingerprint"`
	}
	_ = json.Unmarshal(raw, &s)
	return s.Fingerprint
}

// ListPortfolios lists saved portfolios.
func (s *Service) ListPortfolios(ctx context.Context) ([]store.Portfolio, error) {
	r, err := s.Store.ListPortfolios(ctx)
	return r, fromStore(err, "portfolios")
}

// UpdatePortfolio replaces selection/budget/resolutions (optimistic locking). Any change clears approval.
func (s *Service) UpdatePortfolio(ctx context.Context, id string, in PortfolioInput, a Actor) (PortfolioView, error) {
	budget, errs := s.validateInput(in)
	if errs != nil {
		return PortfolioView{}, validation(errs)
	}
	if in.Version < 1 {
		return PortfolioView{}, fieldErr("version", domain.CodeRequired, "version (current record version) is required for updates")
	}
	cur, err := s.Store.GetPortfolio(ctx, id)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+id)
	}
	if in.Basis == "" {
		in.Basis = cur.Basis
	}
	return s.savePortfolio(ctx, cur, in.Name, budget, in.Basis, in.PilotIDs, nonNil(in.Resolutions), in.Version, a, "update")
}

func (s *Service) savePortfolio(ctx context.Context, cur store.Portfolio, name string, budget money.Amount, basis string, ids []string,
	res []portfolio.Resolution, version int, a Actor, action string) (PortfolioView, error) {
	as, _, err := s.assess(ctx, ids, budget, basis, res)
	if err != nil {
		return PortfolioView{}, err
	}
	p := cur
	p.Name, p.Budget, p.Basis, p.PilotIDs, p.Resolutions, p.Assessment = name, budget, basis, as.PilotIDs, res, as
	p.Status, p.RulesVersion, p.ApprovedSnapshot, p.ApprovedAt = as.Status, rules.Version, nil, nil
	saved, err := s.Store.UpdatePortfolio(ctx, p, version, a, action)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+cur.ID)
	}
	s.Cache.InvalidatePrefix(ctx, cache.PortfolioPrefix(cur.ID))
	s.Log.Info("portfolio saved", "portfolio_id", cur.ID, "action", action, "status", saved.Status, "actor", a.Name, "request_id", a.RequestID)
	return PortfolioView{Portfolio: saved}, nil
}

// OptimizeRequest controls /portfolios/:id/optimize.
type OptimizeRequest struct {
	Apply   bool `json:"apply"`
	Version int  `json:"version"`
}

// OptimizePortfolio runs exact search over ALL stored pilots for this portfolio's budget; optionally applies it.
func (s *Service) OptimizePortfolio(ctx context.Context, id string, req OptimizeRequest, a Actor) (portfolio.OptimizeResult, *PortfolioView, string, error) {
	cur, err := s.Store.GetPortfolio(ctx, id)
	if err != nil {
		return portfolio.OptimizeResult{}, nil, "", fromStore(err, "portfolio "+id)
	}
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return portfolio.OptimizeResult{}, nil, "", fromStore(err, "pilots")
	}
	key := cache.OptimizationKey(cache.Fingerprint(ps, cur.Budget, cur.Basis, s.Rules, rules.Version))
	var res portfolio.OptimizeResult
	hit := "MISS"
	if b, ok := s.Cache.Get(ctx, key); ok && json.Unmarshal(b, &res) == nil {
		hit = "HIT"
	} else {
		start := time.Now()
		res, err = portfolio.Optimize(ps, s.evaluateAll(ps), cur.Budget, cur.Basis, s.Rules)
		if err != nil {
			return res, nil, "", fieldErr("pilots", "TOO_MANY_CANDIDATES", err.Error())
		}
		b, _ := json.Marshal(res)
		s.Cache.Set(ctx, key, b, cache.TTLOptimization)
		s.Log.Info("optimizer run", "portfolio_id", id, "candidates", res.CandidatesConsidered, "combinations", res.CombinationsEvaluated,
			"selected", res.Selected, "duration_ms", time.Since(start).Milliseconds())
	}
	if !req.Apply {
		return res, nil, hit, nil
	}
	if req.Version < 1 {
		return res, nil, hit, fieldErr("version", domain.CodeRequired, "version is required when apply=true")
	}
	v, err := s.savePortfolio(ctx, cur, cur.Name, cur.Budget, cur.Basis, res.Selected, []portfolio.Resolution{}, req.Version, a, "optimize_apply")
	if err != nil {
		return res, nil, hit, err
	}
	return res, &v, hit, nil
}

// ApprovePortfolio approves only if the freshly recomputed assessment has no blockers. Snapshot is frozen.
func (s *Service) ApprovePortfolio(ctx context.Context, id string, version int, a Actor) (PortfolioView, error) {
	if version < 1 {
		return PortfolioView{}, fieldErr("version", domain.CodeRequired, "version is required")
	}
	cur, err := s.Store.GetPortfolio(ctx, id)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+id)
	}
	as, ev, err := s.assess(ctx, cur.PilotIDs, cur.Budget, cur.Basis, cur.Resolutions)
	if err != nil {
		return PortfolioView{}, err
	}
	if !as.ApprovalAllowed {
		return PortfolioView{}, &Error{Status: http.StatusConflict, Code: "APPROVAL_BLOCKED",
			Message: fmt.Sprintf("portfolio status is %s; approval requires VALID", as.Status), Details: map[string]any{"blockers": as.Blockers, "status": as.Status}}
	}
	evs := map[string]decision.Evaluation{}
	for _, pid := range as.PilotIDs {
		evs[pid] = ev[pid]
	}
	snap, _ := json.Marshal(map[string]any{"fingerprint": approvalFingerprint(as), "rules_version": rules.Version, "assessment": as,
		"pilot_evaluations": evs, "approved_by": a.Name})
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := cur
	p.Assessment, p.Status, p.ApprovedSnapshot, p.ApprovedAt, p.RulesVersion = as, portfolio.StatusApproved, snap, &now, rules.Version
	saved, err := s.Store.UpdatePortfolio(ctx, p, version, a, "approve")
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+id)
	}
	s.Cache.InvalidatePrefix(ctx, cache.PortfolioPrefix(id))
	s.Log.Info("portfolio approved", "portfolio_id", id, "actor", a.Name, "request_id", a.RequestID)
	return PortfolioView{Portfolio: saved}, nil
}

// OverlapsView is the overlap report: deterministic overlaps (authoritative) + advisory AI suggestions.
type OverlapsView struct {
	PortfolioID      string              `json:"portfolio_id"`
	Overlaps         []portfolio.Overlap `json:"overlaps"`
	UnknownOverlap   []string            `json:"pilots_with_unknown_overlap_group"`
	ResolutionTypes  []string            `json:"resolution_types"`
	AIStatus         string              `json:"ai_status"`
	AISuggestions    []aiclient.Pair     `json:"ai_suggestions"`
	AIModel          string              `json:"ai_model,omitempty"`
	AuthoritativeFor string              `json:"authoritative"`
}

// Overlaps lists overlaps for a portfolio and asks the AI service for advisory semantic suggestions.
func (s *Service) Overlaps(ctx context.Context, id string) (OverlapsView, error) {
	v, err := s.GetPortfolio(ctx, id)
	if err != nil {
		return OverlapsView{}, err
	}
	out := OverlapsView{PortfolioID: id, Overlaps: v.Assessment.Overlaps, UnknownOverlap: []string{}, AISuggestions: []aiclient.Pair{},
		ResolutionTypes:  []string{portfolio.RemoveOne, portfolio.ExcludeSecond, portfolio.SplitBenefit, portfolio.CustomAllocation},
		AuthoritativeFor: "overlap_group rules decide accounting and approval; ai_suggestions are advisory only"}
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return out, fromStore(err, "pilots")
	}
	in := map[string]bool{}
	for _, pid := range v.PilotIDs {
		in[pid] = true
	}
	var texts []aiclient.Text
	for _, p := range ps {
		if !in[p.ID] {
			continue
		}
		if p.OverlapGroup == nil {
			out.UnknownOverlap = append(out.UnknownOverlap, p.ID)
		}
		t := p.Task
		if p.Description != nil {
			t += ". " + *p.Description
		}
		texts = append(texts, aiclient.Text{ID: p.ID, Text: t, OverlapGroup: p.OverlapGroup})
	}
	if len(texts) < 2 {
		out.AIStatus = aiclient.StatusOK
		if !s.AI.Enabled() {
			out.AIStatus = aiclient.StatusDisabled
		}
		return out, nil
	}
	key := cache.SemanticKey(cache.Fingerprint(texts))
	var r *aiclient.OverlapResponse
	if b, ok := s.Cache.Get(ctx, key); ok {
		var cr aiclient.OverlapResponse
		if json.Unmarshal(b, &cr) == nil {
			// cached suggestions are only served while the AI service is actually up (status = live state)
			if st := s.AI.LiveStatus(ctx); st == aiclient.StatusOK {
				r, out.AIStatus = &cr, st
			} else {
				out.AIStatus = st
				return out, nil
			}
		}
	}
	if r == nil {
		r, out.AIStatus = s.AI.SemanticOverlap(ctx, texts)
		if r != nil {
			b, _ := json.Marshal(r)
			s.Cache.Set(ctx, key, b, cache.TTLSemantic)
			var preds []store.Prediction
			for _, p := range r.Pairs {
				inb, _ := json.Marshal(map[string]any{"pair": []string{p.A, p.B}, "texts": texts})
				preds = append(preds, store.Prediction{PilotID: p.A, RelatedPilotID: p.B, ModelName: r.ModelName, ModelVersion: r.ModelVersion,
					Prediction: fmt.Sprintf("possible_overlap=%t", p.PossibleOverlap), Probability: p.Similarity, Input: inb})
			}
			if err := s.Store.SavePredictions(ctx, preds); err != nil {
				s.Log.Warn("could not store advisory predictions", "error", err.Error())
			}
		}
	}
	if r != nil {
		out.AISuggestions, out.AIModel = r.Pairs, r.ModelName+"@"+r.ModelVersion
	}
	return out, nil
}

// ResolveOverlap stores a reviewer resolution for one overlap group and recomputes the portfolio.
// REMOVE_ONE removes the non-primary pilots from the selection.
func (s *Service) ResolveOverlap(ctx context.Context, id, group string, r portfolio.Resolution, version int, a Actor) (PortfolioView, error) {
	if version < 1 {
		return PortfolioView{}, fieldErr("version", domain.CodeRequired, "version is required")
	}
	cur, err := s.Store.GetPortfolio(ctx, id)
	if err != nil {
		return PortfolioView{}, fromStore(err, "portfolio "+id)
	}
	as, _, err := s.assess(ctx, cur.PilotIDs, cur.Budget, cur.Basis, nil)
	if err != nil {
		return PortfolioView{}, err
	}
	var ov *portfolio.Overlap
	for i := range as.Overlaps {
		if as.Overlaps[i].Group == group {
			ov = &as.Overlaps[i]
		}
	}
	if ov == nil {
		return PortfolioView{}, notFound("overlap " + group + " in portfolio " + id)
	}
	r.Group = group
	if len(r.Note) > 500 {
		return PortfolioView{}, fieldErr("review_note", domain.CodeTooLong, "review_note must be at most 500 characters")
	}
	ids := cur.PilotIDs
	claims := map[string]money.Amount{}
	for _, l := range as.Ledger {
		if l.BenefitGroup != nil && *l.BenefitGroup == group {
			claims[l.PilotID] = l.BasisBenefit
			if !l.BasisBenefit.Known {
				claims[l.PilotID] = money.Rupees(0)
			}
		}
	}
	if r.Type == portfolio.RemoveOne {
		keep := false
		for _, pid := range ov.PilotIDs {
			keep = keep || pid == r.PrimaryPilotID
		}
		if !keep {
			return PortfolioView{}, fieldErr("primary_pilot_id", "NOT_IN_GROUP", "primary_pilot_id must be one of "+fmt.Sprint(ov.PilotIDs))
		}
		drop := map[string]bool{}
		for _, pid := range ov.PilotIDs {
			drop[pid] = pid != r.PrimaryPilotID
		}
		var kept []string
		for _, pid := range ids {
			if !drop[pid] {
				kept = append(kept, pid)
			}
		}
		ids = kept
	} else if err := portfolio.ValidateResolution(r, ov.PilotIDs, claims); err != nil {
		return PortfolioView{}, fieldErr("resolution", "INVALID_RESOLUTION", err.Error())
	}
	res := []portfolio.Resolution{}
	for _, x := range cur.Resolutions {
		if x.Group != group {
			res = append(res, x)
		}
	}
	res = append(res, r)
	s.Log.Info("overlap resolved", "portfolio_id", id, "group", group, "type", r.Type, "actor", a.Name, "request_id", a.RequestID)
	return s.savePortfolio(ctx, cur, cur.Name, cur.Budget, cur.Basis, ids, res, version, a, "resolve_overlap:"+group)
}

// Audit returns the audit trail.
func (s *Service) Audit(ctx context.Context, entity, id string) ([]store.AuditEntry, error) {
	r, err := s.Store.ListAudit(ctx, entity, id)
	return r, fromStore(err, "audit")
}
