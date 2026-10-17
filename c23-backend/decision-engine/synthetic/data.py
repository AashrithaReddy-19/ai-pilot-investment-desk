"""Read-only loading and auditing of the official fixture and the synthetic comparison dataset.

Nothing is coerced silently: CSV text is typed by an explicit column schema, and every value that does not fit
the schema is reported as invalid (never repaired). Source files are opened read-only.
"""
from __future__ import annotations
import csv, hashlib, json, math, statistics
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DATA_DIR = ROOT / "data" / "C23"
FIXTURE_PATH = DATA_DIR / "fixture.json"                      # delivered as "fixture(3).json"
PROTECTED_FIXTURE = ROOT / "data" / "raw" / "official_fixture.json"
SYNTH_CSV = DATA_DIR / "C23_complete_valid_synthetic_dataset.csv"
SYNTH_JSON = DATA_DIR / "C23_complete_valid_synthetic_dataset.json"
SYNTH_XLSX = DATA_DIR / "C23_complete_valid_dataset.xlsx"

MONEY_COLS = ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "net_annual_benefit_inr", "monthly_net_benefit_inr",
              "capacity_value_inr", "realized_cash_saving_inr")
PCT_COLS = ("expected_adoption_pct", "actual_adoption_pct", "minimum_adoption_pct", "breakeven_adoption_pct", "expected_quality_pct",
            "actual_quality_pct", "minimum_quality_pct", "data_readiness_pct", "technical_readiness_pct", "process_readiness_pct",
            "overall_readiness_pct", "data_completeness_pct")
# Derived values that may legitimately be negative or exceed 100 (ROI) / be unbounded (payback).
OTHER_NUM_COLS = ("payback_months", "roi_pct")
NON_NEGATIVE_MONEY = ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr", "capacity_value_inr", "realized_cash_saving_inr")
BOOL_COLS = ("synthetic", "overlap_resolved")
TEXT_COLS = ("record_id", "source_type", "task", "department_group", "benefit_type", "overlap_group", "evidence_status", "decision",
             "decision_reason", "scenario_tag", "notes")
NUMERIC_COLS = MONEY_COLS + PCT_COLS + OTHER_NUM_COLS
ALLOWED = {"decision": {"PROCEED", "REVISE", "STOP"}, "evidence_status": {"ASSUMPTION", "OBSERVED", "MIXED"}}
NULL_TOKENS = {"null", "none", "nan", "n/a", "na"}


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def load_fixture(path: Path = FIXTURE_PATH) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def fixture_pilots(fx: dict) -> list:
    return list(fx["records"]["pilots"])


EXTENSION_PATH = ROOT / "data" / "raw" / "extension.json"
EXTENSION_ALIASES = {"adoption_pct": "expected_adoption_pct", "observed_adoption_pct": "actual_adoption_pct",
                     "observed_quality_pct": "actual_quality_pct", "readiness": "readiness_label"}   # domain.ExtensionAliases


def official_with_extension() -> list:
    """Fixture pilots merged with the TEAM extension exactly as the Go loader does (fixture.LoadOfficial).
    Extension values are TEAM assumptions; null stays null. Used only where the Go engine uses them (decisions, optimizer)."""
    ext = {p["id"]: p for p in json.loads(EXTENSION_PATH.read_text(encoding="utf-8"))["pilots"]}
    out = []
    for p in fixture_pilots(load_fixture()):
        q = dict(p)
        for k, v in ext.get(p["id"], {}).items():
            if k != "id":
                q[EXTENSION_ALIASES.get(k, k)] = v
        out.append(q)
    return out


def _parse_number(s: str):
    """Return (value, problem). Integers stay int; non-finite values are reported, not accepted."""
    t = s.strip()
    if t == "":
        return None, "missing"
    if t.lower() == "nan":
        return None, "nan"
    if t.lower() in NULL_TOKENS:
        return None, "null_token"
    try:
        v = int(t)
    except ValueError:
        try:
            v = float(t)
        except ValueError:
            return None, "not_a_number"
        if not math.isfinite(v):
            return None, "nan" if math.isnan(v) else "infinity"
    return v, None


