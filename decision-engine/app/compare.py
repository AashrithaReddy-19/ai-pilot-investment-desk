"""Three-proposal comparison (Note B), scored by the same cash-flow engine (section 2 rule).

Pure functions, no I/O. Proposals are validated at the API boundary, converted to engine pilots
and read through the engine's own multi-period view, so the steady-state public checks hold.
Recommendation text is a deterministic template on that shared rule.
"""
from __future__ import annotations
import math
from typing import Any, Optional

from . import core, forecast, advice

MIN_PROPOSALS, MAX_PROPOSALS = 2, 8


class CompareError(ValueError):
    """Bad compare request: the gateway shows it as a 422."""


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def validate(proposals: Any) -> list[dict]:
    """2–8 proposals, every decision-driving field present and in range; duplicates rejected."""
    if not isinstance(proposals, list):
        raise CompareError("proposals must be an array of 2 to 8 objects.")
    if not MIN_PROPOSALS <= len(proposals) <= MAX_PROPOSALS:
        raise CompareError(f"Compare needs {MIN_PROPOSALS} to {MAX_PROPOSALS} proposals, got {len(proposals)}.")
    seen: set[str] = set()
    cleaned: list[dict] = []
    for i, p in enumerate(proposals, 1):
        if not isinstance(p, dict):
            raise CompareError(f"proposal {i} must be an object.")
        name = (p.get("name") or "").strip() if isinstance(p.get("name"), str) else ""
        if not name:
            raise CompareError(f"proposal {i}: name must be a non-empty string.")
        if len(name) > 80:
            raise CompareError(f"proposal {i}: name must be 1-80 characters.")
        if name in seen:
            raise CompareError(f"duplicate proposal name {name!r}.")
        seen.add(name)
        out: dict = {"name": name}
        for key in ("setup_cost_inr", "annual_benefit_inr"):
            v = p.get(key)
            if not _num(v) or v < 0:
                raise CompareError(f"proposal {i} ({name}): {key} must be a non-negative number, got {v!r}.")
            out[key] = v
        run = p.get("ai_running_cost_inr")
        if run is not None and (not _num(run) or run < 0):
            raise CompareError(f"proposal {i} ({name}): ai_running_cost_inr must be a non-negative number or null.")
        out["ai_running_cost_inr"] = run
        bt = p.get("benefit_type") or "cashable"
        if bt not in core.CASH_FACTOR_BY_TYPE:
            raise CompareError(f"proposal {i} ({name}): benefit_type {bt!r} must be one of "
                               f"{[k for k in core.CASH_FACTOR_BY_TYPE if k != 'unknown']}.")
        out["benefit_type"] = bt
        rd = p.get("readiness") or "medium"
        if rd not in core.RANK:
            raise CompareError(f"proposal {i} ({name}): readiness {rd!r} is not low/medium/high.")
        out["readiness"] = rd
        ad = p.get("adoption_pct", 100)
        if not _num(ad) or ad < 0 or ad > 100:
            raise CompareError(f"proposal {i} ({name}): adoption_pct must be a number in 0-100.")
        out["adoption_pct"] = ad
        for key in ("ramp_months", "delay_months"):
            v = p.get(key, 0)
            if not _num(v) or not float(v).is_integer() or not 0 <= v <= 120:
                raise CompareError(f"proposal {i} ({name}): {key} must be a whole number of months in 0-120.")
            out[key] = int(v)
        grp = p.get("overlap_group")
        if grp is not None and (not isinstance(grp, str) or len(grp.strip()) > 40):
            raise CompareError(f"proposal {i} ({name}): overlap_group must be a string of at most 40 characters.")
        if grp and grp.strip():
            out["overlap_group"] = grp.strip()
        for key in ("observed_adoption_pct", "observed_quality_pct"):
            v = p.get(key)
            if v is None:
                continue
            if not _num(v) or not 0 <= v <= 100:
                raise CompareError(f"proposal {i} ({name}): {key} must be a number in 0-100 or null.")
            out[key] = v
        for key in ("one_time_costs", "recurring_costs"):
            if p.get(key) is not None:
                if not isinstance(p[key], list):
                    raise CompareError(f"proposal {i} ({name}): {key} must be an array of cost lines.")
                out[key] = p[key]          # line-level checks happen in the loader (malformed lines warn)
        for key in ("benefit_driver", "baseline"):
            if p.get(key) is not None:
                if not isinstance(p[key], dict):
                    raise CompareError(f"proposal {i} ({name}): {key} must be an object.")
                out[key] = p[key]
        ms = p.get("money_source", core.PROVENANCE_ASSUMED)
        if ms not in core.MONEY_SOURCES:
            raise CompareError(f"proposal {i} ({name}): money_source must be one of {core.MONEY_SOURCES} "
                               "(observed = invoice- or contract-backed figures).")
        out["money_source"] = ms
        cs = p.get("cashable_share_pct")
        if cs is not None:
            if not _num(cs) or not 0 <= cs <= 100:
                raise CompareError(f"proposal {i} ({name}): cashable_share_pct must be a number in 0-100 or null.")
            out["cashable_share_pct"] = cs
        cleaned.append(out)
    return cleaned


