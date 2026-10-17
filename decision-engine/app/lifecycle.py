"""Pilot checkpoint, 30-day plan and budget reallocation (Phase 6).

Pure functions, no I/O. The checkpoint rides on real evidence (the stop rule's own
observed adoption/quality) and NEVER replaces GO/REVISE/STOP — the stop rule stays
authoritative. The 30-day plan states expectations up front; actuals come from the
caller (the tracker links observations). Reallocation is a recommendation only:
nothing here writes anything.
"""
from __future__ import annotations
from typing import Optional

from . import core, advice

PLANNED_DURATION_DAYS = 30          # Phase 6: a funded pilot starts as a 30-day checkpoint plan
PLAN_STATUSES = ["planned", "active", "revise", "stopped", "completed"]

_CHECKPOINT_OUTCOMES = CHECKPOINT_OUTCOMES = ["scale", "extend", "revise", "stop", "pending"]


def checkpoint(p: "core.Pilot", d: dict, *, revised_before: bool = False,
               duration_days: Optional[int] = None) -> dict:
    """6.1 — checkpoint outcome after real evidence exists; GO/REVISE/STOP stays as-is.

    - scale:  both gates pass and the evidence band is at least medium (pilot ran >= PLANNED_DURATION_DAYS).
    - extend: gates pass but the band is low, or the pilot is younger than the planned duration.
    - revise: one gate fails while adoption stays at or above break-even (once; after a revise it is a stop).
    - stop:   adoption below break-even, net <= 0, or a gate fails again after a revise.
    - pending: not enough real evidence to run the checkpoint."""
    target = d.get("target_adoption_pct")
    qthresh = d.get("quality_threshold_pct")
    be = core.breakeven_adoption_pct(p)
    v = core.view(p, "independent")
    ev = advice.evidence(p)
    inputs = {"observed_adoption_pct": p.observed_adoption_pct, "observed_quality_pct": p.observed_quality_pct,
              "target_adoption_pct": target, "quality_threshold_pct": qthresh,
              "breakeven_adoption_pct": None if be is None else round(be, 1),
              "duration_days": duration_days, "revised_before": revised_before,
              "evidence_band": ev["band"]}
    if p.observed_adoption_pct is None or p.observed_quality_pct is None:
        missing = [n for n, val in (("adoption", p.observed_adoption_pct), ("quality", p.observed_quality_pct)) if val is None]
        return {"checkpoint": "pending",
                "reasons": ["Checkpoint needs real evidence: observed " + " and observed ".join(missing) + " not recorded yet."],
                "inputs": inputs}

    def out(oc, reasons):
        return {"checkpoint": oc, "reasons": reasons, "inputs": inputs}

    if v["net"] <= 0:
        return out("stop", [f"Net annual benefit {core._inr(v['net'])}/yr <= 0: the pilot never pays back (adapted from the stop rule)."])
    if be is not None and p.observed_adoption_pct < be:
        return out("stop", [f"Observed adoption {p.observed_adoption_pct:g}% is below break-even {be:.1f}%: the pilot loses money."])
    gates = []
    if target is not None and p.observed_adoption_pct < target:
        gates.append(f"adoption {p.observed_adoption_pct:g}% is below the plan target {target:g}%")
    if qthresh is not None and p.observed_quality_pct < qthresh:
        gates.append(f"quality {p.observed_quality_pct:g}% is below the threshold {qthresh:g}%")
    if gates:
        if revised_before:
            return out("stop", ["Gate failed again after a revise: stop the pilot and free its budget."]
                       + [f"  - {g}" for g in gates])
        return out("revise", ["One gate failed while adoption stayed at or above break-even; one revise allowed."]
                   + [f"  - {g}" for g in gates])
    young = duration_days is not None and duration_days < PLANNED_DURATION_DAYS
    if young:
        return out("extend", [f"Gates pass but the pilot has run only {duration_days:g} of {PLANNED_DURATION_DAYS:g} planned days; extend before scaling."])
    if ev["band"] == "low":
        cap = "band capped by fixture (synthetic) values; " if ev["band_capped_by_fixture"] else ""
        return out("extend", [f"Gates pass but the evidence band is low ({cap}observed weight {ev['observed_weight']}/{ev['total_weight']}); "
                              "run the plan longer before scaling."])
    return out("scale", [f"Gates pass (adoption {p.observed_adoption_pct:g}% >= {target:g}%, quality {p.observed_quality_pct:g}% >= {qthresh:g}%) "
                         f"and the evidence band is {ev['band']}; scale the pilot."])


