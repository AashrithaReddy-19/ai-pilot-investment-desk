"""Tests for the original-vs-synthetic validation pipeline (synthetic/). Expected values come from formulas or the
fixture itself, never from the stored derived columns being tested."""
import copy, json, random
from fractions import Fraction

import pytest

from synthetic import analysis as A, data as D, ml as M, portfolio as P, rules as R


@pytest.fixture(scope="module")
def fx():
    return D.load_fixture()


@pytest.fixture(scope="module")
def syn():
    rows, _, problems = D.load_synthetic_csv()
    assert problems == []
    return rows


@pytest.fixture(scope="module")
def dv(syn):
    return A.decision_validation(syn, None)


def pilot(**kw):
    base = {"id": "T1", "setup_cost_inr": 200000, "annual_running_cost_inr": 60000, "annual_benefit_inr": 180000,
            "benefit_type": "cashable assumption", "overlap_group": "g", "actual_adoption_pct": 70, "minimum_adoption_pct": 60,
            "actual_quality_pct": 90, "minimum_quality_pct": 80, "data_readiness_pct": 70, "technical_readiness_pct": 70,
            "process_readiness_pct": 70, "realized_cash_saving_inr": 0}
    base.update(kw)
    return base


# ---------------------------------------------------------------- official reference + formulas

def test_official_payback_is_20_months_from_formula(fx):
    ai1 = fx["records"]["pilots"][0]
    fin = R.finance(ai1["setup_cost_inr"], ai1["annual_running_cost_inr"], ai1["annual_benefit_inr"])
    assert fin.net_annual == 120000 and fin.monthly_net == 10000
    assert fin.payback_months == fx["records"]["expected_first_pilot_payback_months"] == 20
    assert A.reference_check(fx)["status"] == "PASS"


def test_net_annual_benefit_is_benefit_minus_running_for_every_row(syn):
    for p in syn:
        assert R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"]).net_annual == \
            p["annual_benefit_inr"] - p["annual_running_cost_inr"]


def test_roi_and_breakeven_formulas():
    fin = R.finance(200000, 60000, 180000)
    assert fin.roi_pct == 60
    assert R.breakeven_adoption_pct(60000, 180000) == Fraction(100, 3)
    assert R.breakeven_adoption_pct(60000, 0) is None                   # undefined, not zero
    assert R.finance(0, 60000, 180000).roi_pct is None and R.finance(0, 60000, 180000).payback_status == "IMMEDIATE"


def test_payback_exists_only_when_net_positive(syn):
    for p in syn + [pilot(annual_running_cost_inr=180000), pilot(annual_running_cost_inr=200000)]:
        fin = R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"])
        assert (fin.payback_months is not None) == (fin.net_annual > 0)


def test_readiness_mean_and_documented_boundaries():
    assert R.readiness(pilot(data_readiness_pct=40, technical_readiness_pct=50, process_readiness_pct=60)) == (50, "medium")
    assert R.readiness(pilot(data_readiness_pct=49, technical_readiness_pct=50, process_readiness_pct=50))[1] == "low"
    assert R.readiness(pilot(data_readiness_pct=75, technical_readiness_pct=75, process_readiness_pct=75))[1] == "high"


def test_fmt_rounds_like_go_formatrat():
    assert R.fmt(Fraction(1, 200)) == "0.01" and R.fmt(Fraction(-1, 200)) == "-0.01"
    assert R.fmt(Fraction(20)) == "20" and R.fmt(Fraction(100, 3)) == "33.33" and R.fmt(None) is None


# ---------------------------------------------------------------- decisions

def test_proceed_when_every_gate_passes():
    ev = R.evaluate(pilot())
    assert ev.decision == R.PROCEED and ev.primary_reason == R.ALL_PASSED


def test_quality_failure_is_stop_despite_good_financials():
    ev = R.evaluate(pilot(actual_quality_pct=79))
    assert ev.decision == R.STOP and ev.primary_reason == "QUALITY_THRESHOLD_FAILED"