def load_synthetic_csv(path: Path = SYNTH_CSV) -> tuple[list, list, list]:
    """Return (typed rows, column names, problems). Problems are reported, values are never repaired."""
    with path.open(encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        columns = list(reader.fieldnames or [])
        raw_rows = list(reader)
    rows, problems = [], []
    for i, r in enumerate(raw_rows, start=1):
        out = {}
        for c in columns:
            s = r.get(c)
            s = "" if s is None else s
            if c in NUMERIC_COLS:
                v, prob = _parse_number(s)
                if prob:
                    problems.append({"row": i, "record_id": r.get("record_id"), "column": c, "problem": prob, "value": s})
                out[c] = v
            elif c in BOOL_COLS:
                if s not in ("True", "False"):
                    problems.append({"row": i, "record_id": r.get("record_id"), "column": c, "problem": "not_a_boolean", "value": s})
                    out[c] = None
                else:
                    out[c] = s == "True"
            else:
                if s.strip() == "":
                    problems.append({"row": i, "record_id": r.get("record_id"), "column": c, "problem": "missing", "value": s})
                elif s.strip().lower() in NULL_TOKENS:
                    problems.append({"row": i, "record_id": r.get("record_id"), "column": c, "problem": "null_token", "value": s})
                out[c] = s if s.strip() else None
        rows.append(out)
    return rows, columns, problems


def load_synthetic_json(path: Path = SYNTH_JSON) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


_XNS = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
        "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
        "pr": "http://schemas.openxmlformats.org/package/2006/relationships"}


def _col_index(ref: str) -> int:
    n = 0
    for ch in ref:
        if not ch.isalpha():
            break
        n = n * 26 + ord(ch.upper()) - 64
    return n - 1


def read_xlsx(path: Path = SYNTH_XLSX) -> dict:
    """Read every sheet of an .xlsx as a list of row lists (stdlib only; values typed from the cell type, never repaired).
    Returns {sheet_name: [[cell, ...], ...]}; numbers are int when integral, else float; booleans are bool."""
    import zipfile
    import xml.etree.ElementTree as ET
    with zipfile.ZipFile(path) as z:
        shared = []
        if "xl/sharedStrings.xml" in z.namelist():
            for si in ET.fromstring(z.read("xl/sharedStrings.xml")).findall("m:si", _XNS):
                shared.append("".join(t.text or "" for t in si.iter(f"{{{_XNS['m']}}}t")))
        rels = {r.get("Id"): r.get("Target") for r in ET.fromstring(z.read("xl/_rels/workbook.xml.rels")).findall("pr:Relationship", _XNS)}
        out = {}
        for sh in ET.fromstring(z.read("xl/workbook.xml")).find("m:sheets", _XNS):
            target = rels[sh.get(f"{{{_XNS['r']}}}id")].lstrip("/")
            target = target if target.startswith("xl/") else "xl/" + target
            rows = []
            for row in ET.fromstring(z.read(target)).iter(f"{{{_XNS['m']}}}row"):
                cells = {}
                for c in row.findall("m:c", _XNS):
                    t, v = c.get("t", "n"), c.find("m:v", _XNS)
                    if t == "inlineStr":
                        val = "".join(x.text or "" for x in c.iter(f"{{{_XNS['m']}}}t"))
                    elif v is None or v.text is None:
                        continue
                    elif t == "s":
                        val = shared[int(v.text)]
                    elif t == "b":
                        val = v.text == "1"
                    elif t in ("str", "e"):
                        val = v.text
                    else:
                        num = float(v.text)
                        val = int(num) if num.is_integer() and "e" not in v.text.lower() and "." not in v.text else num
                    cells[_col_index(c.get("r"))] = val
                if cells:
                    rows.append([cells.get(i) for i in range(max(cells) + 1)])
            out[sh.get("name")] = rows
    return out


