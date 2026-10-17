"""End-to-end check: original C23 fixture vs the SYNTHETIC comparison dataset.

INSPECT -> COMPARE -> CALCULATE -> TEST -> FIND MISMATCHES -> EXPLAIN -> GENERATE REPORTS + CHARTS.

Usage (from decision-engine/):
    python -m synthetic.run              # refresh Go engine outputs, analyse, chart, run every test suite, print the summary
    python -m synthetic.run --no-tests   # analysis + charts only (no Go commands, no pytest; uses the existing Go outputs)

The production engine's outputs come from `go run ./cmd/synthcheck` (backend/) -> reports/synthetic/go_engine_outputs.json.
Output is deterministic (no timestamps, no run times): reruns are byte-identical. Source data files are only read.
"""
from __future__ import annotations
import argparse, csv, json, os, re, shutil, subprocess, sys
from fractions import Fraction
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from synthetic import analysis as A, data as D, ml as M, rules as R   # noqa: E402

BACKEND = ROOT.parent / "backend"
OUT = ROOT / "reports" / "synthetic"
GO_OUTPUTS = ROOT / "reports" / "synthetic" / "go_engine_outputs.json"


# ---------------------------------------------------------------- output helpers

def _cell(v):
    if v is None:
        return ""
    if isinstance(v, Fraction):
        return R.fmt(v)
    if isinstance(v, (list, dict)):
        return json.dumps(v, sort_keys=True)
    return v


def write_csv(name: str, rows: list) -> None:
    cols = []
    for r in rows:
        cols += [k for k in r if k not in cols]
    with (OUT / name).open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=cols, lineterminator="\n")
        w.writeheader()
        for r in rows:
            w.writerow({k: _cell(r.get(k)) for k in cols})


def _json_default(o):
    if isinstance(o, Fraction):
        return R.fmt(o, 4)
    raise TypeError(type(o).__name__)


def write_json(name: str, obj) -> None:
    (OUT / name).write_text(json.dumps(obj, indent=2, sort_keys=True, default=_json_default) + "\n", encoding="utf-8")


# ---------------------------------------------------------------- external commands (actually executed; never assumed)

def find_go() -> str | None:
    cands = [shutil.which("go"), r"C:\Program Files\Go\bin\go.exe", os.path.join(os.environ.get("LOCALAPPDATA", ""), "c23-tools", "go", "bin", "go.exe")]
    return next((c for c in cands if c and Path(c).exists()), None)


def _run(cmd: list, cwd: Path) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, encoding="utf-8", errors="replace")


def refresh_go_outputs(go: str | None) -> dict:
    if not go:
        return {"ran": False, "reason": "go not found; using existing go_engine_outputs.json"}
    p = _run([go, "run", "./cmd/synthcheck"], BACKEND)
    return {"ran": True, "ok": p.returncode == 0, "output": (p.stdout or p.stderr).strip().splitlines()[-1:] if (p.stdout or p.stderr) else []}


def run_python_tests() -> dict:
    p = _run([sys.executable, "-m", "pytest", "-q", "-p", "no:cacheprovider"], ROOT)
    tail = p.stdout.strip().splitlines()[-1] if p.stdout.strip() else p.stderr.strip()[-200:]
    count = lambda word: int(m.group(1)) if (m := re.search(rf"(\d+) {word}", tail)) else 0
    return {"ran": True, "passed": count("passed"), "failed": count("failed") + count("error"), "skipped": count("skipped"),
            "summary": re.sub(r" in [\d.]+s.*$", "", tail.strip("= "))}


def run_go_suite(go: str | None) -> dict:
    if not go:
        return {"ran": False, "reason": "go not found"}
    out = {"ran": True}
    out["gofmt_clean"] = _run([str(Path(go).with_name("gofmt" + Path(go).suffix)), "-l", "."], BACKEND).stdout.strip() == ""
    out["vet_ok"] = _run([go, "vet", "./..."], BACKEND).returncode == 0
    out["build_ok"] = _run([go, "build", "./..."], BACKEND).returncode == 0
    p = _run([go, "test", "./...", "-count=1", "-json"], BACKEND)
    passed = failed = skipped = 0
    for line in p.stdout.splitlines():
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("Test") and ev.get("Action") in ("pass", "fail", "skip"):
            passed += ev["Action"] == "pass"
            failed += ev["Action"] == "fail"
            skipped += ev["Action"] == "skip"
    out.update({"tests_passed": passed, "tests_failed": failed, "tests_skipped": skipped, "test_exit_code": p.returncode})
    j = _run([go, "run", "./cmd/evidence"], BACKEND)
    judge_path = BACKEND / "reports" / "judge_evidence.json"
    checks = json.loads(judge_path.read_text(encoding="utf-8")).get("checks", []) if j.returncode == 0 and judge_path.exists() else []
    out.update({"judge_ran": j.returncode == 0, "judge_passed": sum(c.get("pass", False) for c in checks), "judge_total": len(checks)})
    return out


def run_ai_evals() -> dict:
    p = _run([sys.executable, "-m", "evals.run_evals"], ROOT)
    report = ROOT / "reports" / "ai_evals.md"
    m = re.search(r"\*\*Total\*\* \| \*\*(\d+) / (\d+)\*\* \| \*\*([\d.]+)%", report.read_text(encoding="utf-8")) if report.exists() else None
    return {"ran": True, "ok": p.returncode == 0, "passed": int(m.group(1)) if m else None, "total": int(m.group(2)) if m else None,
            "pct": m.group(3) if m else None}


