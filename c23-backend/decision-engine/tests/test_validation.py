"""Phase 3: strict validation + loaders. Errors are structured {code, field, row, id, severity}."""
import copy, json, math
from pathlib import Path
import pytest

from app.io_utils import LoadError, load_table, parse_csv, parse_json
from app.schemas import EXTENSION_FIELDS, validate_budget, validate_rows

D = Path(__file__).resolve().parent.parent / "data" / "raw"
FIX = json.loads((D / "official_fixture.json").read_text())
ROWS = FIX["records"]["pilots"]


def rows(**changes_for_ai1):
    r = copy.deepcopy(ROWS)
    r[0].update(changes_for_ai1)
    return r


def codes(vrows, rid="AI1"):
    return {(i.field, i.code) for v in vrows if v.id == rid for i in v.issues if i.severity == "error"}


def test_official_rows_all_valid_no_errors():
    v, file_issues = validate_rows(ROWS)
    assert file_issues == [] and all(r.ok for r in v) and [r.id for r in v] == ["AI1", "AI2", "AI3", "AI4", "AI5"]
    assert not any(i for r in v for i in r.issues)


def test_values_never_altered():
    v, _ = validate_rows(ROWS)
    assert v[0].values["setup_cost_inr"] == 200000 and isinstance(v[0].values["setup_cost_inr"], int)


@pytest.mark.parametrize("field,value,code", [
    ("setup_cost_inr", -5, "NEGATIVE"),
    ("annual_running_cost_inr", -1, "NEGATIVE"),
    ("annual_benefit_inr", -0.01, "NEGATIVE"),
    ("setup_cost_inr", float("nan"), "NOT_FINITE"),
    ("annual_benefit_inr", float("inf"), "NOT_FINITE"),
    ("annual_running_cost_inr", float("-inf"), "NOT_FINITE"),
    ("setup_cost_inr", "2 lakh", "NOT_NUMERIC"),
    ("setup_cost_inr", "200000", "NOT_NUMERIC"),
    ("annual_benefit_inr", True, "NOT_NUMERIC"),
    ("annual_benefit_inr", [180000], "NOT_NUMERIC"),
    ("expected_adoption_pct", 101, "OUT_OF_RANGE"),
    ("actual_quality_pct", 100.5, "OUT_OF_RANGE"),
    ("data_readiness_pct", 150, "OUT_OF_RANGE"),
    ("actual_adoption_pct", -1, "OUT_OF_RANGE"),
    ("task", "", "EMPTY"),
    ("task", "   ", "EMPTY"),
    ("task", 42, "NOT_TEXT"),
    ("overlap_group", 7, "NOT_TEXT"),
    ("overlap_group", "../etc/passwd", "BAD_LABEL"),
    ("overlap_group", "sup;port", "BAD_LABEL"),
    ("id", "../AI1", "BAD_ID"),
    ("id", "AI1'; DROP TABLE pilots;--", "BAD_ID"),
    ("id", "", "BAD_ID"),
    ("id", None, "REQUIRED_MISSING"),
])
def test_bad_values_rejected(field, value, code):
    v, _ = validate_rows(rows(**{field: value}))
    errs = [i for i in v[0].issues if i.severity == "error"]
    assert not v[0].ok and (field, code) in {(i.field, i.code) for i in errs}
    json.dumps([i.to_dict() for i in errs], allow_nan=False)    # evidence stays valid JSON even for NaN input


def test_boundaries_accepted():
    v, _ = validate_rows(rows(setup_cost_inr=0, annual_benefit_inr=0.0, expected_adoption_pct=0, actual_quality_pct=100))
    assert v[0].ok


def test_missing_task_and_id_keys():
    r = copy.deepcopy(ROWS); del r[0]["task"]; del r[1]["id"]
    v, _ = validate_rows(r)
    assert ("task", "REQUIRED_MISSING") in codes(v, "AI1")
    assert any(i.code == "REQUIRED_MISSING" and i.field == "id" for i in v[1].issues)


def test_null_financials_are_valid_but_unknown():
    """null = unknown: not an error at validation; the missing-value policy decides (REVISE)."""
    v, _ = validate_rows(rows(setup_cost_inr=None, annual_running_cost_inr=None, annual_benefit_inr=None))
    assert v[0].ok and v[0].values["setup_cost_inr"] is None


def test_absent_core_field_treated_as_null_with_warning():
    r = copy.deepcopy(ROWS); del r[0]["annual_running_cost_inr"]
    v, _ = validate_rows(r)
    assert v[0].ok and v[0].absent == ("annual_running_cost_inr",)
    assert any(i.code == "FIELD_ABSENT" and i.severity == "warning" for i in v[0].issues)


def test_duplicate_ids_reject_all_occurrences_order_independent():
    r = copy.deepcopy(ROWS); r[1]["id"] = "ai1"   # case-insensitive
    for order in (r, r[::-1]):
        v, _ = validate_rows(order)
        dup = {x.id for x in v if any(i.code == "DUPLICATE_ID" for i in x.issues)}
        assert dup == {"AI1", "ai1"} and all(not x.ok for x in v if x.id in dup)
        assert all(x.ok for x in v if x.id not in dup)


