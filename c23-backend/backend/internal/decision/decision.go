// Package decision is the deterministic per-pilot rules engine (c23-rules-v1).
//
// Every rule is evaluated; all triggered reason codes are returned in priority order and the final
// decision is the MOST SEVERE one (STOP > REVISE > REVIEW > PROCEED). Nothing is hidden behind a score.
//
// Rules (thresholds: pilot's own minimum_*_pct, else rules.Default()):
//
//	R1 setup/benefit/running unknown            -> REVISE  MISSING_*            (payback NOT_CALCULABLE)
//	R2 net annual benefit <= 0                  -> STOP    NEVER_PAYS_BACK
//	R3 actual quality < minimum quality         -> STOP    QUALITY_THRESHOLD_FAILED (overrides good finance)
//	R4 actual adoption < break-even adoption    -> STOP    ADOPTION_BELOW_BREAKEVEN (+ LOW_ADOPTION)
//	R5 actual adoption < minimum adoption       -> REVISE  LOW_ADOPTION
//	R6 readiness level low                      -> REVISE  LOW_READINESS
//	R7 time freed with no realised cash saving  -> REVIEW  TIME_NOT_CASH
//	R8 overlap group unknown                    -> REVIEW  OVERLAP_UNKNOWN
//	R9 actual adoption or quality not measured  -> REVIEW  INSUFFICIENT_EVIDENCE (fund only as a time-boxed pilot)
//	otherwise                                   -> PROCEED
package decision

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"c23desk/internal/domain"
	"c23desk/internal/finance"
	"c23desk/internal/money"
	"c23desk/internal/rules"
)

// FailedRule explains one triggered rule with the numbers that triggered it.
type FailedRule struct {
	Rule      string `json:"rule"`
	Code      string `json:"code"`
	Decision  string `json:"forces_decision"`
	Metric    string `json:"metric"`
	Threshold string `json:"threshold,omitempty"`
	Actual    string `json:"actual,omitempty"`
	Message   string `json:"message"`
}

// Threshold records a threshold value and where it came from.
type Threshold struct {
	Value  money.Pct `json:"value"`
	Source string    `json:"source"`
}

// Metrics are the calculated values behind the decision.
type Metrics struct {
	// Stated basis: annual_benefit_inr as supplied (official public check uses this basis).
	Stated finance.Metrics `json:"stated_basis"`
	// Cash basis: benefit recognisable as cash (time freed counts only realised cash saving).
	Cash                 finance.Metrics `json:"cash_basis"`
	CashBasisRule        string          `json:"cash_basis_rule"`
	BreakevenAdoptionPct money.Dec       `json:"breakeven_adoption_pct"`
	OverallReadinessPct  money.Dec       `json:"overall_readiness_pct"`
	ReadinessLevel       string          `json:"readiness_level"`
	ReadinessBasis       string          `json:"readiness_basis"`
	AdoptionStatus       string          `json:"adoption_status"`
	QualityStatus        string          `json:"quality_status"`
	Stage                string          `json:"stage"`
	MinimumAdoption      Threshold       `json:"minimum_adoption"`
	MinimumQuality       Threshold       `json:"minimum_quality"`
	LowReadinessBelowPct int64           `json:"low_readiness_below_pct"`
}

// Evaluation is the full, explainable result for one pilot.
type Evaluation struct {
	PilotID       string       `json:"pilot_id"`
	RulesVersion  string       `json:"rules_version"`
	Decision      string       `json:"decision"`
	PrimaryReason string       `json:"primary_reason"`
	ReasonCodes   []string     `json:"reason_codes"`
	Warnings      []string     `json:"warnings"`
	FailedRules   []FailedRule `json:"failed_rules"`
	Metrics       Metrics      `json:"calculated_metrics"`
	InputSnapshot domain.Pilot `json:"input_snapshot"`
	Explanation   string       `json:"explanation"`
}

// Context carries portfolio-independent facts the engine may need (e.g. peers sharing an overlap group).
type Context struct {
	GroupPeers []string // other pilot IDs with the same known overlap group
}

// CashBenefit is the annual benefit recognisable as cash under c23-rules-v1.
// cashable -> annual_benefit_inr; time_freed/other/unknown -> realized_cash_saving_inr (unknown if not supplied).
func CashBenefit(p domain.Pilot) (money.Amount, string) {
	if p.BenefitType == domain.BenefitCashable {
		return p.AnnualBenefit, "cashable: annual_benefit_inr counted as cash (fixture labels this an assumption)"
	}
	if p.RealizedCashSaving.Known {
		return p.RealizedCashSaving, p.BenefitType + ": only realized_cash_saving_inr counted as cash"
	}
	return money.Unknown(), p.BenefitType + ": no realised cash saving recorded, cash benefit UNKNOWN (capacity released is not cash)"
}

