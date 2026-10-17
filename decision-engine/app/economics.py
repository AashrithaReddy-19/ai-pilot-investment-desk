"""Economics and budgets (A3). Pure functions, no I/O.

Splits a proposal's benefit into the four amounts a finance reader asks about, checks it
against a budget set, and classes its cost competitiveness:

- gross cost avoidance  = the cost of the work the AI takes off people (the typed annual benefit,
                          or baseline cost × automation share when a baseline is supplied)
- cashable savings      = the part of gross that actually leaves the books
                          (gross × cashable share; the share defaults to the benefit type's cash factor)
- capacity value        = gross − cashable: hours freed but still paid for
- retained human cost   = baseline cost − gross: the human work that stays (review, edits, exceptions)

Missing is never zero: without a baseline, baseline cost and retained human cost are None and a
flag says so. Every threshold lives in ECONOMICS_RULES, shown in /defaults.
"""
from __future__ import annotations
import math
from typing import Any, Optional

from . import core

# Competitiveness: annualised AI cost (setup spread over the horizon + running cost) per rupee of
# gross benefit — or, when a baseline gives a per-unit human cost, AI cost per unit ÷ human cost
# per unit. Lower is better. Upper edges, inclusive.
COMPETITIVENESS_EDGES: list[tuple[str, float]] = [
    ("highly_competitive", 0.40),
    ("competitive", 0.70),
    ("marginal", 1.00),
]
COMPETITIVENESS_WORST = "uncompetitive"
BUDGET_STATUS_ORDER = ["within", "at_risk", "over"]   # worst wins; "unknown" when nothing is checkable
DEFAULT_CONTINGENCY_PCT = 10

ECONOMICS_RULES = {
    "competitiveness_edges": dict(COMPETITIVENESS_EDGES),
    "competitiveness_worst": COMPETITIVENESS_WORST,
    "competitiveness_ratio": "annualised AI cost ÷ gross benefit, or AI cost per unit ÷ human cost per unit "
                             "when the baseline gives volume and per-unit human cost",
    "annualised_ai_cost": "setup × 12 ÷ horizon_months + annual running cost",
    "budget_status_order": BUDGET_STATUS_ORDER,
    "default_contingency_pct": DEFAULT_CONTINGENCY_PCT,
    "budget_checks": {
        "implementation_inr": "setup ≤ implementation budget",
        "first_year_inr": "setup + first-12-month running cost ≤ first-year budget",
        "recurring_inr_per_year": "annual running cost ≤ recurring budget",
        "total_inr": "(setup + running cost over the horizon) × (1 + contingency) ≤ total budget",
    },
    "at_risk_rule": "fits without contingency but not with it (total), or uses more than 90% of a line",
    "baseline_fields": ["annual_cost_inr", "volume_per_month", "minutes_per_unit", "loaded_cost_per_hour",
                        "automation_share_pct"],
}
AT_RISK_USAGE = 0.90


class EconomicsError(ValueError):
    """Bad economics input: shown as a 422."""


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def validate_baseline(b: Optional[dict], where: str = "baseline") -> Optional[dict]:
    if b is None:
        return None
    if not isinstance(b, dict):
        raise EconomicsError(f"{where} must be an object.")
    unknown = set(b) - set(ECONOMICS_RULES["baseline_fields"])
    if unknown:
        raise EconomicsError(f"{where}: unknown field(s) {sorted(unknown)}; allowed {ECONOMICS_RULES['baseline_fields']}.")
    out = {}
    for k, v in b.items():
        if v is None:
            continue
        if not _num(v) or v < 0:
            raise EconomicsError(f"{where}.{k} must be a non-negative number or null, got {v!r}.")
        if k == "automation_share_pct" and v > 100:
            raise EconomicsError(f"{where}.automation_share_pct must be within 0-100, got {v!r}.")
        out[k] = v
    return out


