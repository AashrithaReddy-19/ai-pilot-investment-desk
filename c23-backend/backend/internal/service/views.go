package service

import (
	"context"
	"sort"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/fixture"
	"c23desk/internal/money"
	"c23desk/internal/portfolio"
	"c23desk/internal/rules"
	"c23desk/internal/store"
)

// Read-only views for API clients. They aggregate engine output; they never recompute finance themselves.

// Defaults are team-authored planning defaults (from data/raw/extension.json, SHA-256 verified).
type Defaults struct {
	PilotBudget  money.Amount     `json:"pilot_budget_inr"`
	BudgetSource string           `json:"pilot_budget_source"`
	Thresholds   rules.Thresholds `json:"thresholds"`
	RulesVersion string           `json:"rules_version"`
}

// Defaults loads the planning defaults.
func (s *Service) Defaults() Defaults {
	d := Defaults{Thresholds: s.Rules, RulesVersion: rules.Version, BudgetSource: domain.SourceTeam + " (extension.json)"}
	if r, err := fixture.LoadOfficial(s.RawDir); err == nil && r.BudgetOK {
		d.PilotBudget = r.Budget
	} else {
		d.BudgetSource = domain.SourceUnknown
	}
	return d
}

// Dashboard is the executive summary.
type Dashboard struct {
	RulesVersion         string                    `json:"rules_version"`
	Defaults             Defaults                  `json:"defaults"`
	PilotCount           int                       `json:"pilot_count"`
	DecisionCounts       map[string]int            `json:"decision_counts"`
	MissingEvidenceCount int                       `json:"missing_evidence_count"`
	UnknownFinanceCount  int                       `json:"unknown_finance_count"`
	SharedOverlapGroups  map[string][]string       `json:"shared_overlap_groups"`
	Pilots               []PilotView               `json:"pilots"`
	Portfolio            *PortfolioView            `json:"portfolio"`
	PortfolioSource      string                    `json:"portfolio_source"` // saved | recommended | none
	Recommended          *portfolio.OptimizeResult `json:"recommended"`
	UnresolvedOverlaps   int                       `json:"unresolved_overlap_count"`
}

// Dashboard summarises pilots and one portfolio (the given one, else the newest saved, else the optimizer's recommendation).
func (s *Service) Dashboard(ctx context.Context, portfolioID string) (Dashboard, error) {
	views, err := s.ListPilots(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	ps := make([]domain.Pilot, 0, len(views))
	for _, v := range views {
		ps = append(ps, v.Pilot)
	}
	d := Dashboard{RulesVersion: rules.Version, Defaults: s.Defaults(), PilotCount: len(views), Pilots: views,
		DecisionCounts: map[string]int{rules.Proceed: 0, rules.Revise: 0, rules.Stop: 0, rules.Review: 0}, SharedOverlapGroups: map[string][]string{},
		PortfolioSource: "none"}
	groups := map[string][]string{}
	for _, v := range views {
		d.DecisionCounts[v.Decision]++
		for _, c := range v.ReasonCodes {
			switch c {
			case rules.ReasonInsufficientEvidence:
				d.MissingEvidenceCount++
			}
		}
		if !v.Pilot.FinanceComplete() {
			d.UnknownFinanceCount++
		}
		if g := v.Pilot.GroupKey(); g != "" {
			groups[g] = append(groups[g], v.Pilot.ID)
		}
	}
	for g, ids := range groups {
		if len(ids) > 1 {
			sort.Strings(ids)
			d.SharedOverlapGroups[g] = ids
		}
	}
	if portfolioID == "" {
		if list, err := s.Store.ListPortfolios(ctx); err == nil && len(list) > 0 {
			portfolioID = list[0].ID // newest ...
			for _, p := range list {
				if p.Status == portfolio.StatusApproved { // ... unless an approved portfolio exists (newest approved wins)
					portfolioID = p.ID
					break
				}
			}
		}
	}
	if portfolioID != "" {
		v, err := s.GetPortfolio(ctx, portfolioID)
		if err != nil {
			return d, err
		}
		d.Portfolio, d.PortfolioSource = &v, "saved"
		for _, o := range v.Assessment.Overlaps {
			if o.Status == "UNRESOLVED" {
				d.UnresolvedOverlaps++
			}
		}
	} else if d.Defaults.PilotBudget.Known {
		r, err := portfolio.Optimize(ps, s.evaluateAll(ps), d.Defaults.PilotBudget, portfolio.BasisCash, s.Rules)
		if err == nil {
			d.Recommended, d.PortfolioSource = &r, "recommended"
		}
	}
	return d, nil
}

// PreviewInput is a portfolio selection to assess without saving.
type PreviewInput struct {
	Budget      money.Amount           `json:"budget_inr"`
	Basis       string                 `json:"basis"`
	PilotIDs    []string               `json:"pilot_ids"`
	Resolutions []portfolio.Resolution `json:"overlap_resolutions"`
}

// PreviewPortfolio assesses a selection of stored pilots without saving (live portfolio builder).
func (s *Service) PreviewPortfolio(ctx context.Context, in PreviewInput) (portfolio.Assessment, map[string]decision.Evaluation, error) {
	if !in.Budget.Known {
		return portfolio.Assessment{}, nil, fieldErr("budget_inr", domain.CodeRequired, "budget_inr is required")
	}
	if in.Basis == "" {
		in.Basis = portfolio.BasisCash
	}
	if in.Basis != portfolio.BasisCash && in.Basis != portfolio.BasisStated {
		return portfolio.Assessment{}, nil, fieldErr("basis", domain.CodeInvalidEnum, "basis must be cash or stated")
	}
	seen := map[string]bool{}
	for _, id := range in.PilotIDs {
		if seen[id] {
			return portfolio.Assessment{}, nil, fieldErr("pilot_ids", "DUPLICATE_PILOT", id+" is listed more than once")
		}
		seen[id] = true
	}
	a, ev, err := s.assess(ctx, in.PilotIDs, in.Budget, in.Basis, in.Resolutions)
	if err != nil {
		return a, nil, err
	}
	sel := map[string]decision.Evaluation{}
	for _, id := range a.PilotIDs {
		sel[id] = ev[id]
	}
	return a, sel, nil
}

// PreviewOptimize runs the optimizer over all stored pilots for a given budget (nothing saved).
func (s *Service) PreviewOptimize(ctx context.Context, budget money.Amount, basis string) (portfolio.OptimizeResult, error) {
	if !budget.Known {
		return portfolio.OptimizeResult{}, fieldErr("budget_inr", domain.CodeRequired, "budget_inr is required")
	}
	ps, err := s.Store.ListPilots(ctx)
	if err != nil {
		return portfolio.OptimizeResult{}, fromStore(err, "pilots")
	}
	r, err := portfolio.Optimize(ps, s.evaluateAll(ps), budget, basis, s.Rules)
	if err != nil {
		return r, fieldErr("pilots", "TOO_MANY_CANDIDATES", err.Error())
	}
	return r, nil
}

// RecentActivity returns the latest audit entries across all entities.
func (s *Service) RecentActivity(ctx context.Context, limit int) ([]store.AuditEntry, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	r, err := s.Store.ListRecentAudit(ctx, limit)
	return r, fromStore(err, "audit")
}
