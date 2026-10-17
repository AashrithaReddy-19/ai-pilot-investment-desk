"""Multi-period cash flow, NPV, ROI and break-even. Pure functions, no I/O.

Conventions (documented, one place, used by every layer):
- Months are numbered 1..horizon_months. The one-time cost lands in month 1 (period 0 in the notes).
- Benefits start after `delay_months`; while ramping (`ramp_months >= 2`) month i after the delay earns
  benefit × (i+1)/(ramp_months), capped at 1.0. Defaults are full benefit from day one.
- Run cost is charged from month 1 while the pilot exists — conservative: waiting is not free.
- Break-even month = first t where cumulative cash flow >= 0 (monthly grid, 1-based).
- NPV discounts each monthly flow by (1 + r)^(t/12) with an annual rate r; at r = 0 it equals profit/loss.
- Missing is never zero: callers never invent a zero for a missing input.
"""
from __future__ import annotations
import math
from typing import Any, Optional


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def ramp_factor(t: int, ramp_months: int, delay_months: int) -> float:
    """Benefit multiplier for month t (1-based). Full benefit from day one when no ramp/delay."""
    active = t - max(0, delay_months)
    if active <= 0:
        return 0.0
    if ramp_months is None or ramp_months < 2:
        return 1.0
    return min(1.0, active / ramp_months)


def monthly_cash_flows(one_time_inr: float, net_benefit_per_month: float, run_per_month: float,
                       horizon_months: int, ramp_months: int = 0, delay_months: int = 0) -> list:
    """One cash-flow number per month. net_benefit_per_month is benefit before ramp/delay minus... no:
    pass full monthly benefit; run cost is deducted here."""
    flows: list = []
    for t in range(1, max(1, horizon_months) + 1):
        ben = net_benefit_per_month * ramp_factor(t, ramp_months, delay_months)
        cf = ben - run_per_month - (one_time_inr if t == 1 else 0.0)
        flows.append(cf)
    return flows


def cumulative(cflows: list) -> list:
    out, total = [], 0.0
    for cf in cflows:
        total += cf
        out.append(total)
    return out


def breakeven_month_from_cumulative(cum: list) -> Optional[int]:
    """First month where cumulative >= 0, else None (beyond the horizon).

    Comparison happens on the rupee value (2 decimals): repeating decimals such as
    INR 400,000/12 can leave 1e-10 dust after an exact 24-month crossing, which must
    not push the break-even a whole month late.
    """
    for i, v in enumerate(cum):
        if round(v, 2) >= 0:
            return i + 1
    return None


def profit_loss(cflows: list) -> float:
    return sum(cflows)


def roi_pct(profit: float, one_time_inr: float) -> Optional[float]:
    if not _num(one_time_inr) or one_time_inr <= 0:
        return None  # a free pilot has no ROI against its cost
    return profit / one_time_inr * 100


def npv(cflows: list, discount_rate: float, months_per_step: int = 1) -> float:
    """Σ cashflow(t) ÷ (1+r)^(t / 12) for 1-based months; r may be 0 (= profit/loss)."""
    total = 0.0
    for t, cf in enumerate(cflows):
        total += cf / (1.0 + discount_rate) ** ((t + 1) / 12.0)
    return total