def validate_budget_set(bs: Optional[dict]) -> Optional[dict]:
    if bs is None:
        return None
    if not isinstance(bs, dict):
        raise EconomicsError("budget_set must be an object.")
    allowed = {"total_inr", "first_year_inr", "recurring_inr_per_year", "implementation_inr", "contingency_pct"}
    unknown = set(bs) - allowed
    if unknown:
        raise EconomicsError(f"budget_set: unknown field(s) {sorted(unknown)}; allowed {sorted(allowed)}.")
    out = {}
    for k, v in bs.items():
        if v is None:
            continue
        if not _num(v) or v < 0:
            raise EconomicsError(f"budget_set.{k} must be a non-negative number or null, got {v!r}.")
        if k == "contingency_pct" and v > 100:
            raise EconomicsError("budget_set.contingency_pct must be within 0-100.")
        out[k] = v
    return out


def baseline_cost(b: Optional[dict]) -> tuple[Optional[float], Optional[str]]:
    """Annual cost of the process today: stated, or volume × minutes × hourly rate × 12."""
    if not b:
        return None, None
    if _num(b.get("annual_cost_inr")):
        return float(b["annual_cost_inr"]), "stated annual cost"
    vol, mins, rate = b.get("volume_per_month"), b.get("minutes_per_unit"), b.get("loaded_cost_per_hour")
    if all(_num(x) for x in (vol, mins, rate)):
        return vol * mins / 60 * rate * 12, f"{vol:,.0f} units/month × {mins:g} min × INR {rate:,.0f}/h × 12"
    return None, None


def split(p: "core.Pilot", baseline: Optional[dict] = None, cashable_share_pct: Optional[float] = None) -> dict:
    """Gross cost avoidance vs cashable savings vs capacity value vs retained human cost."""
    flags = []
    base_cost, base_note = baseline_cost(baseline)
    if base_cost is not None and baseline and _num(baseline.get("automation_share_pct")):
        gross, gross_note = base_cost * baseline["automation_share_pct"] / 100, \
            f"baseline × automation share {baseline['automation_share_pct']:g}%"
    else:
        gross, gross_note = p.benefit, "typed annual benefit"
    if base_cost is None:
        flags.append({"code": "BASELINE_MISSING",
                      "message": "No baseline (today's cost of the process); retained human cost is unknown."})
    elif gross > base_cost:
        flags.append({"code": "BENEFIT_EXCEEDS_BASELINE",
                      "message": f"Gross benefit {core._inr(gross)} exceeds today's process cost {core._inr(base_cost)}: "
                                 "the AI cannot avoid more than the work costs. Check the benefit."})
    if _num(cashable_share_pct):
        share, share_src = cashable_share_pct / 100, "stated cashable share"
    else:
        share, share_src = core.cash_factor(p.benefit_type), f"cash factor of benefit type {p.benefit_type!r}"
    cashable = gross * share
    capacity = gross - cashable
    retained = None if base_cost is None else max(0.0, base_cost - gross)
    return {
        "baseline_cost_inr": None if base_cost is None else round(base_cost),
        "baseline_note": base_note,
        "gross_cost_avoidance_inr": round(gross), "gross_note": gross_note,
        "cashable_savings_inr": round(cashable), "capacity_value_inr": round(capacity),
        "retained_human_cost_inr": None if retained is None else round(retained),
        "cashable_share_pct": round(share * 100, 1), "cashable_share_source": share_src,
        "net_cashable_inr": round(cashable - p.run),
        "flags": flags,
    }