def sheet_records(rows: list) -> list:
    """First row = header; following rows -> dicts (missing trailing cells -> None)."""
    head = [h for h in rows[0]]
    return [{h: (r[i] if i < len(r) else None) for i, h in enumerate(head) if h is not None} for r in rows[1:]]


def _same(a, b) -> bool:
    if isinstance(a, bool) or isinstance(b, bool):
        norm = lambda x: x if isinstance(x, bool) else {"true": True, "false": False}.get(str(x).strip().lower(), x)
        return norm(a) == norm(b)
    if isinstance(a, (int, float)) and isinstance(b, (int, float)):
        return abs(a - b) < 1e-9
    return a == b


def three_way_consistency(csv_rows: list, js: dict, xlsx_records: list) -> dict:
    """Compare CSV, JSON and Excel record-by-record on record_id over every common column. Nothing is corrected."""
    sources = {"csv": {r["record_id"]: r for r in csv_rows}, "json": {r["record_id"]: r for r in js.get("records", [])},
               "xlsx": {r.get("record_id"): r for r in xlsx_records if r.get("record_id") is not None}}
    pairs = {"csv_vs_json": ("csv", "json"), "csv_vs_xlsx": ("csv", "xlsx"), "json_vs_xlsx": ("json", "xlsx")}
    result, rows = {}, []
    for name, (a, b) in pairs.items():
        A, B = sources[a], sources[b]
        common_cols = sorted(set(next(iter(A.values()))) & set(next(iter(B.values())))) if A and B else []
        diffs = []
        for rid in sorted(set(A) | set(B)):
            if rid not in A or rid not in B:
                diffs.append({"record_id": rid, "column": "*", "issue": f"missing in {a if rid not in A else b}"})
                continue
            for col in common_cols:
                if not _same(A[rid].get(col), B[rid].get(col)):
                    diffs.append({"record_id": rid, "column": col})
        result[name] = {"status": "MATCH" if not diffs else "MISMATCH", "records": [len(A), len(B)], "common_columns": len(common_cols),
                        "differences": len(diffs),
                        "columns_only_in_" + a: sorted(set(next(iter(A.values()), {})) - set(next(iter(B.values()), {}))),
                        "columns_only_in_" + b: sorted(set(next(iter(B.values()), {})) - set(next(iter(A.values()), {})))}
        for d in diffs:
            rid, col = d["record_id"], d["column"]
            rows.append({"comparison": name, "record_id": rid, "column": col, "csv_value": sources["csv"].get(rid, {}).get(col),
                         "json_value": sources["json"].get(rid, {}).get(col), "excel_value": sources["xlsx"].get(rid, {}).get(col),
                         "issue": d.get("issue", "value differs")})
    return {"summary": result, "differences": rows}


def _type_name(v) -> str:
    if v is None: return "null"
    if isinstance(v, bool): return "bool"
    if isinstance(v, int): return "int"
    if isinstance(v, float): return "float"
    return "str"


def numeric_stats(vals: list) -> dict:
    nums = [v for v in vals if isinstance(v, (int, float)) and not isinstance(v, bool)]
    if not nums:
        return {"count": 0}
    return {"count": len(nums), "mean": round(statistics.fmean(nums), 4), "median": statistics.median(nums), "min": min(nums),
            "max": max(nums), "std": round(statistics.stdev(nums), 4) if len(nums) > 1 else 0.0}


