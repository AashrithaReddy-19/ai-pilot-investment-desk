// Package portfolio assesses a pilot selection: budget, overlap accounting, benefit ledger, totals and status.
//
// Overlap rule (c23-rules-v1): pilots sharing a known overlap_group draw on ONE benefit pool, whose size is
// the largest single claim in the group (the same hours/revenue cannot be saved twice). Until resolved,
// only the largest claim is recognised (conservative) and approval is blocked.
package portfolio

import (
	"fmt"
	"math/big"
	"sort"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/finance"
	"c23desk/internal/money"
	"c23desk/internal/rules"
)

// Portfolio status values.
const (
	StatusDraft       = "DRAFT"
	StatusNeedsReview = "NEEDS_REVIEW"
	StatusValid       = "VALID"
	StatusApproved    = "APPROVED"
	StatusRejected    = "REJECTED"
)

// Benefit bases.
const (
	BasisCash   = "cash"   // default: only cash-recognisable benefit (time freed is not cash)
	BasisStated = "stated" // annual_benefit_inr as supplied
)

// Overlap resolution types.
const (
	RemoveOne        = "REMOVE_ONE"        // keep primary, remove the others from the portfolio
	ExcludeSecond    = "EXCLUDE_SECOND"    // keep all, count only primary's benefit (others still cost money)
	SplitBenefit     = "SPLIT_BENEFIT"     // share the pool (default equal shares, capped at each claim)
	CustomAllocation = "CUSTOM_ALLOCATION" // explicit INR per pilot; each <= own claim, sum <= pool
)

// Issue codes.
const (
	CodeBudgetMissing     = "BUDGET_MISSING"
	CodeBudgetExceeded    = "BUDGET_EXCEEDED"
	CodeStopSelected      = "STOP_PILOT_SELECTED"
	CodeFinanceIncomplete = "FINANCE_INCOMPLETE"
	CodeDoubleCountRisk   = "DOUBLE_COUNT_RISK"
	CodeOverlapUnresolved = "OVERLAP_UNRESOLVED"
	CodeOverlapUnknown    = "OVERLAP_UNKNOWN"
	CodeStaleResolution   = "STALE_RESOLUTION"
	CodeNotCash           = "TIME_NOT_CASH"
	CodeRevisePilot       = "PILOT_NEEDS_REVISION"
)

// Resolution is a reviewer's decision for one overlap group in one portfolio.
type Resolution struct {
	Group          string                  `json:"overlap_group"`
	Type           string                  `json:"resolution_type"`
	PrimaryPilotID string                  `json:"primary_pilot_id,omitempty"`
	SharesPct      map[string]int64        `json:"shares_pct,omitempty"`
	Allocations    map[string]money.Amount `json:"allocations_inr,omitempty"`
	Note           string                  `json:"review_note"`
}

// LedgerEntry is one auditable line of the benefit attribution ledger.
type LedgerEntry struct {
	PilotID           string       `json:"pilot_id"`
	BenefitGroup      *string      `json:"benefit_group"`
	ClaimedBenefit    money.Amount `json:"claimed_benefit_inr"`    // annual_benefit_inr as supplied
	BasisBenefit      money.Amount `json:"basis_benefit_inr"`      // after cash rule (cash basis) or = claimed (stated)
	RecognizedBenefit money.Amount `json:"recognized_benefit_inr"` // after overlap accounting; what totals count
	RunningCost       money.Amount `json:"annual_running_cost_inr"`
	Source            string       `json:"source"`
	Resolution        string       `json:"resolution"`
	ReviewStatus      string       `json:"review_status"` // AUTO | UNRESOLVED | RESOLVED | NOT_RECOGNIZED
	ReviewNote        string       `json:"review_note"`
}

