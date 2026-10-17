"""What-if overrides and one-variable sensitivity sweeps. Pure functions, no I/O.

Phase 3: one variable changes at a time against a base scenario. Overrides never mutate the
caller's dicts — everything is rebuilt on deep copies. Missing is never zero; a sweep over an
input the scenario does not know (e.g. no token price recorded) yields flags and no numbers.
"""
from __future__ import annotations
import copy
import math
from typing import Any, Callable, Optional

from . import core

# Allowed override keys, split by the layer they act on.
PILOT_KEYS = {"setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr",
              "adoption_pct", "readiness", "observed_adoption_pct", "observed_quality_pct",
              "ramp_months", "delay_months"}
ORG_KEYS = {"budget", "mode", "horizon_months", "discount_rate",
            "tokens_per_month", "price_per_1m_tokens", "annual_usage_growth_pct", "annual_price_drift_pct"}

# Sweep axes: deterministic step ladders, documented in README and /defaults.
SWEEP_FRACTIONS = [0.5, 0.75, 1.0, 1.25, 1.5]
SWEEP_GROWTH_PCT = [-20, -10, 0, 10, 20, 30]
SWEEP_PARAMS = {"adoption_pct", "annual_benefit_inr", "annual_running_cost_inr",   # pilot-level
                "price_per_1m_tokens", "annual_usage_growth_pct"}                  # org-level
FLIP_SCAN_STEP = 0.05    # fraction-of-base scan for cost/benefit flips
FLIP_ADOPTION_STEP = 1   # whole-percent scan for adoption flips
FLIP_FIELDS = {"observed_adoption_pct", "annual_benefit_inr", "annual_running_cost_inr", "setup_cost_inr"}


class OverrideError(ValueError):
    """Invalid what-if override: shown to the user as a 422 by the gateway."""


def _nonneg(x: Any, name: str, allow_none: bool = False, max_val: Optional[float] = None) -> None:
    if x is None:
        if allow_none:
            return  # explicit null withdraws the input (never zeroes it)
        raise OverrideError(f"{name} must be a number, got null.")
    if isinstance(x, bool) or not isinstance(x, (int, float)) or not math.isfinite(x) or x < 0:
        raise OverrideError(f"{name} must be a non-negative number, got {x!r}.")
    if max_val is not None and x > max_val:
        raise OverrideError(f"{name} must be within 0..{max_val}, got {x!r}.")


def validate(overrides: Any) -> dict:
    """Validate the override set and return the clean subset. Unknown keys and bad values fail loudly."""
    if overrides is None:
        return {}
    if not isinstance(overrides, dict):
        raise OverrideError("overrides must be a JSON object.")
    unknown = set(overrides) - PILOT_KEYS - ORG_KEYS
    if unknown:
        raise OverrideError(f"Unknown override key(s): {', '.join(sorted(unknown))}. "
                            f"Allowed: {', '.join(sorted(PILOT_KEYS | ORG_KEYS))}.")
    for k, v in overrides.items():
        if k in ("setup_cost_inr", "annual_benefit_inr", "tokens_per_month", "price_per_1m_tokens", "budget"):
            _nonneg(v, k)
        elif k == "annual_running_cost_inr":
            _nonneg(v, k, allow_none=True)
        elif k == "adoption_pct":
            _nonneg(v, k, max_val=100)
        elif k in ("observed_adoption_pct", "observed_quality_pct"):
            _nonneg(v, k, allow_none=True, max_val=100)
        elif k == "readiness":
            if v not in core.RANK:
                raise OverrideError(f"readiness must be one of {sorted(core.RANK)}, got {v!r}.")
        elif k in ("ramp_months", "delay_months", "horizon_months"):
            _nonneg(v, k, max_val=120)
            if not float(v).is_integer():
                raise OverrideError(f"{k} must be a whole number of months.")
        elif k == "mode":
            if v not in ("independent", "conservative"):
                raise OverrideError("mode override must be independent or conservative.")
        elif k == "discount_rate":
            if isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) or not (0 <= v <= 1):
                raise OverrideError(f"discount_rate must be 0..1, got {v!r}.")
        elif k in ("annual_usage_growth_pct", "annual_price_drift_pct"):
            if isinstance(v, bool) or not isinstance(v, (int, float)) or not math.isfinite(v) or abs(v) > 10000:
                raise OverrideError(f"{k} must be a finite percentage, got {v!r}.")
    return overrides


# An explicit override must be the authoritative source: strip the computation path that would
# otherwise silently swallow it (a benefit driver would override a typed benefit figure, etc.).
COMPANION_STRIPS = {"annual_benefit_inr": ["benefit_driver"],
                    "setup_cost_inr": ["one_time_costs"],
                    "annual_running_cost_inr": ["recurring_costs"]}
EXT_ONLY_KEYS = {"adoption_pct", "readiness", "observed_adoption_pct", "observed_quality_pct",
                 "ramp_months", "delay_months"}


