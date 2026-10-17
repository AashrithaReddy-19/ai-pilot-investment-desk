"""Evidence strength, gap detection, decision outcomes and next-best actions (Phase 5).

Pure functions, no I/O. The band is the weighted share of decision-driving inputs backed by
real data — always shown as a band (low/medium/high), never as a confidence percentage.
An input counts as observed when its provenance is `observed` or `calculated` (real,
user-supplied numbers); `assumed (team)` and `fixture (synthetic)` do not count.
Fixture (synthetic) money values cap the band at Low. Missing is never zero: an
unmeasured input is a gap, not a zero contributor.
"""
from __future__ import annotations
from typing import Optional

from . import core

# 5.1 — decision-driving inputs and their weight on the decision (total 11).
# Weight matches impact: the benefit is the biggest lever, money/adoption move the net,
# quality and readiness are supporting gates.
INPUT_WEIGHTS: dict[str, int] = {
    "annual_benefit_inr": 3,
    "annual_running_cost_inr": 2,
    "setup_cost_inr": 2,
    "adoption_pct": 2,
    "quality_pct": 1,
    "readiness": 1,
}
TOTAL_WEIGHT = sum(INPUT_WEIGHTS.values())
MONEY_INPUTS = ["setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr"]
# Band edges on the observed weight share: >= 0.40 Medium, >= 0.75 High, else Low.
BAND_EDGES: dict[str, float] = {"medium": 0.40, "high": 0.75}
OUTCOME_ORDER = ["reject", "insufficient_information", "defer", "stage", "fund"]

_OBSERVED_GRADE = {core.PROVENANCE_OBSERVED, core.PROVENANCE_CALCULATED}

_GAP_WHYS: dict[str, str] = {
    "annual_benefit_inr": "The benefit is the biggest lever on net, payback and the ranking; without a driver or invoice trail the number is stated, not evidenced.",
    "annual_running_cost_inr": "The running cost is subtracted from every view; a wrong run cost flips net, payback and break-even adoption.",
    "setup_cost_inr": "The one-time cost sets payback and NPV; missing lines hide the real outlay.",
    "adoption_pct": "Gates and realized benefit depend on adoption; without a spot-check the pilot is unconfirmed.",
    "quality_pct": "The quality gate cannot be verified until one batch is measured.",
    "readiness": "Readiness is a team judgement; scoring it against the data checklist would make the haircut evidence-backed.",
}


def _band(share: float, capped: bool) -> str:
    if capped:  # fixture (synthetic) values cap the band at Low — never a bold claim on synthetic data
        return "low"
    if share >= BAND_EDGES["high"]:
        return "high"
    if share >= BAND_EDGES["medium"]:
        return "medium"
    return "low"


def evidence(p: "core.Pilot") -> dict:
    """Weighted share of decision-driving inputs backed by real data, the band, the
    per-input list and the gaps ordered by impact on the decision."""
    prov = core.provenance(p)
    rows = []
    for name, weight in INPUT_WEIGHTS.items():
        if name == "quality_pct":
            src = core.PROVENANCE_OBSERVED if p.observed_quality_pct is not None else core.PROVENANCE_ASSUMED
        elif name == "adoption_pct":
            src = core.PROVENANCE_OBSERVED if p.observed_adoption_pct is not None else core.PROVENANCE_ASSUMED
        elif name == "readiness":
            src = core.PROVENANCE_ASSUMED  # always a team judgement
        else:
            src = prov[{"setup_cost_inr": "setup_inr"}.get(name, name)]["source"]
        rows.append({"input": name, "source": src, "observed": src in _OBSERVED_GRADE, "weight": weight})
    observed_weight = sum(r["weight"] for r in rows if r["observed"])
    share = observed_weight / TOTAL_WEIGHT
    # Only MONEY values respect the fixture cap: benefit_type/observed flags are rules, not decision numbers.
    capped = any(r["source"] == core.PROVENANCE_FIXTURE for r in rows if r["input"] in MONEY_INPUTS)
    gaps = [{"input": r["input"], "impact": "high" if r["weight"] >= 3 else ("medium" if r["weight"] == 2 else "low"),
             "why": _GAP_WHYS[r["input"]]} for r in rows if not r["observed"]]
    gaps.sort(key=lambda g: (-INPUT_WEIGHTS[g["input"]], g["input"]))
    return {"inputs": rows, "observed_weight": observed_weight, "total_weight": TOTAL_WEIGHT,
            "share": round(share, 3), "band": _band(share, capped),
            "band_capped_by_fixture": capped, "gaps": gaps}


