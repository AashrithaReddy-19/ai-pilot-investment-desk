// Package rules holds the versioned business-rule configuration. Every evaluation carries Version.
//
// Threshold DEFAULTS are TEAM_ASSUMPTIONs copied from data/raw/extension.json ("defaults");
// a test fails if they drift from that file. A pilot's own minimum_*_pct overrides the default.
package rules

// Version is attached to every evaluation, decision row and portfolio assessment.
const Version = "c23-rules-v1"

// Decision values.
const (
	Proceed = "PROCEED" // measured adoption + quality meet thresholds, economics positive
	Revise  = "REVISE"  // fixable problem: missing finance input, low adoption, low readiness
	Stop    = "STOP"    // hard rule: never pays back, quality failed, adoption below break-even
	Review  = "REVIEW"  // human review needed: evidence not measured yet, unknown overlap, time-not-cash
)

// Severity orders decisions; the final decision is the most severe triggered rule.
var Severity = map[string]int{Proceed: 0, Review: 1, Revise: 2, Stop: 3}

// Reason codes (public contract).
const (
	ReasonMissingSetup         = "MISSING_SETUP_COST"
	ReasonMissingRunning       = "MISSING_RUNNING_COST"
	ReasonMissingBenefit       = "MISSING_BENEFIT"
	ReasonNeverPaysBack        = "NEVER_PAYS_BACK"
	ReasonQualityFailed        = "QUALITY_THRESHOLD_FAILED"
	ReasonAdoptionBelowBreak   = "ADOPTION_BELOW_BREAKEVEN"
	ReasonLowAdoption          = "LOW_ADOPTION"
	ReasonLowReadiness         = "LOW_READINESS"
	ReasonTimeNotCash          = "TIME_NOT_CASH"
	ReasonOverlapUnknown       = "OVERLAP_UNKNOWN"
	ReasonInsufficientEvidence = "INSUFFICIENT_EVIDENCE"
)

// Warning codes (do not change the decision on their own).
const (
	WarnReadinessUnknown   = "READINESS_UNKNOWN"
	WarnAdoptionExpUnknown = "ADOPTION_EXPECTATION_UNKNOWN"
	WarnSharedGroup        = "DOUBLE_COUNT_RISK"
	WarnBenefitUnknownType = "BENEFIT_TYPE_UNKNOWN"
	WarnRunCostAssumed     = "RUN_COST_ASSUMED"
	WarnZeroSetup          = "ZERO_SETUP_COST"
)

// Thresholds are the configurable rule parameters (percent values).
type Thresholds struct {
	MinimumAdoptionPct int64 `json:"default_minimum_adoption_pct"` // extension.json defaults.target_adoption_pct
	MinimumQualityPct  int64 `json:"default_minimum_quality_pct"`  // extension.json defaults.quality_threshold_pct
	LowReadinessPct    int64 `json:"low_readiness_below_pct"`      // overall readiness strictly below this => LOW_READINESS
	HighReadinessPct   int64 `json:"high_readiness_from_pct"`      // used only to rank readiness for tie-breaks
	AssumedRunCostPct  int64 `json:"scenario_assumed_run_cost_pct_of_setup"`
}

// Default returns the c23-rules-v1 thresholds.
//
// LowReadinessPct=50 / HighReadinessPct=75 are TEAM_ASSUMPTIONs (no official readiness scale exists);
// they only apply when numeric readiness percentages are supplied. The official fixture has none.
func Default() Thresholds {
	return Thresholds{MinimumAdoptionPct: 60, MinimumQualityPct: 80, LowReadinessPct: 50, HighReadinessPct: 75, AssumedRunCostPct: 20}
}

// Ordered reason priority, used to pick the primary reason and to order reason_codes.
var Priority = []string{
	ReasonMissingSetup, ReasonMissingBenefit, ReasonMissingRunning, ReasonNeverPaysBack, ReasonQualityFailed,
	ReasonAdoptionBelowBreak, ReasonLowAdoption, ReasonLowReadiness, ReasonTimeNotCash, ReasonOverlapUnknown,
	ReasonInsufficientEvidence,
}

// DecisionFor maps a reason code to the decision it forces.
var DecisionFor = map[string]string{
	ReasonMissingSetup: Revise, ReasonMissingBenefit: Revise, ReasonMissingRunning: Revise,
	ReasonNeverPaysBack: Stop, ReasonQualityFailed: Stop, ReasonAdoptionBelowBreak: Stop,
	ReasonLowAdoption: Revise, ReasonLowReadiness: Revise,
	ReasonTimeNotCash: Review, ReasonOverlapUnknown: Review, ReasonInsufficientEvidence: Review,
}