def test_unknown_field_dropped_with_warning_unless_allowed():
    v, _ = validate_rows(rows(secret_note="x"))
    assert v[0].ok and "secret_note" not in v[0].values
    assert any(i.code == "UNKNOWN_FIELD" and i.severity == "warning" for i in v[0].issues)
    v, _ = validate_rows(rows(secret_note="x"), allowed_extra=("secret_note",))
    assert v[0].values["secret_note"] == "x"


def test_non_object_rows_and_bad_shapes():
    v, _ = validate_rows([1, "x", None])
    assert [i.code for r in v for i in r.issues] == ["NOT_AN_OBJECT"] * 3
    assert validate_rows({"a": 1})[1][0].code == "FILE_SHAPE"
    v, fi = validate_rows([])
    assert v == [] and fi[0].code == "EMPTY_LIST"


def test_extension_schema():
    ext = [{"id": "AI1", "adoption_pct": 70, "readiness": " High ", "observed_adoption_pct": None}]
    v, _ = validate_rows(ext, EXTENSION_FIELDS)
    assert v[0].ok
    for bad, code in (({"readiness": "very high"}, "INVALID_ENUM"), ({"adoption_pct": 150}, "OUT_OF_RANGE"),
                      ({"observed_quality_pct": "90%"}, "NOT_NUMERIC")):
        v, _ = validate_rows([{**ext[0], **bad}], EXTENSION_FIELDS)
        assert not v[0].ok and code in [i.code for i in v[0].issues]


@pytest.mark.parametrize("budget,code", [(None, "BUDGET_MISSING"), (-1, "NEGATIVE"), ("6 lakh", "NOT_NUMERIC"),
                                         (float("nan"), "NOT_FINITE"), (True, "NOT_NUMERIC")])
def test_budget_validation(budget, code):
    assert validate_budget(budget).code == code


def test_budget_valid():
    assert validate_budget(600000) is None and validate_budget(0) is None


# ---- loaders ----
def test_csv_loader_strict_numbers():
    rows_ = parse_csv("id,task,setup_cost_inr,annual_running_cost_inr,annual_benefit_inr,benefit_type,overlap_group\n"
                      "AI1,X,200000,,180000.5,cash,finance\nAI2,Y,2 lakh,nan,1e9,cash,ops\n")
    assert rows_[0]["setup_cost_inr"] == 200000 and rows_[0]["annual_running_cost_inr"] is None
    assert rows_[0]["annual_benefit_inr"] == 180000.5
    assert rows_[1]["setup_cost_inr"] == "2 lakh" and rows_[1]["annual_running_cost_inr"] == "nan" and rows_[1]["annual_benefit_inr"] == "1e9"
    v, _ = validate_rows(rows_)
    assert v[0].ok and not v[1].ok


@pytest.mark.parametrize("text,code", [("", "EMPTY_FILE"), ("id,task\nAI1\n", "MALFORMED_CSV"), ("id,id\n1,2\n", "MALFORMED_CSV"),
                                       ("id,\n1,2\n", "MALFORMED_CSV"), ("id,task\nA,B,C\n", "MALFORMED_CSV")])
def test_malformed_csv(text, code):
    with pytest.raises(LoadError) as e:
        parse_csv(text)
    assert e.value.code == code


@pytest.mark.parametrize("text,code", [("{bad json", "MALFORMED_JSON"), ('{"records": 5}', "FILE_SHAPE"), ("42", "FILE_SHAPE")])
def test_malformed_json(text, code):
    with pytest.raises(LoadError) as e:
        parse_json(text)
    assert e.value.code == code


def test_json_nan_literal_reaches_validator_and_is_rejected():
    rows_, _ = parse_json('[{"id":"AI1","task":"t","setup_cost_inr":NaN,"annual_running_cost_inr":1,"annual_benefit_inr":Infinity,'
                          '"benefit_type":"cash","overlap_group":"x"}]')
    assert math.isnan(rows_[0]["setup_cost_inr"])
    v, _ = validate_rows(rows_)
    assert {i.code for i in v[0].issues} == {"NOT_FINITE"}


def test_load_table_shapes(tmp_path):
    rows_, meta, fmt = load_table(D / "official_fixture.json")
    assert fmt == "json" and len(rows_) == 5 and meta["case_id"] == "C23"
    rows_, meta, _ = load_table(D / "extension.json")
    assert len(rows_) == 5 and meta["pilot_budget_inr"] == 600000
    with pytest.raises(LoadError) as e:
        load_table(tmp_path / "missing.json")
    assert e.value.code == "FILE_NOT_FOUND"
    (tmp_path / "bin.json").write_bytes(b"\xff\xfe\x00bad")
    with pytest.raises(LoadError) as e:
        load_table(tmp_path / "bin.json")
    assert e.value.code == "BAD_ENCODING"