// Overlap describes one group with 2+ selected pilots.
type Overlap struct {
	ID              string       `json:"id"`
	Group           string       `json:"overlap_group"`
	PilotIDs        []string     `json:"pilot_ids"`
	Code            string       `json:"code"`
	Status          string       `json:"status"` // UNRESOLVED | RESOLVED
	Resolution      *Resolution  `json:"resolution"`
	ClaimedTotal    money.Amount `json:"claimed_total_inr"`
	PoolInr         money.Amount `json:"benefit_pool_inr"`
	RecognizedTotal money.Amount `json:"recognized_total_inr"`
}

// Issue is a blocker or warning.
type Issue struct {
	Code    string `json:"code"`
	PilotID string `json:"pilot_id,omitempty"`
	Message string `json:"message"`
}

// Assessment is the full, explainable portfolio result.
type Assessment struct {
	RulesVersion    string            `json:"rules_version"`
	Basis           string            `json:"basis"`
	Status          string            `json:"status"`
	ApprovalAllowed bool              `json:"approval_allowed"`
	Budget          money.Amount      `json:"budget_inr"`
	PilotIDs        []string          `json:"pilot_ids"`
	TotalSetup      money.Amount      `json:"total_setup_cost_inr"`
	TotalRunning    money.Amount      `json:"total_annual_running_cost_inr"`
	TotalClaimed    money.Amount      `json:"total_claimed_benefit_inr"`
	TotalRecognized money.Amount      `json:"total_recognized_benefit_inr"`
	NetAnnual       money.Dec         `json:"net_annual_benefit_inr"`
	MonthlyNet      money.Dec         `json:"monthly_net_benefit_inr"`
	Payback         finance.Payback   `json:"payback"`
	BudgetRemaining money.Dec         `json:"budget_remaining_inr"`
	Ledger          []LedgerEntry     `json:"benefit_ledger"`
	Overlaps        []Overlap         `json:"overlaps"`
	Blockers        []Issue           `json:"blockers"`
	Warnings        []Issue           `json:"warnings"`
	PilotDecisions  map[string]string `json:"pilot_decisions"`
}

// Input to Assess.
type Input struct {
	Budget      money.Amount
	Pilots      []domain.Pilot // selected pilots (resolved by caller; unknown IDs rejected before this point)
	Evaluations map[string]decision.Evaluation
	Resolutions []Resolution
	Basis       string
}

// BasisBenefit returns the benefit a pilot contributes on the chosen basis, and whether it is recognisable.
func BasisBenefit(p domain.Pilot, basis string) (money.Amount, string) {
	if basis == BasisStated {
		return p.AnnualBenefit, "stated annual_benefit_inr"
	}
	a, rule := decision.CashBenefit(p)
	return a, rule
}