def apply(base_fixture: dict, base_ext: dict, overrides: dict) -> tuple:
    """Deep-copy the fixture and extension, overlay the overrides, return the mutated copies.

    The originals are never touched. Each override is written where the loader reads it:
    fixture-driven fields (setup / run / benefit) on the fixture row, extension-driven fields
    (adoption / readiness / observed / ramp / delay) on the extension row, creating that row
    for fixture-only pilots. Overriding a computed path strips it (a cost-line driver would
    otherwise silently swallow the override). An explicit null on the run cost or on observed
    evidence withdraws the input (never zeroes it) — the loader then reverts to the documented
    assumption path.
    """
    fixture, ext = copy.deepcopy(base_fixture), copy.deepcopy(base_ext)
    frows = (fixture.get("records") or {}).get("pilots") or []
    if not isinstance(frows, list):
        raise OverrideError("fixture.records.pilots must be an array to apply pilot overrides.")
    if ext.get("pilots") is not None and not isinstance(ext.get("pilots"), list):
        raise OverrideError("extension.pilots must be an array to apply pilot overrides.")
    epilots = ext.get("pilots") or []
    nullables = ("annual_running_cost_inr", "observed_adoption_pct", "observed_quality_pct")
    pilot_ov = {k: v for k, v in overrides.items() if k in PILOT_KEYS and (v is not None or k in nullables)}
    pids = sorted({r.get("id") for r in frows if isinstance(r, dict)})
    epilots = ext.get("pilots") or []
    for pid in pids:
        frow = next((r for r in frows if isinstance(r, dict) and r.get("id") == pid), None)
        erow = next((r for r in epilots if isinstance(r, dict) and r.get("id") == pid), None)
        for k, v in pilot_ov.items():
            for strip in COMPANION_STRIPS.get(k, ()):  # the override wins over drivers and cost lines
                if frow is not None: frow.pop(strip, None)
                if erow is not None: erow.pop(strip, None)
            if k in EXT_ONLY_KEYS:
                if erow is None:                       # fixture-only pilot: create the extension row
                    erow = {"id": pid}
                    epilots.append(erow)
                    if ext.get("pilots") is None:
                        ext["pilots"] = epilots
                erow[k] = v
            elif frow is not None:
                frow[k] = v
    for k in ("horizon_months", "discount_rate", "tokens_per_month", "price_per_1m_tokens",
              "annual_usage_growth_pct", "annual_price_drift_pct"):
        if k in overrides:
            ext.setdefault("defaults", {})[k] = overrides[k]
    return fixture, ext


def pilot_numbers(p: "core.Pilot", mode: str, d: dict) -> dict:
    """The screen numbers for one pilot in one mode (net, payback, multi-period summary)."""
    v = core.view(p, mode, d)
    mp = v["multi_period"]
    return {"benefit_inr": round(v["benefit"]), "net": round(v["net"]),
            "payback_months": None if v["payback_months"] is None else round(v["payback_months"], 1),
            "profit_inr": mp["profit_inr"], "roi_pct": mp["roi_pct"], "npv_inr": mp["npv_inr"],
            "breakeven_month": mp["breakeven_month"]}


def flip_scan(field: str, reload_pilots: Callable, from_verdict: str, target_verdict: str,
              base_value: Any, pilot_id: str, d_base: dict) -> Optional[str]:
    """Smallest value of ONE overridden pilot field at which the stop-rule reaches target_verdict.

    reload_pilots(value) rebuilds the pilot set with that single value set on the field.
    Only run when exactly one pilot-level override is present (an unambiguous statement).
    """
    if field not in FLIP_FIELDS:
        return None
    if field == "observed_adoption_pct":
        values = list(range(0, 101, FLIP_ADOPTION_STEP))
        where = lambda v: f"observed adoption reaches {v}%"
    else:
        base = float(base_value) if _numok(base_value) else 0.0
        values = [round(base * i * FLIP_SCAN_STEP) for i in range(0, int(3.0 / FLIP_SCAN_STEP) + 1)]
        where = (lambda v: f"{field} reaches INR {v:,.0f} ({v / base * 100:.0f}% of base)"
                 if base else lambda v: f"{field} rises from INR 0 to INR {v:,.0f}")
    for v in values:
        p = next((x for x in reload_pilots(v) if x.id == pilot_id), None)
        if p is None:
            return None
        if core.evaluate_stop_rule(p, d_base)["decision"] == target_verdict:
            return f"{from_verdict} becomes {target_verdict} when {where(v)}."
    return None


def sweep_steps(base_value: Any, parameter: str) -> list:
    """The deterministic step ladder (absolute values) for one sweep parameter."""
    if parameter == "adoption_pct":
        return list(range(0, 101, 10))
    if parameter == "annual_usage_growth_pct":
        return list(SWEEP_GROWTH_PCT)
    if _numok(base_value):
        base = float(base_value)
        return [round(base * f) for f in SWEEP_FRACTIONS]
    return []


def _numok(x) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def monotonic_ok(values: list, better_is_lower: bool) -> bool:
    """Adjacent values must move in the stated improvement direction (flat steps allowed)."""
    for a, b in zip(values, values[1:]):
        d = b - a
        if abs(d) < 1e-9:
            continue
        if better_is_lower and d < 0:
            continue
        if not better_is_lower and d > 0:
            continue
        return False
    return True