def test_adoption_below_breakeven_is_stop_and_below_target_is_revise():
    assert R.evaluate(pilot(actual_adoption_pct=20)).decision == R.STOP           # break-even 33.33%
    ev = R.evaluate(pilot(actual_adoption_pct=50))                                # above break-even, below 60
    assert ev.decision == R.REVISE and ev.reason_codes == ["LOW_ADOPTION"]
    assert R.evaluate(pilot(actual_adoption_pct=60)).decision == R.PROCEED       # equal to minimum passes


def test_low_readiness_is_revise_not_stop():
    ev = R.evaluate(pilot(data_readiness_pct=40, technical_readiness_pct=40, process_readiness_pct=40))
    assert ev.decision == R.REVISE and ev.primary_reason == "LOW_READINESS"


def test_never_pays_back_is_stop():
    ev = R.evaluate(pilot(annual_running_cost_inr=180000))
    assert ev.decision == R.STOP and "NEVER_PAYS_BACK" in ev.reason_codes and ev.on_basis.payback_months is None


def test_null_means_unknown_not_zero():
    ev = R.evaluate(pilot(annual_running_cost_inr=None))
    assert ev.decision == R.REVISE and ev.primary_reason == "MISSING_RUNNING_COST" and ev.on_basis.payback_status == "NOT_CALCULABLE"
    ev = R.evaluate(pilot(actual_adoption_pct=None))
    assert "INSUFFICIENT_EVIDENCE" in ev.reason_codes and "LOW_ADOPTION" not in ev.reason_codes


def test_time_freed_is_not_cash_on_the_production_basis():
    p = pilot(benefit_type="time freed", realized_cash_saving_inr=50000)
    assert R.evaluate(p, R.BASIS_STATED).decision == R.PROCEED
    cash = R.evaluate(p, R.BASIS_CASH)
    assert cash.basis_benefit == 50000 and cash.decision == R.STOP and cash.primary_reason == "NEVER_PAYS_BACK"
    assert "TIME_NOT_CASH" in R.evaluate(pilot(benefit_type="time freed", realized_cash_saving_inr=None)).reason_codes


def test_stored_decisions_reproduced_except_the_documented_readiness_labels(dv):
    s = dv["summary"]["stored_vs_rules_stated_basis"]
    mism = {m["record_id"] for m in dv["mismatches"] if m["comparison"] == "stored_vs_rules_stated_basis"}
    # Known data inconsistency (reported, not 'fixed'): these labels use a readiness rule that is not documented.
    assert mism == {"SYN038", "SYN096"} and s["matching"] == 118
    for m in dv["mismatches"]:
        assert m["reason_for_difference"]


def test_python_replica_matches_go_engine_outputs(syn):
    path = D.ROOT / "reports" / "synthetic" / "go_engine_outputs.json"
    go = json.loads(path.read_text(encoding="utf-8"))
    assert go["rules_version"] == R.RULES_VERSION
    for g in go["synthetic_pilots"]:
        p = next(r for r in syn if r["record_id"] == g["record_id"])
        ev = R.evaluate(p, R.BASIS_CASH)
        assert (ev.decision, ev.reason_codes, R.fmt(ev.breakeven_pct)) == (g["decision"], g["reason_codes"], g["cash_breakeven_adoption_pct"]), g["record_id"]
    for g, o in zip(go["official_pilots"], A.official_decisions(D.official_with_extension(), go)):
        assert o["matches_go"], g["record_id"]


# ---------------------------------------------------------------- quality / adoption analyses

def test_every_quality_failure_is_stop(syn, dv):
    _, q = A.quality_analysis(syn, dv)
    assert q["quality_fail"] > 0 and q["decision_consistency"] == "PASS"