// Assess computes totals, the ledger, overlaps and status. Pure and deterministic (pilots are sorted by ID).
func Assess(in Input) Assessment {
	if in.Basis == "" {
		in.Basis = BasisCash
	}
	pilots := append([]domain.Pilot(nil), in.Pilots...)
	sort.Slice(pilots, func(i, j int) bool { return NatLess(pilots[i].ID, pilots[j].ID) })
	a := Assessment{RulesVersion: rules.Version, Basis: in.Basis, Budget: in.Budget, PilotIDs: []string{}, Ledger: []LedgerEntry{},
		Overlaps: []Overlap{}, Blockers: []Issue{}, Warnings: []Issue{}, PilotDecisions: map[string]string{}}
	block := func(code, id, msg string) { a.Blockers = append(a.Blockers, Issue{code, id, msg}) }
	warn := func(code, id, msg string) { a.Warnings = append(a.Warnings, Issue{code, id, msg}) }

	resByGroup := map[string]Resolution{}
	for _, r := range in.Resolutions {
		resByGroup[r.Group] = r
	}
	groups := map[string][]int{}
	for i, p := range pilots {
		a.PilotIDs = append(a.PilotIDs, p.ID)
		basis, rule := BasisBenefit(p, in.Basis)
		e := LedgerEntry{PilotID: p.ID, BenefitGroup: p.OverlapGroup, ClaimedBenefit: p.AnnualBenefit, BasisBenefit: basis,
			RecognizedBenefit: basis, RunningCost: p.RunningCost, Source: p.Sources["annual_benefit_inr"], Resolution: "NONE",
			ReviewStatus: "AUTO", ReviewNote: rule}
		if !basis.Known {
			e.RecognizedBenefit, e.ReviewStatus = money.Rupees(0), "NOT_RECOGNIZED"
			if p.AnnualBenefit.Known {
				warn(CodeNotCash, p.ID, p.ID+": benefit is not cash-recognisable ("+rule+"); recognised as INR 0 in the cash basis.")
			}
		}
		a.Ledger = append(a.Ledger, e)
		if ev, ok := in.Evaluations[p.ID]; ok {
			a.PilotDecisions[p.ID] = ev.Decision
			switch ev.Decision {
			case rules.Stop:
				block(CodeStopSelected, p.ID, fmt.Sprintf("%s has a mandatory STOP decision (%s).", p.ID, ev.PrimaryReason))
			case rules.Revise:
				// REVISE = fix before more money is committed; approving a portfolio commits money.
				if p.FinanceComplete() { // missing finance is already a FINANCE_INCOMPLETE blocker
					block(CodeRevisePilot, p.ID, fmt.Sprintf("%s must be revised before funding (%s).", p.ID, ev.PrimaryReason))
				}
			}
		}
		if !p.FinanceComplete() {
			block(CodeFinanceIncomplete, p.ID, p.ID+": a critical financial input (setup, running cost or benefit) is unknown.")
		}
		if p.OverlapGroup == nil {
			block(CodeOverlapUnknown, p.ID, p.ID+": overlap group unknown; cannot confirm its benefit is not already counted.")
		} else {
			groups[*p.OverlapGroup] = append(groups[*p.OverlapGroup], i)
		}
	}

	gnames := make([]string, 0, len(groups))
	for g := range groups {
		gnames = append(gnames, g)
	}
	sort.Strings(gnames)
	for _, g := range gnames {
		idx := groups[g]
		if len(idx) < 2 {
			if r, ok := resByGroup[g]; ok && r.Type != RemoveOne {
				warn(CodeStaleResolution, "", fmt.Sprintf("Resolution for group %q ignored: fewer than 2 selected pilots share it.", g))
			}
			continue
		}
		applyOverlap(&a, g, idx, resByGroup, block)
	}
	for g, r := range resByGroup {
		if _, ok := groups[g]; !ok {
			warn(CodeStaleResolution, "", fmt.Sprintf("Resolution %s for group %q ignored: no selected pilot uses it.", r.Type, g))
		}
	}
	sort.SliceStable(a.Warnings, func(i, j int) bool { return a.Warnings[i].Message < a.Warnings[j].Message })

	totals(&a, pilots, block)
	switch {
	case len(pilots) == 0:
		a.Status = StatusDraft
	case hasAny(a.Blockers, CodeBudgetMissing, CodeBudgetExceeded, CodeStopSelected, CodeFinanceIncomplete):
		a.Status = StatusRejected
	case hasAny(a.Blockers, CodeOverlapUnresolved, CodeOverlapUnknown, CodeRevisePilot):
		a.Status = StatusNeedsReview
	default:
		a.Status = StatusValid
	}
	a.ApprovalAllowed = a.Status == StatusValid
	return a
}

func hasAny(is []Issue, codes ...string) bool {
	for _, i := range is {
		for _, c := range codes {
			if i.Code == c {
				return true
			}
		}
	}
	return false
}

