"""Deterministic analyses: fixture validation, original-vs-synthetic comparison, derived-metric and decision
validation, adoption / quality / readiness / cash-vs-capacity / overlap / evidence analyses, breakpoints, stress tests.

Every derived value is recomputed from the inputs with exact arithmetic; stored derived columns are only compared.
All synthetic findings are about this SYNTHETIC test dataset, not about real enterprises.
"""
from __future__ import annotations
import copy, statistics
from collections import Counter
from fractions import Fraction

from synthetic import portfolio as P
from synthetic import rules as R

SYNTH_NOTE = "This metric is part of the synthetic extension and is not provided by the official C23 fixture."
TOLERANCE = Fraction(5, 1000)          # stored derived values are rounded to 2 decimals -> |diff| <= 0.005
BUDGETS = (200000, 400000, 600000, 800000)
STORED_REASON_CODE = {"Meets current financial and pilot gates": R.ALL_PASSED, "Quality threshold failed": "QUALITY_THRESHOLD_FAILED",
                      "Adoption below economic break-even": "ADOPTION_BELOW_BREAKEVEN", "Adoption below target": "LOW_ADOPTION",
                      "Readiness too low": "LOW_READINESS"}


def f2(x) -> float | None:
    return None if x is None else round(float(x), 2)


def _stats(vals: list) -> dict:
    v = [float(x) for x in vals if x is not None]
    if not v:
        return {"count": 0, "mean": None, "median": None, "min": None, "max": None, "std": None}
    return {"count": len(v), "mean": round(statistics.fmean(v), 2), "median": round(statistics.median(v), 2), "min": round(min(v), 2),
            "max": round(max(v), 2), "std": round(statistics.stdev(v), 2) if len(v) > 1 else 0.0}


# ---------------------------------------------------------------- 3. official reference case

def reference_check(fx: dict) -> dict:
    """AI1 recomputed from the fixture inputs; the expected value is read from the fixture itself, not hard-coded."""
    p = fx["records"]["pilots"][0]
    fin = R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"])
    expected = fx["records"]["expected_first_pilot_payback_months"]
    return {"pilot": p["id"], "setup_cost_inr": p["setup_cost_inr"], "annual_running_cost_inr": p["annual_running_cost_inr"],
            "annual_benefit_inr": p["annual_benefit_inr"], "net_annual_benefit_inr": R.fmt(fin.net_annual),
            "monthly_net_benefit_inr": R.fmt(fin.monthly_net), "roi_pct": R.fmt(fin.roi_pct),
            "expected_payback_months": expected, "calculated_payback_months": R.fmt(fin.payback_months),
            "status": "PASS" if fin.payback_months == R.frac(expected) else "FAIL"}


# ---------------------------------------------------------------- 4. original vs synthetic

def _metric_values(rows: list, basis=R.BASIS_STATED) -> dict:
    out = {k: [] for k in ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "net_annual_benefit_inr", "roi_pct", "payback_months")}
    for p in rows:
        fin = R.finance(p.get("setup_cost_inr"), p.get("annual_running_cost_inr"), R.basis_benefit(p, basis))
        out["setup_cost_inr"].append(R.frac(p.get("setup_cost_inr")))
        out["annual_running_cost_inr"].append(R.frac(p.get("annual_running_cost_inr")))
        out["annual_benefit_inr"].append(R.frac(p.get("annual_benefit_inr")))
        out["net_annual_benefit_inr"].append(fin.net_annual)
        out["roi_pct"].append(fin.roi_pct)
        out["payback_months"].append(fin.payback_months)
    return out


SYNTH_ONLY = ("expected_adoption_pct", "actual_adoption_pct", "minimum_adoption_pct", "expected_quality_pct", "actual_quality_pct",
              "minimum_quality_pct", "data_readiness_pct", "technical_readiness_pct", "process_readiness_pct", "overall_readiness_pct",
              "capacity_value_inr", "realized_cash_saving_inr")


def comparison(fx_rows: list, syn: list) -> list:
    """One row per metric x statistic family. Values recomputed from inputs (stated basis, as in the fixture's public check)."""
    o, s = _metric_values(fx_rows), _metric_values(syn)
    rows = [{"metric": "number_of_pilots", "statistic": "count", "original_fixture": len(fx_rows), "synthetic_dataset": len(syn), "note": ""}]
    for m in o:
        so, ss = _stats(o[m]), _stats(s[m])
        for st in ("count", "mean", "median", "min", "max", "std"):
            rows.append({"metric": m, "statistic": st, "original_fixture": so[st], "synthetic_dataset": ss[st],
                         "note": "recalculated from inputs (stated basis)" if m in ("net_annual_benefit_inr", "roi_pct", "payback_months") else ""})
    for cat in ("benefit_type", "overlap_group"):
        co, cs = Counter(p.get(cat) for p in fx_rows), Counter(p.get(cat) for p in syn)
        for k in sorted(set(co) | set(cs), key=str):
            rows.append({"metric": f"{cat}={k}", "statistic": "count", "original_fixture": co.get(k, 0), "synthetic_dataset": cs.get(k, 0), "note": ""})
    for m in SYNTH_ONLY:
        if m == "overall_readiness_pct":
            vals = [R.readiness(p)[0] for p in syn]
            note = SYNTH_NOTE + " Recalculated as the mean of the three readiness components."
        else:
            vals, note = [R.frac(p.get(m)) for p in syn], SYNTH_NOTE
        ss = _stats(vals)
        for st in ("count", "mean", "median", "min", "max", "std"):
            rows.append({"metric": m, "statistic": st, "original_fixture": "not provided", "synthetic_dataset": ss[st], "note": note})
    for cat in ("evidence_status", "decision"):
        for k, n in sorted(Counter(p.get(cat) for p in syn).items()):
            rows.append({"metric": f"{cat}={k}", "statistic": "count", "original_fixture": "not provided", "synthetic_dataset": n, "note": SYNTH_NOTE})
    return rows