def validity_problems(rows: list) -> list:
    """Domain checks on typed rows: negative costs, percentages outside 0-100, unknown labels, synthetic flag."""
    out = []
    for i, r in enumerate(rows, start=1):
        rid = r.get("record_id")
        for c in NON_NEGATIVE_MONEY:
            if isinstance(r.get(c), (int, float)) and r[c] < 0:
                out.append({"row": i, "record_id": rid, "column": c, "problem": "negative_cost", "value": r[c]})
        for c in PCT_COLS:
            if isinstance(r.get(c), (int, float)) and not 0 <= r[c] <= 100:
                out.append({"row": i, "record_id": rid, "column": c, "problem": "pct_out_of_range", "value": r[c]})
        for c, allowed in ALLOWED.items():
            if r.get(c) is not None and r[c] not in allowed:
                out.append({"row": i, "record_id": rid, "column": c, "problem": "unknown_label", "value": r[c]})
        if r.get("synthetic") is not True:
            out.append({"row": i, "record_id": rid, "column": "synthetic", "problem": "not_labelled_synthetic", "value": r.get("synthetic")})
    return out


def profile(rows: list, columns: list) -> dict:
    ids = [r.get("record_id") or r.get("id") for r in rows]
    full = [json.dumps(r, sort_keys=True) for r in rows]
    no_id = [json.dumps({k: v for k, v in r.items() if k not in ("record_id", "id")}, sort_keys=True) for r in rows]
    types = {}
    for c in columns:
        seen = sorted({_type_name(r.get(c)) for r in rows})
        types[c] = seen[0] if len(seen) == 1 else "mixed(" + ",".join(seen) + ")"
    numeric = {c: numeric_stats([r.get(c) for r in rows]) for c in columns if types[c] in ("int", "float", "mixed(float,int)")}
    categorical = {c: dict(sorted(Counter(str(r.get(c)) for r in rows).items())) for c in columns
                   if c not in numeric and len({str(r.get(c)) for r in rows}) <= 25}
    return {
        "rows": len(rows), "columns": len(columns), "column_names": columns, "data_types": types,
        "first_5_rows": rows[:5], "summary_statistics": numeric, "categorical_distributions": categorical,
        "duplicate_ids": sorted(k for k, n in Counter(ids).items() if n > 1),
        "duplicate_rows": sum(n - 1 for n in Counter(full).values() if n > 1),
        "duplicate_rows_ignoring_id": sum(n - 1 for n in Counter(no_id).values() if n > 1),
        "null_values_per_column": {c: sum(r.get(c) is None for r in rows) for c in columns if any(r.get(c) is None for r in rows)},
    }


def csv_json_consistency(csv_rows: list, js: dict) -> dict:
    """Field-by-field equality of the CSV and JSON representations (after typing)."""
    recs = js.get("records", [])
    by_id = {r.get("record_id"): r for r in recs}
    diffs = []
    for r in csv_rows:
        j = by_id.get(r["record_id"])
        if j is None:
            diffs.append({"record_id": r["record_id"], "column": "*", "csv": "present", "json": "missing"})
            continue
        for c, v in r.items():
            jv = j.get(c)
            same = (v == jv) if not (isinstance(v, (int, float)) and isinstance(jv, (int, float))) else abs(v - jv) < 1e-9
            if not same:
                diffs.append({"record_id": r["record_id"], "column": c, "csv": v, "json": jv})
    extra = sorted(set(by_id) - {r["record_id"] for r in csv_rows})
    meta = {k: v for k, v in js.items() if k != "records"}
    return {"json_record_count": len(recs), "json_declared_record_count": js.get("record_count"), "json_metadata": meta,
            "records_only_in_json": extra, "field_differences": len(diffs), "differences_sample": diffs[:20],
            "columns_only_in_json": sorted({k for r in recs for k in r} - set(csv_rows[0]) if csv_rows else set()),
            "consistent": not diffs and not extra and len(recs) == len(csv_rows)}


