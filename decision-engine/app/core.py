"""C23 decision core (Python port). Pure functions, no I/O. null/None is NEVER zero."""
from __future__ import annotations
import math, re
from dataclasses import dataclass, field, asdict
from typing import Any, Optional

RANK = {"low": 0, "medium": 1, "high": 2}
READINESS_FACTOR = {"low": 0.6, "medium": 0.8, "high": 1.0}
TIE_BREAK_ORDER = ["higher net benefit", "faster payback", "higher readiness",
                   "higher adoption", "lower setup cost", "lower id"]

# Engine-wide financial conventions (decision-table defaults; a request may override via extension defaults).
# Values shown in /defaults.pipeline_rules; editable per request through extension.defaults.
HORIZON_MONTHS = 36
DISCOUNT_RATE = 0.10
DEF = {"horizon_months": HORIZON_MONTHS, "discount_rate": DISCOUNT_RATE}

from . import cashflow, forecast  # noqa: E402  (placed after constants; both modules are pure)
from .solver import solve as _solve, Item as _Item, MAX_CANDIDATES as _MAX_SOLVER_CANDIDATES  # noqa: E402

# Recognised benefit types and their cash factor. Unknown types are rejected at load time.
# "non-cashable" → 0.0 (pilot is excluded: never earns cash).
# "time freed"   → 0.3 (time saving, not cash removal; conservative haircut).
# "cashable"     → 1.0 (cash leaves the business).
CASH_FACTOR_BY_TYPE: dict[str, float] = {
    "cashable assumption": 1.0,
    "cashable":            1.0,
    "time freed":          0.3,
    "non-cashable":        0.0,
    "unknown":             0.3,  # fallback when benefit_type is blank
}


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)

def _nat(s: str):
    return [int(t) if t.isdigit() else t for t in re.split(r"(\d+)", s)]

# ---------- Layer 1: exact finance (NO haircuts) ----------
def net_annual(benefit: float, run: float) -> float:
    return benefit - run

def payback_months(setup: float, benefit: float, run: float) -> Optional[float]:
    net = net_annual(benefit, run)
    return None if net <= 0 else setup / net * 12

# ---------- Layer 2: conservative adjustment ----------
def cash_factor(benefit_type: str) -> float:
    """Exact enum lookup; unknown types default to 0.3 (conservative)."""
    return CASH_FACTOR_BY_TYPE.get(benefit_type.strip().lower(), 0.3)

def realized_benefit(p: "Pilot") -> float:
    return p.benefit * (p.adoption_pct / 100) * READINESS_FACTOR[p.readiness] * cash_factor(p.benefit_type)

def breakeven_adoption_pct(p: "Pilot") -> Optional[float]:
    full = p.benefit * READINESS_FACTOR[p.readiness] * cash_factor(p.benefit_type)
    if full <= 0 or full <= p.run: return None
    return p.run / full * 100

# ---------- Model ----------
@dataclass
class Pilot:
    id: str; task: str; description: str
    setup: float; run: float; benefit: float
    benefit_type: str; group: str
    adoption_pct: float; readiness: str
    observed_adoption_pct: Optional[float] = None
    observed_quality_pct: Optional[float] = None
    flags: list = field(default_factory=list)
    # Phase 2 additive inputs (all optional; absent = the flat steady-state case above)
    one_time_costs: Optional[list] = None    # [{label, amount_inr}]; replaces setup when provided
    recurring_costs: Optional[list] = None   # [{label, amount_inr_per_year}]; replaces the assumed run when provided
    benefit_driver: Optional[dict] = None    # {volume_per_month, minutes_saved_per_unit, loaded_cost_per_hour}
    ramp_months: int = 0                     # linear benefit ramp after the delay, default full from day one
    delay_months: int = 0                    # months with no benefit while the pilot is built/rollout waits

PROVENANCE_FIXTURE = "fixture (synthetic)"
PROVENANCE_ASSUMED = "assumed (team)"
PROVENANCE_OBSERVED = "observed"
PROVENANCE_CALCULATED = "calculated"

