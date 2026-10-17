"""Phases 4-5: explicit missing-value policy, normalisation and provenance."""
import copy, json
from pathlib import Path
import pytest

from app import provenance as P
from app.pipeline import FLAG_TEXT, SCENARIO, STRICT, canonical_benefit_type, prepare

D = Path(__file__).resolve().parent.parent / "data" / "raw"
FIX = json.loads((D / "official_fixture.json").read_text())
EXT = json.loads((D / "extension.json").read_text())


def run(mode=STRICT, fx=None, ext=None, meta=None, **kw):
    f = copy.deepcopy(FIX["records"]["pilots"])
    if fx: fx(f)
    e = copy.deepcopy(EXT["pilots"]) if ext is None else ext
    m = {k: v for k, v in EXT.items() if k != "pilots"} if meta is None else meta
    return prepare(f, e, m, mode, **kw)


def pilot(prep, pid):
    return next(p for p in prep.pilots if p.id == pid)


def test_official_strict_all_complete_and_traceable():
    prep = run()
    assert prep.summary() == {"mode": "strict", "valid": 5, "rejected": 0, "complete": 5, "incomplete": 0}
    p = pilot(prep, "AI1")
    assert (p.setup_cost_inr, p.annual_running_cost_inr, p.annual_benefit_inr) == (200000, 60000, 180000)
    assert p.field_sources["setup_cost_inr"] == P.OFFICIAL_FIXTURE
    assert p.field_sources["expected_adoption_pct"] == P.TEAM_ASSUMPTION and p.expected_adoption_pct == 70
    assert p.field_sources["actual_adoption_pct"] == P.UNKNOWN
    assert p.adoption_status == p.quality_status == "NOT_MEASURED"
    assert p.actual_adoption_pct is None and p.actual_quality_pct is None      # never zero
    assert p.row_source == P.OFFICIAL_FIXTURE and p.assumptions == []
    assert prep.budget == 600000 and prep.budget_issue is None


def test_strict_and_scenario_identical_when_nothing_missing():
    a, b = run(STRICT), run(SCENARIO)
    strip = lambda p: {k: v for k, v in p.model_dump().items() if k != "prep_mode"}
    assert [strip(p) for p in a.pilots] == [strip(p) for p in b.pilots]


# ---- running cost ----
def null_run(f): f[0]["annual_running_cost_inr"] = None


def test_null_run_cost_strict_stays_unknown():
    p = pilot(run(STRICT, null_run), "AI1")
    assert p.annual_running_cost_inr is None and p.data_status == "INCOMPLETE"
    assert "MISSING_RUN_COST" in p.flags and p.field_sources["annual_running_cost_inr"] == P.UNKNOWN


def test_null_run_cost_scenario_documented_assumption():
    p = pilot(run(SCENARIO, null_run), "AI1")
    assert p.annual_running_cost_inr == 40000 and p.data_status == "COMPLETE"   # 20% of 200000, as legacy core
    assert "RUN_COST_ASSUMED" in p.flags and p.field_sources["annual_running_cost_inr"] == P.TEAM_ASSUMPTION
    assert p.assumptions == [{"field": "annual_running_cost_inr", "value": 40000.0, "source": P.TEAM_ASSUMPTION,
                              "rule": "setup_cost_inr * 20% (extension.defaults.assumed_run_cost_pct_of_setup)"}]


def test_absent_run_cost_key_same_as_null():
    p = pilot(run(STRICT, lambda f: f[0].pop("annual_running_cost_inr")), "AI1")
    assert p.annual_running_cost_inr is None and "MISSING_RUN_COST" in p.flags


def test_scenario_cannot_impute_run_cost_without_setup():
    def m(f): f[0]["annual_running_cost_inr"] = None; f[0]["setup_cost_inr"] = None
    p = pilot(run(SCENARIO, m), "AI1")
    assert p.annual_running_cost_inr is None and {"MISSING_RUN_COST", "RUN_COST_NOT_IMPUTABLE", "MISSING_SETUP_COST"} <= set(p.flags)


