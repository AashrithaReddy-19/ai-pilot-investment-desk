"""C23 decision core (Python port). Pure functions, no I/O. null/None is NEVER zero."""
from __future__ import annotations
import math, re
from dataclasses import dataclass, field, asdict
from typing import Any, Optional

RANK = {"low": 0, "medium": 1, "high": 2}
READINESS_FACTOR = {"low": 0.6, "medium": 0.8, "high": 1.0}
TIE_BREAK_ORDER = ["higher net benefit", "faster payback", "higher readiness",
                   "higher adoption", "lower setup cost", "lower id"]


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
    t = benefit_type.lower()
    if "time" in t: return 0.3
    if "cashable" in t: return 1.0
    return 0.3

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

def view(p: Pilot, mode: str) -> dict:
    b = p.benefit if mode == "independent" else realized_benefit(p)
    return {"id": p.id, "benefit": b, "net": net_annual(b, p.run), "payback_months": payback_months(p.setup, b, p.run)}

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
        bt = p.get("benefit_type") or ""
        if not bt:
            bt = "unknown"; flags.append({"code": "BENEFIT_TYPE_UNKNOWN", "message": "Benefit type unknown; treated as time freed (30% cash)."})
        elif "time" in bt.lower():
            flags.append({"code": "TIME_NOT_CASH", "message": "Time freed is not cash; haircut to 30% unless cash removal is shown."})
        grp = p.get("overlap_group") or ""
        if not grp:
            grp = f"__own_{pid}"; flags.append({"code": "OVERLAP_UNKNOWN", "message": "Overlap group unknown; treated as standalone. Confirm with owner."})
        pilots.append(Pilot(pid, p["task"], e.get("description", ""), setup, run, p["annual_benefit_inr"], bt, grp, ad, rd,
                            e.get("observed_adoption_pct") if _num(e.get("observed_adoption_pct")) else None,
                            e.get("observed_quality_pct") if _num(e.get("observed_quality_pct")) else None, flags))
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
def build_portfolio(pilots: list, budget: Any, mode: str = "independent") -> dict:
    base = {"mode": mode, "budget": 0, "selected": [], "total_setup": 0, "total_net": 0,
            "budget_left": 0, "rejected": [], "ok": False, "message": ""}
    if not _num(budget) or budget < 0:
        return {**base, "message": "Budget is missing or invalid. Enter a non-negative INR amount."}
    base["budget"] = base["budget_left"] = budget
    cands = [p for p in pilots if view(p, mode)["net"] > 0]
    rejected = [{"id": p.id, "reason": "Net annual benefit <= 0: never pays back."} for p in pilots if view(p, mode)["net"] <= 0]
    cands.sort(key=lambda p: pilot_key(p, mode))
    if len(cands) > 16: raise ValueError("Too many candidates for exact search (max 16).")
    if not cands: return {**base, "rejected": rejected, "message": "No pilot has positive net benefit under current assumptions."}
    cheapest = min(p.setup for p in cands)
    if budget < cheapest:
        return {**base, "rejected": rejected + [{"id": p.id, "reason": f"Setup INR {p.setup:g} exceeds budget INR {budget:g}."} for p in cands],
                "message": f"Budget INR {budget:g} is below the cheapest pilot (INR {cheapest:g}). Empty portfolio."}
    best, best_key = [], None
    for mask in range(1, 1 << len(cands)):
        s = [p for i, p in enumerate(cands) if mask >> i & 1]
        if sum(p.setup for p in s) > budget: continue
        if len({p.group for p in s}) != len(s): continue   # double-count guard
        k = _set_key(s, mode)
        if best_key is None or k < best_key: best, best_key = s, k
    sel = {p.id for p in best}
    for p in cands:
        if p.id in sel: continue
        w = next((q for q in best if q.group == p.group), None)
        rejected.append({"id": p.id, "reason": f'Overlap: shares benefit group "{p.group}" with {w.id}; benefit counted once.' if w
                         else "Not funded: budget fully used by higher-ranked pilots."})
    ts = sum(p.setup for p in best)
    return {**base, "selected": [p.id for p in best], "total_setup": ts,
            "total_net": sum(view(p, mode)["net"] for p in best), "budget_left": budget - ts, "rejected": rejected, "ok": True,
            "message": f"Selected {len(best)} pilot(s), INR {ts:g} of {budget:g}." if best else "No feasible portfolio."}

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

# ---------- What-if scenarios (assumption-driven, NOT a forecast) ----------
def scenarios(p: Pilot) -> list:
    out = []
    for name, ad in (("bad", p.adoption_pct * 0.5), ("base", p.adoption_pct), ("good", min(100.0, p.adoption_pct * 1.25))):
        q = Pilot(**{**p.__dict__, "adoption_pct": ad})
        v = view(q, "conservative")
        out.append({"scenario": name, "adoption_pct": round(ad, 1), "realized_benefit": round(v["benefit"]),
                    "net_annual": round(v["net"]), "payback_months": None if v["payback_months"] is None else round(v["payback_months"], 1)})
    return out