# ---------------------------------------------------------------- 5. derived metrics

DERIVED = ("net_annual_benefit_inr", "monthly_net_benefit_inr", "payback_months", "roi_pct", "breakeven_adoption_pct", "overall_readiness_pct")


def recompute(p: dict) -> dict:
    fin = R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"])
    return {"net_annual_benefit_inr": fin.net_annual, "monthly_net_benefit_inr": fin.monthly_net, "payback_months": fin.payback_months,
            "roi_pct": fin.roi_pct, "breakeven_adoption_pct": R.breakeven_adoption_pct(p["annual_running_cost_inr"], p["annual_benefit_inr"]),
            "overall_readiness_pct": R.readiness(p)[0]}


def derived_validation(syn: list) -> tuple[list, dict]:
    rows, max_diff, row_ok = [], Fraction(0), 0
    per_metric = {m: 0 for m in DERIVED}
    for p in syn:
        calc = recompute(p)
        ok_all = True
        for m in DERIVED:
            stored, c = p.get(m), calc[m]
            if stored is None or c is None:
                ok = stored is None and c is None
                diff = None
            else:
                diff = abs(R.frac(stored) - c)
                ok = diff <= TOLERANCE
                max_diff = max(max_diff, diff)
            per_metric[m] += ok
            ok_all &= ok
            rows.append({"record_id": p["record_id"], "metric": m, "stored": stored, "recalculated": R.fmt(c, 4),
                         "abs_difference": R.fmt(diff, 6) if diff is not None else None, "within_tolerance": ok})
        row_ok += ok_all
    summary = {"total_rows": len(syn), "rows_all_metrics_match": row_ok, "rows_with_mismatch": len(syn) - row_ok,
               "metric_checks": len(rows), "metric_checks_matched": sum(per_metric.values()), "matches_per_metric": per_metric,
               "max_abs_difference": float(max_diff), "tolerance": float(TOLERANCE),
               "tolerance_reason": "stored derived values are rounded to 2 decimals"}
    return rows, summary


# ---------------------------------------------------------------- 6/7. decisions

def evaluate_all(rows: list, basis: str) -> dict:
    return {P.pid(p): R.evaluate(p, basis) for p in rows}


def _mismatch_reason(p: dict, stated: R.Evaluation, cash: R.Evaluation, comparison_name: str) -> str:
    ev = stated if comparison_name == "stored_vs_rules_stated_basis" else cash
    stored = STORED_REASON_CODE.get(p["decision_reason"], p["decision_reason"])
    if stored == "LOW_READINESS" and "LOW_READINESS" not in ev.reason_codes:
        comps = {k.replace("_readiness_pct", ""): p[k] for k in R.READINESS_FIELDS}
        low = min(comps, key=comps.get)
        return (f"Stored label says readiness too low, but overall readiness is {R.fmt(ev.readiness_pct)}% (>= the documented 50% threshold). "
                f"Lowest component: {low} {comps[low]}%. The dataset generator appears to use an undocumented per-component readiness rule; "
                "the documented c23-rules-v1 rule (mean < 50) was kept, not changed to fit the label.")
    if comparison_name == "stored_vs_production_cash_basis" and R.canonical_benefit_type(p["benefit_type"]) != "cashable":
        return (f"benefit_type '{p['benefit_type']}' is not cashable: the production engine counts only realized_cash_saving_inr "
                f"INR {p['realized_cash_saving_inr']} (stated benefit INR {p['annual_benefit_inr']}). Cash net INR {R.fmt(cash.on_basis.net_annual)}, "
                f"cash break-even adoption {R.fmt(cash.breakeven_pct)}% vs actual {p['actual_adoption_pct']}% -> {','.join(cash.reason_codes) or R.ALL_PASSED}. "
                "The stored label treats the full stated benefit as cash.")
    return f"stored {p['decision']} ({stored}) vs recalculated {ev.decision} ({','.join(ev.reason_codes) or R.ALL_PASSED})"


