package portfolio

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/rules"
)

// MaxCandidates bounds exhaustive search (2^20 ~ 1M subsets). C23 has 5-8 pilots.
const MaxCandidates = 20

// ErrTooManyCandidates is returned when exact search would be too large.
var ErrTooManyCandidates = errors.New("too many candidate pilots for exact search")

// TieBreakOrder is the documented deterministic ordering between equally good portfolios.
var TieBreakOrder = []string{"higher recognised annual net benefit", "faster portfolio payback", "higher total readiness",
	"higher total adoption", "lower total setup cost", "stable ID order"}

// Exclusion explains why a pilot is not in the optimal portfolio.
type Exclusion struct {
	PilotID string `json:"pilot_id"`
	Code    string `json:"code"`
	Reason  string `json:"reason"`
}

// OptimizeResult is the optimizer output. The assessment of the chosen set is included.
type OptimizeResult struct {
	RulesVersion          string       `json:"rules_version"`
	Method                string       `json:"method"`
	Basis                 string       `json:"basis"`
	Budget                money.Amount `json:"budget_inr"`
	Selected              []string     `json:"selected"`
	Excluded              []Exclusion  `json:"excluded"`
	CandidatesConsidered  int          `json:"candidates_considered"`
	CombinationsEvaluated int          `json:"combinations_evaluated"`
	TieBreakOrder         []string     `json:"tie_break_order"`
	Message               string       `json:"message"`
	Assessment            Assessment   `json:"assessment"`
}

type cand struct {
	p         domain.Pilot
	net       *big.Rat // basis benefit - running (rupees)
	setup     int64    // paise
	readiness int
	adoption  int64 // centi-pct (actual if measured, else expected; 0 if unknown)
}

// Optimize finds the budget-feasible, overlap-free subset of eligible pilots maximising recognised net benefit.
// Ineligible: invalid finance, STOP decisions, unknown overlap, non-positive net on the basis. No ML.
func Optimize(pilots []domain.Pilot, evals map[string]decision.Evaluation, budget money.Amount, basis string, th rules.Thresholds) (OptimizeResult, error) {
	if basis == "" {
		basis = BasisCash
	}
	res := OptimizeResult{RulesVersion: rules.Version, Method: "exhaustive subset search", Basis: basis, Budget: budget,
		Selected: []string{}, Excluded: []Exclusion{}, TieBreakOrder: TieBreakOrder}
	ps := append([]domain.Pilot(nil), pilots...)
	sort.Slice(ps, func(i, j int) bool { return NatLess(ps[i].ID, ps[j].ID) })

	var cs []cand
	exclude := func(id, code, why string) { res.Excluded = append(res.Excluded, Exclusion{id, code, why}) }
	for _, p := range ps {
		ev := evals[p.ID]
		b, rule := BasisBenefit(p, basis)
		switch {
		case !p.FinanceComplete():
			exclude(p.ID, CodeFinanceIncomplete, "A critical financial input is unknown; payback cannot be calculated.")
		case ev.Decision == rules.Stop:
			exclude(p.ID, CodeStopSelected, "Mandatory STOP decision ("+ev.PrimaryReason+").")
		case ev.Decision == rules.Revise:
			exclude(p.ID, CodeRevisePilot, "Must be revised before funding ("+ev.PrimaryReason+"); an optimal portfolio must be approvable.")
		case p.OverlapGroup == nil:
			exclude(p.ID, CodeOverlapUnknown, "Overlap group unknown; cannot prove its benefit is not double counted.")
		case !b.Known:
			exclude(p.ID, CodeNotCash, "No cash-recognisable benefit on the "+basis+" basis ("+rule+").")
		default:
			net := new(big.Rat).Sub(b.Rat(), p.RunningCost.Rat())
			if net.Sign() <= 0 {
				exclude(p.ID, rules.ReasonNeverPaysBack, fmt.Sprintf("Net annual benefit INR %s on the %s basis: never pays back.", money.FormatRat(net, 2), basis))
				continue
			}
			_, level, _ := decision.ReadinessLevel(p, th)
			ad := p.ExpectedAdoption
			if p.ActualAdoption.Known {
				ad = p.ActualAdoption
			}
			cs = append(cs, cand{p: p, net: net, setup: p.SetupCost.Paise, readiness: decision.ReadinessRank(level), adoption: ad.Centi})
		}
	}
	res.CandidatesConsidered = len(cs)
	if len(cs) > MaxCandidates {
		return res, fmt.Errorf("%w: %d > %d", ErrTooManyCandidates, len(cs), MaxCandidates)
	}
	if !budget.Known {
		res.Message = "Budget is unknown (null): no portfolio can be selected."
		res.Assessment = Assess(Input{Budget: budget, Evaluations: evals, Basis: basis})
		return res, nil
	}

	bestMask, haveBest := 0, false
	var bestKey setKey
	for mask := 0; mask < 1<<len(cs); mask++ {
		res.CombinationsEvaluated++
		var setup int64
		groups := map[string]bool{}
		ok := true
		for i := range cs {
			if mask>>i&1 == 0 {
				continue
			}
			setup += cs[i].setup
			g := *cs[i].p.OverlapGroup
			if groups[g] { // never count the same benefit pool twice
				ok = false
				break
			}
			groups[g] = true
		}
		if !ok || setup > budget.Paise {
			continue
		}
		k := keyOf(cs, mask)
		if !haveBest || k.better(bestKey) {
			bestMask, bestKey, haveBest = mask, k, true
		}
	}

	var chosen []domain.Pilot
	byGroup := map[string]string{}
	for i := range cs {
		if bestMask>>i&1 == 1 {
			chosen = append(chosen, cs[i].p)
			res.Selected = append(res.Selected, cs[i].p.ID)
			byGroup[*cs[i].p.OverlapGroup] = cs[i].p.ID
		}
	}
	for i := range cs {
		if bestMask>>i&1 == 1 {
			continue
		}
		if w, ok := byGroup[*cs[i].p.OverlapGroup]; ok {
			exclude(cs[i].p.ID, CodeDoubleCountRisk, fmt.Sprintf("Shares benefit group %q with selected %s; the benefit is counted once.", *cs[i].p.OverlapGroup, w))
		} else if cs[i].setup > budget.Paise {
			exclude(cs[i].p.ID, CodeBudgetExceeded, fmt.Sprintf("Setup INR %s alone exceeds the budget INR %s.", cs[i].p.SetupCost, budget))
		} else {
			exclude(cs[i].p.ID, "NOT_FUNDED", "Not funded: the budget is better used by the selected pilots (see tie-break order).")
		}
	}
	sort.SliceStable(res.Excluded, func(i, j int) bool { return NatLess(res.Excluded[i].PilotID, res.Excluded[j].PilotID) })
	res.Assessment = Assess(Input{Budget: budget, Pilots: chosen, Evaluations: evals, Basis: basis})
	if len(chosen) == 0 {
		res.Message = fmt.Sprintf("Empty portfolio: no eligible pilot fits budget INR %s.", budget)
	} else {
		res.Message = fmt.Sprintf("Selected %d pilot(s) using INR %s of INR %s.", len(chosen), res.Assessment.TotalSetup, budget)
	}
	return res, nil
}