def workbook_checks(csv_rows: list, fx: dict, audit_counts: dict | None = None) -> dict:
    """Excel-only checks: Original_Fixture sheet vs fixture.json, Summary sheet claims, Data_Dictionary validity rules."""
    from fractions import Fraction
    wb = read_xlsx()
    out = {"sheets": {k: len(v) for k, v in wb.items()}}
    # Original_Fixture: fixture fields must equal fixture.json; derived columns must equal a recalculation.
    sheet_fx = sheet_records(wb.get("Original_Fixture", [[]]))
    official = {p["id"]: p for p in fixture_pilots(fx)}
    diffs = []
    for r in sheet_fx:
        p = official.get(r.get("id"))
        if p is None:
            diffs.append({"id": r.get("id"), "column": "*", "issue": "not in fixture.json"})
            continue
        for k, v in p.items():
            if not _same(r.get(k), v):
                diffs.append({"id": r["id"], "column": k, "sheet": r.get(k), "fixture_json": v})
        net = Fraction(p["annual_benefit_inr"]) - Fraction(p["annual_running_cost_inr"])
        calc = {"net_annual_benefit_inr": net, "monthly_net_benefit_inr": net / 12, "payback_months": Fraction(p["setup_cost_inr"]) / (net / 12),
                "roi_pct": net / Fraction(p["setup_cost_inr"]) * 100}
        for k, v in calc.items():
            if r.get(k) is None or abs(Fraction(str(r[k])) - v) > Fraction(5, 1000):
                diffs.append({"id": r["id"], "column": k, "sheet": r.get(k), "recalculated": float(v)})
    out["original_fixture_sheet"] = {"rows": len(sheet_fx), "fixture_json_rows": len(official), "differences": diffs,
                                     "status": "MATCH" if not diffs and len(sheet_fx) == len(official) else "MISMATCH"}
    # Summary sheet claims vs what the data actually contains.
    claims = {r[0]: r[1] for r in wb.get("Summary", []) if len(r) > 1 and isinstance(r[0], str)}
    decision_claims = {r[3]: r[4] for r in wb.get("Summary", []) if len(r) > 4 and r[3] in ("PROCEED", "REVISE", "STOP", "REVIEW")}
    actual_decisions = Counter(r["decision"] for r in csv_rows)
    checks = [("Original fixture rows", claims.get("Original fixture rows"), len(official)),
              ("Synthetic comparison rows", claims.get("Synthetic comparison rows"), len(csv_rows))]
    checks += [(f"{d} count", n, actual_decisions.get(d, 0)) for d, n in decision_claims.items()]
    checks += [(k, claims.get(k), v) for k, v in (audit_counts or {}).items()]
    ai1 = official["AI1"]
    pay = Fraction(ai1["setup_cost_inr"]) / ((Fraction(ai1["annual_benefit_inr"]) - Fraction(ai1["annual_running_cost_inr"])) / 12)
    checks.append(("Official AI1 payback", claims.get("Official AI1 payback"), f"{pay} months"))
    out["summary_sheet_claims"] = [{"claim": c, "workbook": w, "recalculated": a, "match": str(w) == str(a)} for c, w, a in checks]
    # Data_Dictionary rules that can be checked mechanically.
    dic = {r.get("Column"): r.get("Validity rule") for r in sheet_records(wb.get("Data_Dictionary", [[]]))}
    rule_checks = []
    for col, rule in sorted(dic.items(), key=lambda x: str(x[0])):
        if rule == "Positive numeric":
            bad = [r["record_id"] for r in csv_rows if not (isinstance(r.get(col), (int, float)) and r[col] > 0)]
        elif rule == "0-100":
            bad = [r["record_id"] for r in csv_rows if not (isinstance(r.get(col), (int, float)) and 0 <= r[col] <= 100)]
        elif rule == "Always present and unique":
            ids = [r.get(col) for r in csv_rows]
            bad = sorted({i for i in ids if i is None or ids.count(i) > 1}, key=str)
        else:
            continue
        rule_checks.append({"column": col, "rule": rule, "violations": len(bad), "examples": bad[:5]})
    out["data_dictionary_rules"] = rule_checks
    out["data_dictionary_has_readiness_rule"] = any("readiness" in str(c) and "low" in str(v).lower() for c, v in dic.items())
    out["status"] = "PASS" if out["original_fixture_sheet"]["status"] == "MATCH" and all(c["match"] for c in out["summary_sheet_claims"]) \
        and all(r["violations"] == 0 for r in rule_checks) else "FAIL"
    return out