def test_adoption_states_follow_the_rules(syn, dv):
    rows, s = A.adoption_analysis(syn, dv)
    assert s["rule_consistent"] == s["total"] == 120
    assert all(r["recalculated_decision"] == "STOP" for r in rows if r["adoption_state"] == "Below economic break-even")


# ---------------------------------------------------------------- overlap

def test_unresolved_overlap_counts_only_the_pool():
    rec = P.recognise({"AI4": 180000, "AI5": 180000})
    assert rec == {"AI4": 180000, "AI5": 0}


@pytest.mark.parametrize("res,expected", [
    ({"type": "REMOVE_ONE", "primary": "AI5"}, {"AI5": 180000}),
    ({"type": "EXCLUDE_SECOND", "primary": "AI4"}, {"AI4": 180000, "AI5": 0}),
    ({"type": "SPLIT_BENEFIT"}, {"AI4": 90000, "AI5": 90000}),
    ({"type": "CUSTOM_ALLOCATION", "allocations": {"AI4": 120000, "AI5": 60000}}, {"AI4": 120000, "AI5": 60000}),
])
def test_overlap_resolutions(res, expected):
    out = P.recognise({"AI4": 180000, "AI5": 180000}, res)
    assert out == expected and sum(out.values()) <= 180000


@pytest.mark.parametrize("res", [
    {"type": "CUSTOM_ALLOCATION", "allocations": {"AI4": 180000, "AI5": 180000}},
    {"type": "SPLIT_BENEFIT", "shares_pct": {"AI4": 70, "AI5": 70}},
    {"type": "REMOVE_ONE", "primary": "AI9"},
    {"type": "MAGIC"},
])
def test_invalid_overlap_resolutions_are_rejected(res):
    with pytest.raises(ValueError):
        P.recognise({"AI4": 180000, "AI5": 180000}, res)


def test_same_group_raises_double_count_warning(fx, syn):
    rows, res = A.overlap_analysis(D.fixture_pilots(fx), syn)
    support = next(r for r in rows if r["dataset"] == "official_fixture" and r["overlap_group"] == "support")
    assert support["double_count_warning"] and support["benefit_pool_inr"] == "180000" and support["claimed_total_inr"] == "360000"
    assert res["status"] == "PASS"


# ---------------------------------------------------------------- portfolio

@pytest.mark.parametrize("budget", A.BUDGETS)
def test_portfolio_respects_budget_overlap_and_stop(syn, dv, budget):
    for basis, evals in ((R.BASIS_CASH, dv["cash"]), (R.BASIS_STATED, dv["stated"])):
        r = P.optimize(syn, evals, budget, basis)
        groups = [next(p["overlap_group"] for p in syn if p["record_id"] == i) for i in r["selected"]]
        assert r["total_setup"] <= budget
        assert len(groups) == len(set(groups))                                 # no unresolved overlap in the portfolio
        assert all(evals[i].decision not in (R.STOP, R.REVISE) for i in r["selected"])


def test_optimizer_matches_brute_force(syn, dv):
    rng = random.Random(7)
    for _ in range(25):
        sub = rng.sample(syn, 12)
        budget = rng.choice([200000, 350000, 500000, 800000])
        for basis in (R.BASIS_CASH, R.BASIS_STATED):
            assert P.optimize(sub, dv["cash"], budget, basis)["selected"] == P.brute_force(sub, dv["cash"], budget, basis)


def test_portfolio_is_independent_of_row_order(syn, dv):
    base = P.optimize(syn, dv["cash"], 600000)
    rng = random.Random(1)
    for _ in range(10):
        sh = syn[:]
        rng.shuffle(sh)
        assert P.optimize(sh, dv["cash"], 600000) == base


# ---------------------------------------------------------------- stress