def _inr(x: float) -> str:
    return f"INR {x:,.0f}"

def source_of_run(p: Pilot) -> tuple:
    if p.recurring_costs:
        return PROVENANCE_CALCULATED, f"sums {len(p.recurring_costs)} recurring cost line(s) supplied by the user; fixture run figure not used"
    if any(f["code"] == "RUN_COST_ASSUMED" for f in p.flags):
        return PROVENANCE_ASSUMED, "no recurring cost given; assumed 20% of setup (team assumption)"
    return PROVENANCE_FIXTURE, "annual_running_cost_inr from the fixture"

def view(p: Pilot, mode: str, d: Optional[dict] = None) -> dict:
    b = p.benefit if mode == "independent" else realized_benefit(p)
    out = {"id": p.id, "benefit": b, "net": net_annual(b, p.run), "payback_months": payback_months(p.setup, b, p.run)}
    if d is not None:  # additive multi-period view; steady state (no ramp/delay) bends nothing
        out["multi_period"] = multi_period(p, mode, d)
    return out

def multi_period(p: Pilot, mode: str, d: dict) -> dict:
    """36-month (default) cash-flow view of one pilot in one mode. Rounding: INR whole, pct 1 decimal."""
    horizon = d.get("horizon_months", HORIZON_MONTHS)
    r = d.get("discount_rate", DISCOUNT_RATE)
    if not _num(horizon) or horizon < 1: horizon = HORIZON_MONTHS
    if not _num(r) or r < 0 or r > 1: r = DISCOUNT_RATE
    b = p.benefit if mode == "independent" else realized_benefit(p)
    flows = cashflow.monthly_cash_flows(p.setup, b / 12, p.run / 12, int(horizon), p.ramp_months, p.delay_months)
    cum = cashflow.cumulative(flows)
    be = cashflow.breakeven_month_from_cumulative(cum)
    profit = cashflow.profit_loss(flows)
    roi = cashflow.roi_pct(profit, p.setup)
    npv_v = cashflow.npv(flows, r)
    tv = forecast.value_per_1m_tokens(net_annual(b, p.run), d.get("tokens_per_month"))
    price = d.get("price_per_1m_tokens")
    return {
        "horizon_months": int(horizon), "discount_rate": r,
        "profit_inr": round(profit), "roi_pct": None if roi is None else round(roi, 1), "npv_inr": round(npv_v),
        "breakeven_month": be, "breakeven_beyond_horizon": be is None, "cumulative": [round(v) for v in cum],
        "value_per_1m_tokens": None if tv is None else round(tv, 2),
        "value_vs_price_ratio": None if tv is None or not _num(price) or price <= 0 else round(tv / price, 2),
    }