def to_pilots(proposals: list[dict], d: dict) -> dict:
    """Validated proposals -> the engine's loader result ({pilots, invalid, warnings}).
    Every proposal-based endpoint goes through this one conversion, so all of them score
    a proposal identically."""
    fixture, exts = [], []
    for p in proposals:
        fixture.append({"id": p["name"], "task": p["name"],
                        "setup_cost_inr": p["setup_cost_inr"],
                        "annual_running_cost_inr": p["ai_running_cost_inr"],
                        "annual_benefit_inr": p["annual_benefit_inr"],
                        "benefit_type": p["benefit_type"],
                        **({"overlap_group": p["overlap_group"]} if "overlap_group" in p else {}),
                        **({"ramp_months": p["ramp_months"]} if p["ramp_months"] else {}),
                        **({"delay_months": p["delay_months"]} if p["delay_months"] else {}),
                        **{k: p[k] for k in ("one_time_costs", "recurring_costs", "benefit_driver", "money_source") if k in p}})
        exts.append({"id": p["name"], "readiness": p["readiness"], "adoption_pct": p["adoption_pct"],
                     **{k: p[k] for k in ("observed_adoption_pct", "observed_quality_pct") if k in p}})
    L = core.load_pilots({"records": {"pilots": fixture}}, exts, d)
    if L["invalid"]:
        raise CompareError(f"proposal could not be loaded: {L['invalid'][0]['errors']}")
    return L


def run(proposals: list[dict], budget: Any, mode: str, d: dict) -> dict:
    """Score every proposal with the engine, apply the section-2 rule and build the templates.

    Rule: drop proposals with no positive net annual savings OR break-even beyond the horizon
    (a 3-year loss); among the rest, within budget and with each overlap group counted once,
    pick the combination with the highest total net benefit (ties follow the 6-tier order).
    Without a budget the surviving set is the funding set. ROI and break-even are reported as
    supporting evidence, never the selector.
    """
    L = to_pilots(proposals, d)
    warnings = list(L["warnings"])
    mode = mode if mode in ("independent", "conservative") else "independent"

    ranked = sorted(L["pilots"], key=lambda q: core.pilot_key(q, mode))
    survivors: dict[str, tuple] = {}
    dropped: dict[str, str] = {}
    for p in ranked:
        v = core.view(p, mode, d)
        mp = v["multi_period"]
        if v["net"] <= 0:
            dropped[p.id] = "no positive net annual savings"
        elif mp["breakeven_month"] is None:
            dropped[p.id] = "3-year loss (break-even beyond the horizon)"
        else:
            survivors[p.id] = (p, v, mp)
    rest = [survivors[i][0] for i in survivors]

    budgeted = _num(budget)
    pf = core.build_portfolio(rest, budget, mode) if budgeted else None
    if budgeted:
        selected = list(pf["selected"])
        selection = {"mode": mode, "budget": round(budget), "selected": selected,
                     "total_net_inr": round(pf["total_net"]), "budget_left": round(pf["budget_left"]),
                     "ok": pf["ok"], "message": pf["message"]}
    else:
        selected, groups, total = [], set(), 0
        for p in sorted(rest, key=lambda q: core.pilot_key(q, mode)):
            if p.group in groups:
                dropped.setdefault(p.id, f'overlap: shares benefit group "{p.group}"')
                continue
            groups.add(p.group)
            selected.append(p.id)
            total += core.view(p, mode)["net"]
        selection = {"mode": mode, "budget": None, "selected": selected,
                     "total_net_inr": round(total), "budget_left": None, "ok": bool(selected),
                     "message": "No budget given: every surviving proposal is funded." if selected
                                else "No proposal survived the section-2 rule."}
    selected_set = set(selected)

    rejected_by = {r["id"]: r["reason"] for r in ((pf or {}).get("rejected") or [])}
    rows, rank = [], 0
    for p in ranked:
        if p.id in dropped:                       # eliminated before or during selection
            if p.id in survivors:                 # shed a survivor a later stage dropped (overlap)
                del survivors[p.id]
            v = core.view(p, mode, d)
            mp = v["multi_period"]
            status, reason = "dropped", dropped[p.id]
        elif p.id in survivors:
            p, v, mp = survivors[p.id]
            if p.id in selected_set:
                rank += 1
                status, reason = ("recommended" if rank == 1 else "kept"), None
            else:
                status, reason = "not funded", rejected_by.get(p.id, "not funded")
        else:
            v = core.view(p, mode, d)
            mp = v["multi_period"]
            status, reason = "dropped", dropped[p.id]
        cash = core.cash_factor(p.benefit_type)
        cash_b = p.benefit * cash
        rows.append({
            "id": p.id, "name": p.id,
            "setup_cost_inr": round(p.setup), "ai_running_cost_inr": round(p.run),
            "annual_benefit_inr": round(p.benefit), "benefit_type": p.benefit_type,
            "cash_vs_capacity": {"cash_benefit_inr": round(cash_b), "capacity_benefit_inr": round(p.benefit - cash_b),
                                 "cash_share_pct": round(cash * 100, 1)},
            "net_annual_inr": round(v["net"]),
            "payback_months": None if v["payback_months"] is None else round(v["payback_months"], 1),
            "profit_inr": mp["profit_inr"], "roi_pct": mp["roi_pct"], "npv_inr": mp["npv_inr"],
            "breakeven_month": mp["breakeven_month"], "breakeven_beyond_horizon": mp["breakeven_beyond_horizon"],
            "cumulative": mp["cumulative"],
            "flags": [{"code": f["code"], "message": f["message"]} for f in p.flags],
            "rank": rank if p.id in selected_set else None,
            "status": status, "reason": reason,
        })
    total_net = sum(r["net_annual_inr"] for r in rows if r["status"] in ("recommended", "kept"))

    tokens, price = d.get("tokens_per_month"), d.get("price_per_1m_tokens")
    tv = forecast.value_per_1m_tokens(total_net, tokens)
    tvp = {"net_annual_savings_inr": total_net,
           "value_per_1m_tokens": None if tv is None else round(tv, 2),
           "price_per_1m_tokens": price if _num(price) else None,
           "value_vs_price_ratio": None,
           "flags": []}
    if not _num(tokens):
        tvp["flags"].append({"code": "TOKENS_MISSING", "message": "Monthly AI token usage not recorded; token value unknown."})
    if not _num(price) or (_num(price) and price <= 0):
        tvp["flags"].append({"code": "TOKEN_PRICE_MISSING", "message": "Price per 1M tokens not recorded; token value unknown."})
    elif tv is not None:
        tvp["value_vs_price_ratio"] = round(tv / price, 2)

    rec_text = _recommendation_text(rows, selection, pf, d)
    return {
        "proposals": rows, "selection": selection,
        "head_to_head": advice.head_to_head(rows, {p.id: p for p in ranked}),
        "recommendation": {"text": rec_text, "selected": selected,
                           "assumptions": {"horizon_months": d.get("horizon_months", core.HORIZON_MONTHS),
                                           "discount_rate": d.get("discount_rate", core.DISCOUNT_RATE),
                                           "mode": mode,
                                           "selector": "highest total net annual benefit; ROI and break-even are evidence"}},
        "token_value_vs_price": tvp,
        "flags": [f for f in rows_ok_flags(rows)], "warnings": warnings,
    }