func applyOverlap(a *Assessment, g string, idx []int, res map[string]Resolution, block func(string, string, string)) {
	ov := Overlap{ID: g, Group: g, Code: CodeDoubleCountRisk, Status: "UNRESOLVED"}
	var claimed, pool int64
	largest := idx[0]
	for _, i := range idx {
		e := a.Ledger[i]
		ov.PilotIDs = append(ov.PilotIDs, e.PilotID)
		claimed += e.RecognizedBenefit.Paise
		if e.RecognizedBenefit.Paise > pool {
			pool, largest = e.RecognizedBenefit.Paise, i
		}
	}
	ov.ClaimedTotal, ov.PoolInr = money.Amount{Paise: claimed, Known: true}, money.Amount{Paise: pool, Known: true}
	set := func(i int, v int64, status, resType, note string) {
		a.Ledger[i].RecognizedBenefit = money.Amount{Paise: v, Known: true}
		a.Ledger[i].ReviewStatus, a.Ledger[i].Resolution, a.Ledger[i].ReviewNote = status, resType, note
	}

	r, ok := res[g]
	if ok {
		if err := ValidateResolution(r, ov.PilotIDs, claims(a, idx)); err != nil {
			ok = false
			block(CodeOverlapUnresolved, "", fmt.Sprintf("Resolution for %q is no longer valid (%v).", g, err))
		}
	}
	if !ok {
		// conservative default: recognise only the largest claim (lowest ID on ties) until a reviewer resolves it
		for _, i := range idx {
			if i == largest {
				set(i, a.Ledger[i].RecognizedBenefit.Paise, "UNRESOLVED", "NONE", "Shared pool: counted once (largest claim) pending review.")
			} else {
				set(i, 0, "UNRESOLVED", "NONE", fmt.Sprintf("Shares benefit group %q with %s: not counted until resolved.", g, a.Ledger[largest].PilotID))
			}
		}
		if _, had := res[g]; !had {
			block(CodeOverlapUnresolved, "", fmt.Sprintf("DOUBLE_COUNT_RISK: %v share benefit group %q (claimed INR %s, pool INR %s). Resolve before approval.",
				ov.PilotIDs, g, ov.ClaimedTotal, ov.PoolInr))
		}
	} else {
		rr := r
		ov.Resolution, ov.Status = &rr, "RESOLVED"
		note := r.Note
		switch r.Type {
		case ExcludeSecond:
			for _, i := range idx {
				if a.Ledger[i].PilotID == r.PrimaryPilotID {
					set(i, a.Ledger[i].RecognizedBenefit.Paise, "RESOLVED", r.Type, note)
				} else {
					set(i, 0, "RESOLVED", r.Type, fmt.Sprintf("Benefit excluded: already counted in %s. %s", r.PrimaryPilotID, note))
				}
			}
		case SplitBenefit:
			shares := r.SharesPct
			if len(shares) == 0 {
				shares = map[string]int64{}
				for k, id := range ov.PilotIDs {
					shares[id] = 100 / int64(len(idx))
					if k < int(100%int64(len(idx))) {
						shares[id]++
					}
				}
			}
			for _, i := range idx {
				v := new(big.Rat).SetFrac64(pool*shares[a.Ledger[i].PilotID], 100)
				p := new(big.Int).Quo(v.Num(), v.Denom()).Int64() // floor to paise: never over-recognise
				if c := a.Ledger[i].RecognizedBenefit.Paise; p > c {
					p = c
				}
				set(i, p, "RESOLVED", r.Type, fmt.Sprintf("%d%% of shared pool INR %s. %s", shares[a.Ledger[i].PilotID], ov.PoolInr, note))
			}
		case CustomAllocation:
			for _, i := range idx {
				set(i, r.Allocations[a.Ledger[i].PilotID].Paise, "RESOLVED", r.Type, note)
			}
		}
	}
	var rec int64
	for _, i := range idx {
		rec += a.Ledger[i].RecognizedBenefit.Paise
	}
	ov.RecognizedTotal = money.Amount{Paise: rec, Known: true}
	a.Overlaps = append(a.Overlaps, ov)
}

func claims(a *Assessment, idx []int) map[string]money.Amount {
	m := map[string]money.Amount{}
	for _, i := range idx {
		m[a.Ledger[i].PilotID] = a.Ledger[i].RecognizedBenefit
	}
	return m
}