def decision_validation(syn: list, go_outputs: dict | None) -> dict:
    stated, cash = evaluate_all(syn, R.BASIS_STATED), evaluate_all(syn, R.BASIS_CASH)
    go = {g["record_id"]: g for g in (go_outputs or {}).get("synthetic_pilots", [])}
    rows, mismatches = [], []
    for p in syn:
        i = P.pid(p)
        s, c = stated[i], cash[i]
        g = go.get(i)
        rows.append({"record_id": i, "benefit_type": p["benefit_type"], "stored_decision": p["decision"],
                     "stored_reason": STORED_REASON_CODE.get(p["decision_reason"], p["decision_reason"]),
                     "rules_stated_basis_decision": s.decision, "rules_stated_basis_reasons": ";".join(s.reason_codes) or R.ALL_PASSED,
                     "production_cash_basis_decision": c.decision, "production_cash_basis_reasons": ";".join(c.reason_codes) or R.ALL_PASSED,
                     "go_engine_decision": g["decision"] if g else None,
                     "python_replica_matches_go": (g is not None and g["decision"] == c.decision and g["reason_codes"] == c.reason_codes),
                     "stored_matches_stated_rules": p["decision"] == s.decision, "stored_matches_production": p["decision"] == c.decision})
        for name, ev in (("stored_vs_rules_stated_basis", s), ("stored_vs_production_cash_basis", c)):
            if ev.decision != p["decision"]:
                mismatches.append({"comparison": name, "record_id": i, "stored_decision": p["decision"], "calculated_decision": ev.decision,
                                   "benefit_type": p["benefit_type"], "setup_cost_inr": p["setup_cost_inr"],
                                   "annual_running_cost_inr": p["annual_running_cost_inr"], "annual_benefit_inr": p["annual_benefit_inr"],
                                   "realized_cash_saving_inr": p["realized_cash_saving_inr"], "actual_adoption_pct": p["actual_adoption_pct"],
                                   "minimum_adoption_pct": p["minimum_adoption_pct"], "actual_quality_pct": p["actual_quality_pct"],
                                   "minimum_quality_pct": p["minimum_quality_pct"], "data_readiness_pct": p["data_readiness_pct"],
                                   "technical_readiness_pct": p["technical_readiness_pct"], "process_readiness_pct": p["process_readiness_pct"],
                                   "reason_for_difference": _mismatch_reason(p, s, c, name)})

    def conf(key):
        labels = ["PROCEED", "REVISE", "STOP"]
        return {"labels": labels, "rows_are": "stored", "cols_are": key,
                "matrix": [[sum(r["stored_decision"] == a and r[key] == b for r in rows) for b in labels] for a in labels]}

    n = len(rows)
    ms = sum(r["stored_matches_stated_rules"] for r in rows)
    mc = sum(r["stored_matches_production"] for r in rows)
    parity = sum(r["python_replica_matches_go"] for r in rows)
    return {"rows": rows, "mismatches": mismatches, "stated": stated, "cash": cash, "summary": {
        "total_decisions": n,
        "stored_vs_rules_stated_basis": {"matching": ms, "mismatching": n - ms, "reproduction_pct": round(100 * ms / n, 2),
                                         "confusion_matrix": conf("rules_stated_basis_decision"),
                                         "meaning": "Documented c23-rules-v1 thresholds applied with the stated benefit (the basis of the dataset's derived columns)."},
        "stored_vs_production_cash_basis": {"matching": mc, "mismatching": n - mc, "reproduction_pct": round(100 * mc / n, 2),
                                            "confusion_matrix": conf("production_cash_basis_decision"),
                                            "meaning": "Production engine default: non-cashable benefits count only realized cash saving."},
        "python_replica_vs_go_engine": {"matching": parity, "total": n, "go_outputs_present": bool(go)},
        "production_decision_counts": dict(sorted(Counter(r["production_cash_basis_decision"] for r in rows).items())),
        "stated_decision_counts": dict(sorted(Counter(r["rules_stated_basis_decision"] for r in rows).items())),
        "stored_decision_counts": dict(sorted(Counter(r["stored_decision"] for r in rows).items()))}}


PARITY_FIELDS = (
    ("decision", "decision", lambda e: e.decision),
    ("reason_codes", "reason_codes", lambda e: e.reason_codes),
    ("stated_net_annual_benefit_inr", "stated_net_annual_benefit_inr", lambda e: R.fmt(e.stated.net_annual)),
    ("stated_payback_months", "stated_payback_months", lambda e: R.fmt(e.stated.payback_months)),
    ("stated_roi_pct", "stated_roi_pct", lambda e: R.fmt(e.stated.roi_pct)),
    ("cash_benefit_inr", "cash_benefit_inr", lambda e: R.fmt(e.basis_benefit)),
    ("cash_net_annual_benefit_inr", "cash_net_annual_benefit_inr", lambda e: R.fmt(e.on_basis.net_annual)),
    ("cash_payback_months", "cash_payback_months", lambda e: R.fmt(e.on_basis.payback_months)),
    ("cash_payback_status", "cash_payback_status", lambda e: e.on_basis.payback_status),
    ("cash_roi_pct", "cash_roi_pct", lambda e: R.fmt(e.on_basis.roi_pct)),
    ("cash_breakeven_adoption_pct", "cash_breakeven_adoption_pct", lambda e: R.fmt(e.breakeven_pct)),
    ("overall_readiness_pct", "overall_readiness_pct", lambda e: R.fmt(e.readiness_pct)),
    ("readiness_level", "readiness_level", lambda e: e.readiness_level),
)


def engine_parity(syn: list, official_rows: list, go_outputs: dict | None) -> tuple[list, dict]:
    """Field-by-field comparison of the Python replica with the Go production engine (cash basis = production)."""
    rows, per_dataset = [], {}
    for dataset, pilots, key in (("synthetic", syn, "synthetic_pilots"), ("official_fixture", official_rows, "official_pilots")):
        go = {g["record_id"]: g for g in (go_outputs or {}).get(key, [])}
        full = 0
        for p in pilots:
            ev, g = R.evaluate(p, R.BASIS_CASH), go.get(P.pid(p))
            ok_all = g is not None
            for name, gk, fn in PARITY_FIELDS:
                py, gv = fn(ev), (g.get(gk) if g else None)
                ok = g is not None and py == gv
                ok_all &= ok
                rows.append({"dataset": dataset, "record_id": P.pid(p), "field": name, "python": py, "go": gv, "match": ok})
            full += ok_all
        per_dataset[dataset] = {"pilots": len(pilots), "pilots_fully_matching": full, "go_outputs_present": bool(go)}
    checks = len(rows)
    matched = sum(r["match"] for r in rows)
    total = sum(d["pilots"] for d in per_dataset.values())
    fully = sum(d["pilots_fully_matching"] for d in per_dataset.values())
    return rows, {"by_dataset": per_dataset, "pilots": total, "pilots_fully_matching": fully, "field_checks": checks,
                  "field_checks_matched": matched, "fields_compared": [f[0] for f in PARITY_FIELDS],
                  "status": "PASS" if fully == total and all(d["go_outputs_present"] for d in per_dataset.values()) else "FAIL",
                  "mismatches": [r for r in rows if not r["match"]][:50]}