def plan_30d(p: "core.Pilot", d: dict, *, duration_days: Optional[int] = None,
             owner: Optional[str] = None) -> dict:
    """6.2 — expectations to record when a pilot is funded. Expected comes from here,
    actual from the observations the tracker links."""
    days = int(duration_days or PLANNED_DURATION_DAYS)
    run_portion = round(p.run * days / 365.0)
    return {"pilot_id": p.id, "objective": p.task, "owner": owner,
            "adoption_target_pct": d.get("target_adoption_pct"),
            "quality_target_pct": d.get("quality_threshold_pct"),
            "cost_limit_inr": round(p.setup) + run_portion,
            "cost_limit_note": f"setup {core._inr(p.setup)} + running {core._inr(run_portion)} for {days} days",
            "duration_days": days,
            "stop_rule": (f"Stop when observed adoption falls below break-even "
                          f"{(core.breakeven_adoption_pct(p) or 0):.1f}% or net <= 0; revise once while adoption stays at or above break-even."),
            "status": "planned"}


def reallocation(pilots: list, budget: float, funded_ids: list, stopped_ids: list,
                 mode: str = "independent", objective: str = "net", d: Optional[dict] = None) -> dict:
    """6.3 — on stop: free the stopped pilots' reservation, keep funded pilots locked,
    and hand the freed budget back to the remaining candidates. Recommendation only."""
    base = {"mode": mode, "objective": objective, "budget": budget, "freed_budget_inr": 0,
            "locked_funded": [], "stopped": [], "remaining_budget_inr": None,
            "reallocation": None, "ok": False, "message": ""}
    by_id = {p.id: p for p in pilots}
    missing = [i for i in (*funded_ids, *stopped_ids) if i not in by_id]
    if missing:
        return {**base, "message": f"Unknown pilot id(s): {', '.join(missing)}."}
    funded = {i for i in funded_ids}
    stopped = {i for i in stopped_ids}
    dead = stopped - funded                      # stopping a pilot nobody funded frees nothing
    freed = round(sum(by_id[i].setup for i in stopped & funded))
    locked = sorted(funded - stopped)
    locked_groups = {by_id[i].group for i in locked if not by_id[i].group.startswith("__own_")}
    if not core._num(budget) or budget < 0:
        return {**base, "freed_budget_inr": freed, "locked_funded": locked, "stopped": sorted(stopped),
                "message": "Reallocation needs a numeric budget; the source had none. Free nothing yet."}
    remaining = budget - sum(by_id[i].setup for i in locked) + freed
    # candidates: everyone not funded/stopped, minus anyone sharing a group with a locked pilot
    cands = [p for p in pilots if p.id not in funded and p.id not in dead and p.group not in locked_groups]
    result = core.build_portfolio(cands, remaining, mode, objective, d)
    return {**base, "freed_budget_inr": freed, "locked_funded": locked, "stopped": sorted(stopped),
            "remaining_budget_inr": round(remaining, 2), "reallocation": result,
            "ok": True,
            "message": (f"Freed {core._inr(freed)}; locked {len(locked)} funded pilot(s); "
                        f"{result['message']}") if stopped & funded else "Nothing was freed: the stopped pilot had no allocated budget."}