def provenance(p: Pilot, d: Optional[dict] = None) -> dict:
    """Source of every decision-driving value: fixture (synthetic) vs observed vs assumed (team) vs calculated.
    Fixture values are never labelled observed."""
    run_src, run_note = source_of_run(p)
    if p.one_time_costs:
        setup_src, setup_note = PROVENANCE_CALCULATED, f"sums {len(p.one_time_costs)} one-time cost line(s) supplied by the user"
    else:
        setup_src, setup_note = PROVENANCE_FIXTURE, "setup_cost_inr from the fixture"
    if p.benefit_driver:
        vol, mins, rate = p.benefit_driver.get("volume_per_month"), p.benefit_driver.get("minutes_saved_per_unit"), p.benefit_driver.get("loaded_cost_per_hour")
        ben_src, ben_note = PROVENANCE_CALCULATED, (
            f"driver: {_inr(rate)}/h × {mins} min × {vol} units/month × 12" if all(_num(x) for x in (vol, mins, rate))
            else "benefit driver incomplete; typed fixture figure used")
    else:
        ben_src, ben_note = PROVENANCE_FIXTURE, "annual_benefit_inr from the fixture"
    prov = {
        "setup_inr": {"value": p.setup, "source": setup_src, "note": setup_note},
        "annual_running_cost_inr": {"value": p.run, "source": run_src, "note": run_note},
        "annual_benefit_inr": {"value": p.benefit, "source": ben_src, "note": ben_note},
        "benefit_type": {"value": p.benefit_type, "source": PROVENANCE_FIXTURE, "note":
                         f"cash factor {cash_factor(p.benefit_type)} by the rulebook"},
        "adoption_pct": {"value": p.adoption_pct, "source": PROVENANCE_ASSUMED, "note": "team-authored extension"},
        "readiness": {"value": p.readiness, "source": PROVENANCE_ASSUMED, "note": "team-authored extension"},
        "observed_adoption_pct": {"value": p.observed_adoption_pct, "source": PROVENANCE_OBSERVED if p.observed_adoption_pct is not None else None,
                                  "note": "not recorded yet" if p.observed_adoption_pct is None else "recorded measurement"},
        "observed_quality_pct": {"value": p.observed_quality_pct, "source": PROVENANCE_OBSERVED if p.observed_quality_pct is not None else None,
                                 "note": "not recorded yet" if p.observed_quality_pct is None else "recorded measurement"},
    }
    for mode in ("independent", "conservative"):
        v = view(p, mode)  # no default context: same numbers the endpoints show in the flat keys
        prov[f"net_{mode}"] = {"value": v["net"], "source": PROVENANCE_CALCULATED,
                               "note": f"= {_inr(v['benefit'])} − {_inr(p.run)} = {_inr(v['net'])}/yr"}
        pb = v["payback_months"]
        prov[f"payback_months_{mode}"] = {"value": pb, "source": PROVENANCE_CALCULATED,
                                          "note": None if pb is None else f"= setup {_inr(p.setup)} ÷ net {_inr(v['net'])}/yr × 12"
                                          if math.isfinite(pb) else None}
    if d is not None and d.get("horizon_months"):
        prov["multi_period"] = {"source": PROVENANCE_CALCULATED,
                                "note": f"Σ of monthly flows over {int(d['horizon_months'])} months; one-time cost in month 1; "
                                        f"NPV discounts flow t by (1+r)^(t/12) at r = {d.get('discount_rate', DISCOUNT_RATE)}"}
    return prov

