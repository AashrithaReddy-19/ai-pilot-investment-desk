// Package scenario runs "what if" evaluations: temporary, validated overrides on a COPY of a pilot.
// Official values are never modified. Every overridden field is labelled TEAM_ASSUMPTION in the result.
package scenario

import (
	"encoding/json"
	"fmt"
	"sort"

	"c23desk/internal/decision"
	"c23desk/internal/domain"
	"c23desk/internal/money"
	"c23desk/internal/rules"
)

// Request is a scenario definition. Use Overrides and/or AdjustPct, or a named Preset alone.
type Request struct {
	Name                 string           `json:"name"`
	Overrides            json.RawMessage  `json:"overrides"`
	AdjustPct            map[string]int64 `json:"adjust_pct"` // relative change in percent, e.g. {"annual_running_cost_inr": 25}
	Preset               string           `json:"preset"`
	AssumeMissingRunCost bool             `json:"assume_missing_run_cost"` // opt-in, documented, flagged imputation
}

// Preset is a named business shock; it expands to ordinary overrides/adjustments (no separate maths).
type Preset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Presets lists the supported business shocks.
var Presets = []Preset{
	{"running_cost_up_25", "Running cost +25%", "annual_running_cost_inr x 1.25"},
	{"benefit_down_20", "Benefit -20%", "annual_benefit_inr x 0.80"},
	{"adoption_down_30", "Adoption -30%", "actual adoption = 0.70 x actual (or x expected adoption if nothing is measured yet)"},
	{"quality_down_10", "Quality -10%", "actual quality = 0.90 x actual (or 10% below the minimum quality threshold if not measured yet)"},
	{"missing_running_cost", "Missing recurring cost", "annual_running_cost_inr = null (unknown)"},
}

// adjustable fields and whether they are money (else percent 0..100).
var adjustable = map[string]bool{
	"setup_cost_inr": true, "annual_running_cost_inr": true, "annual_benefit_inr": true, "capacity_value_inr": true, "realized_cash_saving_inr": true,
	"expected_adoption_pct": false, "actual_adoption_pct": false, "expected_quality_pct": false, "actual_quality_pct": false,
	"data_readiness_pct": false, "technical_readiness_pct": false, "process_readiness_pct": false,
}

func errf(field, code, msg string) []domain.FieldError {
	return []domain.FieldError{{Field: field, Code: code, Message: msg}}
}

// scale returns v*(100+pct)/100 rounded half away from zero (exact integer maths on paise / centi-percent).
func scale(v, pct int64) int64 {
	n := v * (100 + pct)
	if n >= 0 {
		return (n + 50) / 100
	}
	return (n - 50) / 100
}

func pctOf(p domain.Pilot, f string) *money.Pct {
	switch f {
	case "expected_adoption_pct":
		return &p.ExpectedAdoption
	case "actual_adoption_pct":
		return &p.ActualAdoption
	case "expected_quality_pct":
		return &p.ExpectedQuality
	case "actual_quality_pct":
		return &p.ActualQuality
	case "data_readiness_pct":
		return &p.DataReadiness
	case "technical_readiness_pct":
		return &p.TechnicalReadiness
	case "process_readiness_pct":
		return &p.ProcessReadiness
	}
	return nil
}

func amountOf(p *domain.Pilot, f string) *money.Amount {
	switch f {
	case "setup_cost_inr":
		return &p.SetupCost
	case "annual_running_cost_inr":
		return &p.RunningCost
	case "annual_benefit_inr":
		return &p.AnnualBenefit
	case "capacity_value_inr":
		return &p.CapacityValue
	case "realized_cash_saving_inr":
		return &p.RealizedCashSaving
	}
	return nil
}

func setPct(p *domain.Pilot, f string, v money.Pct) {
	switch f {
	case "expected_adoption_pct":
		p.ExpectedAdoption = v
	case "actual_adoption_pct":
		p.ActualAdoption = v
	case "expected_quality_pct":
		p.ExpectedQuality = v
	case "actual_quality_pct":
		p.ActualQuality = v
	case "data_readiness_pct":
		p.DataReadiness = v
	case "technical_readiness_pct":
		p.TechnicalReadiness = v
	case "process_readiness_pct":
		p.ProcessReadiness = v
	}
}

