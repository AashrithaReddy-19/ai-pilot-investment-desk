"""Read-only data audit for C23 pilot inputs. Never modifies data.

Usage (from decision-engine/):
    python -m scripts.audit_data                                   # official fixture + team extension
    python -m scripts.audit_data --fixture my.csv --extension x.json --out-dir reports

Writes <out-dir>/data_audit.json and <out-dir>/data_audit.md. Output is deterministic (no timestamps):
the same inputs always give byte-identical reports. Exit code 2 if an input cannot be parsed.
"""
from __future__ import annotations
import argparse, json, math, statistics, sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from app.io_utils import LoadError, load_table, sha256_file           # noqa: E402
from app.schemas import EXTENSION_FIELDS, PILOT_FIELDS, safe_value     # noqa: E402
from app.pipeline import prepare                                       # noqa: E402

AUDIT_VERSION = "1.0.0"
DEFAULT_FIXTURE = ROOT / "data" / "raw" / "official_fixture.json"
DEFAULT_EXTENSION = ROOT / "data" / "raw" / "extension.json"
CATEGORICAL_MAX_DISTINCT = 25
TARGET_COLUMNS = ("adoption_success", "quality_success")


def _type_name(v) -> str:
    if v is None: return "null"
    if isinstance(v, bool): return "bool"
    if isinstance(v, int): return "int"
    if isinstance(v, float): return "float" if math.isfinite(v) else "non_finite_float"
    if isinstance(v, str): return "str"
    return type(v).__name__


def _finite(v) -> bool:
    return isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v)


def _stats(nums: list) -> dict:
    if not nums:
        return {}
    return {"count": len(nums), "min": min(nums), "max": max(nums), "mean": round(statistics.fmean(nums), 4),
            "median": statistics.median(nums), "distinct": len(set(nums))}


def _row_key(r) -> str:
    return json.dumps(r, sort_keys=True, default=repr) if isinstance(r, dict) else repr(r)


def profile_table(rows: list, specs) -> dict:
    n = len(rows)
    dict_rows = [r for r in rows if isinstance(r, dict)]
    columns: list = []
    for r in dict_rows:
        columns += [k for k in r if k not in columns]
    expected = [s.name for s in specs]
    kinds = {s.name: s.kind for s in specs}
    cols = {}
    for c in columns:
        vals = [r[c] for r in dict_rows if c in r]
        absent, nulls = len(dict_rows) - len(vals), sum(v is None for v in vals)
        types = Counter(_type_name(v) for v in vals)
        info = {"in_schema": c in expected, "kind": kinds.get(c, "unknown"), "present": len(vals), "absent": absent,
                "null": nulls, "missing_pct": round(100 * (absent + nulls) / n, 2) if n else 0.0,
                "types": dict(sorted(types.items()))}
        nums = [v for v in vals if _finite(v)]
        if nums:
            info["numeric"] = _stats(nums)
        strs = [v for v in vals if isinstance(v, str)]
        if strs and len(set(strs)) <= CATEGORICAL_MAX_DISTINCT:
            info["distribution"] = dict(sorted(Counter(strs).items()))
        cols[c] = info
    ids = [r.get("id") for r in dict_rows if isinstance(r.get("id"), str)]
    id_counts = Counter(i.strip().lower() for i in ids)
    row_counts = Counter(_row_key(r) for r in rows)
    return {
        "row_count": n,
        "non_object_rows": n - len(dict_rows),
        "columns": columns,
        "schema_columns_missing_everywhere": [c for c in expected if c not in columns],
        "unknown_columns": [c for c in columns if c not in expected],
        "column_profiles": cols,
        "duplicate_rows": sum(k - 1 for k in row_counts.values() if k > 1),
        "duplicate_ids": sorted(i for i, k in id_counts.items() if k > 1),
        "rows_missing_id": len(dict_rows) - len(ids),
    }


def rule_checks(rows: list, specs, table: str) -> dict:
    kinds = {s.name: s.kind for s in specs}
    out = {"negative_values": [], "pct_out_of_range": [], "non_numeric_in_numeric_field": [], "non_finite": []}
    for i, r in enumerate(rows):
        if not isinstance(r, dict):
            continue
        for k, v in r.items():
            kind = kinds.get(k)
            if kind not in ("money", "pct") or v is None:
                continue
            ref = {"table": table, "row": i, "id": safe_value(r.get("id")), "field": k, "value": safe_value(v)}
            if isinstance(v, bool) or not isinstance(v, (int, float)):
                out["non_numeric_in_numeric_field"].append(ref)
            elif not math.isfinite(v):
                out["non_finite"].append(ref)
            elif v < 0:
                out["negative_values"].append(ref)
            elif kind == "pct" and v > 100:
                out["pct_out_of_range"].append(ref)
    return out