# ---------- Loader / validation ----------
def load_pilots(raw: Any, ext: list, d: dict) -> dict:
    pilots, invalid, warnings = [], [], []
    lst = ((raw or {}).get("records") or {}).get("pilots") if isinstance(raw, dict) else None
    if not isinstance(lst, list):
        return {"pilots": [], "invalid": [{"id": "(file)", "errors": ["records.pilots missing or not an array"]}], "warnings": []}
    ext_by = {e.get("id"): e for e in ext}
    ids = {p.get("id") for p in lst if isinstance(p, dict)}
    for e in ext:
        if e.get("id") not in ids: warnings.append(f"Extension row {e.get('id')} has no matching pilot; ignored.")
    seen = set()
    for p in lst:
        p = p if isinstance(p, dict) else {}
        errs, pid = [], (p.get("id") if isinstance(p.get("id"), str) and p.get("id") else "(no id)")
        if pid == "(no id)": errs.append("id missing")
        if pid in seen: errs.append(f"duplicate id {pid}")
        seen.add(pid)
        if not p.get("task"): errs.append("task missing")
        for f in ("setup_cost_inr", "annual_benefit_inr"):
            v = p.get(f)
            if v is None: errs.append(f"{f} is missing (unknown, not zero)")
            elif not _num(v) or v < 0: errs.append(f"{f} must be a non-negative number, got {v!r}")
        run_raw = p.get("annual_running_cost_inr")
        if run_raw is not None and (not _num(run_raw) or run_raw < 0):
            errs.append(f"annual_running_cost_inr must be a non-negative number or null, got {run_raw!r}")
        if errs: invalid.append({"id": pid, "errors": errs}); continue
        flags, setup = [], p["setup_cost_inr"]
        if _num(run_raw): run = run_raw
        else:
            run = setup * d["assumed_run_cost_pct_of_setup"] / 100
            flags.append({"code": "RUN_COST_ASSUMED", "message": f"No recurring cost given; ASSUMED {d['assumed_run_cost_pct_of_setup']}% of setup = INR {run}/yr."})
        e = ext_by.get(pid) or {}
        ad = e.get("adoption_pct")
        if not _num(ad) or ad < 0 or ad > 100:
            if _num(ad): warnings.append(f"{pid}: adoption_pct {ad} outside 0-100; default used.")
            ad = d["assumed_adoption_pct"]
            flags.append({"code": "ADOPTION_ASSUMED", "message": f"Adoption unknown; ASSUMED {ad}%."})
        rd = e.get("readiness")
        if rd not in RANK:
            rd = "low"; flags.append({"code": "READINESS_ASSUMED", "message": "Data readiness unknown; ASSUMED low."})
        bt = (p.get("benefit_type") or "").strip().lower()
        if not bt:
            bt = "unknown"
            flags.append({"code": "BENEFIT_TYPE_UNKNOWN", "message": "Benefit type unknown; treated as time freed (30% cash)."})
        elif bt not in CASH_FACTOR_BY_TYPE:
            flags.append({"code": "BENEFIT_TYPE_UNKNOWN",
                          "message": f"Unrecognised benefit type {bt!r}; treated as time freed (30% cash). "
                                     f"Known types: {', '.join(CASH_FACTOR_BY_TYPE)}."})
            bt = "unknown"
        elif bt == "time freed":
            flags.append({"code": "TIME_NOT_CASH", "message": "Time freed is not cash; haircut to 30% unless cash removal is shown."})
        elif bt == "non-cashable":
            flags.append({"code": "BENEFIT_TYPE_NON_CASHABLE", "message": "Non-cashable benefit: cash factor is 0. Pilot will not be selected."})
        grp = p.get("overlap_group") or ""
        if not grp:
            grp = f"__own_{pid}"; flags.append({"code": "OVERLAP_UNKNOWN", "message": "Overlap group unknown; treated as standalone. Confirm with owner."})
        # Phase 2 additive inputs: cost lines (one-time / recurring), benefit driver, ramp-up and delay.
        def _lines(v, amount_key):
            if not isinstance(v, list): return None
            good = []
            for ln in v:
                if isinstance(ln, dict) and _num(ln.get(amount_key)) and ln[amount_key] >= 0:
                    good.append({"label": ln.get("label") or "cost line", amount_key: ln[amount_key]})
                elif ln is not None:
                    warnings.append(f"{pid}: ignored a malformed cost line (needs a numeric {amount_key}).")
            return good or None
        one_time = _lines(p.get("one_time_costs") if p.get("one_time_costs") is not None else e.get("one_time_costs"), "amount_inr")
        recurring = _lines(p.get("recurring_costs") if p.get("recurring_costs") is not None else e.get("recurring_costs"), "amount_inr_per_year")
        setup2 = sum(ln["amount_inr"] for ln in one_time) if one_time else setup
        run2 = sum(ln["amount_inr_per_year"] for ln in recurring) if recurring else run
        if recurring and not _num(run_raw):
            flags = [f for f in flags if f["code"] != "RUN_COST_ASSUMED"]  # a real supply replaces the assumption
        drv = e.get("benefit_driver") if isinstance(e.get("benefit_driver"), dict) else (p.get("benefit_driver") if isinstance(p.get("benefit_driver"), dict) else None)
        benefit2 = p["annual_benefit_inr"]
        if drv:
            vol, mins, rate = drv.get("volume_per_month"), drv.get("minutes_saved_per_unit"), drv.get("loaded_cost_per_hour")
            if all(_num(x) and x >= 0 for x in (vol, mins, rate)) and vol > 0:
                benefit2 = vol * mins / 60 * rate * 12  # baseline × volume × rate, annualised
            else:
                warnings.append(f"{pid}: benefit_driver incomplete (volume_per_month, minutes_saved_per_unit, "
                                f"loaded_cost_per_hour needed); typed annual_benefit_inr used.")
                drv = None
        def _int0(v, name):
            if v is None: return 0
            if _num(v) and 0 <= v <= 120 and float(v).is_integer(): return int(v)
            warnings.append(f"{pid}: {name} ignored (0–120 whole months); default 0 used.")
            return 0
        ramp, dly = _int0(e.get("ramp_months", p.get("ramp_months")), "ramp_months"), _int0(e.get("delay_months", p.get("delay_months")), "delay_months")
        pilots.append(Pilot(pid, p["task"], e.get("description", ""), setup2, run2, benefit2, bt, grp, ad, rd,
                            e.get("observed_adoption_pct") if _num(e.get("observed_adoption_pct")) else None,
                            e.get("observed_quality_pct") if _num(e.get("observed_quality_pct")) else None, flags,
                            one_time_costs=one_time, recurring_costs=recurring, benefit_driver=drv, ramp_months=ramp, delay_months=dly))
    return {"pilots": pilots, "invalid": invalid, "warnings": warnings}

