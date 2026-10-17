"""Reproducible preprocessing: raw fixture + team extension -> data/processed/pilots_clean*.{json,csv}.

Usage (from decision-engine/):
    python -m scripts.preprocess_data                      # writes strict AND scenario outputs
    python -m scripts.preprocess_data --mode strict

strict   -> pilots_clean.json / pilots_clean.csv           (unknowns stay unknown; the honest default)
scenario -> pilots_clean_scenario.json / .csv              (documented TEAM_ASSUMPTION imputations, flagged)

Raw inputs are only read. Output is deterministic (no timestamps): same inputs => byte-identical files.
Exit code 2 if an input cannot be parsed.
"""
from __future__ import annotations
import argparse, csv, io, json, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from app.io_utils import LoadError, load_table, sha256_file                     # noqa: E402
from app.pipeline import FLAG_TEXT, MODES, PIPELINE_VERSION, CleanPilot, prepare  # noqa: E402

DEFAULT_FIXTURE = ROOT / "data" / "raw" / "official_fixture.json"
DEFAULT_EXTENSION = ROOT / "data" / "raw" / "extension.json"
CSV_COLUMNS = list(CleanPilot.model_fields)


def _csv(pilots: list) -> str:
    buf = io.StringIO(newline="")
    w = csv.DictWriter(buf, fieldnames=CSV_COLUMNS, lineterminator="\n")
    w.writeheader()
    for p in pilots:
        row = p.model_dump()
        row["flags"] = ";".join(row["flags"])
        row["field_sources"] = json.dumps(row["field_sources"], sort_keys=True)
        row["assumptions"] = json.dumps(row["assumptions"], sort_keys=True)
        w.writerow({k: "" if v is None else v for k, v in row.items()})
    return buf.getvalue()


def run(fixture: Path, extension: Path | None, mode: str) -> dict:
    fx_rows, fx_meta, _ = load_table(fixture)
    ext_rows, ext_meta, _ = load_table(extension) if extension else ([], {}, None)
    prep = prepare(fx_rows, ext_rows, ext_meta, mode)
    used = sorted({f for p in prep.pilots for f in p.flags})
    return {
        "pipeline_version": PIPELINE_VERSION,
        "mode": mode,
        "synthetic": fx_meta.get("synthetic"),
        "inputs": [{"role": "fixture", "file": fixture.name, "sha256": sha256_file(fixture)}]
                  + ([{"role": "extension", "file": extension.name, "sha256": sha256_file(extension)}] if extension else []),
        "summary": prep.summary(),
        "budget_inr": prep.budget,
        "budget_issue": prep.budget_issue.to_dict() if prep.budget_issue else None,
        "defaults_used": prep.defaults,
        "flag_glossary": {f: FLAG_TEXT[f] for f in used},
        "pilots": [p.model_dump() for p in prep.pilots],
        "rejected": prep.rejected,
        "issues": [i.to_dict() for i in prep.issues],
    }, prep


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--fixture", type=Path, default=DEFAULT_FIXTURE)
    ap.add_argument("--extension", type=Path, default=DEFAULT_EXTENSION)
    ap.add_argument("--no-extension", action="store_true")
    ap.add_argument("--mode", choices=(*MODES, "both"), default="both")
    ap.add_argument("--out-dir", type=Path, default=ROOT / "data" / "processed")
    a = ap.parse_args(argv)
    a.out_dir.mkdir(parents=True, exist_ok=True)
    for mode in (MODES if a.mode == "both" else (a.mode,)):
        try:
            doc, prep = run(a.fixture, None if a.no_extension else a.extension, mode)
        except LoadError as e:
            print(json.dumps({"error": e.code, "message": str(e)}), file=sys.stderr)
            return 2
        stem = "pilots_clean" if mode == "strict" else "pilots_clean_scenario"
        (a.out_dir / f"{stem}.json").write_text(json.dumps(doc, indent=2, allow_nan=False) + "\n", encoding="utf-8", newline="\n")
        (a.out_dir / f"{stem}.csv").write_text(_csv(prep.pilots), encoding="utf-8", newline="")
        s = doc["summary"]
        print(f"{mode}: {s['valid']} valid ({s['complete']} complete, {s['incomplete']} incomplete), "
              f"{s['rejected']} rejected -> {a.out_dir / stem}.json/.csv")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