def test_stress_scenarios_hold_invariants_and_never_improve_decisions(fx, syn):
    before = copy.deepcopy(syn)
    rows, _, s = A.stress_tests(syn, D.fixture_pilots(fx))
    assert syn == before                                                         # scenarios never touch the source rows
    assert s["all_checks_pass"] and all(r["no_decision_improved"] and r["invariants_hold"] for r in rows)
    assert s["reference_case"] == {"case": "AI1 running cost 60000 -> 120000", "net_annual_benefit_inr": "60000",
                                   "monthly_net_benefit_inr": "5000", "payback_months": "40", "status": "PASS"}
    names = [r["scenario"] for r in rows]
    for want in ("benefit -30%", "running cost +100%", "adoption -20pp", "quality -10pp", "budget -25%"):
        assert want in names


# ---------------------------------------------------------------- dataset

def test_synthetic_dataset_is_complete_and_labelled(syn):
    audit = D.audit_all()["synthetic_csv"]
    assert audit["rows"] == 120 and audit["missing_values"] == 0 and audit["null_values"] == 0 and audit["invalid_rows"] == 0
    assert audit["duplicate_ids"] == [] and audit["nan_values"] == 0 and audit["infinity_values"] == 0
    assert all(p["synthetic"] is True and p["source_type"] == "SYNTHETIC_COMPARISON" for p in syn)


def test_all_percentages_are_within_0_and_100(syn):
    for p in syn:
        for c in D.PCT_COLS:
            assert 0 <= p[c] <= 100, (p["record_id"], c)


def test_csv_and_json_are_identical(syn):
    assert D.csv_json_consistency(syn, D.load_synthetic_json())["consistent"]


def test_official_fixture_is_the_protected_copy():
    assert D.sha256(D.FIXTURE_PATH) == D.sha256(D.PROTECTED_FIXTURE)


def test_derived_columns_match_recalculation(syn):
    _, s = A.derived_validation(syn)
    assert s["rows_all_metrics_match"] == 120 and s["max_abs_difference"] <= 0.005


def test_audit_reports_invalid_values_instead_of_repairing(tmp_path):
    src = D.SYNTH_CSV.read_text(encoding="utf-8").splitlines()
    head = src[0].split(",")
    row = src[1].split(",")
    row[head.index("setup_cost_inr")] = "NaN"
    row[head.index("actual_quality_pct")] = "120"
    row[head.index("annual_running_cost_inr")] = "-5"
    bad = tmp_path / "bad.csv"
    bad.write_text("\n".join([src[0], ",".join(row)]) + "\n", encoding="utf-8")
    rows, _, problems = D.load_synthetic_csv(bad)
    assert rows[0]["setup_cost_inr"] is None and {p["problem"] for p in problems} == {"nan"}
    kinds = {v["problem"] for v in D.validity_problems(rows)}
    assert kinds == {"pct_out_of_range", "negative_cost"}


# ---------------------------------------------------------------- end to end

def test_pipeline_end_to_end_writes_every_report_and_is_ready(tmp_path, capsys):
    pytest.importorskip("sklearn")
    from synthetic import run
    before = {p: D.sha256(p) for p in (D.FIXTURE_PATH, D.SYNTH_CSV, D.SYNTH_JSON)}
    try:
        assert run.main(["--no-tests", "--out-dir", str(tmp_path)]) == 0
    finally:
        run.OUT = D.ROOT / "reports" / "synthetic"
    assert {p: D.sha256(p) for p in before} == before                          # sources are only read
    for name in ("data_validation_report.json", "original_vs_synthetic_comparison.csv", "derived_metric_validation.csv",
                 "decision_rule_validation.csv", "decision_mismatches.csv", "decision_class_analysis.csv", "adoption_analysis.csv",
                 "quality_analysis.csv", "readiness_analysis.csv", "cash_vs_capacity_analysis.csv", "overlap_analysis.csv",
                 "portfolio_results.csv", "stress_test_results.csv", "breakpoint_analysis.csv", "evidence_analysis.csv",
                 "model_comparison.csv", "final_analysis_report.md"):
        assert (tmp_path / name).stat().st_size > 0, name
    report = (tmp_path / "final_analysis_report.md").read_text(encoding="utf-8")
    for section in range(1, 20):
        assert f"\n## {section}. " in report
    assert "SYNTHETIC" in report and "FINAL STATUS\nREADY" in capsys.readouterr().out