def audit(fixture_path: Path, extension_path: Path | None) -> dict:
    fx_rows, fx_meta, fx_fmt = load_table(fixture_path)
    ext_rows, ext_meta, ext_fmt = (load_table(extension_path) if extension_path else ([], {}, None))
    inputs = [{"role": "fixture", "file": fixture_path.name, "format": fx_fmt, "sha256": sha256_file(fixture_path)}]
    if extension_path:
        inputs.append({"role": "extension", "file": extension_path.name, "format": ext_fmt, "sha256": sha256_file(extension_path)})

    fx = profile_table(fx_rows, PILOT_FIELDS)
    ext = profile_table(ext_rows, EXTENSION_FIELDS)
    checks = {k: a + b for (k, a), (_, b) in zip(rule_checks(fx_rows, PILOT_FIELDS, "fixture").items(),
                                                 rule_checks(ext_rows, EXTENSION_FIELDS, "extension").items())}

    fx_dicts = [r for r in fx_rows if isinstance(r, dict)]
    groups: dict = {}
    for r in fx_dicts:
        g = r.get("overlap_group")
        if isinstance(g, str) and g.strip():
            key = g.strip().lower()
        else:
            key = "(unknown)" if g is None or isinstance(g, str) else f"(invalid {safe_value(g)!r})"
        groups.setdefault(key, []).append(safe_value(r.get("id")))
    fx_ids = {safe_value(r.get("id")) for r in fx_dicts}
    ext_ids = {safe_value(r.get("id")) for r in ext_rows if isinstance(r, dict)}

    targets = {c: dict(Counter(_type_name(r.get(c)) if r.get(c) is None else str(r.get(c)) for r in fx_dicts))
               for c in TARGET_COLUMNS if any(c in r for r in fx_dicts)}

    previews = {}
    for mode in ("strict", "scenario"):
        prep = prepare(fx_rows, ext_rows, ext_meta, mode)
        codes = Counter(i.code for i in prep.issues)
        for rj in prep.rejected:
            codes.update(i["code"] for i in rj["issues"])
        previews[mode] = {**prep.summary(), "issue_codes": dict(sorted(codes.items())),
                          "rejected_ids": [r["id"] for r in prep.rejected],
                          "flags": {p.id: p.flags for p in prep.pilots},
                          "budget_issue": prep.budget_issue.to_dict() if prep.budget_issue else None}

    meta_keys = ("case_id", "synthetic", "data_version", "currency", "missing_values", "limit")
    report = {
        "audit_version": AUDIT_VERSION,
        "inputs": inputs,
        "dataset_meta": {k: fx_meta.get(k) for k in meta_keys if k in fx_meta},
        "extension_meta": {k: safe_value(v) if not isinstance(v, dict) else v for k, v in ext_meta.items()},
        "tables": {"fixture_pilots": fx, "extension_pilots": ext},
        "rule_checks": checks,
        "overlap_groups": dict(sorted(groups.items())),
        "shared_overlap_groups": {g: ids for g, ids in sorted(groups.items()) if len(ids) > 1 and g != "(unknown)"},
        "id_cross_check": {"extension_ids_without_pilot": sorted(map(str, ext_ids - fx_ids)),
                           "pilot_ids_without_extension": sorted(map(str, fx_ids - ext_ids))},
        "target_class_distribution": targets or None,
        "validation_preview": previews,
    }
    report["observations"] = observations(report)
    return report


def observations(r: dict) -> list:
    obs = []
    fx = r["tables"]["fixture_pilots"]
    meta = r["dataset_meta"]
    if meta.get("synthetic") is True:
        obs.append("Dataset is declared synthetic: a teaching fixture, not a benchmark. No statistical claims are made from it.")
    obs.append(f"{fx['row_count']} pilot rows; ML training/CV on these rows would be meaningless.")
    for c in ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr"):
        num = fx["column_profiles"].get(c, {}).get("numeric")
        if num and num["distinct"] == 1 and num["count"] > 1:
            obs.append(f"{c} is identical for all {num['count']} pilots ({num['min']:g}): official economics do not "
                       "differentiate pilots; ranking depends on team assumptions and the documented tie-break.")
    for g, ids in r["shared_overlap_groups"].items():
        obs.append(f"Overlap group '{g}' is shared by {', '.join(ids)}: DOUBLE_COUNT_RISK; benefit may be counted only once.")
    if "(unknown)" in r["overlap_groups"]:
        obs.append(f"Overlap group unknown for {', '.join(r['overlap_groups']['(unknown)'])}: must not be treated as 'no overlap'.")
    bt = fx["column_profiles"].get("benefit_type", {}).get("distribution", {})
    for k, n in bt.items():
        if "assumption" in k.lower():
            obs.append(f"benefit_type '{k}' ({n} rows): cashability is itself an assumption, not proven cash removal.")
    for k in ("negative_values", "pct_out_of_range", "non_numeric_in_numeric_field", "non_finite"):
        if r["rule_checks"][k]:
            obs.append(f"{len(r['rule_checks'][k])} {k.replace('_', ' ')} found (see rule_checks).")
    if fx["duplicate_ids"]:
        obs.append(f"Duplicate ids: {', '.join(fx['duplicate_ids'])} (all occurrences will be rejected).")
    if r["target_class_distribution"] is None:
        obs.append("No training labels (adoption_success / quality_success) present: no target class distribution.")
    ext = r["tables"]["extension_pilots"]["column_profiles"]
    for c in ("observed_adoption_pct", "observed_quality_pct"):
        if c in ext and ext[c]["present"] and ext[c]["null"] == ext[c]["present"]:
            obs.append(f"{c} is null for every pilot: NOT_MEASURED (unknown, not zero).")
    return obs