// ReadinessLevel derives low/medium/high from numeric readiness (mean of supplied values) or the label.
func ReadinessLevel(p domain.Pilot, th rules.Thresholds) (money.Dec, string, string) {
	vals := []money.Pct{p.DataReadiness, p.TechnicalReadiness, p.ProcessReadiness}
	sum, n := new(big.Rat), int64(0)
	for _, v := range vals {
		if v.Known {
			sum.Add(sum, v.Rat())
			n++
		}
	}
	if n > 0 {
		avg := sum.Quo(sum, big.NewRat(n, 1))
		level := "medium"
		if avg.Cmp(big.NewRat(th.LowReadinessPct, 1)) < 0 {
			level = "low"
		} else if avg.Cmp(big.NewRat(th.HighReadinessPct, 1)) >= 0 {
			level = "high"
		}
		return money.Dec{R: avg}, level, fmt.Sprintf("mean of %d supplied readiness pct value(s)", n)
	}
	if p.ReadinessLabel != nil {
		return money.Dec{}, *p.ReadinessLabel, "readiness_label (" + p.Sources["readiness_label"] + ")"
	}
	return money.Dec{}, "unknown", "no readiness supplied"
}

// ReadinessRank orders readiness for tie-breaks: unknown < low < medium < high.
func ReadinessRank(level string) int {
	return map[string]int{"low": 1, "medium": 2, "high": 3}[level]
}

func threshold(own money.Pct, def int64) Threshold {
	if own.Known {
		return Threshold{Value: own, Source: "pilot"}
	}
	return Threshold{Value: money.Percent(def), Source: domain.SourceTeam + " (rules default)"}
}

// Evaluate applies c23-rules-v1 to one pilot. Pure and deterministic.
func Evaluate(p domain.Pilot, th rules.Thresholds, ctx Context) Evaluation {
	e := Evaluation{PilotID: p.ID, RulesVersion: rules.Version, InputSnapshot: p, Warnings: []string{}, FailedRules: []FailedRule{}}
	m := &e.Metrics
	m.Stated = finance.Compute(p.SetupCost, p.RunningCost, p.AnnualBenefit)
	cash, cashRule := CashBenefit(p)
	m.CashBasisRule = cashRule
	m.Cash = finance.Compute(p.SetupCost, p.RunningCost, cash)
	m.BreakevenAdoptionPct = finance.BreakevenAdoptionPct(p.RunningCost, cash)
	m.OverallReadinessPct, m.ReadinessLevel, m.ReadinessBasis = ReadinessLevel(p, th)
	m.MinimumAdoption = threshold(p.MinimumAdoption, th.MinimumAdoptionPct)
	m.MinimumQuality = threshold(p.MinimumQuality, th.MinimumQualityPct)
	m.LowReadinessBelowPct = th.LowReadinessPct
	m.AdoptionStatus, m.QualityStatus = measured(p.ActualAdoption), measured(p.ActualQuality)
	switch {
	case p.ActualAdoption.Known && p.ActualQuality.Known:
		m.Stage = "MEASURED"
	case p.ActualAdoption.Known || p.ActualQuality.Known:
		m.Stage = "PARTIALLY_MEASURED"
	default:
		m.Stage = "PRE_PILOT"
	}

	fail := func(rule, code, metric, thr, actual, msg string) {
		e.FailedRules = append(e.FailedRules, FailedRule{Rule: rule, Code: code, Decision: rules.DecisionFor[code], Metric: metric,
			Threshold: thr, Actual: actual, Message: msg})
	}

	// R1 missing finance inputs
	if !p.SetupCost.Known {
		fail("R1", rules.ReasonMissingSetup, "setup_cost_inr", "", "unknown", "Setup cost is unknown (null, not zero): payback cannot be calculated.")
	}
	if !p.AnnualBenefit.Known {
		fail("R1", rules.ReasonMissingBenefit, "annual_benefit_inr", "", "unknown", "Annual benefit is unknown (null, not zero): net benefit cannot be calculated.")
	}
	if !p.RunningCost.Known {
		fail("R1", rules.ReasonMissingRunning, "annual_running_cost_inr", "", "unknown",
			"Annual running cost is unknown (null, not zero): no reliable payback. Supply an estimate or run a labelled scenario.")
	}
	// R2 economics (cash basis; identical to stated basis for cashable pilots)
	if m.Cash.NetAnnual.Known() && m.Cash.NetAnnual.R.Sign() <= 0 {
		fail("R2", rules.ReasonNeverPaysBack, "cash_net_annual_benefit_inr", "> 0", m.Cash.NetAnnual.String(),
			fmt.Sprintf("Net annual cash benefit is INR %s (benefit %s - running %s): it never pays back.", m.Cash.NetAnnual, cash, p.RunningCost))
	}
	// R3 quality
	if p.ActualQuality.Known && p.ActualQuality.Centi < m.MinimumQuality.Value.Centi {
		fail("R3", rules.ReasonQualityFailed, "actual_quality_pct", m.MinimumQuality.Value.String(), p.ActualQuality.String(),
			fmt.Sprintf("Quality %s%% is below the minimum %s%%: stop regardless of financial return.", p.ActualQuality, m.MinimumQuality.Value))
	}
	// R4/R5 adoption
	if p.ActualAdoption.Known {
		be := m.BreakevenAdoptionPct
		if be.Known() && p.ActualAdoption.Rat().Cmp(be.R) < 0 {
			fail("R4", rules.ReasonAdoptionBelowBreak, "actual_adoption_pct", be.String(), p.ActualAdoption.String(),
				fmt.Sprintf("Adoption %s%% is below break-even %s%%: the pilot loses money at this adoption.", p.ActualAdoption, be))
		}
		if p.ActualAdoption.Centi < m.MinimumAdoption.Value.Centi {
			fail("R5", rules.ReasonLowAdoption, "actual_adoption_pct", m.MinimumAdoption.Value.String(), p.ActualAdoption.String(),
				fmt.Sprintf("Adoption %s%% is below the minimum %s%%.", p.ActualAdoption, m.MinimumAdoption.Value))
		}
	}
	// R6 readiness
	switch m.ReadinessLevel {
	case "low":
		fail("R6", rules.ReasonLowReadiness, "readiness", fmt.Sprintf(">= %d (or label medium/high)", th.LowReadinessPct), m.ReadinessLevel,
			"Readiness is low ("+m.ReadinessBasis+"): revise data/process readiness before funding.")
	case "unknown":
		e.Warnings = append(e.Warnings, rules.WarnReadinessUnknown)
	}
	// R7 time freed is not cash
	if p.BenefitType != domain.BenefitCashable && !p.RealizedCashSaving.Known {
		fail("R7", rules.ReasonTimeNotCash, "benefit_type", "realised cash saving recorded", p.BenefitType,
			"Benefit type is '"+p.BenefitType+"', not cashable: time or capacity released is not cash saved. "+
				"Cash payback is unproven until realized_cash_saving_inr is recorded.")
	}
	if p.BenefitType == domain.BenefitUnknown {
		e.Warnings = append(e.Warnings, rules.WarnBenefitUnknownType)
	}
	// R8 overlap
	if p.OverlapGroup == nil {
		fail("R8", rules.ReasonOverlapUnknown, "overlap_group", "known", "unknown",
			"Overlap group is unknown: it is NOT treated as 'no overlap'. Confirm with the process owner before approval.")
	} else if len(ctx.GroupPeers) > 0 {
		e.Warnings = append(e.Warnings, rules.WarnSharedGroup+": shares benefit group '"+*p.OverlapGroup+"' with "+strings.Join(ctx.GroupPeers, ", "))
	}
	// R9 evidence
	if !p.ActualAdoption.Known || !p.ActualQuality.Known {
		fail("R9", rules.ReasonInsufficientEvidence, "actual_adoption_pct/actual_quality_pct", "measured", "NOT_MEASURED",
			"Actual adoption and/or quality not measured yet: fund only as a small time-boxed pilot and measure before wider investment.")
	}
	if !p.ExpectedAdoption.Known {
		e.Warnings = append(e.Warnings, rules.WarnAdoptionExpUnknown)
	}
	if p.SetupCost.Known && p.SetupCost.Paise == 0 {
		e.Warnings = append(e.Warnings, rules.WarnZeroSetup)
	}
	for _, a := range p.Assumptions {
		if a.Field == "annual_running_cost_inr" {
			e.Warnings = append(e.Warnings, rules.WarnRunCostAssumed+": "+a.Rule)
		}
	}

	finalize(&e)
	e.Explanation = Explain(e)
	return e
}

