"""Decision engine (A6): data readiness, mandatory eligibility, weighted ranking with visible
weights, investment outcomes, why A over B, what would reverse it, and next-best actions.

Pure functions, no I/O, deterministic. Builds on the Phase 5 advice module (evidence band, gaps,
action table) without changing it: the legacy `advice.outcome` (fund/stage/...) stays in the
responses, and this module's outcome set is published next to it as `decision.outcome`.

Outcomes (per proposal, rule order fixed and visible in DECISION_RULES):
  reject → renegotiate → defer → gather_evidence → limited_pilot → invest
Set level adds `no_clear_winner` when the top investable proposals score within the margin.
"""
from __future__ import annotations
import copy
import math
from typing import Any, Optional

from . import core, advice, economics

OUTCOMES = ["reject", "renegotiate", "defer", "gather_evidence", "limited_pilot", "invest"]
SET_OUTCOMES = OUTCOMES + ["no_clear_winner"]
# When nothing is investable, the set-level answer is the most constructive outcome present.
_FALLBACK_PREFERENCE = ["gather_evidence", "renegotiate", "defer", "reject"]

RANKING_WEIGHTS: dict[str, float] = {
    "net_annual": 0.30,      # independent net annual benefit, higher better
    "npv": 0.20,             # NPV over the horizon, higher better
    "payback": 0.15,         # payback months, lower better (never pays back scores 0)
    "evidence": 0.15,        # evidence share (observed/calculated weight), higher better
    "readiness": 0.10,       # team data readiness low/medium/high
    "cash_share": 0.10,      # share of the benefit that is cashable
}
NO_CLEAR_WINNER_MARGIN = 5.0          # score points on the 0-100 scale
RENEGOTIATE_MAX_CUT = 0.30            # net <= 0 is renegotiable when a <= 30% run-cost cut fixes it
READINESS_EDGES = {"ready": 0.85, "partial": 0.50}
REVERSAL_FIELDS = ["annual_benefit_inr", "annual_running_cost_inr", "setup_cost_inr"]
REVERSAL_RANGE = 3.0                  # search 0..3x the base value
REVERSAL_RESOLUTION = 0.005           # stop at 0.5% of the base value

DECISION_RULES = {
    "outcome_order": OUTCOMES,
    "set_outcomes": SET_OUTCOMES,
    "rules": [
        "reject: net annual benefit <= 0 and a running-cost cut of more than 30% would be needed, "
        "or no part of the benefit is cashable",
        "renegotiate: net <= 0 but a running-cost cut of at most 30% makes it positive, "
        "or the cost competitiveness class is uncompetitive",
        "defer: break-even beyond the horizon, or the proposal is over budget",
        "gather_evidence: no decision-driving input is observed or calculated",
        "limited_pilot: evidence band low, data readiness not ready, or competitiveness marginal",
        "invest: every gate passes with evidence band medium or high",
    ],
    "eligibility_gates": ["NET_POSITIVE", "BREAKEVEN_IN_HORIZON", "BUDGET_FIT", "BENEFIT_HAS_CASH"],
    "ranking_weights": RANKING_WEIGHTS,
    "ranking_scale": "each criterion min-max normalised to 0-100 across the compared set (all equal = 100); "
                     "score = Σ weight × normalised; ties follow the 6-tier order",
    "no_clear_winner_margin_points": NO_CLEAR_WINNER_MARGIN,
    "renegotiate_max_run_cut": RENEGOTIATE_MAX_CUT,
    "data_readiness_edges": READINESS_EDGES,
    "data_readiness_checks": ["run cost stated", "adoption stated", "readiness stated", "benefit type known",
                              "overlap group known", "baseline supplied", "team data readiness at least medium"],
    "reversal": {"fields": REVERSAL_FIELDS, "range_x_base": REVERSAL_RANGE, "resolution_of_base": REVERSAL_RESOLUTION},
}

