"""Phases 2 and 5: audit and preprocessing scripts are read-only, deterministic and fail cleanly."""
import csv, json, shutil
from pathlib import Path

from scripts import audit_data, preprocess_data

RAW = Path(__file__).resolve().parent.parent / "data" / "raw"
FIX, EXT = RAW / "official_fixture.json", RAW / "extension.json"


def snapshot():
    return {p.name: p.read_bytes() for p in RAW.iterdir()}


def test_audit_official(tmp_path):
    before = snapshot()
    assert audit_data.main(["--out-dir", str(tmp_path)]) == 0
    assert snapshot() == before                          # audit never modifies data
    r = json.loads((tmp_path / "data_audit.json").read_text())
    fx = r["tables"]["fixture_pilots"]
    assert fx["row_count"] == 5 and fx["duplicate_ids"] == [] and fx["duplicate_rows"] == 0
    assert fx["column_profiles"]["setup_cost_inr"]["types"] == {"int": 5}
    assert fx["column_profiles"]["overlap_group"]["distribution"] == {"catalog": 1, "finance": 1, "ops": 1, "support": 2}
    assert r["shared_overlap_groups"] == {"support": ["AI4", "AI5"]}
    assert r["dataset_meta"]["synthetic"] is True and r["target_class_distribution"] is None
    assert all(v == [] for v in r["rule_checks"].values())
    ext = r["tables"]["extension_pilots"]["column_profiles"]
    assert ext["observed_adoption_pct"]["null"] == 5 and ext["readiness"]["distribution"] == {"high": 2, "low": 1, "medium": 2}
    assert r["validation_preview"]["strict"]["valid"] == 5
    assert "C23 Data Audit" in (tmp_path / "data_audit.md").read_text(encoding="utf-8")


def test_audit_reproducible(tmp_path):
    audit_data.main(["--out-dir", str(tmp_path / "a")]); audit_data.main(["--out-dir", str(tmp_path / "b")])
    for f in ("data_audit.json", "data_audit.md"):
        assert (tmp_path / "a" / f).read_bytes() == (tmp_path / "b" / f).read_bytes()


def test_audit_dirty_csv(tmp_path):
    p = tmp_path / "dirty.csv"
    p.write_text("id,task,setup_cost_inr,annual_running_cost_inr,annual_benefit_inr,benefit_type,overlap_group,expected_adoption_pct,extra\n"
                 "AI1,A,200000,,180000,cashable,Support,70,x\n"
                 "AI1,A,200000,,180000,cashable,Support,70,x\n"
                 "AI2,B,-5,60000,2 lakh,time freed,,150,\n", encoding="utf-8")
    assert audit_data.main(["--fixture", str(p), "--no-extension", "--out-dir", str(tmp_path)]) == 0
    r = json.loads((tmp_path / "data_audit.json").read_text())
    fx = r["tables"]["fixture_pilots"]
    assert fx["duplicate_ids"] == ["ai1"] and fx["duplicate_rows"] == 1 and fx["unknown_columns"] == ["extra"]
    assert fx["column_profiles"]["annual_running_cost_inr"]["missing_pct"] == 66.67
    rc = r["rule_checks"]
    assert [x["field"] for x in rc["negative_values"]] == ["setup_cost_inr"]
    assert [x["field"] for x in rc["pct_out_of_range"]] == ["expected_adoption_pct"]
    assert [x["value"] for x in rc["non_numeric_in_numeric_field"]] == ["2 lakh"]
    assert r["overlap_groups"]["(unknown)"] == ["AI2"]
    assert sorted(r["validation_preview"]["strict"]["rejected_ids"]) == ["AI1", "AI1", "AI2"]


def test_audit_malformed_inputs_exit_2(tmp_path, capsys):
    (tmp_path / "bad.json").write_text("{nope", encoding="utf-8")
    (tmp_path / "bad.csv").write_text("id,task\nAI1\n", encoding="utf-8")
    for f in ("bad.json", "bad.csv", "missing.json"):
        assert audit_data.main(["--fixture", str(tmp_path / f), "--out-dir", str(tmp_path / "o")]) == 2
    assert "MALFORMED" in capsys.readouterr().err and not (tmp_path / "o").exists()


def test_preprocess_outputs_and_reproducible(tmp_path):
    before = snapshot()
    assert preprocess_data.main(["--out-dir", str(tmp_path / "a")]) == 0
    assert preprocess_data.main(["--out-dir", str(tmp_path / "b")]) == 0
    assert snapshot() == before
    names = ["pilots_clean.json", "pilots_clean.csv", "pilots_clean_scenario.json", "pilots_clean_scenario.csv"]
    for n in names:
        assert (tmp_path / "a" / n).read_bytes() == (tmp_path / "b" / n).read_bytes()
    doc = json.loads((tmp_path / "a" / "pilots_clean.json").read_text())
    assert doc["mode"] == "strict" and doc["synthetic"] is True and doc["summary"]["valid"] == 5
    assert doc["inputs"][0]["sha256"] == "83a42f1c0f4a607fe5bc649158dafbbea174057dc59284ebf57581a473b7faf4"
    with (tmp_path / "a" / "pilots_clean.csv").open(encoding="utf-8", newline="") as fh:
        rows = list(csv.DictReader(fh))
    assert [r["id"] for r in rows] == ["AI1", "AI2", "AI3", "AI4", "AI5"]
    assert rows[0]["setup_cost_inr"] == "200000" and rows[0]["actual_adoption_pct"] == "" and rows[0]["row_source"] == "OFFICIAL_FIXTURE"


def test_preprocess_null_run_cost_strict_vs_scenario(tmp_path):
    fx = json.loads(FIX.read_text()); fx["records"]["pilots"][0]["annual_running_cost_inr"] = None
    p = tmp_path / "fx.json"; p.write_text(json.dumps(fx), encoding="utf-8")
    shutil.copy(EXT, tmp_path / "ext.json")
    assert preprocess_data.main(["--fixture", str(p), "--extension", str(tmp_path / "ext.json"), "--out-dir", str(tmp_path)]) == 0
    s = json.loads((tmp_path / "pilots_clean.json").read_text())
    c = json.loads((tmp_path / "pilots_clean_scenario.json").read_text())
    assert s["pilots"][0]["annual_running_cost_inr"] is None and "MISSING_RUN_COST" in s["flag_glossary"]
    assert c["pilots"][0]["annual_running_cost_inr"] == 40000 and "RUN_COST_ASSUMED" in c["flag_glossary"]


def test_preprocess_malformed_exit_2(tmp_path):
    (tmp_path / "bad.json").write_text("[1,", encoding="utf-8")
    assert preprocess_data.main(["--fixture", str(tmp_path / "bad.json"), "--out-dir", str(tmp_path)]) == 2