type setKey struct {
	net       *big.Rat
	payback   *big.Rat // setup / monthly net; nil for empty set
	readiness int
	adoption  int64
	setup     int64
	ids       []string
}

func keyOf(cs []cand, mask int) setKey {
	k := setKey{net: new(big.Rat)}
	for i := range cs {
		if mask>>i&1 == 1 {
			k.net.Add(k.net, cs[i].net)
			k.readiness += cs[i].readiness
			k.adoption += cs[i].adoption
			k.setup += cs[i].setup
			k.ids = append(k.ids, cs[i].p.ID)
		}
	}
	if k.net.Sign() > 0 {
		k.payback = new(big.Rat).Quo(new(big.Rat).SetFrac64(k.setup*12, 100), k.net)
	}
	return k
}

// better reports whether k beats o under TieBreakOrder.
func (k setKey) better(o setKey) bool {
	if c := k.net.Cmp(o.net); c != 0 {
		return c > 0
	}
	if k.payback != nil && o.payback != nil {
		if c := k.payback.Cmp(o.payback); c != 0 {
			return c < 0
		}
	}
	if k.readiness != o.readiness {
		return k.readiness > o.readiness
	}
	if k.adoption != o.adoption {
		return k.adoption > o.adoption
	}
	if k.setup != o.setup {
		return k.setup < o.setup
	}
	for i := 0; i < len(k.ids) && i < len(o.ids); i++ {
		if k.ids[i] != o.ids[i] {
			return NatLess(k.ids[i], o.ids[i])
		}
	}
	return len(k.ids) < len(o.ids)
}

var natRE = regexp.MustCompile(`\d+|\D+`)

// NatLess compares IDs naturally (AI2 < AI10), falling back to byte order.
func NatLess(a, b string) bool {
	pa, pb := natRE.FindAllString(a, -1), natRE.FindAllString(b, -1)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] == pb[i] {
			continue
		}
		na, ea := strconv.ParseUint(pa[i], 10, 64)
		nb, eb := strconv.ParseUint(pb[i], 10, 64)
		if ea == nil && eb == nil && na != nb {
			return na < nb
		}
		return pa[i] < pb[i]
	}
	if len(pa) != len(pb) {
		return len(pa) < len(pb)
	}
	return a < b
}