def competitiveness(p: "core.Pilot", gross: float, horizon: int, baseline: Optional[dict] = None) -> dict:
    annual_cost = p.setup * 12 / horizon + p.run
    vol, mins, rate = (baseline or {}).get("volume_per_month"), (baseline or {}).get("minutes_per_unit"), \
        (baseline or {}).get("loaded_cost_per_hour")
    if all(_num(x) for x in (vol, mins, rate)) and vol > 0 and mins > 0 and rate > 0:
        human_unit = mins / 60 * rate
        ai_unit = annual_cost / (vol * 12)
        ratio, basis = ai_unit / human_unit, (f"AI INR {ai_unit:,.2f}/unit vs human INR {human_unit:,.2f}/unit")
    elif gross > 0:
        ratio, basis = annual_cost / gross, f"annualised AI cost {core._inr(annual_cost)} ÷ gross benefit {core._inr(gross)}"
    else:
        return {"class": None, "ratio": None, "basis": "no gross benefit: competitiveness undefined",
                "annualised_ai_cost_inr": round(annual_cost)}
    cls = next((name for name, edge in COMPETITIVENESS_EDGES if ratio <= edge), COMPETITIVENESS_WORST)
    return {"class": cls, "ratio": round(ratio, 3), "basis": basis, "annualised_ai_cost_inr": round(annual_cost)}


def _line(name: str, need: float, have: Optional[float], note: str) -> dict:
    if have is None:
        return {"line": name, "need_inr": round(need), "budget_inr": None, "status": "unknown", "note": note}
    if need > have:
        status = "over"
    elif have > 0 and need > have * AT_RISK_USAGE:
        status = "at_risk"
    else:
        status = "within"
    return {"line": name, "need_inr": round(need), "budget_inr": round(have), "status": status, "note": note}


def budget_status(setup: float, run: float, horizon: int, bs: Optional[dict]) -> dict:
    """Check one proposal (or a portfolio's summed costs) against every line of the budget set."""
    bs = bs or {}
    cont = bs.get("contingency_pct", DEFAULT_CONTINGENCY_PCT)
    first_year = setup + run * min(12, horizon) / 12
    total_raw = setup + run * horizon / 12
    total = total_raw * (1 + cont / 100)
    lines = [
        _line("implementation_inr", setup, bs.get("implementation_inr"), "setup ≤ implementation budget"),
        _line("first_year_inr", first_year, bs.get("first_year_inr"), "setup + 12 months of running cost"),
        _line("recurring_inr_per_year", run, bs.get("recurring_inr_per_year"), "annual running cost"),
        _line("total_inr", total, bs.get("total_inr"),
              f"(setup + running over {horizon} months) × (1 + {cont:g}% contingency)"),
    ]
    tot = lines[3]
    if tot["status"] == "over" and bs.get("total_inr") is not None and total_raw <= bs["total_inr"]:
        tot["status"] = "at_risk"
        tot["note"] += "; fits only without the contingency"
    checked = [ln["status"] for ln in lines if ln["status"] != "unknown"]
    overall = max(checked, key=BUDGET_STATUS_ORDER.index) if checked else "unknown"
    return {"status": overall, "contingency_pct": cont, "lines": lines,
            "total_with_contingency_inr": round(total)}


def economics_for(p: "core.Pilot", d: dict, bs: Optional[dict] = None, baseline: Optional[dict] = None,
                  cashable_share_pct: Optional[float] = None) -> dict:
    horizon = d.get("horizon_months", core.HORIZON_MONTHS)
    horizon = int(horizon) if _num(horizon) and horizon >= 1 else core.HORIZON_MONTHS
    sp = split(p, baseline, cashable_share_pct)
    return {**sp,
            "competitiveness": competitiveness(p, sp["gross_cost_avoidance_inr"], horizon, baseline),
            "budget": budget_status(p.setup, p.run, horizon, bs)}


def portfolio_budget(pilots: list, ids: list, d: dict, bs: Optional[dict]) -> dict:
    """Budget status of a funded set: summed setup and running cost against the same budget lines."""
    chosen = [p for p in pilots if p.id in set(ids)]
    horizon = d.get("horizon_months", core.HORIZON_MONTHS)
    horizon = int(horizon) if _num(horizon) and horizon >= 1 else core.HORIZON_MONTHS
    return {"ids": [p.id for p in chosen],
            **budget_status(sum(p.setup for p in chosen), sum(p.run for p in chosen), horizon, bs)}