# ---------- Deterministic tie-break ----------
def pilot_key(p: Pilot, mode: str):
    v = view(p, mode)
    return (-v["net"], v["payback_months"] if v["payback_months"] is not None else math.inf,
            -RANK[p.readiness], -p.adoption_pct, p.setup, _nat(p.id))

def _set_key(s: list, mode: str):
    ordered = sorted(s, key=lambda p: pilot_key(p, mode))
    return (-sum(view(p, mode)["net"] for p in s), sum(p.setup for p in s),
            -sum(RANK[p.readiness] for p in s), -sum(p.adoption_pct for p in s),
            [_nat(p.id) for p in ordered])

# ---------- Portfolio ----------
OBJECTIVES = ["net", "npv", "risk_adjusted"]

_OBJECTIVE_CULL = {
    "net": "Net annual benefit <= 0: never pays back.",
    "npv": "NPV <= 0 at the horizon with the given discount rate: never pays back in today's money.",
    "risk_adjusted": "Risk-adjusted value (net x readiness factor) <= 0: never pays back.",
}


def _objective_value(p: "Pilot", objective: str, mode: str, d: Optional[dict]) -> float:
    if objective == "net":
        return view(p, mode)["net"]
    if objective == "npv":
        return multi_period(p, mode, d or DEF)["npv_inr"]
    return round(view(p, mode)["net"] * READINESS_FACTOR[p.readiness])


