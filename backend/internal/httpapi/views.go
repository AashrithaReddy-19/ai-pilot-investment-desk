package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
)

// Workspace views (B3): the dashboard, decision, comparison and yearly savings built from the workspace's
// stored proposals and budget settings. The gateway only assembles the engine request and keeps the answer
// as a snapshot; every number comes from the engine, nothing is recalculated here.

// ---------------------------------------------------------------- settings

var (
	budgetSetKeys = map[string]bool{"total_inr": true, "first_year_inr": true, "recurring_inr_per_year": true,
		"implementation_inr": true, "contingency_pct": true}
	engineDefaultKeys = map[string]bool{"horizon_months": true, "discount_rate": true, "tokens_per_month": true,
		"price_per_1m_tokens": true, "ai_fixed_cost_inr_per_year": true, "annual_usage_growth_pct": true,
		"annual_price_drift_pct": true, "target_adoption_pct": true, "quality_threshold_pct": true,
		"assumed_run_cost_pct_of_setup": true, "assumed_adoption_pct": true}
	weightKeys = map[string]bool{"net_annual": true, "npv": true, "payback": true, "evidence": true, "readiness": true, "cash_share": true}
)

// Settings is the workspace's decision context: what budget and conventions every workspace view uses.
type Settings struct {
	Budget    *float64           `json:"budget"`
	Mode      string             `json:"mode,omitempty"`
	BudgetSet map[string]float64 `json:"budget_set,omitempty"`
	Defaults  map[string]float64 `json:"defaults,omitempty"`
	Weights   map[string]float64 `json:"weights,omitempty"`
}

func numberMap(raw json.RawMessage, allowed map[string]bool, field string, nonNeg bool) (map[string]float64, error) {
	if len(raw) == 0 || isNull(raw) {
		return nil, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	out := map[string]float64{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !allowed[k] {
			return nil, fmt.Errorf("%s.%s is not a known setting", field, k)
		}
		if isNull(m[k]) {
			continue // null withdraws the value; missing is never zero
		}
		var f float64
		if err := json.Unmarshal(m[k], &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("%s.%s must be a number", field, k)
		}
		if nonNeg && f < 0 {
			return nil, fmt.Errorf("%s.%s must not be negative", field, k)
		}
		out[k] = f
	}
	return out, nil
}

func parseSettings(raw json.RawMessage) (Settings, error) {
	var s Settings
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return s, errors.New("settings must be a JSON object")
	}
	for k := range m {
		switch k {
		case "budget", "mode", "budget_set", "defaults", "weights":
		default:
			return s, fmt.Errorf("%s is not a known setting (budget, mode, budget_set, defaults, weights)", k)
		}
	}
	if b, ok := m["budget"]; ok && !isNull(b) {
		var f float64
		if json.Unmarshal(b, &f) != nil || f < 0 || math.IsInf(f, 0) {
			return s, errors.New("budget must be a non-negative number or null")
		}
		s.Budget = &f
	}
	if v, ok := m["mode"]; ok && !isNull(v) {
		if json.Unmarshal(v, &s.Mode) != nil || (s.Mode != "independent" && s.Mode != "conservative") {
			return s, errors.New("mode must be independent or conservative")
		}
	}
	var err error
	if s.BudgetSet, err = numberMap(m["budget_set"], budgetSetKeys, "budget_set", true); err != nil {
		return s, err
	}
	if s.Defaults, err = numberMap(m["defaults"], engineDefaultKeys, "defaults", false); err != nil {
		return s, err
	}
	if s.Weights, err = numberMap(m["weights"], weightKeys, "weights", true); err != nil {
		return s, err
	}
	return s, nil
}

func (h *Handlers) loadSettings(c *gin.Context) (Settings, bool) {
	raw, err := h.platform.GetSettings(c.Request.Context())
	if err != nil {
		h.storeErr(c, err)
		return Settings{}, false
	}
	s, err := parseSettings(raw)
	if err != nil { // stored settings were written by PutSettings, so this is corruption, not user error
		h.log.Error("stored workspace settings are unreadable", "err", err, "request_id", reqID(c))
		return Settings{}, true
	}
	return s, true
}

func (h *Handlers) getSettings(c *gin.Context) {
	s, ok := h.loadSettings(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, s)
}

// putSettings — PUT /api/v1/settings: replace the workspace's budget settings (admin).
func (h *Handlers) putSettings(c *gin.Context) {
	b, ok := h.readBody(c)
	if !ok {
		return
	}
	s, err := parseSettings(b)
	if err != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "")
		return
	}
	clean, _ := json.Marshal(s)
	if _, err := h.platform.PutSettings(c.Request.Context(), clean); err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "settings.update", "settings", model.WorkspaceID(c.Request.Context()), nil)
	c.JSON(http.StatusOK, s)
}