# ---------------------------------------------------------------- optional ML

def test_ml_features_exclude_every_leaking_column():
    assert M.leakage_check()["status"] == "PASS"
    for col in ("actual_adoption_pct", "actual_quality_pct", "decision", "decision_reason"):
        assert col not in M.FEATURES
    assert M.leakage_check(M.FEATURES + ("actual_adoption_pct",))["status"] == "FAIL"


def test_ml_is_advisory_and_never_changes_decisions(syn, dv):
    pytest.importorskip("sklearn")
    decisions = {k: e.decision for k, e in dv["cash"].items()}
    out = M.run(syn, decisions)
    assert out["status"] == "RUN" and out["split"] == {"train": 96, "test": 24, "stratified": True, "random_state": 42}
    assert [m["model"] for m in out["models"]][0].startswith("Dummy")
    assert all(a["deterministic_decision"] == decisions[a["record_id"]] for a in out["advisory"])
    assert all(0 <= a["adoption_success_probability"] <= 1 for a in out["advisory"])
    assert set(out["interesting_combinations"]) == {"PROCEED + HIGH", "PROCEED + MEDIUM", "REVISE + HIGH", "STOP + LOW"}


# ---------------------------------------------------------------- files, parity, flags, charts

def test_csv_json_xlsx_agree_cell_for_cell(syn):
    if not D.SYNTH_XLSX.exists():
        pytest.skip("xlsx not present")
    res = D.three_way_consistency(syn, D.load_synthetic_json(), D.sheet_records(D.read_xlsx()["Synthetic_Comparison"]))
    assert {k: v["status"] for k, v in res["summary"].items()} == {"csv_vs_json": "MATCH", "csv_vs_xlsx": "MATCH", "json_vs_xlsx": "MATCH"}
    assert all(v["common_columns"] == 34 for v in res["summary"].values())


def test_three_way_consistency_reports_a_changed_cell(syn):
    js = D.load_synthetic_json()
    xl = copy.deepcopy(js["records"])
    xl[0]["annual_benefit_inr"] += 1
    res = D.three_way_consistency(syn, js, xl)
    assert res["summary"]["csv_vs_xlsx"]["status"] == "MISMATCH" and res["summary"]["csv_vs_json"]["status"] == "MATCH"
    d = res["differences"][0]
    assert (d["record_id"], d["column"], d["excel_value"] - d["csv_value"]) == ("SYN001", "annual_benefit_inr", 1)


def test_workbook_sheets_agree_with_fixture_and_data(fx, syn):
    if not D.SYNTH_XLSX.exists():
        pytest.skip("xlsx not present")
    wb = D.workbook_checks(syn, fx, {"Synthetic missing values": 0})
    assert wb["original_fixture_sheet"]["status"] == "MATCH" and wb["status"] == "PASS"
    assert all(c["match"] for c in wb["summary_sheet_claims"])


def test_python_vs_go_parity_on_every_field(syn):
    go = json.loads((D.ROOT / "reports" / "synthetic" / "go_engine_outputs.json").read_text(encoding="utf-8"))
    rows, s = A.engine_parity(syn, D.official_with_extension(), go)
    assert s["status"] == "PASS" and s["pilots_fully_matching"] == s["pilots"] == 125, s["mismatches"][:3]
    assert {"decision", "reason_codes", "stated_net_annual_benefit_inr", "stated_payback_months", "cash_payback_months"} <= set(s["fields_compared"])


def test_parity_detects_a_disagreeing_engine(syn):
    go = json.loads((D.ROOT / "reports" / "synthetic" / "go_engine_outputs.json").read_text(encoding="utf-8"))
    go["synthetic_pilots"][0]["stated_payback_months"] = "1"
    _, s = A.engine_parity(syn, D.official_with_extension(), go)
    assert s["status"] == "FAIL" and s["mismatches"][0]["field"] == "stated_payback_months"