# ---- setup / benefit never imputed ----
@pytest.mark.parametrize("mode", [STRICT, SCENARIO])
@pytest.mark.parametrize("field,flag", [("setup_cost_inr", "MISSING_SETUP_COST"), ("annual_benefit_inr", "MISSING_BENEFIT")])
def test_null_setup_or_benefit_never_imputed(mode, field, flag):
    p = pilot(run(mode, lambda f: f[1].update({field: None})), "AI2")
    assert getattr(p, field) is None and flag in p.flags and p.data_status == "INCOMPLETE"
    assert not any(a["field"] == field for a in p.assumptions)


# ---- adoption / quality / readiness ----
def ext_with(**kw):
    return [{**x, **kw} if x["id"] == "AI1" else x for x in copy.deepcopy(EXT["pilots"])]


def test_observed_values_labelled_observed():
    p = pilot(run(ext=ext_with(observed_adoption_pct=0, observed_quality_pct=55)), "AI1")
    assert p.actual_adoption_pct == 0 and p.adoption_status == "MEASURED"    # a measured 0 is a real 0
    assert p.field_sources["actual_adoption_pct"] == P.OBSERVED and p.field_sources["actual_quality_pct"] == P.OBSERVED


def test_missing_expected_adoption_and_readiness():
    e = [{k: v for k, v in x.items() if k not in ("adoption_pct", "readiness")} for x in EXT["pilots"]]
    s = pilot(run(STRICT, ext=e), "AI1")
    assert s.expected_adoption_pct is None and s.readiness_label is None
    assert {"ADOPTION_EXPECTATION_UNKNOWN", "READINESS_UNKNOWN"} <= set(s.flags)
    c = pilot(run(SCENARIO, ext=e), "AI1")
    assert c.expected_adoption_pct == 50 and c.readiness_label == "low"
    assert {"ADOPTION_ASSUMED", "READINESS_ASSUMED"} <= set(c.flags)
    assert {a["field"] for a in c.assumptions} == {"expected_adoption_pct", "readiness_label"}


def test_no_extension_at_all():
    prep = run(ext=[], meta={})
    assert prep.summary()["valid"] == 5 and prep.budget is None and prep.budget_issue.code == "BUDGET_MISSING"


def test_readiness_label_normalised_raw_kept():
    p = pilot(run(ext=ext_with(readiness="  HIGH ")), "AI1")
    assert p.readiness_label == "high" and p.readiness_label_raw == "  HIGH "


def test_invalid_extension_value_rejects_pilot_loudly():
    prep = run(ext=ext_with(adoption_pct=150))
    rj = next(r for r in prep.rejected if r["id"] == "AI1")
    assert rj["issues"][0]["code"] == "OUT_OF_RANGE" and rj["issues"][0]["field"] == "extension.adoption_pct"
    prep = run(ext=ext_with(readiness="very high"))
    assert next(r for r in prep.rejected if r["id"] == "AI1")["issues"][0]["code"] == "INVALID_ENUM"


def test_orphan_and_duplicate_extension_rows():
    prep = run(ext=copy.deepcopy(EXT["pilots"]) + [{"id": "AI99", "adoption_pct": 50}])
    assert any(i.code == "ORPHAN_EXTENSION_ROW" for i in prep.issues) and prep.summary()["valid"] == 5
    prep = run(ext=copy.deepcopy(EXT["pilots"]) + [{"id": "AI2", "adoption_pct": 10}])
    assert "AI2" in [r["id"] for r in prep.rejected]


def test_extension_cannot_override_official_values():
    fx = lambda f: f[0].update(expected_adoption_pct=30)
    prep = run(fx=fx)
    p = pilot(prep, "AI1")
    assert p.expected_adoption_pct == 30 and p.field_sources["expected_adoption_pct"] == P.OFFICIAL_FIXTURE
    assert any(i.code == "EXTENSION_CONFLICT" for i in prep.issues)
    prep = run(ext=ext_with(setup_cost_inr=1))       # not an extension field at all
    assert pilot(prep, "AI1").setup_cost_inr == 200000 and any(i.code == "UNKNOWN_FIELD" for i in prep.issues)