// ---------------------------------------------------------------- proposal set → engine request

// proposalEngineKeys are the stored-input keys the engine's proposal shape understands.
var proposalEngineKeys = []string{"setup_cost_inr", "annual_benefit_inr", "ai_running_cost_inr", "benefit_type", "readiness",
	"adoption_pct", "ramp_months", "delay_months", "overlap_group", "observed_adoption_pct", "observed_quality_pct",
	"one_time_costs", "recurring_costs", "benefit_driver", "baseline", "cashable_share_pct", "money_source"}

// engineProposal maps a stored proposal to the engine's proposal object. The latest linked observation
// fills observed adoption/quality when the inputs do not state them.
func engineProposal(p model.Proposal, obs *model.Observation) (map[string]json.RawMessage, error) {
	var in map[string]json.RawMessage
	if len(p.Inputs) > 0 && !isNull(p.Inputs) {
		if err := json.Unmarshal(p.Inputs, &in); err != nil {
			return nil, fmt.Errorf("proposal %q: stored inputs are not an object", p.Name)
		}
	}
	if in == nil {
		in = map[string]json.RawMessage{}
	}
	if _, ok := in["ai_running_cost_inr"]; !ok {
		if v, ok := in["annual_running_cost_inr"]; ok {
			in["ai_running_cost_inr"] = v
		}
	}
	out := map[string]json.RawMessage{"name": mustJSON(p.Name)}
	for _, k := range proposalEngineKeys {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	for _, k := range []string{"setup_cost_inr", "annual_benefit_inr"} {
		if v, ok := out[k]; !ok || isNull(v) {
			return nil, fmt.Errorf("proposal %q has no %s in its inputs; edit the proposal first", p.Name, k)
		}
	}
	if obs != nil {
		if _, ok := out["observed_adoption_pct"]; !ok && obs.AdoptionPct != nil {
			out["observed_adoption_pct"] = mustJSON(*obs.AdoptionPct)
		}
		if _, ok := out["observed_quality_pct"]; !ok && obs.QualityPct != nil {
			out["observed_quality_pct"] = mustJSON(*obs.QualityPct)
		}
	}
	return out, nil
}

type viewRequest struct {
	ProposalIDs []string        `json:"proposal_ids"`
	UseFixture  bool            `json:"use_fixture"`
	Budget      json.RawMessage `json:"budget"` // overrides the settings budget when present (null = no budget)
	Mode        *string         `json:"mode"`
	AIBurn      *float64        `json:"ai_burn_to_date"`
	Weights     json.RawMessage `json:"weights"`
}

// buildEngineRequest turns stored proposals + workspace settings into the engine's proposal-set body.
// kind decides which fields the engine endpoint accepts.
func (h *Handlers) buildEngineRequest(c *gin.Context, kind string) ([]byte, []model.SnapshotProposal, bool) {
	b, ok := h.readBody(c)
	if !ok {
		return nil, nil, false
	}
	var vr viewRequest
	if err := json.Unmarshal(b, &vr); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "body must be {proposal_ids?, use_fixture?, budget?, mode?, ai_burn_to_date?, weights?}", "")
		return nil, nil, false
	}
	if len(vr.ProposalIDs) > 8 && kind == "comparison" || len(vr.ProposalIDs) > 50 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "too many proposal_ids", "proposal_ids")
		return nil, nil, false
	}
	for _, id := range vr.ProposalIDs {
		if !validUUID(id) {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "proposal_ids must be UUIDs", "proposal_ids")
			return nil, nil, false
		}
	}
	settings, ok := h.loadSettings(c)
	if !ok {
		return nil, nil, false
	}
	body := map[string]any{}
	var used []model.SnapshotProposal
	if !vr.UseFixture {
		props, err := h.platform.ProposalsForEngine(c.Request.Context(), vr.ProposalIDs)
		if errors.Is(err, model.ErrUnknownProposal) {
			fail(c, http.StatusUnprocessableEntity, "unknown_proposal", err.Error(), "proposal_ids")
			return nil, nil, false
		}
		if err != nil {
			h.storeErr(c, err)
			return nil, nil, false
		}
		if len(props) == 0 {
			fail(c, http.StatusUnprocessableEntity, "no_proposals",
				"this workspace has no active proposals; create proposals or send use_fixture: true for the shipped case", "proposal_ids")
			return nil, nil, false
		}
		list := make([]map[string]json.RawMessage, 0, len(props))
		for _, p := range props {
			obs, err := h.platform.LatestProposalObservation(c.Request.Context(), p.ID)
			if err != nil && !errors.Is(err, model.ErrNotFound) {
				h.storeErr(c, err)
				return nil, nil, false
			}
			ep, err := engineProposal(p, obs)
			if err != nil {
				fail(c, http.StatusUnprocessableEntity, "incomplete_proposal", err.Error(), "proposal_ids")
				return nil, nil, false
			}
			list = append(list, ep)
			used = append(used, model.SnapshotProposal{ID: p.ID, Name: p.Name, Version: p.Version})
		}
		body["proposals"] = list
	}
	// budget: request override > settings > none
	switch {
	case vr.Budget != nil && isNull(vr.Budget):
		body["budget"] = nil
	case vr.Budget != nil:
		var f float64
		if json.Unmarshal(vr.Budget, &f) != nil || f < 0 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "budget must be a non-negative number or null", "budget")
			return nil, nil, false
		}
		body["budget"] = f
	case settings.Budget != nil:
		body["budget"] = *settings.Budget
	default:
		body["budget"] = nil
	}
	mode := settings.Mode
	if vr.Mode != nil {
		mode = *vr.Mode
	}
	if mode != "" {
		body["mode"] = mode
	}
	if len(settings.Defaults) > 0 {
		body["extension"] = map[string]any{"defaults": settings.Defaults}
	}
	if kind != "comparison" {
		if len(settings.BudgetSet) > 0 {
			body["budget_set"] = settings.BudgetSet
		}
		switch {
		case vr.Weights != nil && !isNull(vr.Weights):
			body["weights"] = vr.Weights
		case len(settings.Weights) > 0:
			body["weights"] = settings.Weights
		}
		if vr.AIBurn != nil {
			body["ai_burn_to_date"] = *vr.AIBurn
		}
	}
	if vr.UseFixture && kind == "comparison" {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "comparison needs stored proposals (2 to 8)", "use_fixture")
		return nil, nil, false
	}
	out, _ := json.Marshal(body)
	return out, used, true
}