def rows_ok_flags(rows: list[dict]):
    """The pilot-level flags worth surfacing at compare level (assumed run cost, benefit type)."""
    for r in rows:
        for f in r["flags"]:
            yield f


_DROPPED_TEXT = {
    "no positive net annual savings": "no positive net annual savings",
    "3-year loss (break-even beyond the horizon)": "3-year loss (break-even beyond the horizon)",
}


def _recommendation_text(rows: list[dict], selection: dict, pf: Optional[dict], d: dict) -> str:
    sel = selection["selected"]
    if not sel:
        txt = "No proposal is recommended."
        if selection["budget"] is not None and pf is not None and "below the cheapest" in pf.get("message", ""):
            txt += f" {pf['message']}"
        for r in rows:
            if r["status"] == "dropped":
                txt += f" {r['name']} dropped: {_DROPPED_TEXT.get(r['reason'], r['reason'])}."
        return txt
    first = next(r for r in rows if r["id"] == sel[0])
    be = f", break-even {first['breakeven_month']:.1f} months" if first["breakeven_month"] is not None else ""
    within = " within budget" if selection["budget"] is not None else ""
    txt = (f"{first['name']} recommended: highest net benefit{within} "
           f"({_lakh(first['net_annual_inr'])} L/yr net annual savings{be}).")
    others = [r["name"] for r in rows if r["id"] in sel and r["id"] != first["id"]]
    if others:
        txt += f" Funded together with {', '.join(others)}: total {_inrupee(selection['total_net_inr'])}/yr net savings."
    horizon = d.get("horizon_months", core.HORIZON_MONTHS)
    txt += (f" Horizon {horizon:g} months at r = {d.get('discount_rate', core.DISCOUNT_RATE)}: "
            "ROI and break-even are supporting evidence, not the selector.")
    for r in rows:
        if r["status"] == "dropped":
            txt += f" {r['name']} dropped: {_DROPPED_TEXT.get(r['reason'], r['reason'])}."
        elif r["status"] == "not funded":
            reason = r["reason"].rstrip(".")
            if reason.startswith("Not funded: "):
                reason = reason[len("Not funded: "):]
            txt += f" {r['name']} not funded: {reason}."
    return txt


def _lakh(x: float) -> str:
    """INR as lakh without scientific notation: 250000 -> '2.5', 12345678 -> '123.46'."""
    return f"{x / 100000:,.2f}".rstrip("0").rstrip(".")


def _inrupee(x: float) -> str:
    return f"INR {x:,.0f}"