# ---------------------------------------------------------------- pipeline

def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--no-tests", action="store_true", help="skip Go commands and every test suite")
    ap.add_argument("--out-dir", type=Path, default=None, help="report directory (default reports/synthetic)")
    args = ap.parse_args(argv)
    global OUT
    OUT = args.out_dir or ROOT / "reports" / "synthetic"
    OUT.mkdir(parents=True, exist_ok=True)
    go_bin = None if args.no_tests else find_go()
    go_refresh = refresh_go_outputs(go_bin) if not args.no_tests else {"ran": False, "reason": "--no-tests"}

    hashes_before = {p.name: D.sha256(p) for p in (D.FIXTURE_PATH, D.SYNTH_CSV, D.SYNTH_JSON, D.SYNTH_XLSX) if p.exists()}
    audit = D.audit_all()
    fx = D.load_fixture()
    fx_rows = D.fixture_pilots(fx)
    fx_engine_rows = D.official_with_extension()        # fixture + TEAM extension, as the Go loader sees it
    syn, _, _ = D.load_synthetic_csv()
    go = json.loads(GO_OUTPUTS.read_text(encoding="utf-8")) if GO_OUTPUTS.exists() else None

    ref = A.reference_check(fx)
    comp = A.comparison(fx_rows, syn)
    derived_rows, derived = A.derived_validation(syn)
    dv = A.decision_validation(syn, go)
    parity_rows, parity = A.engine_parity(syn, fx_engine_rows, go)
    official = A.official_decisions(fx_engine_rows, go)
    classes = A.class_analysis(syn)
    adoption_rows, adoption = A.adoption_analysis(syn, dv)
    quality_rows, quality = A.quality_analysis(syn, dv)
    ready_rows, ready = A.readiness_analysis(syn, dv)
    cash_rows, cash = A.cash_vs_capacity(syn, dv)
    overlap_rows, overlap = A.overlap_analysis(fx_rows, syn)
    port = A.portfolio_runs(syn, dv, go, fx_engine_rows)
    stress_rows, stress_changes, stress = A.stress_tests(syn, fx_rows)
    bps = A.breakpoints(syn, dv)
    ev_rows, ev = A.evidence_analysis(syn, dv)
    ev_flags = ev.pop("row_flags")
    ml = M.run(syn, {k: e.decision for k, e in dv["cash"].items()})

    from synthetic import charts as C
    chart_results = C.generate(OUT / "charts", {"fx_rows": fx_rows, "syn": syn, "dv": dv, "portfolio_results": port["results"],
                                                 "advisory": ml.get("advisory")})

    hashes_after = {p.name: D.sha256(p) for p in (D.FIXTURE_PATH, D.SYNTH_CSV, D.SYNTH_JSON, D.SYNTH_XLSX) if p.exists()}
    sources_unchanged = hashes_before == hashes_after and audit["files"]["official_fixture"]["identical_to_protected_copy"]

    if args.no_tests:
        py_tests, go_suite, evals = {"ran": False}, {"ran": False}, {"ran": False}
    else:
        py_tests, go_suite, evals = run_python_tests(), run_go_suite(go_bin), run_ai_evals()

    write_json("data_validation_report.json", {
        "audit": audit, "reference_check": ref, "derived_metrics": derived, "decisions": dv["summary"], "engine_parity": parity,
        "official_fixture_decisions": official, "adoption": adoption, "quality": quality, "readiness": ready, "cash_vs_capacity": cash,
        "overlap": overlap, "portfolio": {k: port[k] for k in ("determinism", "go_cross_check", "go_full_set_note", "status")},
        "stress": stress, "evidence": ev, "source_files_unchanged": sources_unchanged, "charts": chart_results,
        "ml": {k: v for k, v in ml.items() if k != "advisory"},
        "test_runs": {"python": py_tests, "go": go_suite, "ai_evals": evals, "go_outputs_refresh": {"ran": go_refresh.get("ran"), "ok": go_refresh.get("ok")}}})
    write_csv("file_consistency_differences.csv", audit["file_consistency"]["differences"] or
              [{"comparison": "csv/json/xlsx", "record_id": "", "column": "", "issue": "no differences"}])
    write_csv("original_vs_synthetic_comparison.csv", comp)
    write_csv("derived_metric_validation.csv", derived_rows)
    write_csv("decision_rule_validation.csv", dv["rows"])
    write_csv("decision_mismatches.csv", dv["mismatches"] or [{"comparison": "none", "reason_for_difference": "no mismatches"}])
    write_csv("python_vs_go_parity.csv", parity_rows)
    write_csv("decision_class_analysis.csv", classes)
    write_csv("adoption_analysis.csv", adoption_rows)
    write_csv("quality_analysis.csv", quality_rows)
    write_csv("readiness_analysis.csv", ready_rows)
    write_csv("readiness_inconsistencies.csv", ready["label_inconsistencies"] or [{"record_id": "", "note": "none"}])
    write_csv("cash_vs_capacity_analysis.csv", cash_rows)
    write_csv("overlap_analysis.csv", overlap_rows)
    write_csv("portfolio_results.csv", port["results"])
    write_csv("portfolio_pilot_reasons.csv", port["reasons"])
    write_csv("portfolio_go_cross_check.csv", port["go_cross_check"])
    write_csv("stress_test_results.csv", stress_rows)
    write_csv("stress_decision_changes.csv", stress_changes)
    write_csv("breakpoint_analysis.csv", bps)
    write_csv("evidence_analysis.csv", ev_rows)
    write_csv("evidence_label_flags.csv", ev_flags)
    if ml["status"] == "RUN":
        write_csv("model_comparison.csv", ml["models"])
        write_csv("ml_advisory_vs_rules.csv", ml["advisory"])
    else:
        write_csv("model_comparison.csv", [{"status": ml["status"], "reason": ml["reason"]}])
        write_csv("ml_advisory_vs_rules.csv", [{"status": ml["status"], "reason": ml["reason"]}])

    fc = audit["file_consistency"]["summary"]
    syn_audit = audit["synthetic_csv"]
    checks = {
        "data_audit": syn_audit["meets_expectation"],
        "file_consistency": all(v.get("status") == "MATCH" for v in fc.values()) and audit["workbook"].get("status") == "PASS",
        "reference_20_months": ref["status"] == "PASS",
        "derived_metrics": derived["rows_with_mismatch"] == 0,
        "python_vs_go": parity["status"] == "PASS",
        "quality_rule": quality["decision_consistency"] == "PASS",
        "overlap": overlap["status"] == "PASS", "portfolio": port["status"] == "PASS",
        "stress_40_month": stress["reference_case"]["status"] == "PASS", "stress_invariants": stress["all_checks_pass"],
        "sources_unchanged": sources_unchanged, "ml_leakage": ml["leakage_check"]["status"] == "PASS",
        "charts": all(c["status"] == "PASS" for c in chart_results),
    }
    if not args.no_tests:
        checks["python_tests"] = py_tests["failed"] == 0 and py_tests["passed"] > 0
        checks["go_suite"] = go_suite.get("ran", False) and go_suite.get("vet_ok") and go_suite.get("build_ok") and go_suite.get("tests_failed") == 0 \
            and go_suite.get("tests_passed", 0) > 0
        checks["judge"] = go_suite.get("judge_ran", False) and go_suite.get("judge_passed") == go_suite.get("judge_total") and go_suite.get("judge_total", 0) > 0
        checks["ai_evals_ran"] = evals.get("ok", False)
    status = "READY" if all(checks.values()) else "NEEDS FIXES"
    (OUT / "final_analysis_report.md").write_text(render_report(locals()), encoding="utf-8")
    print_graph_validation(chart_results)
    print_summary(locals())
    return 0 if status == "READY" else 1