def official_decisions(fx_rows: list, go_outputs: dict | None) -> list:
    go = {g["record_id"]: g for g in (go_outputs or {}).get("official_pilots", [])}
    out = []
    for p in fx_rows:
        ev = R.evaluate(p, R.BASIS_CASH)
        g = go.get(p["id"])
        out.append({"id": p["id"], "decision": ev.decision, "reasons": ev.reason_codes, "go_decision": g["decision"] if g else None,
                    "matches_go": bool(g) and g["decision"] == ev.decision and g["reason_codes"] == ev.reason_codes})
    return out


# ---------------------------------------------------------------- 8. decision classes

def class_analysis(syn: list) -> list:
    out = []
    for d in ("PROCEED", "REVISE", "STOP"):
        g = [p for p in syn if p["decision"] == d]
        calc = [recompute(p) for p in g]
        row = {"stored_decision": d, "pilots": len(g)}
        for k in ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr"):
            row["avg_" + k] = _stats([p[k] for p in g])["mean"]
        row["avg_roi_pct"] = _stats([c["roi_pct"] for c in calc])["mean"]
        row["avg_payback_months"] = _stats([c["payback_months"] for c in calc])["mean"]
        row["avg_actual_adoption_pct"] = _stats([p["actual_adoption_pct"] for p in g])["mean"]
        row["avg_adoption_minus_minimum_pp"] = _stats([p["actual_adoption_pct"] - p["minimum_adoption_pct"] for p in g])["mean"]
        row["avg_actual_quality_pct"] = _stats([p["actual_quality_pct"] for p in g])["mean"]
        row["avg_quality_minus_minimum_pp"] = _stats([p["actual_quality_pct"] - p["minimum_quality_pct"] for p in g])["mean"]
        row["avg_overall_readiness_pct"] = _stats([c["overall_readiness_pct"] for c in calc])["mean"]
        row["share_quality_failed_pct"] = round(100 * sum(p["actual_quality_pct"] < p["minimum_quality_pct"] for p in g) / len(g), 1) if g else None
        out.append(row)
    return out


# ---------------------------------------------------------------- 9-12. adoption, quality, readiness, cash vs capacity

def adoption_analysis(syn: list, dv: dict) -> tuple[list, dict]:
    rows = []
    for p in syn:
        s = dv["stated"][P.pid(p)]
        a, mn, be = R.frac(p["actual_adoption_pct"]), R.frac(p["minimum_adoption_pct"]), s.breakeven_pct
        state = "Below economic break-even" if a < be else "Below target but above break-even" if a < mn else "Above target"
        expect = "STOP" if state == "Below economic break-even" else "REVISE or STOP (STOP only if another STOP rule fires)" if state.startswith("Below target") else "no adoption rule fires"
        consistent = (s.decision == R.STOP) if state == "Below economic break-even" else (s.decision in (R.REVISE, R.STOP)) if state.startswith("Below target") else True
        rows.append({"record_id": P.pid(p), "expected_adoption_pct": p["expected_adoption_pct"], "actual_adoption_pct": p["actual_adoption_pct"],
                     "minimum_adoption_pct": p["minimum_adoption_pct"], "breakeven_adoption_pct_stated": R.fmt(be),
                     "breakeven_adoption_pct_cash": R.fmt(dv["cash"][P.pid(p)].breakeven_pct),
                     "actual_minus_expected_pp": p["actual_adoption_pct"] - p["expected_adoption_pct"], "adoption_state": state,
                     "rule_expectation": expect, "recalculated_decision": s.decision, "stored_decision": p["decision"],
                     "rule_consistent": consistent})
    states = Counter(r["adoption_state"] for r in rows)
    summary = {"states": dict(sorted(states.items())), "rule_consistent": sum(r["rule_consistent"] for r in rows), "total": len(rows),
               "below_target_viable_and_revise": sum(r["adoption_state"].startswith("Below target") and r["recalculated_decision"] == "REVISE" for r in rows),
               "below_target_viable_but_stop_from_other_rule": sum(r["adoption_state"].startswith("Below target") and r["recalculated_decision"] == "STOP" for r in rows),
               "below_breakeven_and_stop": sum(r["adoption_state"] == "Below economic break-even" and r["recalculated_decision"] == "STOP" for r in rows)}
    return rows, summary


def quality_analysis(syn: list, dv: dict) -> tuple[list, dict]:
    rows = []
    for p in syn:
        s = dv["stated"][P.pid(p)]
        fail = p["actual_quality_pct"] < p["minimum_quality_pct"]
        rows.append({"record_id": P.pid(p), "expected_quality_pct": p["expected_quality_pct"], "actual_quality_pct": p["actual_quality_pct"],
                     "minimum_quality_pct": p["minimum_quality_pct"], "margin_pp": p["actual_quality_pct"] - p["minimum_quality_pct"],
                     "quality_result": "FAIL" if fail else "PASS", "recalculated_decision": s.decision, "stored_decision": p["decision"],
                     "fail_implies_stop_recalculated": (s.decision == R.STOP) if fail else None,
                     "fail_implies_stop_stored": (p["decision"] == "STOP") if fail else None})
    fails = [r for r in rows if r["quality_result"] == "FAIL"]
    return rows, {"quality_pass": len(rows) - len(fails), "quality_fail": len(fails),
                  "fails_stop_recalculated": sum(r["fail_implies_stop_recalculated"] for r in fails),
                  "fails_stop_stored": sum(r["fail_implies_stop_stored"] for r in fails),
                  "decision_consistency": "PASS" if all(r["fail_implies_stop_recalculated"] and r["fail_implies_stop_stored"] for r in fails) else "FAIL"}


