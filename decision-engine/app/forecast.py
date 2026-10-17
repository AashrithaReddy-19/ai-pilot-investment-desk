"""Token-cost model (Note A point 3 and 8). Pure functions, no I/O.

Missing is never zero: missing tokens or price yields a flag and NO number.
"""
from __future__ import annotations
import math
from typing import Any, Optional


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def ai_run_cost_inr(tokens_per_month, price_per_1m_tokens, fixed_annual_inr=0) -> Optional[float]:
    """monthly_tokens × 12 × price_per_1M ÷ 1,000,000 + fixed annual. None when a required input is missing."""
    if not (_num(tokens_per_month) and _num(price_per_1m_tokens)):
        return None
    if tokens_per_month < 0 or price_per_1m_tokens < 0:
        return None  # a negative input is invalid, not a number we would invent
    return tokens_per_month * 12 * price_per_1m_tokens / 1_000_000 + (_num(fixed_annual_inr) and fixed_annual_inr or 0)


def next_fy_cost_inr(tokens_per_month, price_per_1m_tokens, fixed_annual_inr, usage_growth_pct, price_drift_pct) -> Optional[float]:
    """Next FY: tokens × (1 + usage growth) and price × (1 + price drift); the fixed annual part does not grow.
    Returns None when tokens or price are missing (never invents)."""
    if not (_num(tokens_per_month) and _num(price_per_1m_tokens)):
        return None
    g = (usage_growth_pct if _num(usage_growth_pct) else 0.0) / 100
    p = (price_drift_pct if _num(price_drift_pct) else 0.0) / 100
    return tokens_per_month * (1 + g) * 12 * price_per_1m_tokens * (1 + p) / 1_000_000 + (_num(fixed_annual_inr) and fixed_annual_inr or 0)


def inflation_flag(usage_growth_pct) -> Optional[bool]:
    """Note A: same or higher usage carries AI spend into the next FY plan. None when growth itself is unknown."""
    if not _num(usage_growth_pct):
        return None
    return usage_growth_pct >= 0


def value_per_1m_tokens(net_annual_savings: float, tokens_per_month) -> Optional[float]:
    """(net annual savings ÷ annual tokens in millions). Compare it with price per 1M: above 1× price the agent earns more than it costs."""
    if not _num(net_annual_savings) or not _num(tokens_per_month) or tokens_per_month <= 0:
        return None
    return net_annual_savings / (tokens_per_month * 12 / 1_000_000)