# 5.4 — the data-driven rule table: reason code → action, owner hint, expected test.
NEXT_ACTIONS: dict[str, dict[str, str]] = {
    "NET_NOT_POSITIVE": {"action": "re-scope the pilot: remove one cost line or grow the benefit before it is proposed again",
                         "owner_hint": "service owner", "expected_test": "net annual benefit > 0"},
    "NON_CASHABLE": {"action": "restate the benefit with a cash line (budget line, invoice reduction) instead of capacity value",
                     "owner_hint": "service owner", "expected_test": "benefit type with cash factor 1.0 and a stated cash line"},
    "FIXTURE_ONLY_EVIDENCE": {"action": "replace one fixture number with a real one: an invoice-based recurring cost line or a benefit driver from real volumes",
                              "owner_hint": "finance + service owner", "expected_test": "no decision-driving input sourced from the fixture"},
    "NO_OBSERVED_ADOPTION": {"action": "run one adoption spot-check batch over the real users",
                             "owner_hint": "org lead", "expected_test": "observed adoption recorded from the batch"},
    "NO_OBSERVED_QUALITY": {"action": "run one quality spot-check on pilot outputs and record it",
                            "owner_hint": "ops lead", "expected_test": "observed quality recorded from the batch"},
    "RUN_COST_ASSUMED": {"action": "get written vendor pricing or an invoice trail for the AI running cost",
                         "owner_hint": "finance", "expected_test": "a recurring-cost line replaces the 20%-of-setup assumption"},
    "ADOPTION_ASSUMED": {"action": "schedule an adoption measurement for week 1-2 of any stage", "owner_hint": "org lead",
                         "expected_test": "observed adoption recorded"},
    "LOW_OBSERVED_ADOPTION": {"action": "short user-training experiment", "owner_hint": "org lead",
                              "expected_test": "observed adoption >= break-even adoption after the experiment"},
    "LOW_READINESS": {"action": "clean the data first", "owner_hint": "data engineering",
                      "expected_test": "readiness at medium or higher before funding"},
    "BREAK_EVEN_BEYOND_HORIZON": {"action": "re-cost with a faster ramp (build and rollout sooner) or re-decide at the next budget cycle",
                                  "owner_hint": "finance", "expected_test": "break-even month <= horizon"},
    "CONSERVATIVE_DIES": {"action": "fund from the training/capacity budget instead of the savings ledger, or convert the benefit to cash",
                          "owner_hint": "finance", "expected_test": "conservative net > 0"},
}


def _reason_codes(p: "core.Pilot", ev: dict, d: dict) -> list[str]:
    codes = []
    flag_codes = {f["code"] for f in p.flags}
    if "RUN_COST_ASSUMED" in flag_codes:
        codes.append("RUN_COST_ASSUMED")
    if "BENEFIT_TYPE_NON_CASHABLE" in flag_codes:
        codes.append("NON_CASHABLE")
    if "ADOPTION_ASSUMED" in flag_codes:
        codes.append("ADOPTION_ASSUMED")
    if any(f["code"] == "TIME_NOT_CASH" for f in p.flags) and core.view(p, "conservative")["net"] <= 0:
        codes.append("CONSERVATIVE_DIES")
    if p.observed_adoption_pct is None:
        codes.append("NO_OBSERVED_ADOPTION")
    if p.observed_quality_pct is None:
        codes.append("NO_OBSERVED_QUALITY")
    if observed_ado := p.observed_adoption_pct:
        be = core.breakeven_adoption_pct(p)
        if be is not None and observed_ado < be:
            codes.append("LOW_OBSERVED_ADOPTION")
    if p.readiness == "low":
        codes.append("LOW_READINESS")
    mp = core.multi_period(p, "independent", d)
    if mp["breakeven_month"] is None:
        codes.append("BREAK_EVEN_BEYOND_HORIZON")
    if any(r["source"] == core.PROVENANCE_FIXTURE for r in ev["inputs"] if r["input"] in MONEY_INPUTS):
        codes.append("FIXTURE_ONLY_EVIDENCE")
    return codes


def _actions_for(codes: list[str]) -> list[dict]:
    return [{"code": code, **NEXT_ACTIONS[code]} for code in dict.fromkeys(codes) if code in NEXT_ACTIONS]