func measured(p money.Pct) string {
	if p.Known {
		return "MEASURED"
	}
	return "NOT_MEASURED"
}

func finalize(e *Evaluation) {
	rank := map[string]int{}
	for i, c := range rules.Priority {
		rank[c] = i
	}
	sort.SliceStable(e.FailedRules, func(i, j int) bool { return rank[e.FailedRules[i].Code] < rank[e.FailedRules[j].Code] })
	e.Decision, e.ReasonCodes = rules.Proceed, []string{}
	for _, f := range e.FailedRules {
		e.ReasonCodes = append(e.ReasonCodes, f.Code)
		if rules.Severity[f.Decision] > rules.Severity[e.Decision] {
			e.Decision = f.Decision
		}
	}
	// primary reason = highest-priority rule among those forcing the final decision
	for _, f := range e.FailedRules {
		if f.Decision == e.Decision {
			e.PrimaryReason = f.Code
			break
		}
	}
	if e.PrimaryReason == "" {
		e.PrimaryReason = "ALL_RULES_PASSED"
	}
}

// Explain renders a deterministic, fully grounded explanation (every number comes from the evaluation).
func Explain(e Evaluation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s (%s).", e.PilotID, e.Decision, e.PrimaryReason)
	s := e.Metrics.Stated
	if s.Months.Known() {
		fmt.Fprintf(&b, " Stated basis: net INR %s/yr, INR %s/month, payback %s months.", s.NetAnnual, s.MonthlyNet, s.Months)
	} else {
		fmt.Fprintf(&b, " Stated basis payback: %s.", s.Status)
	}
	for _, f := range e.FailedRules {
		fmt.Fprintf(&b, " [%s] %s", f.Code, f.Message)
	}
	if len(e.FailedRules) == 0 {
		b.WriteString(" All c23-rules-v1 checks passed.")
	}
	return b.String()
}