var viewPaths = map[string]string{"dashboard": "/views/dashboard", "decision": "/decision",
	"comparison": "/compare", "yearly_savings": "/views/yearly-savings"}

// workspaceView — POST /api/v1/workspace/{dashboard|decision|comparison|yearly-savings}: run the engine on
// the stored proposals and keep the answer as a reproducible snapshot.
func (h *Handlers) workspaceView(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqBody, used, ok := h.buildEngineRequest(c, kind)
		if !ok {
			return
		}
		r, ok := h.post(c, http.MethodPost, viewPaths[kind], reqBody, true)
		if !ok {
			return
		}
		rv := h.version()
		cv := contractVersion(r.Body)
		snap, err := h.platform.SaveSnapshot(c.Request.Context(), model.SnapshotIn{
			Kind: kind, Request: reqBody, Response: r.Body, Proposals: used, RulesVersion: strOrNil(rv), ContractVersion: cv,
		})
		if err != nil {
			h.storeErr(c, err)
			return
		}
		h.record(c, "snapshot.create", "snapshot", snap.ID, gin.H{"kind": kind, "proposals": len(used)})
		c.JSON(http.StatusOK, gin.H{"snapshot": snap, "result": json.RawMessage(r.Body), "stale": false})
	}
}

// workspaceExport — the decision report and yearly CSV for the stored proposals, as downloads.
func (h *Handlers) workspaceExport(path, contentType, filename string) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqBody, used, ok := h.buildEngineRequest(c, "dashboard")
		if !ok {
			return
		}
		r, ok := h.post(c, http.MethodPost, path, reqBody, true)
		if !ok {
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		h.record(c, "export.download", "export", filename, gin.H{"proposals": len(used)})
		c.Data(http.StatusOK, contentType, r.Body)
	}
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// contractVersion reads header.contract_version from a dashboard answer, when present.
func contractVersion(body []byte) *string {
	var d struct {
		Header *struct {
			ContractVersion json.RawMessage `json:"contract_version"`
		} `json:"header"`
	}
	if json.Unmarshal(body, &d) != nil || d.Header == nil || len(d.Header.ContractVersion) == 0 {
		return nil
	}
	v := strings.Trim(string(d.Header.ContractVersion), `"`)
	return &v
}

// ---------------------------------------------------------------- snapshots

func (h *Handlers) listSnapshots(c *gin.Context) {
	kind := c.Query("kind")
	if kind != "" && viewPaths[kind] == "" {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "kind must be dashboard, decision, comparison or yearly_savings", "kind")
		return
	}
	list, err := h.platform.ListSnapshots(c.Request.Context(), kind, queryLimit(c, 20))
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshots": list})
}