DECISION_ACTIONS: dict[str, dict[str, str]] = {
    "RENEGOTIATE_RUN_COST": {"action": "renegotiate the AI running cost with the vendor (volume tier, commitment discount)",
                             "owner_hint": "procurement", "expected_test": "net annual benefit > 0 at the quoted price"},
    "UNCOMPETITIVE_COST": {"action": "re-quote or re-scope: the AI costs more per unit than the work it replaces",
                           "owner_hint": "procurement + service owner", "expected_test": "competitiveness class marginal or better"},
    "OVER_BUDGET": {"action": "phase the rollout or move it to the next budget cycle", "owner_hint": "finance",
                    "expected_test": "budget status within or at risk"},
    "DATA_NOT_READY": {"action": "fill the missing inputs (stated run cost, adoption, baseline, overlap group)",
                       "owner_hint": "service owner", "expected_test": "data readiness partial or ready"},
    "BASELINE_MISSING": {"action": "record today's cost of the process (volume, minutes per unit, loaded hourly cost)",
                         "owner_hint": "ops lead", "expected_test": "baseline cost known; retained human cost computed"},
    "NO_CLEAR_WINNER": {"action": "run the top contenders as limited pilots side by side and decide on observed adoption",
                        "owner_hint": "steering group", "expected_test": "score gap above the margin after measurement"},
}
ALL_ACTIONS = {**advice.NEXT_ACTIONS, **DECISION_ACTIONS}


class DecisionError(ValueError):
    """Bad decision input: shown as a 422."""


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def validate_weights(w: Any) -> dict:
    if w is None:
        return dict(RANKING_WEIGHTS)
    if not isinstance(w, dict):
        raise DecisionError("weights must be an object of criterion -> non-negative number.")
    unknown = set(w) - set(RANKING_WEIGHTS)
    if unknown:
        raise DecisionError(f"Unknown weight(s) {sorted(unknown)}; allowed {sorted(RANKING_WEIGHTS)}.")
    merged = {**RANKING_WEIGHTS, **w}
    if any(not _num(v) or v < 0 for v in merged.values()):
        raise DecisionError("weights must be non-negative numbers.")
    total = sum(merged.values())
    if total <= 0:
        raise DecisionError("weights must not all be zero.")
    return {k: v / total for k, v in merged.items()}


# ---------------------------------------------------------------- per proposal

def data_readiness(p: "core.Pilot", econ: dict) -> dict:
    codes = {f["code"] for f in p.flags}
    checks = [
        ("run cost stated", "RUN_COST_ASSUMED" not in codes),
        ("adoption stated", "ADOPTION_ASSUMED" not in codes),
        ("readiness stated", "READINESS_ASSUMED" not in codes),
        ("benefit type known", "BENEFIT_TYPE_UNKNOWN" not in codes),
        ("overlap group known", "OVERLAP_UNKNOWN" not in codes),
        ("baseline supplied", econ["baseline_cost_inr"] is not None),
        ("team data readiness at least medium", p.readiness != "low"),
    ]
    share = sum(ok for _, ok in checks) / len(checks)
    level = "ready" if share >= READINESS_EDGES["ready"] else ("partial" if share >= READINESS_EDGES["partial"] else "not_ready")
    return {"level": level, "share": round(share, 3),
            "checks": [{"check": name, "ok": ok} for name, ok in checks],
            "missing": [name for name, ok in checks if not ok]}


def eligibility(p: "core.Pilot", d: dict, econ: dict) -> dict:
    v = core.view(p, "independent", d)
    mp = v["multi_period"]
    gates = [
        {"gate": "NET_POSITIVE", "status": "pass" if v["net"] > 0 else "fail",
         "detail": f"net annual benefit {core._inr(v['net'])}/yr"},
        {"gate": "BREAKEVEN_IN_HORIZON", "status": "pass" if mp["breakeven_month"] is not None else "fail",
         "detail": f"break-even month {mp['breakeven_month']}" if mp["breakeven_month"] is not None
         else f"break-even beyond month {mp['horizon_months']}"},
        {"gate": "BUDGET_FIT",
         "status": {"over": "fail", "unknown": "not_checked"}.get(econ["budget"]["status"], "pass"),
         "detail": f"budget status {econ['budget']['status']}"},
        {"gate": "BENEFIT_HAS_CASH", "status": "pass" if econ["cashable_savings_inr"] > 0 else "fail",
         "detail": f"cashable savings {core._inr(econ['cashable_savings_inr'])}/yr "
                   f"({econ['cashable_share_pct']:g}% of gross)"},
    ]
    return {"eligible": all(g["status"] != "fail" for g in gates), "gates": gates}