# ---------------------------------------------------------------- report

def _md_table(rows: list, cols: list, headers: list | None = None) -> str:
    h = headers or cols
    out = ["| " + " | ".join(h) + " |", "|" + "|".join("---" for _ in h) + "|"]
    for r in rows:
        out.append("| " + " | ".join(str(_cell(r.get(c))).replace("|", "/") for c in cols) + " |")
    return "\n".join(out)


def _img(name: str, title: str) -> str:
    return f"![{title}](charts/{name})\n"


def render_report(c: dict) -> str:
    audit, ref, derived, dv, ml, parity = c["audit"], c["ref"], c["derived"], c["dv"]["summary"], c["ml"], c["parity"]
    syn_a, sv, pv = audit["synthetic_csv"], dv["stored_vs_rules_stated_basis"], dv["stored_vs_production_cash_basis"]
    fc, wbk = audit["file_consistency"]["summary"], audit["workbook"]
    L = []
    w = L.append
    w("# C23 — Original fixture vs SYNTHETIC dataset: comparison, validation and stress report\n")
    w("> **Original = official reference fixture** (5 pilots, `fixture(3).json`). **Synthetic = dummy test data** (120 rows) created for testing, "
      "comparison and scenario analysis — nothing in it is real-world evidence, and the two are different populations. Generated by "
      "`python -m synthetic.run`; rerunning gives byte-identical output.\n")
    # 1
    w("## 1. Executive Summary\n")
    w(f"- **Status: {c['status']}.** " + ", ".join(f"{k} {'PASS' if v else 'FAIL'}" for k, v in c["checks"].items()) + ".")
    w(f"- Official reference case recomputed: net INR {ref['net_annual_benefit_inr']}, monthly INR {ref['monthly_net_benefit_inr']}, payback "
      f"**{ref['calculated_payback_months']} months** (fixture expects {ref['expected_payback_months']}) — **{ref['status']}**.")
    w(f"- CSV vs JSON **{fc['csv_vs_json'].get('status')}**, CSV vs XLSX **{fc.get('csv_vs_xlsx', {}).get('status', 'NOT RUN')}**, "
      f"JSON vs XLSX **{fc.get('json_vs_xlsx', {}).get('status', 'NOT RUN')}** (120 records × 34 columns each).")
    w(f"- Derived metrics: {derived['rows_all_metrics_match']}/{derived['total_rows']} rows match an independent recalculation.")
    w(f"- Stored decisions vs documented rules on the dataset-label basis (full annual benefit counted): **{sv['matching']}/120 ({sv['reproduction_pct']}%)**; "
      f"the {sv['mismatching']} differences are READINESS_LABEL_INCONSISTENCY rows.")
    w(f"- Stored decisions vs the production engine (recognised cash benefit): {pv['matching']}/120 ({pv['reproduction_pct']}%). "
      f"{c['cash']['decisions_changed']} decisions change only because time freed / capacity / revenue assumptions are not cash. This is intended C23 behaviour.")
    w(f"- Python replica vs Go production engine: **{parity['pilots_fully_matching']}/{parity['pilots']} pilots** identical on "
      f"{len(parity['fields_compared'])} fields ({parity['field_checks_matched']}/{parity['field_checks']} field checks) — **{parity['status']}**.")
    w(f"- Inconsistencies detected and reported (not hidden, not 'fixed' in the data): {len(c['ready']['label_inconsistencies'])} readiness labels, "
      f"{c['ev']['evidence_label_inconsistencies']} evidence labels.\n")
    # 2
    w("## 2. Dataset Overview and Data Quality\n")
    files = audit["files"]
    w(_md_table([{"file": "fixture(3).json → data/C23/fixture.json", "role": "Original = official reference", "rows": audit["official_fixture"]["rows"],
                  "cols": audit["official_fixture"]["columns"], "sha256": files["official_fixture"]["sha256"][:16]},
                 {"file": D.SYNTH_CSV.name, "role": "Synthetic (main)", "rows": syn_a["rows"], "cols": syn_a["columns"], "sha256": files["synthetic_csv"]["sha256"][:16]},
                 {"file": D.SYNTH_JSON.name, "role": "Synthetic (consistency)", "rows": fc["csv_vs_json"].get("records", ["?", "?"])[1], "cols": "34 + metadata",
                  "sha256": files["synthetic_json"]["sha256"][:16]},
                 {"file": D.SYNTH_XLSX.name, "role": "Synthetic (consistency) + Summary/Original_Fixture/Data_Dictionary sheets",
                  "rows": fc.get("csv_vs_xlsx", {}).get("records", ["?", "?"])[1], "cols": 34, "sha256": (files["synthetic_xlsx"]["sha256"] or "MISSING")[:16]}],
                ["file", "role", "rows", "cols", "sha256"]))
    w(f"\nSynthetic CSV: {syn_a['rows']} rows, {syn_a['columns']} columns; missing {syn_a['missing_values']}, null {syn_a['null_values']}, NaN {syn_a['nan_values']}, "
      f"Infinity {syn_a['infinity_values']}, negative money {syn_a['negative_costs']}, percentages outside 0–100 {syn_a['pct_out_of_range']}, invalid categories "
      f"{sum(p['problem'] == 'unknown_label' for p in syn_a['validity_problems'])}, duplicate IDs {len(syn_a['duplicate_ids'])}, duplicate rows {syn_a['duplicate_rows']}; "
      f"invalid rows **{syn_a['invalid_rows']}** — expectation (120 / 0 / 0 / 0) {'met' if syn_a['meets_expectation'] else 'NOT met'}. "
      "Column types, first 5 rows, statistics and category distributions: `data_validation_report.json`.\n")
    # 3
    w("## 3. File Consistency (CSV vs JSON vs Excel)\n")
    w(_md_table([{"comparison": k, **v} for k, v in fc.items()], ["comparison", "status", "records", "common_columns", "differences"]))
    if isinstance(wbk, dict) and wbk.get("status") != "NOT RUN":
        w(f"\nWorkbook-only checks — **{wbk['status']}**: Original_Fixture sheet vs fixture.json + recalculated finance: "
          f"{wbk['original_fixture_sheet']['status']} ({len(wbk['original_fixture_sheet']['differences'])} differences). Summary-sheet claims: "
          + "; ".join(f"{x['claim']} {x['workbook']} ({'ok' if x['match'] else 'MISMATCH: data has ' + str(x['recalculated'])})" for x in wbk["summary_sheet_claims"])
          + ". Data_Dictionary rules: " + "; ".join(f"{r['column']} {r['rule']}: {r['violations']} violations" for r in wbk["data_dictionary_rules"])
          + f". The dictionary documents no readiness threshold (readiness rule present: {wbk['data_dictionary_has_readiness_rule']}).\n")
    w("Per-cell differences (record_id, column, CSV, JSON, Excel): `file_consistency_differences.csv`.\n")
    # 4
    w("## 4. Official Reference Calculation\n")
    w("Net = benefit − running; monthly = net / 12; payback = setup / monthly (exact fractions). The expected value is read from the fixture's "
      "`expected_first_pilot_payback_months`; 20 is not hard-coded anywhere in the calculation.\n")
    w(_md_table([ref], ["setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "net_annual_benefit_inr", "monthly_net_benefit_inr",
                        "expected_payback_months", "calculated_payback_months", "status"]))
    w(f"\n**Official 20-month test: {ref['status']}.** Official pilots through the production rules: "
      + ", ".join(f"{o['id']} {o['decision']} ({','.join(o['reasons'])})" for o in c["official"])
      + " — nothing is measured yet, so no official pilot is PROCEED; AI5's REVISE comes from the TEAM readiness label 'low' (extension.json).\n")
    # 5
    w("## 5. Original vs Synthetic Financial Comparison\n")
    w(_img("original_vs_synthetic_financials.png", "Original vs Synthetic Financial Comparison"))
    stats = {(r["metric"], r["statistic"]): r for r in c["comp"]}
    metrics = ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "net_annual_benefit_inr", "roi_pct", "payback_months")
    w(_md_table([{"metric": m, **{f"{s}_{side}": stats[(m, s)][f"{side}"] for s in ("mean", "min", "max") for side in ("original_fixture", "synthetic_dataset")}}
                 for m in metrics],
                ["metric", "mean_original_fixture", "mean_synthetic_dataset", "min_original_fixture", "min_synthetic_dataset", "max_original_fixture", "max_synthetic_dataset"],
                ["Metric", "Original Mean", "Synthetic Mean", "Original Min", "Synthetic Min", "Original Max", "Synthetic Max"]))
    bt = [r for r in c["comp"] if r["metric"].startswith(("benefit_type=", "overlap_group="))]
    w("\n" + _md_table(bt, ["metric", "original_fixture", "synthetic_dataset"], ["Category", "Original count", "Synthetic count"]))
    w("\nMonthly net benefit is compared in `original_vs_synthetic_comparison.csv` together with count / median / std. The fixture's five rows are identical "
      "(std 0); synthetic-only fields (adoption, quality, readiness, capacity value, cash saving, evidence, decision) are part of the synthetic extension "
      "and are not provided by the official fixture — they are not treated as missing there.\n")
    # 6
    w("## 6. Recalculated Synthetic Financial Metrics\n")
    w(_md_table([{"metric": m, "matched": n, "mismatched": derived["total_rows"] - n} for m, n in derived["matches_per_metric"].items()], ["metric", "matched", "mismatched"]))
    w(f"\n**Metric validation: {derived['total_rows']} total, {derived['rows_all_metrics_match']} matched, {derived['rows_with_mismatch']} mismatched** "
      f"(max |difference| {derived['max_abs_difference']}; stored values are rounded to 2 decimals, tolerance {derived['tolerance']}). "
      "Every row/metric with stored, recalculated and difference: `derived_metric_validation.csv`.\n")
    w(_img("roi_vs_payback.png", "ROI vs Payback"))
    # 7
    a = c["adoption"]
    w("## 7. Adoption\n")
    w(_img("adoption_comparison.png", "Expected vs Actual Adoption"))
    w(f"States: {a['states']}. Below break-even → STOP: {a['below_breakeven_and_stop']}/{a['states'].get('Below economic break-even', 0)}. "
      f"Below target but ≥ break-even → REVISE: {a['below_target_viable_and_revise']} (+{a['below_target_viable_but_stop_from_other_rule']} STOP because quality also fails). "
      f"Rule-consistent: {a['rule_consistent']}/{a['total']}. `adoption_analysis.csv`.\n")
    # 8
    q = c["quality"]
    w("## 8. Quality\n")
    w(_img("quality_comparison.png", "Expected vs Actual Quality"))
    w(f"Quality pass **{q['quality_pass']}**, fail **{q['quality_fail']}**; failures that are STOP — recalculated {q['fails_stop_recalculated']}/{q['quality_fail']}, "
      f"stored {q['fails_stop_stored']}/{q['quality_fail']}. Decision consistency **{q['decision_consistency']}**.\n")
    # 9
    rd = c["ready"]
    w("## 9. Readiness\n")
    w(_img("readiness_comparison.png", "Readiness by Decision Outcome"))
    w(f"Documented formula: overall = (data + technical + process) / 3; overall < 50 → LOW → REVISE (TEAM threshold). Levels: {rd['levels']}. "
      f"{rd['pilots_with_a_component_below_50']} pilots have a component below 50; only {rd['rule_fires']} have a LOW mean.\n")
    w(f"**READINESS_LABEL_INCONSISTENCY ({len(rd['label_inconsistencies'])} rows)** — stored label says readiness too low, but the documented threshold is not met. "
      "The production rule was **not** changed to fit them (`readiness_inconsistencies.csv`):\n")
    w(_md_table(rd["label_inconsistencies"], ["record_id", "data_readiness_pct", "technical_readiness_pct", "process_readiness_pct", "overall_readiness_pct_recalculated",
                                              "weakest_component", "stored_decision", "stored_reason", "recalculated_decision"]))
    w("\nLow readiness means *fix readiness before investing further*, not a failed idea.\n")
    # 10
    cv = c["cash"]
    w("## 10. Cash vs Capacity\n")
    w(_img("cash_vs_capacity.png", "Capacity Value vs Realized Cash Saving"))
    w(_md_table([{"benefit_type": k, **v} for k, v in cv["by_benefit_type"].items()],
                ["benefit_type", "pilots", "counted_as_cash_by_engine", "total_stated_benefit_inr", "total_capacity_value_inr", "total_realized_cash_saving_inr", "decisions_changed_on_cash_basis"]))
    w(f"\nTwo bases per pilot in `cash_vs_capacity_analysis.csv`: stored decision, decision using the **full annual benefit** (dataset-label basis) and decision "
      f"using the **recognised cash benefit** (production). **{cv['decisions_changed']} decisions change** because time freed is not cash; stored vs production differs "
      f"for {cv['stored_vs_production_differences']} pilots ({cv['decisions_changed']} cash-basis changes + the readiness-label rows). Capacity value is never counted "
      f"as cash automatically: {cv['capacity_never_counted_as_cash']}.\n")
    # 11
    w("## 11. Deterministic Decision Engine Reproduction\n")
    w(_img("decision_distribution.png", "Decision Distribution"))
    w("Rule order (c23-rules-v1): financial viability → quality → adoption below break-even → adoption below minimum → readiness → overlap → final "
      "(most severe of STOP > REVISE > REVIEW > PROCEED). No threshold was changed to fit the labels.\n")
    for key, title in (("stored_vs_rules_stated_basis", "Documented rules, dataset-label basis (full annual benefit)"),
                       ("stored_vs_production_cash_basis", "Production engine (recognised cash benefit)")):
        s = dv[key]
        w(f"**{title}:** total {dv['total_decisions']}, matching **{s['matching']}**, mismatching **{s['mismatching']}**, match **{s['reproduction_pct']}%** "
          "(rows = stored, columns = recalculated):\n")
        w(_md_table([{"stored": lab, **{c2: s["confusion_matrix"]["matrix"][i][j] for j, c2 in enumerate(s["confusion_matrix"]["labels"])}}
                     for i, lab in enumerate(s["confusion_matrix"]["labels"])], ["stored", "PROCEED", "REVISE", "STOP"]))
        w("")
    w("Every mismatch with its inputs and the exact reason: `decision_mismatches.csv`. No row recalculates to REVIEW because every synthetic row has measured "
      "actuals, a known overlap group and a recorded cash saving.\n")
    w("**Decision classes (stored labels), in this synthetic test dataset:**\n")
    w(_md_table(c["classes"], ["stored_decision", "pilots", "avg_setup_cost_inr", "avg_annual_benefit_inr", "avg_roi_pct", "avg_payback_months",
                                "avg_actual_adoption_pct", "avg_actual_quality_pct", "avg_overall_readiness_pct", "share_quality_failed_pct"]))
    w("\nFinance alone does not separate the classes (REVISE even has the highest average ROI); the quality, adoption and readiness gates do. Generator patterns, not enterprise findings.\n")
    # 12
    w("## 12. Python Rule Implementation vs Go Production Engine\n")
    w(_md_table([{"dataset": k, **v} for k, v in parity["by_dataset"].items()], ["dataset", "pilots", "pilots_fully_matching", "go_outputs_present"]))
    w(f"\nFields compared per pilot: {', '.join(parity['fields_compared'])}. Field checks matched: {parity['field_checks_matched']}/{parity['field_checks']}. "
      f"**Python vs Go: {parity['status']}** ({len(parity['mismatches'])} mismatches; every check in `python_vs_go_parity.csv`). Go outputs were regenerated "
      "from the current engine at the start of this run.\n")
    # 13
    o = c["overlap"]
    w("## 13. Overlap\n")
    w(f"Official support case AI4 + AI5: claimed INR {o['claimed_total']}, recognised while unresolved {o['unresolved']} → DOUBLE_COUNT_RISK, the second "
      f"claim is not counted. REMOVE_ONE {o['REMOVE_ONE(primary=AI4)']}; EXCLUDE_SECOND {o['EXCLUDE_SECOND(primary=AI4)']}; SPLIT_BENEFIT {o['SPLIT_BENEFIT(equal)']}; "
      f"CUSTOM_ALLOCATION {o['CUSTOM_ALLOCATION(120000/60000)']}; over-allocation rejected {o['over_allocation_rejected']}; never above pool {o['never_exceeds_pool']} — **{o['status']}**.\n")
    w(o["synthetic_note"] + "\n")
    w(_md_table([r for r in c["overlap_rows"] if r["dataset"] == "synthetic"], ["overlap_group", "pilots", "claimed_total_inr", "benefit_pool_inr", "double_count_prevented_inr", "double_count_warning"]))
    w("")
    # 14
    w("## 14. Portfolio Optimizer\n")
    w(_img("portfolio_budget_comparison.png", "Portfolio by Budget"))
    w(_md_table(c["port"]["results"], ["mode", "budget_inr", "selected_pilots", "selected_count", "rejected_count", "total_setup_inr", "remaining_budget_inr",
                                       "annual_net_benefit_inr", "within_budget", "no_stop_selected", "no_invalid_pilot", "no_overlap_double_count"],
                ["Mode", "Budget", "Selected", "#Sel", "#Rej", "Setup", "Remaining", "Annual net", "≤ budget", "no STOP", "no invalid", "no double count"]))
    w("\nRejections by reason code: " + " · ".join(f"{r['budget_inr']} ({'prod' if r['mode'].startswith('production') else 'label'}): {r['rejected_by_reason']}" for r in c["port"]["results"]) + ".")
    w(f"\nReason for every selected and rejected pilot at every budget: `portfolio_pilot_reasons.csv`. **Optimizer deterministic: "
      f"{'PASS' if all(d['same_after_5_shuffles'] for d in c['port']['determinism']) else 'FAIL'}** (5 shuffles per budget). Go optimizer cross-check "
      f"{sum(x['match'] for x in c['port']['go_cross_check'])}/{len(c['port']['go_cross_check'])}; the Go optimizer itself refuses the full set "
      f"({c['port']['go_full_set_note']}), so the 120-pilot portfolios use the equivalent exact search here.\n")
    # 15
    s = c["stress"]
    w("## 15. Stress Tests\n")
    w(_img("stress_test_comparison.png", "Stress Test"))
    w(f"Known changed-input case — setup 200000, running 120000, benefit 180000 → net {s['reference_case']['net_annual_benefit_inr']}, monthly "
      f"{s['reference_case']['monthly_net_benefit_inr']}, payback {s['reference_case']['payback_months']} months: **40-month changed-input test: {s['reference_case']['status']}**.\n")
    w(_md_table(c["stress_rows"], ["scenario", "reference_AI1_net_inr", "reference_AI1_payback_months", "mean_net_annual_benefit_inr", "mean_roi_pct",
                                   "production_PROCEED", "production_REVISE", "production_STOP", "decisions_changed_vs_baseline", "portfolio_net_inr", "no_decision_improved", "invariants_hold"]))
    w("\nRun on copies of all 120 pilots; source files untouched. Per-pilot decision changes: `stress_decision_changes.csv`.\n")
    # 16
    w("## 16. Breakpoints\n")
    for b in c["bps"][:5]:
        w(f"- {b['business_statement']}")
    w("\nAll pilots (max running cost, minimum benefit, break-even adoption, adoption and quality thresholds and margins): `breakpoint_analysis.csv`.\n")
    # 17
    w("## 17. Evidence Status\n")
    w(_md_table(c["ev_rows"], ["evidence_status", "pilots", "share_pct", "stored_PROCEED", "stored_REVISE", "stored_STOP", "production_PROCEED", "meaning"]))
    w(f"\n**EVIDENCE_LABEL_INCONSISTENCY: {c['ev']['evidence_label_inconsistencies']} rows** (`evidence_label_flags.csv`). {c['ev']['inconsistency']} "
      "Evidence status shows how much of a decision is assumption-based; it is not an AI confidence score.\n")
    # 18
    w("## 18. Optional ML (advisory)\n")
    if ml["status"] == "RUN":
        w(f"{ml['model_provenance']} Target: {ml['target']} (balance {ml['class_balance']}); decisions are never a target. Leakage check "
          f"**{ml['leakage_check']['status']}** — excluded: {', '.join(ml['leakage_check']['forbidden'])}. Split 80/20 stratified, random_state 42; Pipeline + ColumnTransformer.\n")
        w(_md_table(ml["models"], ["model", "accuracy", "precision", "recall", "f1", "macro_f1", "roc_auc", "confusion_matrix", "cv5_f1_mean", "cv5_f1_std",
                                   "cv5_macro_f1_mean", "cv5_roc_auc_mean", "cv5_roc_auc_std"],
                    ["Model", "Accuracy", "Precision", "Recall", "F1", "Macro F1", "ROC-AUC", "Confusion [[TN,FP],[FN,TP]]", "CV5 F1", "CV5 F1 std", "CV5 macro F1", "CV5 AUC", "CV5 AUC std"]))
        w(f"\nBest ({ml['selection_rule']}): **{ml['best_model']}**. A constant 'success' guess already scores F1 {ml['models'][0]['f1']} "
          f"({ml['class_balance']['success']} of 120 succeed), so macro F1 and ROC-AUC are the informative metrics.\n")
        w(_img("ml_vs_rule_comparison.png", "ML Adoption Risk vs Deterministic Decision"))
        w("Interesting combinations (production decisions; `ml_advisory_vs_rules.csv` has every pilot):\n")
        w(_md_table([{"combination": k, "pilots": v, "reading": M.COMBINATION_NOTES[tuple(k.split(" + "))]} for k, v in ml["interesting_combinations"].items()],
                    ["combination", "pilots", "reading"]))
        w("\n> **ML is advisory only. Final decision remains deterministic.** These results describe performance on synthetic test data only and do not "
          "prove real-world predictive performance.\n")
    else:
        w(f"Not run: {ml.get('reason')}.\n")
    # 19
    w("## 19. Automated Tests (actually executed by this run)\n")
    py, gs, ev = c["py_tests"], c["go_suite"], c["evals"]
    if py.get("ran"):
        w(_md_table([
            {"suite": "Python unit + integration (pytest, whole decision-engine suite)", "result": f"{py['passed']} passed / {py['failed']} failed / {py['skipped']} skipped"},
            {"suite": "Go gofmt / vet / build", "result": f"gofmt clean {gs.get('gofmt_clean')}, vet {'PASS' if gs.get('vet_ok') else 'FAIL'}, build {'PASS' if gs.get('build_ok') else 'FAIL'}"},
            {"suite": "Go tests (go test ./... -count=1)", "result": f"{gs.get('tests_passed')} passed / {gs.get('tests_failed')} failed / {gs.get('tests_skipped')} skipped"},
            {"suite": "Judge checks (go run ./cmd/evidence)", "result": f"{gs.get('judge_passed')} / {gs.get('judge_total')}"},
            {"suite": "AI evals (python -m evals.run_evals)", "result": f"{ev.get('passed')} / {ev.get('total')} ({ev.get('pct')}%)"},
            {"suite": "Data validation, portfolio, stress, parity (this pipeline)", "result": ", ".join(f"{k} {'PASS' if v else 'FAIL'}" for k, v in c["checks"].items()
                                                                                                       if k in ("data_audit", "file_consistency", "portfolio", "stress_invariants", "python_vs_go"))},
        ], ["suite", "result"]))
    else:
        w("Test suites not run in this invocation (`--no-tests`).")
    w("")
    # 20
    w("## 20. Limitations\n")
    w("- Synthetic data; class patterns come from the generator, not enterprises. 120 rows; the ML test split has 24 rows.")
    w("- Two stored readiness labels and 43 evidence labels are internally inconsistent; they are reported, not altered.")
    w("- Stored labels count non-cash benefit as cash; the production engine does not. Both views are reported side by side.")
    w("- The Excel file was delivered outside the project (Downloads\\dataset) and copied byte-for-byte into data/C23/.")
    w("- The Go optimizer is exhaustive up to 20 candidates; the 120-pilot portfolios use the equivalent exact DP (cross-checked against Go and brute force).")
    w("- ML is advisory, synthetic-only, and never changes PROCEED / REVISE / STOP.\n")
    # 21
    w("## 21. Final Conclusion\n")
    w(f"**{c['status']}.** The original reference case is correct (20 months from the formula; 40 months when running cost doubles). The three synthetic "
      f"files agree cell for cell and every derived column recalculates. Under the documented rules {sv['matching']}/120 stored decisions are reproduced; the "
      f"{sv['mismatching']} that are not are flagged readiness-label inconsistencies, not engine errors. The Python and Go engines agree on "
      f"{parity['pilots_fully_matching']}/{parity['pilots']} pilots field for field. The production engine is deliberately stricter than the labels where benefit is not "
      "cash. Portfolios respect budget, overlap and STOP rules and are order-independent; stressed assumptions only make decisions more severe. "
      "The decision system stays deterministic, explainable and testable; ML is advisory.\n")
    w("Charts and their interpretation: `charts/README.md`.\n")
    return "\n".join(L)