def test_inconsistent_labels_are_flagged_not_changed(syn, dv):
    before = copy.deepcopy(syn)
    _, rd = A.readiness_analysis(syn, dv)
    assert [r["record_id"] for r in rd["label_inconsistencies"]] == ["SYN038", "SYN096"]
    _, ev = A.evidence_analysis(syn, dv)
    flagged = [f for f in ev["row_flags"] if f["flag"] == "EVIDENCE_LABEL_INCONSISTENCY"]
    assert len(flagged) == sum(p["evidence_status"] == "ASSUMPTION" for p in syn) and all(f["evidence_status"] == "ASSUMPTION" for f in flagged)
    assert syn == before


def test_cash_vs_capacity_shows_three_decisions_per_pilot(syn, dv):
    rows, s = A.cash_vs_capacity(syn, dv)
    assert {"stored_decision", "decision_full_annual_benefit", "decision_recognised_cash_benefit"} <= set(rows[0])
    assert s["decisions_changed"] == sum(r["decision_full_annual_benefit"] != r["decision_recognised_cash_benefit"] for r in rows) > 0
    assert all(r["decision_full_annual_benefit"] == r["decision_recognised_cash_benefit"] for r in rows if r["canonical_type"] == "cashable")


@pytest.mark.parametrize("budget", A.BUDGETS)
def test_portfolio_reports_remaining_budget_and_reasons(syn, dv, budget, fx):
    port = A.portfolio_runs(syn, dv, None, D.official_with_extension())
    row = next(r for r in port["results"] if r["budget_inr"] == budget and r["mode"].startswith("production"))
    assert R.frac(row["total_setup_inr"]) + R.frac(row["remaining_budget_inr"]) == budget
    assert row["selected_count"] + row["rejected_count"] == 120 and row["no_invalid_pilot"]
    reasons = [x for x in port["reasons"] if x["budget_inr"] == budget and x["mode"] == row["mode"]]
    assert len(reasons) == 120 and all(x["reason"] for x in reasons)


def test_charts_are_generated_and_validated(tmp_path, fx, syn, dv):
    pytest.importorskip("matplotlib")
    from synthetic import charts as C
    port = A.portfolio_runs(syn, dv, None, D.official_with_extension())
    adv = [{"record_id": p["record_id"], "deterministic_decision": dv["cash"][p["record_id"]].decision,
            "adoption_success_probability": 0.9, "adoption_risk_band": "LOW"} for p in syn]
    res = C.generate(tmp_path, {"fx_rows": D.fixture_pilots(fx), "syn": syn, "dv": dv, "portfolio_results": port["results"], "advisory": adv})
    assert len(res) == 10 and all(r["status"] == "PASS" for r in res), [r for r in res if r["status"] != "PASS"]
    assert (tmp_path / "README.md").read_text(encoding="utf-8").count("**Limitation:**") == 10
    stress = C.stress(D.fixture_pilots(fx), tmp_path / "s.png")
    assert dict((r[0], r[1]) for r in stress["rows"])["Running +100%"] == 40.0       # the 40-month changed-input case


def test_chart_validation_rejects_non_finite_values(tmp_path):
    pytest.importorskip("matplotlib")
    from synthetic import charts as C
    import matplotlib.pyplot as plt
    fig, ax = plt.subplots()
    ax.plot([1, 2], [1, 2])
    p = tmp_path / "x.png"
    fig.savefig(p)
    plt.close(fig)
    assert C.validate(p, {"values": [1.0, float("nan")], "labelled": True})["status"] == "FAIL"
    assert C.validate(p, {"values": [1.0], "labelled": False})["status"] == "FAIL"
    assert C.validate(p, {"values": [1.0], "labelled": True})["status"] == "PASS"