def readiness_analysis(syn: list, dv: dict) -> tuple[list, dict]:
    rows = []
    for p in syn:
        s = dv["stated"][P.pid(p)]
        comps = {k: p[k] for k in R.READINESS_FIELDS}
        low_c = min(comps, key=comps.get)
        causes = "LOW_READINESS" in s.reason_codes
        rows.append({"record_id": P.pid(p), "data_readiness_pct": p["data_readiness_pct"], "technical_readiness_pct": p["technical_readiness_pct"],
                     "process_readiness_pct": p["process_readiness_pct"], "overall_readiness_pct_recalculated": R.fmt(s.readiness_pct),
                     "readiness_level": s.readiness_level, "weakest_component": low_c.replace("_pct", ""), "weakest_value": comps[low_c],
                     "components_below_50": sum(v < 50 for v in comps.values()),
                     "low_readiness_rule_fires": causes, "readiness_is_decisive": causes and s.decision == R.REVISE and s.primary_reason == "LOW_READINESS",
                     "recalculated_decision": s.decision, "stored_decision": p["decision"], "stored_reason": p["decision_reason"],
                     "flag": "READINESS_LABEL_INCONSISTENCY" if p["decision_reason"] == "Readiness too low" and not causes else "",
                     "interpretation": "Fix readiness before investing further (not a failed idea)." if causes else ""})
    levels = Counter(r["readiness_level"] for r in rows)
    inconsistent = [r for r in rows if r["flag"]]
    return rows, {"levels": dict(sorted(levels.items())), "rule_fires": sum(r["low_readiness_rule_fires"] for r in rows),
                  "label_inconsistencies": [{k: r[k] for k in ("record_id", "data_readiness_pct", "technical_readiness_pct", "process_readiness_pct",
                                                               "overall_readiness_pct_recalculated", "weakest_component", "weakest_value",
                                                               "stored_decision", "stored_reason", "recalculated_decision")} for r in inconsistent],
                  "readiness_decisive_revise": [r["record_id"] for r in rows if r["readiness_is_decisive"]],
                  "stored_readiness_too_low": [r["record_id"] for r in rows if r["stored_reason"] == "Readiness too low"],
                  "pilots_with_a_component_below_50": sum(r["components_below_50"] > 0 for r in rows),
                  "threshold": "overall (mean of 3 components) < 50 -> LOW (TEAM assumption, c23-rules-v1)"}


def cash_vs_capacity(syn: list, dv: dict) -> tuple[list, dict]:
    rows = []
    for p in syn:
        s, c = dv["stated"][P.pid(p)], dv["cash"][P.pid(p)]
        rows.append({"record_id": P.pid(p), "stored_decision": p["decision"], "decision_full_annual_benefit": s.decision,
                     "decision_recognised_cash_benefit": c.decision,
                     "benefit_type": p["benefit_type"], "canonical_type": R.canonical_benefit_type(p["benefit_type"]),
                     "annual_benefit_inr_stated": p["annual_benefit_inr"], "capacity_value_inr": p["capacity_value_inr"],
                     "realized_cash_saving_inr": p["realized_cash_saving_inr"], "cash_benefit_inr_used_by_engine": R.fmt(c.basis_benefit),
                     "uncashed_gap_inr": R.fmt(R.frac(p["annual_benefit_inr"]) - c.basis_benefit),
                     "stated_net_inr": R.fmt(s.on_basis.net_annual), "cash_net_inr": R.fmt(c.on_basis.net_annual),
                     "decision_changes_on_cash_basis": s.decision != c.decision,
                     "stored_differs_from_production": p["decision"] != c.decision})
    by = {}
    for t in sorted({r["benefit_type"] for r in rows}):
        g = [r for r in rows if r["benefit_type"] == t]
        by[t] = {"pilots": len(g), "counted_as_cash_by_engine": g[0]["canonical_type"] == "cashable",
                 "total_stated_benefit_inr": sum(r["annual_benefit_inr_stated"] for r in g),
                 "total_capacity_value_inr": sum(r["capacity_value_inr"] for r in g),
                 "total_realized_cash_saving_inr": sum(r["realized_cash_saving_inr"] for r in g),
                 "decisions_changed_on_cash_basis": sum(r["decision_changes_on_cash_basis"] for r in g)}
    return rows, {"by_benefit_type": by, "decisions_changed": sum(r["decision_changes_on_cash_basis"] for r in rows),
                  "stored_vs_production_differences": sum(r["stored_differs_from_production"] for r in rows),
                  "capacity_never_counted_as_cash": all(r["canonical_type"] == "cashable" or R.frac(r["cash_benefit_inr_used_by_engine"]) == r["realized_cash_saving_inr"] for r in rows)}


# ---------------------------------------------------------------- 13. overlap

