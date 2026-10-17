"""Read-only loaders for pilot tables (JSON fixture shape, bare JSON list, or CSV).

Loaders never coerce questionable values: a CSV cell becomes a number only if it is a plain decimal
literal; anything else ("2 lakh", "nan", "1e9") stays a string so validation can reject it.
Empty CSV cells become None (unknown), never 0.
"""
from __future__ import annotations
import csv, hashlib, io, json, re
from pathlib import Path
from typing import Any

_DECIMAL = re.compile(r"-?\d+(\.\d+)?")


class LoadError(ValueError):
    """Input file cannot be parsed into a list of row objects."""
    def __init__(self, code: str, message: str):
        super().__init__(message)
        self.code = code


def sha256_file(path: Path) -> str:
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def _csv_cell(s: str) -> Any:
    s = s.strip()
    if s == "":
        return None
    if _DECIMAL.fullmatch(s):
        return float(s) if "." in s else int(s)
    return s


def parse_csv(text: str) -> list[dict]:
    rows = list(csv.reader(io.StringIO(text, newline="")))
    rows = [r for r in rows if any(c.strip() for c in r)]
    if not rows:
        raise LoadError("EMPTY_FILE", "CSV has no header row")
    header = [h.strip() for h in rows[0]]
    if any(h == "" for h in header):
        raise LoadError("MALFORMED_CSV", "CSV header has an empty column name")
    if len(set(header)) != len(header):
        raise LoadError("MALFORMED_CSV", "CSV header has duplicate column names")
    out = []
    for n, r in enumerate(rows[1:], start=2):
        if len(r) != len(header):
            raise LoadError("MALFORMED_CSV", f"CSV line {n} has {len(r)} cells, header has {len(header)}")
        out.append({h: _csv_cell(c) for h, c in zip(header, r)})
    return out


def parse_json(text: str) -> tuple[list, dict]:
    """Returns (rows, meta). Accepts {"records": {"pilots": [...]}, ...} or a bare list.
    `rows` may contain non-dict entries; validation reports those."""
    try:
        obj = json.loads(text)
    except json.JSONDecodeError as e:
        raise LoadError("MALFORMED_JSON", f"JSON parse error at line {e.lineno} col {e.colno}: {e.msg}") from None
    if isinstance(obj, list):
        return obj, {}
    if isinstance(obj, dict):
        pilots = (obj.get("records") or {}).get("pilots") if isinstance(obj.get("records"), dict) else None
        if isinstance(pilots, list):
            return pilots, {k: v for k, v in obj.items() if k != "records"}
        if isinstance(obj.get("pilots"), list):   # extension.json shape
            return obj["pilots"], {k: v for k, v in obj.items() if k != "pilots"}
    raise LoadError("FILE_SHAPE", "expected records.pilots array, pilots array, or a top-level array")


def load_table(path: Path) -> tuple[list, dict, str]:
    """Returns (rows, meta, format). Raises LoadError on unreadable input."""
    path = Path(path)
    try:
        text = path.read_text(encoding="utf-8-sig")
    except FileNotFoundError:
        raise LoadError("FILE_NOT_FOUND", f"{path} not found") from None
    except UnicodeDecodeError:
        raise LoadError("BAD_ENCODING", f"{path} is not UTF-8") from None
    if path.suffix.lower() == ".csv":
        return parse_csv(text), {}, "csv"
    rows, meta = parse_json(text)
    return rows, meta, "json"