def build_portfolio(pilots: list, budget: Any, mode: str = "independent", objective: str = "net", d: Optional[dict] = None) -> dict:
    if objective not in OBJECTIVES:
        raise ValueError(f"objective must be one of: {', '.join(OBJECTIVES)}.")
    base = {"mode": mode, "objective": objective, "budget": 0, "selected": [], "total_setup": 0, "total_net": 0,
            "budget_left": 0, "rejected": [], "ok": False, "message": ""}
    if not _num(budget) or budget < 0:
        return {**base, "message": "Budget is missing or invalid. Enter a non-negative INR amount."}
    base["budget"] = base["budget_left"] = budget
    vals = {p.id: _objective_value(p, objective, mode, d) for p in pilots}
    cands = [p for p in pilots if vals[p.id] > 0]
    rejected = [{"id": p.id, "reason": _OBJECTIVE_CULL[objective]} for p in pilots if vals[p.id] <= 0]
    cands.sort(key=lambda p: pilot_key(p, mode))
    if len(cands) > _MAX_SOLVER_CANDIDATES: raise ValueError(f"Too many candidates (max {_MAX_SOLVER_CANDIDATES}).")
    if not cands:
        return {**base, "rejected": rejected,
                "message": "No pilot has positive net benefit under current assumptions." if objective == "net"
                else f"No pilot has a positive {objective} value under current assumptions."}
    cheapest = min(p.setup for p in cands)
    if budget < cheapest:
        return {**base, "rejected": rejected + [{"id": p.id, "reason": f"Setup INR {p.setup:,.0f} exceeds budget INR {budget:,.0f}."} for p in cands],
                "message": f"Budget INR {budget:,.0f} is below the cheapest pilot (INR {cheapest:,.0f}). Empty portfolio."}
    if objective == "net" and len(cands) <= 16:   # legacy exact search: byte-identical public behaviour
        best, best_key = [], None
        for mask in range(1, 1 << len(cands)):
            s = [p for i, p in enumerate(cands) if mask >> i & 1]
            if sum(p.setup for p in s) > budget: continue
            if len({p.group for p in s}) != len(s): continue   # double-count guard
            k = _set_key(s, mode)
            if best_key is None or k < best_key: best, best_key = s, k
        extra = {}
    else:                                          # generalized path: branch-and-bound solver (5.5)
        items = [_Item(pid=p.id, order=i, value=round(vals[p.id]), cost=round(p.setup), group=p.group,
                       rank=RANK[p.readiness], adoption=p.adoption_pct) for i, p in enumerate(cands)]
        chosen, nodes, capped = _solve(items, budget)
        by_id = {p.id: p for p in cands}
        pmap = {it.pid: it.order for it in chosen}
        best = [by_id[pid] for pid, _ in sorted(pmap.items(), key=lambda kv: kv[1])]
        extra = {"objective_value": sum(round(vals[p.id]) for p in best),
                 "solver": {"nodes": nodes, "capped": capped}}
    sel = {p.id for p in best}
    for p in cands:
        if p.id in sel: continue
        w = next((q for q in best if q.group == p.group), None)
        rejected.append({"id": p.id, "reason": f'Overlap: shares benefit group "{p.group}" with {w.id}; benefit counted once.' if w
                         else "Not funded: budget fully used by higher-ranked pilots."})
    ts = sum(p.setup for p in best)
    return {**base, "selected": [p.id for p in best], "total_setup": ts,
            "total_net": sum(view(p, mode)["net"] for p in best), "budget_left": budget - ts, "rejected": rejected, "ok": True,
            "message": f"Selected {len(best)} pilot(s), INR {ts:,.0f} of {budget:,.0f}." if best else "No feasible portfolio.", **extra}

def check_manual_selection(pilots: list, ids: list, budget: float) -> dict:
    by = {p.id: p for p in pilots}
    problems = [f"{i} is not a valid pilot." for i in ids if i not in by]
    chosen = [by[i] for i in ids if i in by]
    total = sum(p.setup for p in chosen)
    if total > budget: problems.append(f"Over budget: INR {total:g} > INR {budget:g}.")
    groups: dict = {}
    for p in chosen: groups.setdefault(p.group, []).append(p.id)
    for g, v in groups.items():
        if len(v) > 1: problems.append(f'Double-count: {" + ".join(v)} share benefit group "{g}". Pick one.')
    return {"ok": not problems, "problems": problems}

# ---------- Stop rule ----------
def evaluate_stop_rule(p: Pilot, d: dict, quality_threshold_pct=None, target_adoption_pct=None) -> dict:
    qt = d["quality_threshold_pct"] if quality_threshold_pct is None else quality_threshold_pct
    at = d["target_adoption_pct"] if target_adoption_pct is None else target_adoption_pct
    be = breakeven_adoption_pct(p)
    def r(dec, why, m, th, ac): return {"decision": dec, "reasons": [why], "metric": m, "threshold": th, "actual": ac}
    if net_annual(p.benefit, p.run) <= 0 or be is None:
        return r("STOP", "Never pays back even at full adoption.", "net", 0, net_annual(p.benefit, p.run))
    if p.observed_adoption_pct is None or p.observed_quality_pct is None:
        return r("REVISE", "Observed adoption or quality not recorded yet; cannot confirm. Collect before further spend.", "data", None, None)
    if p.observed_adoption_pct < be:
        return r("STOP", f"Adoption {p.observed_adoption_pct:g}% is below breakeven {be:.1f}%: the pilot loses money.", "adoption", be, p.observed_adoption_pct)
    if p.observed_quality_pct < qt:
        return r("REVISE", f"Quality {p.observed_quality_pct:g}% is below threshold {qt:g}%.", "quality", qt, p.observed_quality_pct)
    if p.observed_adoption_pct < at:
        return r("REVISE", f"Adoption {p.observed_adoption_pct:g}% is above breakeven ({be:.1f}%) but below target {at:g}%.", "adoption", at, p.observed_adoption_pct)
    return r("GO", "Adoption and quality meet thresholds.", "none", None, None)