# 5.2 — investment decision outcomes. Deterministic rule order:
# reject → insufficient_information → defer → stage → fund.
def decide(p: "core.Pilot", ev: dict, d: dict) -> dict:
    vi = core.view(p, "independent", d)
    vcons = core.view(p, "conservative")
    mp = vi["multi_period"]
    be_month, horizon = mp["breakeven_month"], mp["horizon_months"]
    inr = core._inr

    def out(outcome: str, reasons: list[str], change: list[str], codes: list[str]) -> dict:
        actions = _actions_for(codes)
        if outcome != "fund":  # invariant: every non-fund outcome resolves to at least one action
            assert actions, f"no action for {outcome} on pilot {p.id}: codes={codes}"
        return {"outcome": outcome, "reasons": reasons, "what_would_change_it": change,
                "actions": actions, "reason_codes": list(dict.fromkeys(codes))}

    codes = _reason_codes(p, ev, d)
    if vi["net"] <= 0:
        return out("reject",
                   [f"Net annual benefit {inr(vi['net'])}/yr <= 0: even the full-benefit view never pays back.",
                    *(f'Conservative view is {inr(vcons["net"])}/yr: the haircut confirms it.' if vcons["net"] < vi["net"] else [])],
                   [f"Cut the running cost below {inr(p.benefit)}/yr or grow the benefit by {inr(-vi['net'])}/yr, then re-run the rule."],
                   ["NET_NOT_POSITIVE"] + codes)
    if ev["observed_weight"] == 0:
        return out("insufficient_information",
                   ["No decision-driving input is backed by real data: the money numbers are fixture (synthetic) check values "
                    "and neither adoption nor quality has ever been measured."],
                   ["Record one real measurement (an adoption spot-check or a quality batch) and one real cost line "
                    "(recurring costs or a vendor quote); the band then leaves Low and the rule can judge the pilot."],
                   codes)
    if be_month is None:
        return out("defer",
                   [f"Net {inr(vi['net'])}/yr is positive but break-even falls after month {horizon}: {horizon} months of loss first at today's ramp."],
                   [f"Pull break-even inside the horizon with a faster ramp (smaller ramp_months/delay_months) or a longer contracted horizon; "
                    f"otherwise re-decide at the next budget cycle."],
                   codes)
    if ev["band"] == "low":
        capped = "The band is capped by fixture (synthetic) values. " if ev["band_capped_by_fixture"] else ""
        return out("stage",
                   [f"Looks payable (net {inr(vi['net'])}/yr, break-even month {be_month}) but the evidence band is Low: {capped}"
                    "Too thin to commit the full amount."],
                   ["Fund a time-boxed stage first (30-60 days); moving the band to Medium "
                    f"(observed weight >= {BAND_EDGES['medium']:.0%} of the weights) unlocks full funding."],
                   codes)
    reasons = [f"Net {inr(vi['net'])}/yr positive; break-even month {be_month} within the {horizon}-month horizon; evidence band {ev['band']}."]
    change: list[str] = []
    if vcons["net"] <= 0:
        codes.append("CONSERVATIVE_DIES")
        reasons.append(f"Cash-realised (conservative) view is {inr(vcons['net'])}/yr: the benefit is mostly capacity, not cash.")
        change.append("Fund from the training/capacity budget, not the savings ledger, or convert part of the benefit to cash.")
    else:
        reasons.append("Conservative view stays positive; the decision is robust to the haircut.")
    return out("fund", reasons, change, codes)


def advice_for(p: "core.Pilot", d: dict) -> dict:
    """5.1 + 5.2 + 5.4 for one pilot on the independent lens; conservative strength folded into reasons."""
    ev = evidence(p)
    return {"evidence": ev, **decide(p, ev, d)}


# 5.3 — why the pilot above outranks the one below: adjacent pairs over the funding order
# (ranked and funded first, then the payable survivors the budget left out), sentences cover
# net, payback, readiness and overlap. Deterministic.
# Overlap can never sit between two funded rows (selection is group-exclusive), so each funded
# row additionally pairs against the first same-group row eliminated for overlap: that
# competition is exactly what the explanation is for.
def head_to_head(rows: list[dict], pilots_by_id: dict) -> list[dict]:
    funded = sorted((r for r in rows if r["rank"] is not None), key=lambda r: r["rank"])
    ordered = funded + [r for r in rows if r["status"] == "not funded"]

    def pair(a: dict, b: dict) -> dict:
        reasons = []
        na, nb = a["net_annual_inr"], b["net_annual_inr"]
        reasons.append(f"net annual savings {core._inr(na)}/yr vs {core._inr(nb)}/yr ({core._inr(na - nb)}/yr in favour of {a['id']})")
        pa, pb = a["payback_months"], b["payback_months"]
        if pa is not None and pb is not None and pa != pb:
            reasons.append(f"payback {pa:g} vs {pb:g} months ({abs(pa - pb):g} months faster: {a['id'] if pa < pb else b['id']})")
        elif pa is not None and pb is not None:
            reasons.append(f"payback equal at {pa:g} months")
        ra, rb = pilots_by_id[a["id"]].readiness, pilots_by_id[b["id"]].readiness
        reasons.append(f"readiness {ra} vs {rb}" if ra != rb else f"same readiness ({ra})")
        ga = pilots_by_id[a["id"]].group
        if ga == pilots_by_id[b["id"]].group and not ga.startswith("__own_"):
            reasons.append(f'shared overlap group "{ga}": at most one of the two can be funded')
        return {"over": a["id"], "under": b["id"], "reasons": reasons}

    out = [pair(ordered[i], ordered[i + 1]) for i in range(len(ordered) - 1)]
    for r in funded:
        ga = pilots_by_id[r["id"]].group
        if ga.startswith("__own_"):
            continue
        for q in rows:
            if q["rank"] is not None or q["id"] not in pilots_by_id or q["id"] == r["id"]:
                continue
            if q["status"] == "dropped" and q["reason"].startswith(("Overlap", "overlap:")) \
                    and pilots_by_id[q["id"]].group == ga:
                out.append(pair(r, q))
                break
    return out