// expandPreset turns a preset into overrides/adjustments for this pilot.
func expandPreset(base domain.Pilot, id string, th rules.Thresholds) (json.RawMessage, map[string]int64, []domain.Assumption, []domain.FieldError) {
	switch id {
	case "running_cost_up_25":
		return nil, map[string]int64{"annual_running_cost_inr": 25}, nil, nil
	case "benefit_down_20":
		return nil, map[string]int64{"annual_benefit_inr": -20}, nil, nil
	case "missing_running_cost":
		return json.RawMessage(`{"annual_running_cost_inr":null}`), nil, nil, nil
	case "adoption_down_30":
		if base.ActualAdoption.Known {
			return nil, map[string]int64{"actual_adoption_pct": -30}, nil, nil
		}
		if !base.ExpectedAdoption.Known {
			return nil, nil, nil, errf("preset", "ADJUST_UNKNOWN_BASE", "neither actual nor expected adoption is known; cannot apply adoption_down_30")
		}
		v := money.Pct{Centi: scale(base.ExpectedAdoption.Centi, -30), Known: true}
		return json.RawMessage(fmt.Sprintf(`{"actual_adoption_pct":%s}`, v)), nil,
			[]domain.Assumption{{Field: "actual_adoption_pct", Value: v.String(), Source: domain.SourceTeam,
				Rule: "not measured yet: expected_adoption_pct x 0.70 (adoption_down_30)"}}, nil
	case "quality_down_10":
		if base.ActualQuality.Known {
			return nil, map[string]int64{"actual_quality_pct": -10}, nil, nil
		}
		min := money.Percent(th.MinimumQualityPct)
		if base.MinimumQuality.Known {
			min = base.MinimumQuality
		}
		v := money.Pct{Centi: scale(min.Centi, -10), Known: true}
		return json.RawMessage(fmt.Sprintf(`{"actual_quality_pct":%s}`, v)), nil,
			[]domain.Assumption{{Field: "actual_quality_pct", Value: v.String(), Source: domain.SourceTeam,
				Rule: "not measured yet: minimum quality threshold x 0.90 (quality_down_10)"}}, nil
	}
	return nil, nil, nil, errf("preset", domain.CodeInvalidEnum, "unknown preset "+fmt.Sprintf("%q", id))
}