def overlap_analysis(fx_rows: list, syn: list) -> tuple[list, dict]:
    out = []
    for name, rows in (("official_fixture", fx_rows), ("synthetic", syn)):
        for g in P.overlap_summary(rows, R.BASIS_STATED):
            out.append({"dataset": name, "overlap_group": g["overlap_group"], "pilots": g["pilots"], "pilot_ids": " ".join(g["ids"]),
                        "claimed_total_inr": R.fmt(g["claimed_total"]), "benefit_pool_inr": R.fmt(g["pool"]),
                        "recognised_if_unresolved_inr": R.fmt(g["recognised_unresolved"]),
                        "double_count_prevented_inr": R.fmt(g["double_count_if_summed"]), "primary_if_unresolved": g["primary"],
                        "double_count_warning": g["pilots"] > 1})
    sup = {p["id"]: p["annual_benefit_inr"] for p in fx_rows if p.get("overlap_group") == "support"}
    res = {"official_support_pair": sorted(sup), "claimed_total": sum(sup.values()),
           "unresolved": {k: R.fmt(v) for k, v in P.recognise(sup).items()},
           "REMOVE_ONE(primary=AI4)": {k: R.fmt(v) for k, v in P.recognise(sup, {"type": "REMOVE_ONE", "primary": "AI4"}).items()},
           "EXCLUDE_SECOND(primary=AI4)": {k: R.fmt(v) for k, v in P.recognise(sup, {"type": "EXCLUDE_SECOND", "primary": "AI4"}).items()},
           "SPLIT_BENEFIT(equal)": {k: R.fmt(v) for k, v in P.recognise(sup, {"type": "SPLIT_BENEFIT"}).items()},
           "CUSTOM_ALLOCATION(120000/60000)": {k: R.fmt(v) for k, v in P.recognise(sup, {"type": "CUSTOM_ALLOCATION", "allocations": {"AI4": 120000, "AI5": 60000}}).items()}}
    try:
        P.recognise(sup, {"type": "CUSTOM_ALLOCATION", "allocations": {"AI4": 180000, "AI5": 180000}})
        res["over_allocation_rejected"] = False
    except ValueError:
        res["over_allocation_rejected"] = True
    pool = max(sup.values())
    res["never_exceeds_pool"] = all(sum(R.frac(v) for v in res[k].values()) <= pool for k in res if isinstance(res[k], dict))
    res["status"] = "PASS" if res["never_exceeds_pool"] and res["over_allocation_rejected"] and sum(R.frac(v) for v in res["unresolved"].values()) == pool else "FAIL"
    res["synthetic_note"] = ("Every synthetic row has overlap_resolved=True but no resolution details (type, primary, shares). c23-rules-v1 "
                             "does not accept a bare flag: the pool rule applies and the optimizer funds at most one pilot per group.")
    return out, res


# ---------------------------------------------------------------- 14/15. portfolios

def portfolio_runs(syn: list, dv: dict, go_outputs: dict | None, fx_rows: list) -> dict:
    results, reasons = [], []
    for budget in BUDGETS:
        for label, basis, evals in (("production (cash basis, c23-rules-v1 decisions)", R.BASIS_CASH, dv["cash"]),
                                    ("label-consistent (stated basis + stated decisions)", R.BASIS_STATED, dv["stated"])):
            r = P.optimize(syn, evals, budget, basis)
            by_id = {P.pid(p): p for p in syn}
            stops = [i for i in r["selected"] if evals[i].decision == R.STOP]
            invalid = [i for i in r["selected"] if evals[i].decision != R.PROCEED and evals[i].decision != R.REVIEW]
            groups = [by_id[i]["overlap_group"] for i in r["selected"]]
            codes = Counter(e["code"] for e in r["excluded"])
            results.append({"dataset": "synthetic_120", "mode": label, "budget_inr": budget, "selected_pilots": " ".join(r["selected"]),
                            "selected_count": len(r["selected"]), "rejected_count": len(r["excluded"]),
                            "rejected_by_reason": "; ".join(f"{k}={v}" for k, v in sorted(codes.items())),
                            "total_setup_inr": R.fmt(r["total_setup"]), "remaining_budget_inr": R.fmt(budget - r["total_setup"]),
                            "annual_net_benefit_inr": R.fmt(r["net_annual"]),
                            "portfolio_payback_months": R.fmt(r["payback_months"]), "eligible_candidates": r["candidates"],
                            "within_budget": r["total_setup"] <= budget, "no_stop_selected": not stops, "no_invalid_pilot": not invalid,
                            "no_overlap_double_count": len(groups) == len(set(groups))})
            for i in r["selected"]:
                p, ev = by_id[i], evals[i]
                net = R.basis_benefit(p, basis) - R.frac(p["annual_running_cost_inr"])
                reasons.append({"budget_inr": budget, "mode": label, "pilot_id": i, "status": "SELECTED", "code": ev.decision,
                                "reason": f"{ev.decision}; recognised net INR {R.fmt(net)}/yr for setup INR {p['setup_cost_inr']} "
                                          f"(the only funded pilot in group '{p['overlap_group']}'); part of the highest-net set that fits the budget."})
            for e in r["excluded"]:
                reasons.append({"budget_inr": budget, "mode": label, "pilot_id": e["pilot_id"], "status": "NOT_SELECTED", "code": e["code"], "reason": e["reason"]})
    # determinism: shuffled input order must give the same portfolio
    import random
    rng = random.Random(42)
    det = []
    for budget in BUDGETS:
        base = P.optimize(syn, dv["cash"], budget)["selected"]
        same = True
        for _ in range(5):
            sh = syn[:]
            rng.shuffle(sh)
            same &= P.optimize(sh, dv["cash"], budget)["selected"] == base
        det.append({"budget_inr": budget, "selected": " ".join(base), "same_after_5_shuffles": same})
    # cross-check with the production Go optimizer (official fixture + first 20 synthetic pilots)
    go_checks = []
    for g in (go_outputs or {}).get("optimizer", []):
        rows = fx_rows if g["dataset"] == "official_fixture" else syn[:20]
        evals = {P.pid(p): R.evaluate(p, R.BASIS_CASH) for p in rows}
        r = P.optimize(rows, evals, g["budget_inr"], g["basis"])
        go_checks.append({"dataset": g["dataset"], "budget_inr": g["budget_inr"], "basis": g["basis"], "go_selected": " ".join(g["selected"]),
                          "python_selected": " ".join(r["selected"]), "go_net": g["net_annual_benefit_inr"], "python_net": R.fmt(r["net_annual"]),
                          "match": g["selected"] == r["selected"] and g["net_annual_benefit_inr"] == R.fmt(r["net_annual"])})
    full = (go_outputs or {}).get("optimizer_full_check", {})
    return {"results": results, "reasons": reasons, "determinism": det, "go_cross_check": go_checks,
            "go_full_set_note": full.get("error", ""),
            "status": "PASS" if all(r["within_budget"] and r["no_stop_selected"] and r["no_invalid_pilot"] and r["no_overlap_double_count"] for r in results)
            and all(d["same_after_5_shuffles"] for d in det) and go_checks and all(c["match"] for c in go_checks) else "FAIL"}