# ---- overlap ----
@pytest.mark.parametrize("value", [None, "", "   "])
def test_overlap_unknown_is_not_no_overlap(value):
    p = pilot(run(fx=lambda f: f[4].update(overlap_group=value)), "AI5")
    assert p.overlap_group is None and p.overlap_status == "UNKNOWN" and "OVERLAP_UNKNOWN" in p.flags


def test_overlap_group_normalised():
    prep = run(fx=lambda f: f[4].update(overlap_group="  SUPPORT "))
    assert pilot(prep, "AI5").overlap_group == pilot(prep, "AI4").overlap_group == "support"
    assert pilot(prep, "AI5").overlap_group_raw == "  SUPPORT "


# ---- benefit type ----
@pytest.mark.parametrize("raw,canon", [("cashable assumption", "cashable"), ("Time freed", "time_freed"),
                                       ("cashable time", "time_freed"), ("revenue uplift", "other"), (None, "unknown"), ("  ", "unknown")])
def test_canonical_benefit_type(raw, canon):
    assert canonical_benefit_type(raw) == canon


def test_time_freed_flagged():
    p = pilot(run(fx=lambda f: f[0].update(benefit_type="time freed")), "AI1")
    assert p.benefit_type == "time_freed" and "TIME_NOT_CASH" in p.flags and p.benefit_type_raw == "time freed"


# ---- rejection & invariants ----
def test_invalid_rows_rejected_with_structured_errors():
    def m(f): f[0]["setup_cost_inr"] = "2 lakh"; f[1]["annual_benefit_inr"] = -5; f[2]["id"] = "AI4"
    prep = run(fx=m)
    assert sorted(r["id"] for r in prep.rejected) == ["AI1", "AI2", "AI4", "AI4"]
    assert [p.id for p in prep.pilots] == ["AI5"]
    for r in prep.rejected:
        assert {"code", "message", "field", "row", "severity"} <= r["issues"][0].keys()


def test_bad_budget_and_mode():
    assert run(budget=-1).budget_issue.code == "NEGATIVE" and run(budget=-1).budget is None
    assert run(budget=100000).budget == 100000
    with pytest.raises(ValueError):
        run(mode="optimistic")


def test_invalid_defaults_reported_and_not_used():
    meta = {"pilot_budget_inr": 600000, "defaults": {"assumed_run_cost_pct_of_setup": 500, "assumed_readiness": "max"}}
    prep = run(SCENARIO, null_run, meta=meta)
    assert {i.code for i in prep.issues} >= {"OUT_OF_RANGE", "INVALID_ENUM"}
    p = pilot(prep, "AI1")
    assert p.annual_running_cost_inr is None and "RUN_COST_NOT_IMPUTABLE" in p.flags


def test_every_flag_documented_and_deterministic():
    def m(f): f[0]["annual_running_cost_inr"] = None; f[4]["overlap_group"] = None; f[3]["benefit_type"] = "time"
    for mode in (STRICT, SCENARIO):
        a, b = run(mode, m), run(mode, m)
        assert [p.model_dump() for p in a.pilots] == [p.model_dump() for p in b.pilots]
        assert all(f in FLAG_TEXT for p in a.pilots for f in p.flags)


def test_input_order_does_not_change_cleaned_rows():
    a = {p.id: p.model_dump() for p in run().pilots}
    b = {p.id: p.model_dump() for p in run(fx=lambda f: f.reverse()).pilots}
    assert a == b


def test_raw_input_not_mutated():
    f = copy.deepcopy(FIX["records"]["pilots"]); f[0]["annual_running_cost_inr"] = None
    before = copy.deepcopy(f)
    prepare(f, copy.deepcopy(EXT["pilots"]), EXT, SCENARIO)
    assert f == before