// Change shows one overridden field before -> after.
type Change struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Result of a scenario run.
type Result struct {
	Name         string              `json:"name"`
	BasePilotID  string              `json:"base_pilot_id"`
	RulesVersion string              `json:"rules_version"`
	Changes      []Change            `json:"changes"`
	Assumptions  []domain.Assumption `json:"assumptions"`
	Baseline     decision.Evaluation `json:"baseline"`
	Scenario     decision.Evaluation `json:"scenario"`
	Label        string              `json:"label"`
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// PeersFunc returns the other pilots sharing p's overlap group (recomputed after overrides).
type PeersFunc func(p domain.Pilot) []string

// Run evaluates base and the overridden copy side by side.
func Run(base domain.Pilot, req Request, th rules.Thresholds, peers PeersFunc) (Result, []domain.FieldError) {
	if peers == nil {
		peers = func(domain.Pilot) []string { return nil }
	}
	ov, adj := req.Overrides, req.AdjustPct
	var presetAssumptions []domain.Assumption
	if req.Preset != "" {
		if len(ov) > 0 && string(ov) != "{}" && string(ov) != "null" || len(adj) > 0 {
			return Result{}, errf("preset", "PRESET_EXCLUSIVE", "use a preset on its own, without overrides or adjust_pct")
		}
		var errs []domain.FieldError
		if ov, adj, presetAssumptions, errs = expandPreset(base, req.Preset, th); errs != nil {
			return Result{}, errs
		}
	}
	if len(ov) == 0 || string(ov) == "null" {
		ov = json.RawMessage(`{}`)
	}
	p, keys, errs := domain.ApplyOverrides(base, ov, domain.SourceTeam)
	if errs != nil {
		return Result{}, errs
	}
	res := Result{Name: req.Name, BasePilotID: base.ID, RulesVersion: rules.Version, Changes: []Change{}, Assumptions: []domain.Assumption{},
		Label: "SCENARIO (team assumption, not official data)"}
	res.Assumptions = append(res.Assumptions, presetAssumptions...)
	p.Assumptions = append(p.Assumptions, presetAssumptions...)

	// relative adjustments, applied after overrides, exact integer maths
	adjKeys := make([]string, 0, len(adj))
	for k := range adj {
		adjKeys = append(adjKeys, k)
	}
	sort.Strings(adjKeys)
	for _, k := range adjKeys {
		pct := adj[k]
		isMoney, ok := adjustable[k]
		if !ok {
			return Result{}, errf("adjust_pct."+k, domain.CodeUnknownField, "field cannot be adjusted")
		}
		if pct < -100 || pct > 1000 {
			return Result{}, errf("adjust_pct."+k, money.CodeOutOfRange, "adjustment must be between -100 and +1000 percent")
		}
		rule := fmt.Sprintf("%s %+d%%", k, pct)
		if isMoney {
			a := amountOf(&p, k)
			if !a.Known {
				return Result{}, errf("adjust_pct."+k, "ADJUST_UNKNOWN_BASE", k+" is unknown; an unknown value cannot be adjusted (it is not zero)")
			}
			*a = money.Amount{Paise: scale(a.Paise, pct), Known: true}
			res.Assumptions = append(res.Assumptions, domain.Assumption{Field: k, Value: a.String(), Rule: rule, Source: domain.SourceTeam})
		} else {
			cur := pctOf(p, k)
			if !cur.Known {
				return Result{}, errf("adjust_pct."+k, "ADJUST_UNKNOWN_BASE", k+" is not measured/known; it cannot be adjusted")
			}
			v := scale(cur.Centi, pct)
			if v > 10000 {
				v, rule = 10000, rule+" (capped at 100%)"
			}
			setPct(&p, k, money.Pct{Centi: v, Known: true})
			res.Assumptions = append(res.Assumptions, domain.Assumption{Field: k, Value: money.Pct{Centi: v, Known: true}.String(), Rule: rule, Source: domain.SourceTeam})
		}
		p.Sources[k] = domain.SourceTeam
		if !contains(keys, k) {
			keys = append(keys, k)
		}
	}
	bj, _ := json.Marshal(base)
	pj, _ := json.Marshal(p)
	var bm, pm map[string]any
	_ = json.Unmarshal(bj, &bm)
	_ = json.Unmarshal(pj, &pm)
	sort.Strings(keys)
	for _, k := range keys {
		res.Changes = append(res.Changes, Change{Field: k, Before: bm[k], After: pm[k]})
	}
	if req.AssumeMissingRunCost && !p.RunningCost.Known && p.SetupCost.Known {
		v := money.Amount{Paise: p.SetupCost.Paise * th.AssumedRunCostPct / 100, Known: true}
		p.RunningCost = v
		p.Sources["annual_running_cost_inr"] = domain.SourceTeam
		a := domain.Assumption{Field: "annual_running_cost_inr", Value: v.String(), Source: domain.SourceTeam,
			Rule: fmt.Sprintf("setup_cost_inr x %d%% (extension.json defaults.assumed_run_cost_pct_of_setup)", th.AssumedRunCostPct)}
		p.Assumptions = append(p.Assumptions, a)
		res.Assumptions = append(res.Assumptions, a)
	}
	res.Baseline = decision.Evaluate(base, th, decision.Context{GroupPeers: peers(base)})
	res.Scenario = decision.Evaluate(p, th, decision.Context{GroupPeers: peers(p)})
	return res, nil
}
