// Package finance contains the pure, exact financial formulas. No I/O, no database, no floats.
//
//	Net Annual Benefit  = Annual Benefit - Annual Running Cost
//	Monthly Net Benefit = Net Annual Benefit / 12
//	Simple Payback      = Setup Cost / Monthly Net Benefit       (null when monthly net <= 0: NEVER_PAYS_BACK)
//	ROI %               = Net Annual Benefit / Setup Cost * 100  (null when setup = 0)
//
// Any unknown input makes the dependent output unknown (NOT_CALCULABLE). Unknown is never zero.
package finance

import (
	"math/big"

	"c23desk/internal/money"
)

// Payback status values.
const (
	PaybackCalculable    = "CALCULABLE"
	PaybackNotCalculable = "NOT_CALCULABLE" // an input is unknown
	PaybackNever         = "NEVER_PAYS_BACK"
	PaybackImmediate     = "IMMEDIATE" // setup cost is zero and net is positive
)

var twelve = big.NewRat(12, 1)

// NetAnnualBenefit = benefit - running. Unknown if either input is unknown.
func NetAnnualBenefit(benefit, running money.Amount) money.Dec {
	if !benefit.Known || !running.Known {
		return money.Dec{}
	}
	return money.Dec{R: new(big.Rat).Sub(benefit.Rat(), running.Rat())}
}

// MonthlyNetBenefit = net annual / 12.
func MonthlyNetBenefit(netAnnual money.Dec) money.Dec {
	if !netAnnual.Known() {
		return money.Dec{}
	}
	return money.Dec{R: new(big.Rat).Quo(netAnnual.R, twelve)}
}

// Payback is the simple payback result with an explicit status.
type Payback struct {
	Months money.Dec `json:"payback_months"`
	Status string    `json:"payback_status"`
}

// PaybackMonths = setup / monthly net. setup=0 with positive net pays back immediately (0 months).
func PaybackMonths(setup money.Amount, monthlyNet money.Dec) Payback {
	if !setup.Known || !monthlyNet.Known() {
		return Payback{Status: PaybackNotCalculable}
	}
	if monthlyNet.R.Sign() <= 0 {
		return Payback{Status: PaybackNever}
	}
	if setup.Paise == 0 {
		return Payback{Months: money.Dec{R: new(big.Rat)}, Status: PaybackImmediate}
	}
	return Payback{Months: money.Dec{R: new(big.Rat).Quo(setup.Rat(), monthlyNet.R)}, Status: PaybackCalculable}
}

// ROIPct = net annual / setup * 100. Unknown when setup is 0 (undefined) or any input unknown.
func ROIPct(netAnnual money.Dec, setup money.Amount) money.Dec {
	if !netAnnual.Known() || !setup.Known || setup.Paise == 0 {
		return money.Dec{}
	}
	r := new(big.Rat).Quo(netAnnual.R, setup.Rat())
	return money.Dec{R: r.Mul(r, big.NewRat(100, 1))}
}

// BreakevenAdoptionPct: adoption at which recognised benefit (assumed linear in adoption) covers running cost.
// = running / benefit * 100. Unknown if inputs unknown or benefit is 0; values above 100 mean it can never break even.
func BreakevenAdoptionPct(running, benefit money.Amount) money.Dec {
	if !running.Known || !benefit.Known || benefit.Paise == 0 {
		return money.Dec{}
	}
	r := new(big.Rat).Quo(running.Rat(), benefit.Rat())
	return money.Dec{R: r.Mul(r, big.NewRat(100, 1))}
}

// SumAmounts adds known amounts. ok=false if any amount is unknown (the total is then unknown).
func SumAmounts(xs ...money.Amount) (money.Amount, bool) {
	var total int64
	for _, x := range xs {
		if !x.Known {
			return money.Amount{}, false
		}
		total += x.Paise
	}
	return money.Amount{Paise: total, Known: true}, true
}

// BudgetRemaining = budget - total setup (may be negative: over budget).
func BudgetRemaining(budget, totalSetup money.Amount) money.Dec {
	if !budget.Known || !totalSetup.Known {
		return money.Dec{}
	}
	return money.Dec{R: new(big.Rat).Sub(budget.Rat(), totalSetup.Rat())}
}

// Metrics bundles the standard per-pilot calculation on a given benefit basis.
type Metrics struct {
	AnnualBenefit money.Amount `json:"annual_benefit_inr"`
	NetAnnual     money.Dec    `json:"net_annual_benefit_inr"`
	MonthlyNet    money.Dec    `json:"monthly_net_benefit_inr"`
	Payback
	ROIPct money.Dec `json:"roi_pct"`
}

// Compute runs the four core formulas on one basis.
func Compute(setup, running, benefit money.Amount) Metrics {
	net := NetAnnualBenefit(benefit, running)
	monthly := MonthlyNetBenefit(net)
	return Metrics{AnnualBenefit: benefit, NetAnnual: net, MonthlyNet: monthly, Payback: PaybackMonths(setup, monthly), ROIPct: ROIPct(net, setup)}
}