# ---------------------------------------------------------------- 16. stress tests

def _scale(field, factor):
    def f(p):
        p[field] = R.frac(p[field]) * Fraction(factor)
        if field == "annual_benefit_inr" and p.get("realized_cash_saving_inr") is not None:
            p["realized_cash_saving_inr"] = R.frac(p["realized_cash_saving_inr"]) * Fraction(factor)
    return f


def _shift(field, pp):
    def f(p):
        if p.get(field) is not None:            # unknown stays unknown (the official fixture has no actual_* values)
            p[field] = max(Fraction(0), R.frac(p[field]) + pp)
    return f


SCENARIOS = [
    ("baseline", None, 1), ("benefit -10%", _scale("annual_benefit_inr", "0.9"), 1), ("benefit -20%", _scale("annual_benefit_inr", "0.8"), 1),
    ("benefit -30%", _scale("annual_benefit_inr", "0.7"), 1), ("running cost +20%", _scale("annual_running_cost_inr", "1.2"), 1),
    ("running cost +50%", _scale("annual_running_cost_inr", "1.5"), 1), ("running cost +100%", _scale("annual_running_cost_inr", "2"), 1),
    ("adoption -10pp", _shift("actual_adoption_pct", -10), 1), ("adoption -20pp", _shift("actual_adoption_pct", -20), 1),
    ("quality -10pp", _shift("actual_quality_pct", -10), 1), ("budget -25%", None, Fraction(3, 4)),
]
STRESS_BUDGET = 600000