def decide_one(p: "core.Pilot", d: dict, econ: dict, ev: dict, dr: dict, el: dict) -> dict:
    """Outcome for one proposal with reasons and reason codes (order fixed, see DECISION_RULES)."""
    v = core.view(p, "independent", d)
    mp = v["multi_period"]
    gate = {g["gate"]: g for g in el["gates"]}
    comp = econ["competitiveness"]["class"]
    inr = core._inr

    def out(outcome: str, reasons: list, codes: list) -> dict:
        return {"outcome": outcome, "reasons": reasons, "reason_codes": list(dict.fromkeys(codes))}

    if v["net"] <= 0:
        need = p.run - p.benefit
        cut = need / p.run if p.run > 0 else math.inf
        if p.benefit > 0 and cut <= RENEGOTIATE_MAX_CUT:
            return out("renegotiate", [f"Net {inr(v['net'])}/yr is not positive, but cutting the running cost by "
                                       f"{cut:.0%} (to below {inr(p.benefit)}/yr) makes it pay: the price is the blocker."],
                       ["RENEGOTIATE_RUN_COST", "NET_NOT_POSITIVE"])
        return out("reject", [f"Net {inr(v['net'])}/yr is not positive and no running-cost cut of "
                              f"{RENEGOTIATE_MAX_CUT:.0%} or less fixes it."], ["NET_NOT_POSITIVE"])
    if gate["BENEFIT_HAS_CASH"]["status"] == "fail":
        return out("reject", ["No part of the benefit is cashable: it frees capacity but saves no money. "
                              "Fund it from a capacity budget, not the savings ledger."], ["NON_CASHABLE"])
    if comp == economics.COMPETITIVENESS_WORST:
        return out("renegotiate", [f"Cost competitiveness is {comp} ({econ['competitiveness']['basis']}): "
                                   "the AI costs more than the work it replaces."], ["UNCOMPETITIVE_COST"])
    if gate["BREAKEVEN_IN_HORIZON"]["status"] == "fail":
        return out("defer", [f"Positive net {inr(v['net'])}/yr but break-even falls after month {mp['horizon_months']}."],
                   ["BREAK_EVEN_BEYOND_HORIZON"])
    if gate["BUDGET_FIT"]["status"] == "fail":
        over = [ln["line"] for ln in econ["budget"]["lines"] if ln["status"] == "over"]
        return out("defer", [f"Over budget on {', '.join(over)}: it does not fit this budget cycle."], ["OVER_BUDGET"])
    if ev["observed_weight"] == 0:
        return out("gather_evidence", ["Payable on paper, but no decision-driving input is observed or calculated: "
                                       "every number is fixture or team assumption."],
                   ["FIXTURE_ONLY_EVIDENCE" if ev["band_capped_by_fixture"] else "NO_OBSERVED_ADOPTION"])
    thin = []
    codes = []
    if ev["band"] == "low":
        thin.append("the evidence band is low" + (" (capped by fixture values)" if ev["band_capped_by_fixture"] else ""))
        codes.append("FIXTURE_ONLY_EVIDENCE" if ev["band_capped_by_fixture"] else
                     "NO_OBSERVED_ADOPTION" if p.observed_adoption_pct is None else "MONEY_ASSUMED")
    if dr["level"] == "not_ready":
        thin.append(f"data readiness is not ready (missing: {', '.join(dr['missing'])})")
        codes.append("DATA_NOT_READY")
    if comp == "marginal":
        thin.append(f"cost competitiveness is marginal ({econ['competitiveness']['basis']})")
        codes.append("UNCOMPETITIVE_COST")
    if thin:
        return out("limited_pilot", [f"Payable (net {inr(v['net'])}/yr, break-even month {mp['breakeven_month']}) but "
                                     + "; ".join(thin) + ": run a time-boxed limited pilot first."], codes)
    return out("invest", [f"Net {inr(v['net'])}/yr, break-even month {mp['breakeven_month']}, evidence band {ev['band']}, "
                          f"data readiness {dr['level']}, competitiveness {comp or 'n/a'}: every gate passes."], [])


def _assess(p: "core.Pilot", d: dict, bs: Optional[dict], extra: dict) -> dict:
    econ = economics.economics_for(p, d, bs, extra.get("baseline"), extra.get("cashable_share_pct"))
    ev = advice.evidence(p)
    dr = data_readiness(p, econ)
    el = eligibility(p, d, econ)
    return {"economics": econ, "evidence": ev, "data_readiness": dr, "eligibility": el,
            **decide_one(p, d, econ, ev, dr, el)}


_FIELD_ATTR = {"annual_benefit_inr": "benefit", "annual_running_cost_inr": "run", "setup_cost_inr": "setup"}


