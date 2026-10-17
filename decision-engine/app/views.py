"""Views (A7): the yearly savings view and the executive dashboard view.

Pure functions, no I/O. Every number here is read from the same engine calls the other
endpoints use (compare.run, decision.decide_set, core.multi_period), never recomputed a second
way, and `consistency` carries self-checks proving the dashboard, the yearly view and the compare
rows agree. The screens render these numbers; they never recalculate them.
"""
from __future__ import annotations
from typing import Any, Optional

from . import core, cashflow, compare, decision, economics
from .version import RULES_VERSION, CONTRACT_VERSION, FORMULA_VERSION

PROPOSAL_STATES = ["recommended", "viable", "not_viable"]
VIEW_RULES = {
    "proposal_states": {"recommended": "in the funded set (section-2 rule: highest total net within budget)",
                        "viable": "passes every eligibility gate but is not funded (budget or overlap)",
                        "not_viable": "fails at least one mandatory eligibility gate"},
    "charts": ["cumulative_cash_flow", "yearly_savings", "roi_breakeven", "cash_vs_capacity"],
    "chart_sync": "every chart lists proposals in `series_order` (funding rank, then the rest) so colours and "
                  "selection stay in step across the four charts",
    "yearly_rule": "monthly engine flows summed into 12-month years; the sum of yearly net equals profit_inr",
}


def _years_for(p: "core.Pilot", mode: str, d: dict, cash_share: float) -> dict:
    mp = core.multi_period(p, mode, d)
    b = p.benefit if mode == "independent" else core.realized_benefit(p)
    comps = cashflow.monthly_components(p.setup, b / 12, p.run / 12, mp["horizon_months"], p.ramp_months, p.delay_months)
    years, cum = [], 0.0
    for y in cashflow.yearly_totals(comps):
        cum += y["net"]
        years.append({"year": y["year"], "months": y["months"],
                      "benefit_inr": round(y["benefit"]),
                      "cashable_inr": round(y["benefit"] * cash_share),
                      "capacity_inr": round(y["benefit"] * (1 - cash_share)),
                      "running_cost_inr": round(y["run"]), "one_time_inr": round(y["one_time"]),
                      "net_inr": round(y["net"]), "cumulative_inr": round(cum)})
    totals = {k: sum(y[k] for y in years) for k in ("benefit_inr", "cashable_inr", "capacity_inr",
                                                    "running_cost_inr", "one_time_inr")}
    totals["net_inr"] = round(sum(y["net"] for y in cashflow.yearly_totals(comps)))
    return {"id": p.id, "years": years, "totals": totals, "profit_inr": mp["profit_inr"],
            "matches_profit": totals["net_inr"] == mp["profit_inr"]}


def yearly_view(pilots: list, d: dict, mode: str = "independent", extras: Optional[dict] = None,
                funded: Optional[list] = None) -> dict:
    extras = extras or {}
    rows = []
    for p in pilots:
        ex = extras.get(p.id) or {}
        share = economics.split(p, ex.get("baseline"), ex.get("cashable_share_pct"))["cashable_share_pct"] / 100
        rows.append(_years_for(p, mode, d, share))
    funded_ids = [r["id"] for r in rows] if funded is None else list(funded)
    n_years = max((len(r["years"]) for r in rows), default=0)
    portfolio = []
    cum = 0
    for i in range(n_years):
        ys = [r["years"][i] for r in rows if r["id"] in funded_ids and i < len(r["years"])]
        net = sum(y["net_inr"] for y in ys)
        cum += net
        portfolio.append({"year": i + 1, "benefit_inr": sum(y["benefit_inr"] for y in ys),
                          "cashable_inr": sum(y["cashable_inr"] for y in ys),
                          "running_cost_inr": sum(y["running_cost_inr"] for y in ys),
                          "one_time_inr": sum(y["one_time_inr"] for y in ys),
                          "net_inr": net, "cumulative_inr": cum})
    return {"mode": mode, "horizon_months": core.multi_period(pilots[0], mode, d)["horizon_months"] if pilots else None,
            "proposals": rows, "portfolio": {"ids": funded_ids, "years": portfolio},
            "formula_version": FORMULA_VERSION}


def _breakeven(cum: list) -> Optional[int]:
    return cashflow.breakeven_month_from_cumulative(cum) if cum else None


def _kpi(key: str, label: str, value: Any, unit: str, source: str) -> dict:
    return {"key": key, "label": label, "value": value, "unit": unit, "source": source}