// ValidateResolution checks a resolution against the pilots currently sharing the group and their basis claims.
func ValidateResolution(r Resolution, ids []string, claim map[string]money.Amount) error {
	in := map[string]bool{}
	for _, id := range ids {
		in[id] = true
	}
	var pool int64
	for _, c := range claim {
		if c.Paise > pool {
			pool = c.Paise
		}
	}
	switch r.Type {
	case RemoveOne:
		if len(ids) > 1 {
			return fmt.Errorf("REMOVE_ONE keeps only %s but %d pilots still share the group", r.PrimaryPilotID, len(ids))
		}
	case ExcludeSecond:
		if !in[r.PrimaryPilotID] {
			return fmt.Errorf("primary_pilot_id %q is not in the group", r.PrimaryPilotID)
		}
	case SplitBenefit:
		if len(r.SharesPct) > 0 {
			var sum int64
			for id, s := range r.SharesPct {
				if !in[id] || s < 0 {
					return fmt.Errorf("shares_pct has invalid pilot %q or negative share", id)
				}
				sum += s
			}
			if sum != 100 || len(r.SharesPct) != len(ids) {
				return fmt.Errorf("shares_pct must cover every pilot in the group and sum to 100")
			}
		}
	case CustomAllocation:
		if len(r.Allocations) != len(ids) {
			return fmt.Errorf("allocations_inr must list every pilot in the group")
		}
		var sum int64
		for id, v := range r.Allocations {
			if !in[id] || !v.Known {
				return fmt.Errorf("allocation for %q is invalid", id)
			}
			if v.Paise > claim[id].Paise {
				return fmt.Errorf("allocation for %s (INR %s) exceeds its own claim (INR %s)", id, v, claim[id])
			}
			sum += v.Paise
		}
		if sum > pool {
			return fmt.Errorf("allocations total INR %s exceeds the shared pool INR %s (benefit cannot be counted twice)",
				money.Amount{Paise: sum, Known: true}, money.Amount{Paise: pool, Known: true})
		}
	default:
		return fmt.Errorf("unknown resolution_type %q", r.Type)
	}
	return nil
}

func totals(a *Assessment, pilots []domain.Pilot, block func(string, string, string)) {
	var setup, run []money.Amount
	var claimed, rec int64
	claimedKnown := true
	for i, p := range pilots {
		setup, run = append(setup, p.SetupCost), append(run, p.RunningCost)
		if p.AnnualBenefit.Known {
			claimed += p.AnnualBenefit.Paise
		} else {
			claimedKnown = false
		}
		rec += a.Ledger[i].RecognizedBenefit.Paise
	}
	ts, okS := finance.SumAmounts(setup...)
	tr, okR := finance.SumAmounts(run...)
	if okS {
		a.TotalSetup = ts
	}
	if okR {
		a.TotalRunning = tr
	}
	if claimedKnown {
		a.TotalClaimed = money.Amount{Paise: claimed, Known: true}
	}
	a.TotalRecognized = money.Amount{Paise: rec, Known: true}
	if okR {
		a.NetAnnual = finance.NetAnnualBenefit(a.TotalRecognized, tr)
		a.MonthlyNet = finance.MonthlyNetBenefit(a.NetAnnual)
	}
	a.Payback = finance.PaybackMonths(a.TotalSetup, a.MonthlyNet)
	if !a.Budget.Known {
		block(CodeBudgetMissing, "", "Budget is unknown (null); a portfolio cannot be validated without it.")
		return
	}
	a.BudgetRemaining = finance.BudgetRemaining(a.Budget, a.TotalSetup)
	if okS && ts.Paise > a.Budget.Paise {
		block(CodeBudgetExceeded, "", fmt.Sprintf("Total setup INR %s exceeds budget INR %s by INR %s.", ts, a.Budget,
			money.Amount{Paise: ts.Paise - a.Budget.Paise, Known: true}))
	}
}