def what_would_reverse(p: "core.Pilot", d: dict, bs: Optional[dict], extra: dict, outcome: str) -> list:
    """For each money input, the nearest value (searching up and down from today's) at which the
    outcome changes, and what it changes to. Binary search on the boundary of today's outcome."""
    out = []
    for field in REVERSAL_FIELDS:
        attr = _FIELD_ATTR[field]
        base = getattr(p, attr)
        if not _num(base) or base <= 0:
            continue

        def outcome_at(value: float) -> str:
            q = copy.copy(p)
            setattr(q, attr, value)
            if attr == "setup":
                q.one_time_costs = None
            if attr == "run":
                q.recurring_costs = None
            return _assess(q, d, bs, extra)["outcome"]

        for direction in (-1, 1):
            far = base * (1 + direction * (1.0 if direction < 0 else REVERSAL_RANGE - 1))
            far_outcome = outcome_at(far)
            if far_outcome == outcome:
                continue
            lo, hi = base, far          # lo keeps today's outcome, hi does not
            while abs(hi - lo) > base * REVERSAL_RESOLUTION:
                mid = (lo + hi) / 2
                if outcome_at(mid) == outcome:
                    lo = mid
                else:
                    hi = mid
            new = outcome_at(hi)
            pct = (hi - base) / base * 100
            out.append({"field": field, "from_value": round(base), "to_value": round(hi),
                        "change_pct": round(pct, 1), "new_outcome": new,
                        "statement": f"{outcome} becomes {new} if {field} {'falls' if hi < base else 'rises'} to "
                                     f"{core._inr(hi)} ({pct:+.1f}%)."})
    out.sort(key=lambda r: (abs(r["change_pct"]), r["field"]))
    return out


def decision_for(p: "core.Pilot", d: dict, bs: Optional[dict] = None, extra: Optional[dict] = None,
                 with_reversal: bool = True) -> dict:
    extra = extra or {}
    a = _assess(p, d, bs, extra)
    codes = list(a["reason_codes"])
    if a["data_readiness"]["level"] != "ready" and "DATA_NOT_READY" not in codes and a["outcome"] != "invest":
        codes.append("DATA_NOT_READY")
    if a["economics"]["baseline_cost_inr"] is None:
        codes.append("BASELINE_MISSING")
    codes += [g["code"] for g in advice.decide(p, a["evidence"], d)["actions"]]
    actions = [{"code": c, **ALL_ACTIONS[c]} for c in dict.fromkeys(codes) if c in ALL_ACTIONS]
    return {"outcome": a["outcome"], "reasons": a["reasons"], "reason_codes": a["reason_codes"],
            "eligibility": a["eligibility"], "data_readiness": a["data_readiness"],
            "evidence": {k: a["evidence"][k] for k in ("band", "share", "observed_weight", "total_weight",
                                                        "band_capped_by_fixture")},
            "gaps": a["evidence"]["gaps"],
            "economics": a["economics"],
            "what_would_reverse": what_would_reverse(p, d, bs, extra, a["outcome"]) if with_reversal else [],
            "next_actions": actions}


# ---------------------------------------------------------------- set level

def _criteria(p: "core.Pilot", d: dict, dec: dict) -> dict:
    v = core.view(p, "independent", d)
    return {"net_annual": v["net"], "npv": v["multi_period"]["npv_inr"], "payback": v["payback_months"],
            "evidence": dec["evidence"]["share"], "readiness": core.RANK[p.readiness],
            "cash_share": dec["economics"]["cashable_share_pct"]}


def _normalise(values: dict, lower_better: bool) -> dict:
    known = {k: v for k, v in values.items() if v is not None}
    if not known:
        return {k: 0.0 for k in values}
    lo, hi = min(known.values()), max(known.values())
    out = {}
    for k, v in values.items():
        if v is None:
            out[k] = 0.0
        elif hi == lo:
            out[k] = 100.0
        else:
            s = (v - lo) / (hi - lo) * 100
            out[k] = 100.0 - s if lower_better else s
    return out


def rank(pilots: list, d: dict, decisions: dict, weights: dict) -> list:
    crit = {p.id: _criteria(p, d, decisions[p.id]) for p in pilots}
    norm = {c: _normalise({pid: crit[pid][c] for pid in crit}, lower_better=(c == "payback")) for c in RANKING_WEIGHTS}
    rows = []
    for p in pilots:
        contrib = {c: round(weights[c] * norm[c][p.id], 2) for c in RANKING_WEIGHTS}
        rows.append({"id": p.id, "score": round(sum(contrib.values()), 2), "contributions": contrib,
                     "criteria": {c: (None if crit[p.id][c] is None else round(crit[p.id][c], 3)) for c in RANKING_WEIGHTS},
                     "eligible": decisions[p.id]["eligibility"]["eligible"],
                     "outcome": decisions[p.id]["outcome"]})
    by_id = {p.id: p for p in pilots}
    rows.sort(key=lambda r: (not r["eligible"], -r["score"], core.pilot_key(by_id[r["id"]], "independent")))
    for i, r in enumerate(rows, 1):
        r["rank"] = i
    return rows