# ---------- Named what-if scenarios (assumption-driven, NOT a forecast) ----------
# Visible config: the shipped bad/base/good spread multiplies the adoption assumption only.
# A request may override through extension.defaults.scenario_multipliers = {"bad": 0.5, ...}.
SCENARIO_MULTIPLIERS: dict[str, float] = {"bad": 0.5, "base": 1.0, "good": 1.25}
SCENARIO_ORDER = ["bad", "base", "good"]

def scenarios(p: Pilot, d: Optional[dict] = None) -> list:
    mult = {**SCENARIO_MULTIPLIERS}
    if d and isinstance(d.get("scenario_multipliers"), dict):
        mult.update({k: v for k, v in d["scenario_multipliers"].items() if _num(v) and v >= 0})
    out = []
    for name in SCENARIO_ORDER:
        m = mult.get(name, SCENARIO_MULTIPLIERS.get(name))
        ad = p.adoption_pct * m
        ad = min(100.0, ad)
        q = Pilot(**{**p.__dict__, "adoption_pct": ad})
        v = view(q, "conservative")
        out.append({"scenario": name, "adoption_pct": round(ad, 1), "realized_benefit": round(v["benefit"]),
                    "net_annual": round(v["net"]), "payback_months": None if v["payback_months"] is None else round(v["payback_months"], 1),
                    "assumptions": {"adoption_multiplier": m, "multiplies": "adoption_pct",
                                    "held_at_base": ["annual_benefit_inr", "annual_running_cost_inr", "readiness", "setup"]}})
    return out

# ---------- Token forecast (Note A) ----------
def forecast_block(d: dict) -> dict:
    """Organisation-level AI spend: current run cost, next-FY forecast and the inflation flag.
    Missing tokens or price yields a flag and NO number (missing is never zero)."""
    t, price = d.get("tokens_per_month"), d.get("price_per_1m_tokens")
    fixed = d.get("ai_fixed_cost_inr_per_year")
    missing = []
    if not _num(t):
        missing.append({"code": "TOKENS_MISSING", "message": "Monthly AI token usage not recorded; run cost unknown."})
    if not _num(price):
        missing.append({"code": "TOKEN_PRICE_MISSING", "message": "Price per 1M tokens not recorded; run cost unknown."})
    run = forecast.ai_run_cost_inr(t, price, fixed)
    nxt = forecast.next_fy_cost_inr(t, price, fixed, d.get("annual_usage_growth_pct"), d.get("annual_price_drift_pct"))
    infl = forecast.inflation_flag(d.get("annual_usage_growth_pct"))
    return {"inputs": {"tokens_per_month": t, "price_per_1m_tokens": price, "ai_fixed_cost_inr_per_year": fixed,
                       "annual_usage_growth_pct": d.get("annual_usage_growth_pct"), "annual_price_drift_pct": d.get("annual_price_drift_pct")},
            "ai_run_cost_inr": None if run is None else round(run),
            "next_fy_cost_inr": None if nxt is None else round(nxt),
            "inflation_flag": infl,
            "flags": missing,
            "provenance": {"source": "assumed (team) inputs; calculate from the balance-sheet import for observed numbers"}
            if run is None else
            {"monthly_tokens": {"value": t, "source": "assumed (team)", "note": "not imported from the balance sheet yet"},
             "ai_run_cost_inr": {"value": round(run), "source": PROVENANCE_CALCULATED,
                                 "note": f"= tokens/month × 12 × price ÷ 1,000,000 + fixed = {t/1e6:.0f}M × {price} × 12 + {fixed or 0}"},
             "next_fy_cost_inr": {"value": round(nxt), "source": PROVENANCE_CALCULATED,
                                  "note": "tokens × (1 + usage growth), price × (1 + price drift); fixed annual unchanged"}}}