def stress_tests(syn: list, fx_rows: list) -> tuple[list, list, dict]:
    """Scenarios run on deep copies; the source rows are never modified. Benefit shocks scale realized cash saving too."""
    base_cash = evaluate_all(syn, R.BASIS_CASH)
    base_stated = evaluate_all(syn, R.BASIS_STATED)
    base_port = P.optimize(syn, base_cash, STRESS_BUDGET)
    out, changes, checks = [], [], []
    for name, fn, bf in SCENARIOS:
        rows = copy.deepcopy(syn)
        ref = copy.deepcopy(fx_rows[0])
        if fn:
            for p in rows:
                fn(p)
            fn(ref)
        cash, stated = evaluate_all(rows, R.BASIS_CASH), evaluate_all(rows, R.BASIS_STATED)
        budget = Fraction(STRESS_BUDGET) * bf
        port = P.optimize(rows, cash, budget)
        fins = [R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"]) for p in rows]
        ref_fin = R.finance(ref["setup_cost_inr"], ref["annual_running_cost_inr"], ref["annual_benefit_inr"])
        never_better = all(R.SEVERITY[cash[i].decision] >= R.SEVERITY[base_cash[i].decision] and
                           R.SEVERITY[stated[i].decision] >= R.SEVERITY[base_stated[i].decision] for i in cash)
        invariants = all((f.payback_months is None) == (f.net_annual <= 0) for f in fins) and port["total_setup"] <= budget and \
            not any(cash[i].decision == R.STOP for i in port["selected"]) and port["net_annual"] <= base_port["net_annual"]
        checks.append(never_better and invariants)
        dc = Counter(e.decision for e in cash.values())
        ds = Counter(e.decision for e in stated.values())
        out.append({"scenario": name, "budget_inr": R.fmt(budget),
                    "reference_AI1_running_inr": R.fmt(R.frac(ref["annual_running_cost_inr"])), "reference_AI1_benefit_inr": R.fmt(R.frac(ref["annual_benefit_inr"])),
                    "reference_AI1_net_inr": R.fmt(ref_fin.net_annual), "reference_AI1_payback_months": R.fmt(ref_fin.payback_months) or "NEVER_PAYS_BACK",
                    "mean_net_annual_benefit_inr": _stats([f.net_annual for f in fins])["mean"], "mean_roi_pct": _stats([f.roi_pct for f in fins])["mean"],
                    "mean_payback_months": _stats([f.payback_months for f in fins])["mean"], "never_pays_back": sum(f.payback_months is None for f in fins),
                    "mean_breakeven_adoption_pct": _stats([R.breakeven_adoption_pct(p["annual_running_cost_inr"], p["annual_benefit_inr"]) for p in rows])["mean"],
                    "production_PROCEED": dc["PROCEED"], "production_REVISE": dc["REVISE"], "production_STOP": dc["STOP"],
                    "stated_PROCEED": ds["PROCEED"], "stated_REVISE": ds["REVISE"], "stated_STOP": ds["STOP"],
                    "decisions_changed_vs_baseline": sum(cash[i].decision != base_cash[i].decision for i in cash),
                    "portfolio_selected": " ".join(port["selected"]), "portfolio_setup_inr": R.fmt(port["total_setup"]),
                    "portfolio_net_inr": R.fmt(port["net_annual"]), "no_decision_improved": never_better, "invariants_hold": invariants})
        for i in cash:
            if cash[i].decision != base_cash[i].decision:
                changes.append({"scenario": name, "record_id": i, "baseline_decision": base_cash[i].decision, "scenario_decision": cash[i].decision,
                                "scenario_reasons": ";".join(cash[i].reason_codes)})
    # the documented reference scenario: running cost doubled on AI1
    ai1 = fx_rows[0]
    dbl = R.finance(ai1["setup_cost_inr"], 120000, ai1["annual_benefit_inr"])
    reference = {"case": "AI1 running cost 60000 -> 120000", "net_annual_benefit_inr": R.fmt(dbl.net_annual),
                 "monthly_net_benefit_inr": R.fmt(dbl.monthly_net), "payback_months": R.fmt(dbl.payback_months),
                 "status": "PASS" if (dbl.net_annual, dbl.monthly_net, dbl.payback_months) == (60000, 5000, 40) else "FAIL"}
    return out, changes, {"scenarios": len(out), "all_checks_pass": all(checks) and reference["status"] == "PASS", "reference_case": reference}


# ---------------------------------------------------------------- 17. breakpoints

def breakpoints(syn: list, dv: dict) -> list:
    out = []
    for p in syn:
        i = P.pid(p)
        run, ben, ad = R.frac(p["annual_running_cost_inr"]), R.frac(p["annual_benefit_inr"]), R.frac(p["actual_adoption_pct"])
        cash = dv["cash"][i].basis_benefit
        s = dv["stated"][i]
        stop_run = ben * ad / 100                   # adoption falls below break-even when running > benefit x adoption
        min_ben = run * 100 / ad if ad > 0 else None
        net = ben - run
        sentence = (f"{i} becomes financially non-viable (net <= 0) if annual running cost reaches INR {R.fmt(ben)}; "
                    f"at actual adoption {p['actual_adoption_pct']}% it becomes a STOP (adoption below break-even) once running cost exceeds INR {R.fmt(stop_run)}.")
        out.append({"record_id": i, "stated_decision": s.decision, "production_decision": dv["cash"][i].decision,
                    "max_running_cost_before_net_zero_inr": R.fmt(ben), "max_running_cost_before_breakeven_stop_inr": R.fmt(stop_run),
                    "running_cost_headroom_pct": R.fmt(net / run * 100) if run else None,
                    "min_benefit_for_positive_net_inr": R.fmt(run), "min_benefit_at_actual_adoption_inr": R.fmt(min_ben),
                    "benefit_drop_tolerated_pct": R.fmt(net / ben * 100) if ben else None,
                    "breakeven_adoption_pct_stated": R.fmt(s.breakeven_pct), "breakeven_adoption_pct_cash": R.fmt(dv["cash"][i].breakeven_pct),
                    "cash_basis_max_running_cost_inr": R.fmt(cash), "adoption_threshold_pct": p["minimum_adoption_pct"],
                    "adoption_margin_vs_minimum_pp": p["actual_adoption_pct"] - p["minimum_adoption_pct"],
                    "adoption_margin_vs_breakeven_pp": R.fmt(ad - s.breakeven_pct), "quality_threshold_pct": p["minimum_quality_pct"],
                    "quality_margin_pp": p["actual_quality_pct"] - p["minimum_quality_pct"],
                    "readiness_margin_vs_50_pp": R.fmt(s.readiness_pct - R.LOW_READINESS_BELOW_PCT), "business_statement": sentence})
    return out


# ---------------------------------------------------------------- 18. evidence

def evidence_analysis(syn: list, dv: dict) -> tuple[list, dict]:
    rows = []
    for ev in ("ASSUMPTION", "MIXED", "OBSERVED"):
        g = [p for p in syn if p["evidence_status"] == ev]
        stored = Counter(p["decision"] for p in g)
        prod = Counter(dv["cash"][P.pid(p)].decision for p in g)
        rows.append({"evidence_status": ev, "pilots": len(g), "share_pct": round(100 * len(g) / len(syn), 1),
                     "stored_PROCEED": stored["PROCEED"], "stored_REVISE": stored["REVISE"], "stored_STOP": stored["STOP"],
                     "production_PROCEED": prod["PROCEED"], "production_REVISE": prod["REVISE"], "production_STOP": prod["STOP"],
                     "meaning": {"ASSUMPTION": "decision rests on assumed inputs; validate before scaling",
                                 "MIXED": "partly measured, partly assumed", "OBSERVED": "inputs reported as measured"}[ev]})
    proceed = [p for p in syn if p["decision"] == "PROCEED"]
    flags = []
    for p in syn:
        measured = p.get("actual_adoption_pct") is not None and p.get("actual_quality_pct") is not None
        bad = p["evidence_status"] == "ASSUMPTION" and measured
        flags.append({"record_id": P.pid(p), "evidence_status": p["evidence_status"], "actual_adoption_pct": p["actual_adoption_pct"],
                      "actual_quality_pct": p["actual_quality_pct"], "stored_decision": p["decision"],
                      "production_decision": dv["cash"][P.pid(p)].decision, "flag": "EVIDENCE_LABEL_INCONSISTENCY" if bad else "",
                      "explanation": ("Labelled ASSUMPTION but carries actual (observed) adoption and quality values; the row was not changed."
                                      if bad else "")})
    return rows, {"counts": {r["evidence_status"]: r["pilots"] for r in rows}, "row_flags": flags,
                  "evidence_label_inconsistencies": sum(bool(f["flag"]) for f in flags),
                  "stored_proceed_on_assumption_only": sum(p["evidence_status"] == "ASSUMPTION" for p in proceed),
                  "stored_proceed_total": len(proceed),
                  "inconsistency": ("All 120 rows carry actual_adoption_pct and actual_quality_pct, including the 43 rows labelled ASSUMPTION. "
                                    "Under the C23 null policy an unmeasured actual is null, which the engine turns into REVIEW "
                                    "(INSUFFICIENT_EVIDENCE). The labels were reported, not changed."),
                  "not_a_confidence_score": True}