_CRIT_LABEL = {"net_annual": "net annual benefit", "npv": "NPV", "payback": "payback", "evidence": "evidence strength",
               "readiness": "data readiness", "cash_share": "cashable share"}


def _fmt(criterion: str, v) -> str:
    if v is None:
        return "never" if criterion == "payback" else "n/a"
    if criterion in ("net_annual", "npv"):
        return core._inr(v)
    if criterion == "payback":
        return f"{v:.1f} months"
    if criterion == "readiness":
        return {0: "low", 1: "medium", 2: "high"}[int(v)]
    if criterion == "evidence":
        return f"{v:.0%} observed"
    return f"{v:g}% cashable"


def why_a_over_b(ranked: list) -> list:
    """Adjacent pairs: the score gap and the criteria that make it, largest first."""
    out = []
    for a, b in zip(ranked, ranked[1:]):
        diffs = sorted(((c, a["contributions"][c] - b["contributions"][c]) for c in RANKING_WEIGHTS),
                       key=lambda x: (-abs(x[1]), x[0]))
        reasons = []
        if a["eligible"] and not b["eligible"]:
            reasons.append(f"{b['id']} fails a mandatory eligibility gate; {a['id']} passes them all")
        for c, dv in diffs:
            if abs(dv) < 0.01:
                continue
            fav = a["id"] if dv > 0 else b["id"]
            reasons.append(f"{_CRIT_LABEL[c]}: {_fmt(c, a['criteria'][c])} vs {_fmt(c, b['criteria'][c])} "
                           f"({abs(dv):.1f} points to {fav})")
        if not reasons:
            reasons.append("identical on every criterion; the 6-tier tie-break order decides")
        out.append({"over": a["id"], "under": b["id"], "score_gap": round(a["score"] - b["score"], 2),
                    "reasons": reasons})
    return out


def decide_set(pilots: list, d: dict, bs: Optional[dict] = None, extras: Optional[dict] = None,
               weights: Any = None) -> dict:
    w = validate_weights(weights)
    extras = extras or {}
    decisions = {p.id: decision_for(p, d, bs, extras.get(p.id)) for p in pilots}
    ranked = rank(pilots, d, decisions, w)
    investable = [r for r in ranked if r["eligible"] and r["outcome"] in ("invest", "limited_pilot")]
    top_tier = [r for r in investable if r["outcome"] == "invest"] or investable
    contenders, best, overall = [], None, None
    if top_tier:
        lead = top_tier[0]
        contenders = [r["id"] for r in top_tier if lead["score"] - r["score"] < NO_CLEAR_WINNER_MARGIN]
        if len(contenders) > 1:
            overall = "no_clear_winner"
            reasons = [f"{', '.join(contenders)} score within {NO_CLEAR_WINNER_MARGIN:g} points of each other "
                       f"({', '.join(str(next(r['score'] for r in ranked if r['id'] == c)) for c in contenders)}): "
                       "the numbers do not separate them."]
        else:
            best, overall = lead["id"], lead["outcome"]
            runner = top_tier[1] if len(top_tier) > 1 else None
            reasons = [f"{lead['id']} ranks first with score {lead['score']}"
                       + (f", {lead['score'] - runner['score']:.1f} points ahead of {runner['id']}." if runner else ".")]
    else:
        present = {r["outcome"] for r in ranked}
        overall = next((o for o in _FALLBACK_PREFERENCE if o in present), "reject")
        reasons = [f"No proposal is investable yet; the most constructive next step across the set is {overall}."]
    actions = []
    if overall == "no_clear_winner":
        actions.append({"code": "NO_CLEAR_WINNER", **DECISION_ACTIONS["NO_CLEAR_WINNER"]})
    elif best:
        actions = decisions[best]["next_actions"]
    else:                       # nothing investable: the actions of every proposal at the set-level outcome
        seen = set()
        for r in ranked:
            if r["outcome"] == overall:
                for a in decisions[r["id"]]["next_actions"]:
                    if a["code"] not in seen:
                        seen.add(a["code"])
                        actions.append(a)
    return {"overall": {"outcome": overall, "best": best, "contenders": contenders if overall == "no_clear_winner" else [],
                        "reasons": reasons, "next_actions": actions},
            "ranking": ranked, "why_a_over_b": why_a_over_b(ranked), "weights": {k: round(v, 4) for k, v in w.items()},
            "decisions": decisions}