def audit_all() -> dict:
    """Complete read-only audit of every input file. Returns a JSON-serialisable dict."""
    fx = load_fixture()
    fx_rows = fixture_pilots(fx)
    fx_cols = []
    for r in fx_rows:
        fx_cols += [k for k in r if k not in fx_cols]
    rows, columns, parse_problems = load_synthetic_csv()
    validity = validity_problems(rows)
    js = load_synthetic_json()
    missing = sum(p["problem"] == "missing" for p in parse_problems)
    nulls = sum(p["problem"] == "null_token" for p in parse_problems)
    invalid_rows = sorted({p["record_id"] for p in parse_problems + validity if p["problem"] not in ("missing",)})
    return {
        "note": "SYNTHETIC dataset = dummy data for testing/comparison. Official fixture = reference inputs. Neither is real-world evidence.",
        "files": {
            "official_fixture": {"path": "data/C23/fixture.json (delivered as fixture(3).json)", "sha256": sha256(FIXTURE_PATH),
                                 "identical_to_protected_copy": sha256(FIXTURE_PATH) == sha256(PROTECTED_FIXTURE),
                                 "protected_copy": "data/raw/official_fixture.json"},
            "synthetic_csv": {"path": "data/C23/" + SYNTH_CSV.name, "sha256": sha256(SYNTH_CSV)},
            "synthetic_json": {"path": "data/C23/" + SYNTH_JSON.name, "sha256": sha256(SYNTH_JSON)},
            "synthetic_xlsx": {"path": "data/C23/" + SYNTH_XLSX.name, "present": SYNTH_XLSX.exists(),
                               "sha256": sha256(SYNTH_XLSX) if SYNTH_XLSX.exists() else None,
                               "note": "Not found in the project; Excel consistency check NOT RUN." if not SYNTH_XLSX.exists() else "present"},
        },
        "file_consistency": (three_way_consistency(rows, js, sheet_records(read_xlsx()["Synthetic_Comparison"]))
                             if SYNTH_XLSX.exists() else {"summary": {"csv_vs_json": csv_json_consistency(rows, js)}, "differences": [],
                                                          "note": "xlsx missing: only CSV vs JSON compared"}),
        "workbook": (workbook_checks(rows, fx, {"Synthetic missing values": missing, "Synthetic null values": nulls,
                                                "Synthetic invalid values": len(invalid_rows)})
                     if SYNTH_XLSX.exists() else {"status": "NOT RUN", "reason": "xlsx missing"}),
        "official_fixture": {**profile(fx_rows, fx_cols), "metadata": {k: v for k, v in fx.items() if k != "records"},
                             "expected_first_pilot_payback_months": fx["records"].get("expected_first_pilot_payback_months")},
        "synthetic_csv": {**profile(rows, columns), "missing_values": missing, "null_values": nulls,
                          "parse_problems": parse_problems, "validity_problems": validity,
                          "nan_values": sum(p["problem"] == "nan" for p in parse_problems),
                          "infinity_values": sum(p["problem"] == "infinity" for p in parse_problems),
                          "negative_costs": sum(p["problem"] == "negative_cost" for p in validity),
                          "pct_out_of_range": sum(p["problem"] == "pct_out_of_range" for p in validity),
                          "invalid_rows": len(invalid_rows), "invalid_record_ids": invalid_rows,
                          "expectation": {"rows": 120, "missing": 0, "null": 0, "invalid": 0},
                          "meets_expectation": len(rows) == 120 and missing == 0 and nulls == 0 and not invalid_rows},
        "synthetic_json_vs_csv": csv_json_consistency(rows, js),
    }