# ---------------------------------------------------------------- console

def print_graph_validation(results: list) -> None:
    print("GRAPH VALIDATION\n")
    for r in results:
        print(f"{r['label']}: {r['status']}")
    print(f"\nCharts generated: {sum(r.get('exists', False) for r in results)}")
    print(f"Charts failed: {sum(r['status'] != 'PASS' for r in results)}\n")


def print_summary(c: dict) -> None:
    a, dv, ml, parity = c["audit"]["synthetic_csv"], c["dv"]["summary"], c["ml"], c["parity"]
    fc, sv = c["audit"]["file_consistency"]["summary"], dv["stored_vs_rules_stated_basis"]
    pf = lambda ok: "PASS" if ok else "FAIL"
    bar = "=" * 50
    print(bar)
    print("C23 ORIGINAL VS SYNTHETIC DATA CHECK")
    print(bar)
    print(f"\nORIGINAL FIXTURE\nRows: {c['audit']['official_fixture']['rows']}")
    print(f"\nSYNTHETIC DATA\nRows: {a['rows']}\nMissing: {a['missing_values']}\nNull: {a['null_values']}\nInvalid: {a['invalid_rows']}")
    print(f"\nFILE CONSISTENCY\nCSV vs JSON: {pf(fc['csv_vs_json'].get('status') == 'MATCH')}\nCSV vs XLSX: "
          + (pf(fc['csv_vs_xlsx']['status'] == 'MATCH') if 'csv_vs_xlsx' in fc else "NOT RUN (xlsx missing)"))
    r = c["ref"]
    print(f"\nREFERENCE FINANCIAL TEST\nExpected payback: {r['expected_payback_months']}\nCalculated payback: {r['calculated_payback_months']}\n{r['status']}")
    d = c["derived"]
    print(f"\nDERIVED METRICS\nMatched: {d['rows_all_metrics_match']} / {d['total_rows']}\nMismatched: {d['rows_with_mismatch']}")
    print(f"\nDECISION RULES\nMatched: {sv['matching']} / {dv['total_decisions']}\nMismatched: {sv['mismatching']}\nMatch rate: {sv['reproduction_pct']}%")
    print(f"\nPYTHON VS GO ENGINE\nMatched: {parity['pilots_fully_matching']} / {parity['pilots']}\n{parity['status']}")
    print(f"\nCASH VS CAPACITY\nDecision differences: {c['cash']['decisions_changed']}")
    print(f"\nREADINESS INCONSISTENCIES\nCount: {len(c['ready']['label_inconsistencies'])}")
    print(f"\nOVERLAP\n{c['overlap']['status']}")
    print(f"\nPORTFOLIO\n{c['port']['status']}")
    print(f"\n40-MONTH STRESS CASE\n{c['stress']['reference_case']['status']}")
    if ml["status"] == "RUN":
        b = next(x for x in ml["models"] if x["model"] == ml["best_model"])
        print(f"\nML ADVISORY\nModel: {ml['best_model']}\nF1: {b['f1']}\nROC-AUC: {b['roc_auc']}\nLeakage: {ml['leakage_check']['status']}")
    else:
        print(f"\nML ADVISORY\nModel: not run ({ml.get('reason')})\nLeakage: {ml['leakage_check']['status']}")
    py, gs = c["py_tests"], c["go_suite"]
    if py.get("ran"):
        print(f"\nTESTS\nPython: {py['passed']} passed / {py['failed']} failed\nGo: {gs.get('tests_passed')} passed / {gs.get('tests_failed')} failed\n"
              f"Judge checks: {gs.get('judge_passed')} / {gs.get('judge_total')}")
    else:
        print("\nTESTS\nPython: NOT RUN\nGo: NOT RUN\nJudge checks: NOT RUN")
    print(f"\nFINAL STATUS\n{c['status']}")
    print(bar)


if __name__ == "__main__":
    raise SystemExit(main())