def dashboard(clean: list, budget: Any, mode: str, d: dict, budget_set: Optional[dict] = None,
              ai_burn_to_date: Any = None, weights: Any = None) -> dict:
    """The executive dashboard: header, summary, KPIs, recommendation, proposal selector states,
    risks, next actions, four synchronised charts and links. `clean` = compare.validate output."""
    L = compare.to_pilots(clean, d)
    pilots = L["pilots"]
    extras = {p["name"]: {k: p[k] for k in ("baseline", "cashable_share_pct") if k in p} for p in clean}
    for ex in extras.values():
        if "baseline" in ex:
            ex["baseline"] = economics.validate_baseline(ex["baseline"])
    cmp = compare.run(clean, budget, mode, d)
    dec = decision.decide_set(pilots, d, budget_set, extras, weights)
    funded = list(cmp["selection"]["selected"])
    yv = yearly_view(pilots, d, mode, extras, funded)
    rows = {r["id"]: r for r in cmp["proposals"]}
    by_id = {p.id: p for p in pilots}
    econ = {pid: dec["decisions"][pid]["economics"] for pid in by_id}

    # --- KPIs over the funded set
    invest = sum(rows[i]["setup_cost_inr"] for i in funded)
    profit = sum(rows[i]["profit_inr"] for i in funded)
    npv = sum(rows[i]["npv_inr"] for i in funded)
    horizon = yv["horizon_months"] or core.HORIZON_MONTHS
    port_cum = [sum(rows[i]["cumulative"][t] for i in funded) for t in range(horizon)] if funded else []
    fc = core.forecast_block(d)
    burn_ok = isinstance(ai_burn_to_date, (int, float)) and not isinstance(ai_burn_to_date, bool) and ai_burn_to_date >= 0
    kpis = [
        _kpi("total_investment_inr", "Total investment", invest, "INR", "Σ setup of the funded set"),
        _kpi("net_annual_savings_inr", "Net annual savings", cmp["selection"]["total_net_inr"], "INR/yr",
             "Σ net annual benefit of the funded set (compare selection)"),
        _kpi("cashable_savings_inr", "Cashable savings", sum(econ[i]["cashable_savings_inr"] for i in funded), "INR/yr",
             "Σ gross × cashable share"),
        _kpi("capacity_value_inr", "Capacity value", sum(econ[i]["capacity_value_inr"] for i in funded), "INR/yr",
             "Σ gross − cashable"),
        _kpi("profit_inr", f"{horizon}-month profit", profit, "INR", "Σ multi-period profit of the funded set"),
        _kpi("npv_inr", "NPV", npv, "INR", f"Σ NPV at r = {d.get('discount_rate', core.DISCOUNT_RATE)}"),
        _kpi("roi_pct", f"{horizon}-month ROI", None if invest <= 0 else round(profit / invest * 100, 1), "%",
             "portfolio profit ÷ total investment"),
        _kpi("breakeven_month", "Portfolio break-even", _breakeven(port_cum), "month",
             "first month the funded set's summed cumulative cash flow is ≥ 0"),
        _kpi("ai_burn_to_date_inr", "AI burn to date", round(ai_burn_to_date) if burn_ok else None, "INR",
             "observed (confirmed balance-sheet rows)" if burn_ok else "not supplied: confirm balance-sheet rows first"),
        _kpi("next_fy_ai_cost_inr", "Next-FY AI cost", fc["next_fy_cost_inr"], "INR",
             "assumed growth fallback (send usage history to /forecast/usage for a backtested forecast)"),
    ]

    # --- proposal selector states
    proposals = []
    order = funded + [r["id"] for r in dec["ranking"] if r["id"] not in funded]
    rank_by = {r["id"]: r for r in dec["ranking"]}
    for pid in order:
        dd = dec["decisions"][pid]
        state = "recommended" if pid in funded else ("viable" if dd["eligibility"]["eligible"] else "not_viable")
        r = rows[pid]
        proposals.append({"id": pid, "state": state, "outcome": dd["outcome"], "score": rank_by[pid]["score"],
                          "rank": rank_by[pid]["rank"], "funding_rank": r["rank"],
                          "net_annual_inr": r["net_annual_inr"], "payback_months": r["payback_months"],
                          "roi_pct": r["roi_pct"], "npv_inr": r["npv_inr"], "breakeven_month": r["breakeven_month"],
                          "evidence_band": dd["evidence"]["band"], "budget_status": dd["economics"]["budget"]["status"],
                          "competitiveness": dd["economics"]["competitiveness"]["class"],
                          "reason": dd["reasons"][0]})

    # --- risks and next actions
    risks, seen = [], {}
    for pid in order:
        dd = dec["decisions"][pid]
        for f in by_id[pid].flags + dd["economics"]["flags"]:
            seen.setdefault(f["code"], {"code": f["code"], "message": f["message"], "ids": []})["ids"].append(pid)
        for g in dd["eligibility"]["gates"]:
            if g["status"] == "fail":
                seen.setdefault(f"GATE_{g['gate']}", {"code": f"GATE_{g['gate']}",
                                                      "message": f"fails the {g['gate']} gate", "ids": []})["ids"].append(pid)
        if dd["economics"]["budget"]["status"] == "at_risk":
            seen.setdefault("BUDGET_AT_RISK", {"code": "BUDGET_AT_RISK",
                                               "message": "fits the budget only without contingency or uses > 90% of a line",
                                               "ids": []})["ids"].append(pid)
    severity = {"GATE_NET_POSITIVE": "high", "GATE_BREAKEVEN_IN_HORIZON": "high", "GATE_BUDGET_FIT": "high",
                "GATE_BENEFIT_HAS_CASH": "high", "BUDGET_AT_RISK": "medium", "BENEFIT_EXCEEDS_BASELINE": "high",
                "RUN_COST_ASSUMED": "medium", "TIME_NOT_CASH": "medium", "BENEFIT_TYPE_NON_CASHABLE": "high"}
    for code, r in seen.items():
        risks.append({**r, "severity": severity.get(code, "low")})
    risks.sort(key=lambda r: ({"high": 0, "medium": 1, "low": 2}[r["severity"]], r["code"]))
    actions, seen_a = [], set()
    for a in dec["overall"]["next_actions"] + [a for pid in funded for a in dec["decisions"][pid]["next_actions"]]:
        if a["code"] not in seen_a:
            seen_a.add(a["code"])
            actions.append(a)

    # --- summary
    ov = dec["overall"]
    best = ov["best"]
    flags = []
    if best and best not in funded:
        flags.append({"code": "BEST_NOT_FUNDED", "message": f"{best} ranks first on the weighted score but the budget "
                                                           "selection (highest total net) does not fund it."})
    headline = {"invest": f"Invest in {best}", "limited_pilot": f"Run {best} as a limited pilot",
                "no_clear_winner": f"No clear winner between {', '.join(ov['contenders'])}",
                "gather_evidence": "Gather evidence before funding", "renegotiate": "Renegotiate costs before funding",
                "defer": "Defer to the next budget cycle", "reject": "Do not fund"}[ov["outcome"]]
    net_txt = core._inr(cmp["selection"]["total_net_inr"])
    short = (f"{headline}. Funded set {', '.join(funded) or 'none'}: {net_txt}/yr net savings on "
             f"{core._inr(invest)} invested.")
    read_more = " ".join([*ov["reasons"], cmp["recommendation"]["text"]]
                         + [f"{p['id']}: {p['reason']}" for p in proposals])

    # --- charts (one shared order)
    def series(fn):
        return [{"id": pid, "values": fn(pid)} for pid in order]
    yv_by = {r["id"]: r for r in yv["proposals"]}
    charts = {
        "series_order": order,
        "cumulative_cash_flow": {"x": list(range(1, horizon + 1)), "x_label": "month", "unit": "INR",
                                 "series": series(lambda pid: rows[pid]["cumulative"]),
                                 "portfolio": port_cum},
        "yearly_savings": {"x": [y["year"] for y in yv["portfolio"]["years"]], "x_label": "year", "unit": "INR",
                           "series": series(lambda pid: [y["net_inr"] for y in yv_by[pid]["years"]]),
                           "portfolio": [y["net_inr"] for y in yv["portfolio"]["years"]]},
        "roi_breakeven": {"unit": {"roi_pct": "%", "breakeven_month": "month"},
                          "rows": [{"id": pid, "roi_pct": rows[pid]["roi_pct"], "breakeven_month": rows[pid]["breakeven_month"]}
                                   for pid in order]},
        "cash_vs_capacity": {"unit": "INR/yr",
                             "rows": [{"id": pid, "cashable_inr": econ[pid]["cashable_savings_inr"],
                                       "capacity_inr": econ[pid]["capacity_value_inr"],
                                       "running_cost_inr": rows[pid]["ai_running_cost_inr"]} for pid in order]},
    }

    consistency = [
        {"check": "yearly net totals equal multi-period profit for every proposal",
         "ok": all(r["matches_profit"] for r in yv["proposals"])},
        {"check": "KPI net annual savings equals Σ funded compare rows",
         "ok": cmp["selection"]["total_net_inr"] == sum(rows[i]["net_annual_inr"] for i in funded)},
        {"check": "KPI profit equals Σ funded yearly net",
         "ok": profit == sum(y["net_inr"] for y in yv["portfolio"]["years"])},
        {"check": "portfolio cumulative series ends at the KPI profit",
         "ok": (not port_cum) or abs(port_cum[-1] - profit) <= len(funded)},
    ]
    return {
        "header": {"title": "AI investment decision", "rules_version": RULES_VERSION,
                   "contract_version": CONTRACT_VERSION, "formula_version": FORMULA_VERSION,
                   "mode": mode, "horizon_months": horizon, "discount_rate": d.get("discount_rate", core.DISCOUNT_RATE),
                   "budget": cmp["selection"]["budget"], "proposal_count": len(pilots)},
        "summary": {"headline": headline, "text": short, "read_more": read_more},
        "kpis": kpis,
        "recommendation": {"outcome": ov["outcome"], "best": best, "contenders": ov["contenders"],
                           "funded": funded, "reasons": ov["reasons"], "compare_text": cmp["recommendation"]["text"]},
        "best_proposal": next((p for p in proposals if p["id"] == best), None),
        "proposals": proposals,
        "risks": risks,
        "next_actions": actions,
        "charts": charts,
        "yearly_portfolio": yv["portfolio"]["years"],
        "budget": economics.portfolio_budget(pilots, funded, d, budget_set),
        "links": {"decision_report": "/export/decision-report", "yearly_csv": "/export/yearly-csv",
                  "compare": "/compare", "decision": "/decision", "yearly_savings": "/views/yearly-savings"},
        "consistency": consistency,
        "flags": flags + list(cmp["flags"]), "warnings": cmp["warnings"],
    }