def to_markdown(r: dict) -> str:
    L = ["# C23 Data Audit", "", "> Read-only audit. Synthetic teaching fixture; not a benchmark dataset.", "",
         f"Audit version {r['audit_version']}.", "", "## Inputs", "", "| Role | File | Format | SHA-256 |", "|---|---|---|---|"]
    L += [f"| {i['role']} | {i['file']} | {i['format']} | `{i['sha256'][:16]}…` |" for i in r["inputs"]]
    if r["dataset_meta"]:
        L += ["", "## Dataset metadata", ""] + [f"- **{k}**: {v}" for k, v in r["dataset_meta"].items()]
    L += ["", "## Key observations", ""] + [f"- {o}" for o in r["observations"]]
    for name, t in r["tables"].items():
        L += ["", f"## Table `{name}`", "", f"- Rows: {t['row_count']} (non-object rows: {t['non_object_rows']})",
              f"- Duplicate rows: {t['duplicate_rows']}; duplicate ids: {', '.join(t['duplicate_ids']) or 'none'}",
              f"- Unknown columns: {', '.join(t['unknown_columns']) or 'none'}", "",
              "| Column | Kind | Types | Absent | Null | Missing % | Numeric (min / median / max) | Distribution |",
              "|---|---|---|---|---|---|---|---|"]
        for c, p in t["column_profiles"].items():
            num = p.get("numeric")
            ns = f"{num['min']:g} / {num['median']:g} / {num['max']:g}" if num else "-"
            dist = ", ".join(f"{k}: {v}" for k, v in p.get("distribution", {}).items()) if c not in ("id", "task", "description") else "-"
            types = ", ".join(f"{k}:{v}" for k, v in p["types"].items())
            L.append(f"| {c} | {p['kind']} | {types} | {p['absent']} | {p['null']} | {p['missing_pct']} | {ns} | {dist or '-'} |")
    L += ["", "## Rule checks", ""]
    for k, v in r["rule_checks"].items():
        L.append(f"- {k.replace('_', ' ')}: {len(v)}" + ("" if not v else " — " + "; ".join(f"{x['table']} row {x['row']} {x['field']}={x['value']}" for x in v)))
    L += ["", "## Overlap groups", ""] + [f"- {g}: {', '.join(map(str, ids))}" for g, ids in r["overlap_groups"].items()]
    L += ["", "## Validation preview (read-only)", ""]
    for mode, p in r["validation_preview"].items():
        L.append(f"- **{mode}**: {p['valid']} valid ({p['complete']} complete, {p['incomplete']} incomplete), {p['rejected']} rejected"
                 + (f" ({', '.join(map(str, p['rejected_ids']))})" if p["rejected_ids"] else "")
                 + (f"; issue codes: {', '.join(f'{k}×{v}' for k, v in p['issue_codes'].items())}" if p["issue_codes"] else ""))
    L += ["", "## Target class distribution", "", "- " + (json.dumps(r["target_class_distribution"]) if r["target_class_distribution"] else "none (no training labels)")]
    return "\n".join(L) + "\n"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--fixture", type=Path, default=DEFAULT_FIXTURE)
    ap.add_argument("--extension", type=Path, default=DEFAULT_EXTENSION)
    ap.add_argument("--no-extension", action="store_true")
    ap.add_argument("--out-dir", type=Path, default=ROOT / "reports")
    a = ap.parse_args(argv)
    try:
        report = audit(a.fixture, None if a.no_extension else a.extension)
    except LoadError as e:
        print(json.dumps({"error": e.code, "message": str(e)}), file=sys.stderr)
        return 2
    a.out_dir.mkdir(parents=True, exist_ok=True)
    (a.out_dir / "data_audit.json").write_text(json.dumps(report, indent=2, allow_nan=False) + "\n", encoding="utf-8", newline="\n")
    (a.out_dir / "data_audit.md").write_text(to_markdown(report), encoding="utf-8", newline="\n")
    print(f"audit written to {a.out_dir / 'data_audit.json'} and data_audit.md")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
