// Package domain defines the pilot model, provenance labels and strict input parsing.
package domain

import (
	"time"

	"c23desk/internal/money"
)

// Provenance labels: every stored field says where its value came from.
const (
	SourceOfficial   = "OFFICIAL_FIXTURE"
	SourceTeam       = "TEAM_ASSUMPTION"
	SourceObserved   = "OBSERVED"
	SourceCalculated = "CALCULATED"
	SourceSynthetic  = "SYNTHETIC"
	SourceUnknown    = "UNKNOWN"
)

// Canonical benefit types. The official fixture uses "cashable assumption" -> cashable.
const (
	BenefitCashable  = "cashable"
	BenefitTimeFreed = "time_freed"
	BenefitOther     = "other"
	BenefitUnknown   = "unknown"
)

// Pilot is one AI pilot. All money/percent fields are nullable: Known=false means UNKNOWN, never zero.
type Pilot struct {
	ID          string  `json:"id"`
	Task        string  `json:"task"`
	Description *string `json:"description"`

	SetupCost          money.Amount `json:"setup_cost_inr"`
	RunningCost        money.Amount `json:"annual_running_cost_inr"`
	AnnualBenefit      money.Amount `json:"annual_benefit_inr"`
	CapacityValue      money.Amount `json:"capacity_value_inr"`
	RealizedCashSaving money.Amount `json:"realized_cash_saving_inr"`

	BenefitType     string  `json:"benefit_type"`
	BenefitTypeRaw  *string `json:"benefit_type_raw"`
	OverlapGroup    *string `json:"overlap_group"` // normalised; nil = OVERLAP_UNKNOWN
	OverlapGroupRaw *string `json:"overlap_group_raw"`

	ExpectedAdoption   money.Pct `json:"expected_adoption_pct"`
	ActualAdoption     money.Pct `json:"actual_adoption_pct"`
	MinimumAdoption    money.Pct `json:"minimum_adoption_pct"`
	ExpectedQuality    money.Pct `json:"expected_quality_pct"`
	ActualQuality      money.Pct `json:"actual_quality_pct"`
	MinimumQuality     money.Pct `json:"minimum_quality_pct"`
	DataReadiness      money.Pct `json:"data_readiness_pct"`
	TechnicalReadiness money.Pct `json:"technical_readiness_pct"`
	ProcessReadiness   money.Pct `json:"process_readiness_pct"`
	ReadinessLabel     *string   `json:"readiness_label"` // low|medium|high (team assumption in extension.json)

	Sources     map[string]string `json:"field_sources"`
	Assumptions []Assumption      `json:"assumptions"` // documented TEAM_ASSUMPTION imputations (scenarios only)
	Version     int               `json:"version"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Assumption documents one value that was imputed (never silently).
type Assumption struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Rule   string `json:"rule"`
	Source string `json:"source"`
}

// FinanceComplete reports whether the three official money inputs are all known.
func (p Pilot) FinanceComplete() bool {
	return p.SetupCost.Known && p.RunningCost.Known && p.AnnualBenefit.Known
}

// GroupKey returns the normalised overlap group, or "" when unknown.
func (p Pilot) GroupKey() string {
	if p.OverlapGroup == nil {
		return ""
	}
	return *p.OverlapGroup
}

// Clone returns a deep copy (pointers and map), safe for scenario overrides.
func (p Pilot) Clone() Pilot {
	c := p
	c.Description = cloneStr(p.Description)
	c.BenefitTypeRaw = cloneStr(p.BenefitTypeRaw)
	c.OverlapGroup = cloneStr(p.OverlapGroup)
	c.OverlapGroupRaw = cloneStr(p.OverlapGroupRaw)
	c.ReadinessLabel = cloneStr(p.ReadinessLabel)
	c.Sources = make(map[string]string, len(p.Sources))
	for k, v := range p.Sources {
		c.Sources[k] = v
	}
	c.Assumptions = append([]Assumption(nil), p.Assumptions...)
	return c
}

func cloneStr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// Str is a small helper for building *string literals.
func Str(s string) *string { return &s }