// staleness says why a stored snapshot no longer matches what a new run would use.
func (h *Handlers) staleness(c *gin.Context, s *model.Snapshot) (gin.H, bool) {
	ids := make([]string, 0, len(s.Proposals))
	for _, p := range s.Proposals {
		ids = append(ids, p.ID)
	}
	now, err := h.platform.ProposalVersions(c.Request.Context(), ids)
	if err != nil {
		h.storeErr(c, err)
		return nil, false
	}
	var reasons []string
	for _, p := range s.Proposals {
		v, ok := now[p.ID]
		switch {
		case !ok:
			reasons = append(reasons, fmt.Sprintf("proposal %q was deleted", p.Name))
		case v != p.Version:
			reasons = append(reasons, fmt.Sprintf("proposal %q inputs changed (v%d → v%d)", p.Name, p.Version, v))
		}
	}
	cur := h.version()
	if s.RulesVersion != nil && cur != "" && *s.RulesVersion != cur {
		reasons = append(reasons, fmt.Sprintf("rules engine changed (rules version %s → %s)", *s.RulesVersion, cur))
	}
	if reasons == nil {
		reasons = []string{}
	}
	return gin.H{"stale": len(reasons) > 0, "reasons": reasons, "rules_version_now": strOrNil(cur)}, true
}

func (h *Handlers) getSnapshot(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	s, err := h.platform.GetSnapshot(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	st, ok := h.staleness(c, s)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": s, "result": s.Response, "staleness": st, "stale": st["stale"]})
}

// replaySnapshot — POST /api/v1/snapshots/:id/replay: re-run the stored request (uncached) and report whether
// today's engine reproduces the stored answer exactly.
func (h *Handlers) replaySnapshot(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	s, err := h.platform.GetSnapshot(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	r, ok := h.post(c, http.MethodPost, viewPaths[s.Kind], s.Request, false)
	if !ok {
		return
	}
	matches := sameJSON(s.Response, r.Body)
	c.JSON(http.StatusOK, gin.H{"snapshot_id": s.ID, "kind": s.Kind, "matches": matches,
		"rules_version_then": s.RulesVersion, "rules_version_now": strOrNil(h.version()),
		"note": map[bool]string{true: "The engine reproduces the stored answer exactly.",
			false: "Today's engine gives a different answer for the stored request; compare rules versions."}[matches]})
}

func sameJSON(a, b []byte) bool {
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// ---------------------------------------------------------------- approvals

var approvalDecisions = map[string]bool{"approved": true, "rejected": true, "changes_requested": true}

// addApproval — POST /api/v1/proposals/:id/approvals {decision, comment?, snapshot_id?} (admin).
// The approval names the proposal's current inputs version; a later edit makes it stale.
func (h *Handlers) addApproval(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	var decision string
	if json.Unmarshal(in["decision"], &decision) != nil || !approvalDecisions[decision] {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "decision must be approved, rejected or changes_requested", "decision")
		return
	}
	a := model.ApprovalIn{ProposalID: id, Decision: decision}
	if raw, present := in["comment"]; present && !isNull(raw) {
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) > 2000 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "comment must be a string of at most 2000 characters", "comment")
			return
		}
		a.Comment = &s
	}
	if raw, present := in["snapshot_id"]; present && !isNull(raw) {
		var s string
		if json.Unmarshal(raw, &s) != nil || !validUUID(s) {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "snapshot_id must be a UUID", "snapshot_id")
			return
		}
		a.SnapshotID = &s
		// Record the engine outcome the approver saw for this proposal, when the snapshot holds a decision.
		if snap, err := h.platform.GetSnapshot(c.Request.Context(), s); err == nil {
			a.EngineOutcome = outcomeFor(snap, id)
		}
	}
	saved, err := h.platform.AddApproval(c.Request.Context(), a)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "no such proposal or snapshot in this workspace", "")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "approval.create", "approval", fmt.Sprint(saved.ID),
		gin.H{"proposal_id": id, "proposal_version": saved.ProposalVersion, "decision": decision})
	c.JSON(http.StatusCreated, saved)
}

// outcomeFor finds the engine's decision outcome for one proposal inside a dashboard/decision snapshot.
func outcomeFor(s *model.Snapshot, proposalID string) *string {
	name := ""
	for _, p := range s.Proposals {
		if p.ID == proposalID {
			name = p.Name
		}
	}
	if name == "" {
		return nil
	}
	var d struct {
		Proposals []struct {
			ID      string `json:"id"`
			Outcome string `json:"outcome"`
		} `json:"proposals"`
		Decisions map[string]struct {
			Outcome string `json:"outcome"`
		} `json:"decisions"`
	}
	if json.Unmarshal(s.Response, &d) != nil {
		return nil
	}
	if dd, ok := d.Decisions[name]; ok && dd.Outcome != "" {
		return &dd.Outcome
	}
	for _, p := range d.Proposals {
		if p.ID == name && p.Outcome != "" {
			o := p.Outcome
			return &o
		}
	}
	return nil
}

func (h *Handlers) listApprovals(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	list, err := h.platform.ListApprovals(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approvals": list})
}
