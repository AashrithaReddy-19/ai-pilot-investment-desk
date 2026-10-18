"""CSV/XLSX ingest: the balance-sheet parser (Note A point 4) and the proposal importer (Note A point 5).

Pure functions over uploaded bytes. Nothing is stored — parse returns candidates for the user
to confirm, confirm re-parses the same upload. Missing is never zero: an unparsable amount is
None plus a warning, never 0.
"""
from __future__ import annotations
import csv
import io
import math
import re
from typing import Any, Optional

from . import xlsx

MAX_UPLOAD_BYTES = 2_000_000

# Period normalisation: amounts are annualised from the period their header names.
PERIOD_FACTORS = {"annual": 1, "half_year": 2, "quarterly": 4, "monthly": 12}
# Month names are deliberately NOT hints: "31 Mar 2024" on a balance sheet is a year-end date.
PERIOD_HEADER_HINTS = [("monthly", re.compile(r"month|\bpm\b|/\s*mo\b", re.I)),
                       ("quarterly", re.compile(r"quarter|\bqtr\b|\bq[1-4]\b", re.I)),
                       ("half_year", re.compile(r"half|\bh[12]\b", re.I)),
                       ("annual", re.compile(r"annual|year|\bfy|\bp\.?a\.?\b", re.I))]

# Note A point 4: candidates matched only by keyword; the user confirms which rows are AI spend.
BALANCE_KEYWORDS = ["ai", "software", "cloud", "licence", "license", "token"]
BALANCE_KEYWORD_RE = re.compile(r"\b(?:ai|software|cloud|licen[cs]e|tokens?)\b", re.IGNORECASE)

UNIT_MULTIPLIERS = {"rupees": 1, "thousands": 1_000, "lakh": 100_000, "crore": 10_000_000}
# Detection order matters: a header saying "₹ lakh" carries both hints; the biggest unit wins.
UNIT_HEADER_HINTS = [("crore", re.compile(r"crore", re.I)),
                     ("lakh", re.compile(r"lakh|lac\b", re.I)),
                     ("thousands", re.compile(r"thousand|'000|\bk\b", re.I)),
                     ("rupees", re.compile(r"₹|inr|rupee", re.I))]
# Inline suffixes on a value ("2.5 cr") win over the column multiplier.
_INLINE_SUFFIX = [(re.compile(rf"(?:{pat})$", re.IGNORECASE), mult) for pat, mult in
                  (("crore|cr", 10_000_000), ("lakh|lac|L", 100_000), ("thousand|k", 1_000))]

_BLANKS = {"", "-", "\u2013", "\u2014", "n/a", "na", "nil", "not available"}


class ParseError(ValueError):
    """Unusable upload or bad choice: the gateway shows it as a 422."""


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def _decode(raw: bytes) -> str:
    """UTF-8 with BOM fallback; NUL bytes / undecodable bytes are rejected as the wrong type."""
    if len(raw) > MAX_UPLOAD_BYTES:
        raise ParseError(f"Upload exceeds the {MAX_UPLOAD_BYTES:,}-byte limit.")
    if b"\x00" in raw:
        raise ParseError("The file looks binary (contains NUL bytes), not a CSV text file.")
    try:
        return raw.decode("utf-8-sig")  # handles a leading BOM transparently
    except UnicodeDecodeError as exc:
        raise ParseError("The file is not UTF-8 text; save it as CSV UTF-8 and retry.") from exc


def _delimiter(text: str) -> str:
    head = "\n".join(text.splitlines()[:5])
    counts = {",": head.count(","), ";": head.count(";"), "\t": head.count("\t")}
    best = max(counts, key=lambda k: (counts[k], k == ","))
    return best if counts[best] else ","


def _rows(text: str) -> list[list[str]]:
    return [r for r in csv.reader(io.StringIO(text), delimiter=_delimiter(text)) if any(c.strip() for c in r)]


def _table(raw: bytes) -> tuple[list[list[str]], str]:
    """Rows of strings from a CSV or an XLSX upload (first sheet), plus the detected format."""
    if len(raw) > MAX_UPLOAD_BYTES:
        raise ParseError(f"Upload exceeds the {MAX_UPLOAD_BYTES:,}-byte limit.")
    if xlsx.is_xlsx(raw):
        try:
            return xlsx.read_first_sheet(raw), "xlsx"
        except xlsx.XlsxError as exc:
            raise ParseError(str(exc)) from exc
    return _rows(_decode(raw)), "csv"


def resolve_period(headers: list[str], period: str = "auto") -> tuple[str, int, str]:
    """(period name, annualisation factor, source). User choice wins; auto reads the headers."""
    choice = (period or "auto").strip().lower()
    if choice != "auto" and choice not in PERIOD_FACTORS:
        raise ParseError(f"Unknown period {period!r}; supported: auto, {', '.join(PERIOD_FACTORS)}.")
    if choice != "auto":
        return choice, PERIOD_FACTORS[choice], "user choice"
    for name, hint in PERIOD_HEADER_HINTS:
        if any(hint.search(h) for h in headers):
            return name, PERIOD_FACTORS[name], "column header"
    return "annual", 1, "default (no period detected)"


_FLAG_SEVERITY = {"IMPORT_NO_VALID_ROWS": "error", "BALANCE_NO_AI_ROW": "warning",
                  "IMPORT_ROWS_REJECTED": "warning", "CONFIRMED_ROW_AMOUNT_MISSING": "warning"}


def findings(records: list[dict] = (), warnings: list[str] = (), flags: list[dict] = (), extra: list[dict] = ()) -> list[dict]:
    """One review list over everything the parser noticed: {severity, code, line, field, message}.
    Errors exclude a row, warnings need a look, info is for the record."""
    out = []
    for r in records:
        for e in r.get("errors", []):
            out.append({"severity": "error", "code": "ROW_INVALID", "line": r.get("line"), "field": e["field"],
                        "message": e["reason"]})
    out += list(extra)
    for f in flags:
        out.append({"severity": _FLAG_SEVERITY.get(f["code"], "warning"), "code": f["code"], "line": None,
                    "field": None, "message": f["message"]})
    for w in warnings:
        m = re.match(r"Row (\d+)", w)
        out.append({"severity": "warning", "code": "PARSE_WARNING", "line": int(m.group(1)) if m else None,
                    "field": None, "message": w})
    order = {"error": 0, "warning": 1, "info": 2}
    return sorted(out, key=lambda f: (order[f["severity"]], f["line"] if f["line"] is not None else 10**9, f["code"]))


def parse_money(raw: Any, column_multiplier: float = 1.0) -> Optional[float]:
    """Robust amount parser: bracketed negatives, thousands separators, ₹ and inline units.

    Returns None for blanks, unparsable junk and anything that is not money. Bare numbers are
    scaled by the column multiplier; an inline suffix (2.5 cr / 1.2 L / 50k) wins over it.
    Commas are thousands separators; the decimal point is '.'.
    """
    if isinstance(raw, (int, float)) and not isinstance(raw, bool):
        return float(raw) if _num(raw) else None
    if not isinstance(raw, str) or not raw.strip():
        return None
    s = raw.strip().replace("\u2009", " ").replace("\u00a0", " ")
    if s in _BLANKS or "%" in s:
        return None
    negative = False
    if s.startswith("(") and s.endswith(")"):
        negative, s = True, s[1:-1]
    if s.endswith("-"):
        negative, s = True, s[:-1]
    elif s.startswith("-"):
        negative, s = True, s[1:]
    s = s.replace("₹", "").replace(" ", "").replace(",", "")
    if not s:
        return None
    inline_mult = None
    for pat, mult in _INLINE_SUFFIX:
        m = pat.search(s)
        if m:
            inline_mult, s = mult, s[:m.start()]
            break
    if not re.fullmatch(r"-?\d+(?:\.\d+)?", s):
        return None
    value = float(s)
    if negative:
        value = -value
    return value * (inline_mult if inline_mult is not None else column_multiplier)


def resolve_unit(amount_headers: list[str], unit: str) -> tuple[str, float]:
    """User choice wins; auto detects from the amount-column headers; default rupees."""
    choice = (unit or "auto").strip().lower()
    if choice != "auto" and choice not in UNIT_MULTIPLIERS:
        raise ParseError(f"Unknown unit {unit!r}; supported: auto, {', '.join(UNIT_MULTIPLIERS)}.")
    if choice != "auto":
        return choice, float(UNIT_MULTIPLIERS[choice])
    for name, hint in UNIT_HEADER_HINTS:
        if any(hint.search(h) for h in amount_headers):
            return name, float(UNIT_MULTIPLIERS[name])
    return "rupees", 1.0


def _unit_source(choice: str, unit_name: str) -> str:
    if choice != "auto":
        return "user choice"
    return "amount-column header" if unit_name != "rupees" else "default (no unit detected)"


# ---------------------------------------------------------------- balance sheet

def _classify_columns(header: list[str], data_rows: list[list[str]]) -> tuple[int, list[int]]:
    """The label column holds the most alphabetic cells; the remaining columns that can
    produce money anywhere are the amount columns."""
    look = data_rows[:50]
    width = max([len(r) for r in [header] + look] or [0])
    label_idx, best_score = 0, -1
    for j in range(width):
        alpha = sum(1 for r in look if j < len(r) and any(ch.isalpha() for ch in r[j]))
        if alpha > best_score:
            label_idx, best_score = j, alpha
    amounts = [j for j in range(width) if j != label_idx
               and any(parse_money(r[j]) is not None for r in look if j < len(r))]
    return label_idx, amounts


def _cell(row: list[str], idx: int) -> str:
    return row[idx].strip() if idx < len(row) else ""


def parse_balance_sheet(raw: bytes, unit: str = "auto", amount_column: Optional[str] = None,
                        period: str = "auto") -> dict:
    """Parse a PY balance-sheet CSV or XLSX into candidate AI-spend rows. ai_burn_to_date stays None:
    the user decides which candidates are AI spend (never guessed)."""
    all_rows, fmt = _table(raw)
    if len(all_rows) < 2:
        raise ParseError("The CSV needs a header row and at least one data row.")
    header = [h.strip() for h in all_rows[0]]
    data = all_rows[1:]
    label_idx, amount_idx = _classify_columns(header, data)
    if not amount_idx:
        raise ParseError("No numeric amount column found; cannot read money from this sheet.")
    amt_headers = [header[j] for j in amount_idx]
    chosen = amount_idx[0]
    warnings: list[str] = []
    if amount_column is not None:
        if amount_column not in header:
            raise ParseError(f"amount_column {amount_column!r} is not a header of this CSV (headers: {header}).")
        if header.index(amount_column) not in amount_idx:
            raise ParseError(f"Column {amount_column!r} holds no parsable amounts; numeric columns: {amt_headers}.")
        chosen = header.index(amount_column)
    elif len(amount_idx) > 1:
        warnings.append(f"Multiple numeric amount columns ({', '.join(amt_headers)}); using {header[chosen]!r}. "
                        "Send 'amount_column' to pick another.")
    unit_name, mult = resolve_unit(amt_headers, unit)
    period_name, pfactor, period_src = resolve_period([header[chosen]], period)

    candidates, flags = [], []
    for n, row in enumerate(data, start=2):
        label = _cell(row, label_idx)
        matched = sorted({k.lower() for k in BALANCE_KEYWORD_RE.findall(label)})
        if not matched:
            continue
        raw_amt = _cell(row, chosen)
        amt = parse_money(raw_amt, mult)
        if raw_amt and amt is None:
            warnings.append(f"Row {n}: could not parse amount {raw_amt!r}; treated as missing.")
        candidates.append({"row": n, "label": label, "matched": matched,
                           "raw": raw_amt or None, "amount_inr": None if amt is None else round(amt),
                           "annualised_amount_inr": None if amt is None else round(amt * pfactor)})
    if not candidates:
        flags.append({"code": "BALANCE_NO_AI_ROW",
                      "message": f"No line item matched the AI-spend keywords ({', '.join(BALANCE_KEYWORDS)}); "
                                 "AI burn to date is unknown. Rename the relevant line items or extend the keyword set."})
    return {
        "file_bytes": len(raw), "rows": len(data), "label_column": header[label_idx],
        "amount_columns": amt_headers, "amount_column_used": header[chosen],
        "unit": {"choice": (unit or "auto").strip().lower(), "unit": unit_name, "multiplier": mult,
                 "source": _unit_source((unit or "auto").strip().lower(), unit_name)},
        "ai_burn_to_date": None,  # populated only by /balance-sheet/confirm after the user picks rows
        "candidates": candidates, "warnings": warnings, "flags": flags,
        "format": fmt,
        "period": {"choice": (period or "auto").strip().lower(), "period": period_name,
                   "annualisation_factor": pfactor, "source": period_src},
        "findings": findings(warnings=warnings, flags=flags),
    }


def _overrides(raw: Any) -> dict:
    """Reviewer corrections arrive as a JSON object (form field `overrides`)."""
    if raw is None or raw == "" or raw == {}:
        return {}
    if isinstance(raw, str):
        import json
        try:
            raw = json.loads(raw)
        except ValueError as exc:
            raise ParseError("overrides must be a JSON object.") from exc
    if not isinstance(raw, dict):
        raise ParseError("overrides must be a JSON object.")
    return raw


def confirm_balance_sheet(raw: bytes, selected_rows: list[int], unit: str = "auto",
                          amount_column: Optional[str] = None, d: Optional[dict] = None,
                          period: str = "auto", overrides: Any = None) -> dict:
    """Re-parse the same upload and total the confirmed rows into ai_burn_to_date (observed),
    beside the next-FY AI forecast from the token assumptions in d. Nothing is stored."""
    parsed = parse_balance_sheet(raw, unit, amount_column, period)
    by_row = {c["row"]: c for c in parsed["candidates"]}
    if not selected_rows:
        valid = sorted(by_row) or "none (no candidates in the file)"
        raise ParseError(f"Confirm needs at least one candidate row; valid rows: {valid}.")
    unknown = sorted(r for r in selected_rows if r not in by_row)
    if unknown:
        raise ParseError(f"These rows are not AI-spend candidates of the uploaded file: {unknown}. "
                         f"Valid rows: {sorted(by_row)}.")
    flags, warnings = list(parsed["flags"]), list(parsed["warnings"])
    ov = _overrides(overrides)
    corrected: dict[int, float] = {}
    for k, v in ov.items():
        try:
            row = int(k)
        except (TypeError, ValueError):
            raise ParseError(f"overrides key {k!r} is not a row number.")
        if row not in by_row:
            raise ParseError(f"overrides row {row} is not an AI-spend candidate of the uploaded file.")
        if not _num(v) or v < 0:
            raise ParseError(f"overrides row {row}: amount must be a non-negative number of rupees.")
        corrected[row] = float(v)
    per_row, burn = [], 0
    for r in sorted(selected_rows):
        c = dict(by_row[r])
        if r in corrected:
            c["original_amount_inr"], c["amount_inr"] = c["amount_inr"], round(corrected[r])
            flags.append({"code": "CORRECTED_BY_REVIEWER",
                          "message": f"Row {r} ({c['label']}): amount corrected in review from "
                                     f"{c['original_amount_inr']} to {c['amount_inr']}; the original is kept."})
        if c["amount_inr"] is None:
            flags.append({"code": "CONFIRMED_ROW_AMOUNT_MISSING",
                          "message": f"Row {r} ({c['label']}) has no parsable amount; it is excluded from the burn, not zeroed."})
            warnings.append(f"Row {r} ({c['label']}): amount missing; excluded from the burn total.")
        else:
            burn += c["amount_inr"]
        per_row.append({"row": c["row"], "label": c["label"], "amount_inr": c["amount_inr"],
                        **({"original_amount_inr": c["original_amount_inr"], "corrected": True} if r in corrected else {})})
    from . import core  # local import keeps ingest standalone of the decision core
    nxt = core.forecast_block(d or {})
    return {
        "ai_burn_to_date": round(burn), "source": "observed", "rows_confirmed": sorted(selected_rows),
        "confirmations": per_row, "amount_column_used": parsed["amount_column_used"], "unit": parsed["unit"],
        "next_fy": {k: nxt[k] for k in ("inputs", "ai_run_cost_inr", "next_fy_cost_inr", "inflation_flag")},
        "flags": flags, "warnings": warnings,
        "ai_burn_annualised_inr": round(burn * parsed["period"]["annualisation_factor"]),
        "period": parsed["period"], "format": parsed["format"],
        "findings": findings(warnings=warnings, flags=flags),
    }


# ---------------------------------------------------------------- proposal (pilot) CSV import

# Alias maps: logical field -> accepted headers (lower-cased). First matching header wins.
FIELD_ALIASES: dict[str, list[str]] = {
    "id": ["id", "pilot_id", "pilot", "proposal_id"],
    "task": ["task", "name", "title", "description"],
    "setup_cost_inr": ["setup_cost_inr", "setup_inr", "setup_cost", "setup", "one_time_cost_inr", "one_time", "cost"],
    "annual_running_cost_inr": ["annual_running_cost_inr", "running_cost_inr", "ai_running_cost_inr", "ai_running_cost", "running_cost", "run_cost", "cost_inr",
                                "monthly_running_cost_inr", "monthly_running_cost", "running_cost_per_month", "quarterly_running_cost_inr", "quarterly_running_cost"],
    "annual_benefit_inr": ["annual_benefit_inr", "yearly_savings_inr", "yearly_savings", "savings", "benefit",
                           "monthly_savings_inr", "monthly_savings", "savings_per_month", "quarterly_savings_inr", "quarterly_savings"],
    "benefit_type": ["benefit_type", "type", "cash_type"],
    "adoption_pct": ["adoption_pct", "adoption"],
    "readiness": ["readiness", "data_readiness"],
    "ramp_months": ["ramp_months", "ramp"],
    "delay_months": ["delay_months", "delay"],
    "overlap_group": ["overlap_group", "group", "overlap"],
}

PARSE_RULES = {
    "max_upload_bytes": MAX_UPLOAD_BYTES,
    "balance_sheet_keywords": BALANCE_KEYWORDS,
    "unit_multipliers": UNIT_MULTIPLIERS,
    "pilot_csv_columns": FIELD_ALIASES,
    "formats": ["csv", "xlsx (first sheet)"],
    "xlsx_limits": {"max_uncompressed_bytes": xlsx.MAX_UNCOMPRESSED_BYTES, "max_rows": xlsx.MAX_ROWS},
    "period_factors": PERIOD_FACTORS,
    "period_rule": "recurring amounts (savings, running cost, balance-sheet lines) are annualised from the period "
                   "their column header names; one-time setup is never annualised",
    "finding_severities": ["error", "warning", "info"],
    "possible_duplicate_rule": "different ids with the same task (case-insensitive), setup, benefit and running cost",
}


def _map_columns(header: list[str]) -> tuple[dict[str, int], list[str]]:
    lowered = [h.strip().lower().lstrip("#").strip() for h in header]
    colmap: dict[str, int] = {}
    warnings: list[str] = []
    for field, aliases in FIELD_ALIASES.items():
        hits = [i for i, h in enumerate(lowered) if h in aliases]
        if not hits:
            continue
        colmap[field] = hits[0]
        for extra in hits[1:]:
            warnings.append(f"Header {header[extra]!r} also maps to {field}; ignored (first match wins).")
    all_alias = {a for al in FIELD_ALIASES.values() for a in al}
    unused = [header[i] for i, h in enumerate(lowered) if h and h not in all_alias]
    if unused:
        warnings.append(f"Unrecognised columns ignored: {', '.join(unused)}.")
    return colmap, warnings


def parse_pilots_csv(raw: bytes, unit: str = "auto", period: str = "auto") -> dict:
    """Parse a proposal CSV into engine pilot rows with per-row error review.

    Every error is {line, field, reason}; rows with any error are excluded from the result but
    listed for the user to fix. Duplicate ids inside the file are blocked (all but the first).
    Money columns inherit the unit stated in their header (auto-detect), or the user choice.
    """
    all_rows, fmt = _table(raw)
    if len(all_rows) < 2:
        raise ParseError("The CSV needs a header row and at least one data row.")
    header = [h.strip() for h in all_rows[0]]
    colmap, warnings = _map_columns(header)
    periods: dict = {}
    for f in ("annual_running_cost_inr", "annual_benefit_inr"):
        if f in colmap:
            name, factor, src = resolve_period([header[colmap[f]]], period)
            periods[f] = {"column": header[colmap[f]], "period": name, "annualisation_factor": factor, "source": src}
    if "setup_cost_inr" in colmap and resolve_period([header[colmap["setup_cost_inr"]]])[0] != "annual" \
            and resolve_period([header[colmap["setup_cost_inr"]]])[2] == "column header":
        warnings.append(f"Setup column {header[colmap['setup_cost_inr']]!r} names a period; setup is one-time "
                        "and is not annualised.")
    if "id" not in colmap:
        raise ParseError(f"No id column found; accepted headers: {', '.join(FIELD_ALIASES['id'])}.")
    amount_headers = [header[colmap[f]] for f in ("setup_cost_inr", "annual_running_cost_inr", "annual_benefit_inr")
                      if f in colmap]
    unit_name, mult = resolve_unit(amount_headers, unit)
    choice = (unit or "auto").strip().lower()

    def money(row: list[str], field: str, required: bool) -> tuple[Optional[float], list[dict]]:
        if field not in colmap:
            return (None, [{"field": field, "reason": "column missing"}] if required else [])
        raw_v = _cell(row, colmap[field])
        if raw_v == "" or raw_v.lower() in _BLANKS:
            if required:
                return (None, [{"field": field, "reason": "missing (unknown, not zero)"}])
            return (None, [])
        v = parse_money(raw_v, mult)
        if v is None:
            return (None, [{"field": field, "reason": f"{raw_v!r} is not a parsable amount"}])
        if v < 0:
            return (None, [{"field": field, "reason": f"must be non-negative, got {v:,.0f}"}])
        return (round(v * periods.get(field, {}).get("annualisation_factor", 1)), [])

    records: list[dict] = []
    seen_ids: set[str] = set()
    for n, row in enumerate(all_rows[1:], start=2):
        errs: list[dict] = []
        pid = _cell(row, colmap["id"])
        if not pid:
            errs.append({"field": "id", "reason": "id missing"})
        elif pid in seen_ids:
            errs.append({"field": "id", "reason": f"duplicate id {pid!r} (already defined in this file)"})
        seen_ids.add(pid)
        task = _cell(row, colmap["task"]) if "task" in colmap else ""
        if not task:
            errs.append({"field": "task", "reason": "task/name missing"})
        setup, e1 = money(row, "setup_cost_inr", True)
        benefit, e2 = money(row, "annual_benefit_inr", True)
        run, e3 = money(row, "annual_running_cost_inr", False)
        errs += e1 + e2 + e3
        bt_raw = _cell(row, colmap["benefit_type"]).lower() if "benefit_type" in colmap else ""
        if bt_raw and bt_raw not in ("cashable", "cashable assumption", "time freed", "non-cashable", "unknown"):
            errs.append({"field": "benefit_type",
                         "reason": f"{bt_raw!r} is not a known benefit type (cashable, time freed, non-cashable, unknown)"})
        rd_raw = _cell(row, colmap["readiness"]).lower() if "readiness" in colmap else ""
        if rd_raw and rd_raw not in ("low", "medium", "high"):
            errs.append({"field": "readiness", "reason": f"{rd_raw!r} is not low/medium/high"})
        adoption = None
        if "adoption_pct" in colmap:
            a_raw = _cell(row, colmap["adoption_pct"])
            if a_raw != "":
                a = parse_money(a_raw, 1.0)
                if a is None or a < 0 or a > 100:
                    errs.append({"field": "adoption_pct", "reason": f"{a_raw!r} is not a percentage in 0-100"})
                else:
                    adoption = round(a, 1)
        ramp, delay = None, None
        for col in ("ramp_months", "delay_months"):
            if col not in colmap:
                continue
            raw_m = _cell(row, colmap[col])
            if raw_m == "":
                continue
            mm = parse_money(raw_m, 1.0)
            if mm is None or mm < 0 or mm > 120 or not float(mm).is_integer():
                errs.append({"field": col, "reason": f"{raw_m!r} is not a whole number of months in 0-120"})
            elif col == "ramp_months":
                ramp = int(mm)
            else:
                delay = int(mm)
        grp = _cell(row, colmap["overlap_group"]) if "overlap_group" in colmap else ""
        rec: dict = {"line": n}
        if errs:
            rec["errors"] = errs
        else:
            pilot = {"id": pid, "task": task, "setup_cost_inr": setup, "annual_benefit_inr": benefit}
            if run is not None:
                pilot["annual_running_cost_inr"] = run   # absent stays None: assumption path downstream
            if bt_raw:
                pilot["benefit_type"] = bt_raw
            if grp:
                pilot["overlap_group"] = grp
            if ramp is not None:
                pilot["ramp_months"] = ramp
            if delay is not None:
                pilot["delay_months"] = delay
            ext_body: dict = {"id": pid}
            if adoption is not None:
                ext_body["adoption_pct"] = int(adoption) if float(adoption).is_integer() else adoption
            if rd_raw:
                ext_body["readiness"] = rd_raw
            rec["pilot"], rec["extension"] = pilot, ext_body
        records.append(rec)
    valid = [(r["pilot"], r["extension"]) for r in records if "pilot" in r]
    error_rows = sum(1 for r in records if "errors" in r)
    flags = []
    if error_rows:
        flags.append({"code": "IMPORT_ROWS_REJECTED",
                      "message": f"{error_rows} of {len(records)} rows had errors and are excluded; fix them and re-upload."})
    if records and not valid:
        flags.append({"code": "IMPORT_NO_VALID_ROWS", "message": "No row parsed cleanly; nothing would be imported."})
    dup_findings, seen_content = [], {}
    for r in records:
        if "pilot" not in r:
            continue
        p = r["pilot"]
        key = (p["task"].strip().lower(), p["setup_cost_inr"], p["annual_benefit_inr"], p.get("annual_running_cost_inr"))
        if key in seen_content:
            first = seen_content[key]
            dup_findings.append({"severity": "warning", "code": "POSSIBLE_DUPLICATE", "line": r["line"], "field": "id",
                                 "message": f"{p['id']!r} repeats {first[1]!r} (line {first[0]}): same task and money "
                                            "figures under a different id. Keep one unless they are truly separate."})
        else:
            seen_content[key] = (r["line"], p["id"])
    return {"rows": len(records), "valid_rows": len(valid), "error_rows": error_rows,
            "headers": header, "column_map": {k: header[v] for k, v in colmap.items()},
            "unit": {"choice": choice, "unit": unit_name, "multiplier": mult, "source": _unit_source(choice, unit_name)},
            "records": records,
            "pilots": [p for p, _ in valid],
            "extensions": [e for _, e in valid],  # aligned with `pilots` (adoption/readiness live on extension rows)
            "warnings": warnings, "flags": flags,
            "format": fmt, "periods": periods,
            "findings": findings(records, warnings, flags, dup_findings)}


_PILOT_OVERRIDE_FIELDS = {"task", "setup_cost_inr", "annual_benefit_inr", "annual_running_cost_inr", "benefit_type",
                          "overlap_group", "ramp_months", "delay_months"}
_EXT_OVERRIDE_FIELDS = {"adoption_pct", "readiness"}


def _check_override(pid: str, field: str, v: Any) -> Any:
    bad = lambda why: ParseError(f"overrides {pid}.{field}: {why}")
    if field in ("setup_cost_inr", "annual_benefit_inr"):
        if not _num(v) or v < 0:
            raise bad("must be a non-negative number of rupees")
        return round(v)
    if field == "annual_running_cost_inr":
        if v is None:
            return None
        if not _num(v) or v < 0:
            raise bad("must be a non-negative number of rupees or null")
        return round(v)
    if field in ("ramp_months", "delay_months"):
        if not _num(v) or v < 0 or v > 120 or not float(v).is_integer():
            raise bad("must be a whole number of months in 0-120")
        return int(v)
    if field == "adoption_pct":
        if not _num(v) or v < 0 or v > 100:
            raise bad("must be a percentage in 0-100")
        return v
    if field == "readiness":
        if v not in ("low", "medium", "high"):
            raise bad("must be low, medium or high")
        return v
    if field == "benefit_type":
        if v not in ("cashable", "cashable assumption", "time freed", "non-cashable", "unknown"):
            raise bad("must be cashable, time freed, non-cashable or unknown")
        return v
    if not isinstance(v, str) or not v.strip() or len(v) > (40 if field == "overlap_group" else 200):
        raise bad("must be non-empty text")
    return v.strip()


def confirm_pilots_csv(raw: bytes, keep_ids: list[str], unit: str = "auto", fixture_ids: list[str] = (),
                       period: str = "auto", overrides: Any = None) -> dict:
    """Re-parse and return only the kept ids, in the shape the client uses on subsequent calls:
    `pilots` go into `fixture`, `extension_pilots` into `extension`. Persistence stays in Go."""
    parsed = parse_pilots_csv(raw, unit, period)
    by_id = {p["id"]: (p, e) for p, e in zip(parsed["pilots"], parsed["extensions"])}
    if not keep_ids:
        ids = sorted(by_id) or "none parsed cleanly"
        raise ParseError(f"Confirm needs at least one pilot id; ids parsed from this file: {ids}.")
    missing = sorted(i for i in keep_ids if i not in by_id)
    if missing:
        raise ParseError(f"These ids are not cleanly parsed rows of the uploaded file: {missing}. "
                         f"Valid ids: {sorted(by_id)}.")
    warnings = list(parsed["warnings"])
    collisions = sorted(set(keep_ids) & set(fixture_ids or ()))
    if collisions:
        warnings.append(f"Ids also present in the shipped fixture: {', '.join(collisions)}; sending these rows as "
                        "`fixture` replaces the shipped pilots with the same ids.")
    ov = _overrides(overrides)
    for pid, fields in ov.items():
        if pid not in keep_ids:
            raise ParseError(f"overrides name {pid!r}, which is not among the kept ids.")
        if not isinstance(fields, dict):
            raise ParseError(f"overrides {pid!r} must be an object of field -> corrected value.")
        unknown = set(fields) - _PILOT_OVERRIDE_FIELDS - _EXT_OVERRIDE_FIELDS
        if unknown:
            raise ParseError(f"overrides {pid!r}: field(s) {sorted(unknown)} cannot be corrected.")
    flags = list(parsed["flags"])
    corrections = []
    pilots, exts = [], []
    for i in keep_ids:
        p, e = dict(by_id[i][0]), dict(by_id[i][1])
        for field, v in (ov.get(i) or {}).items():
            target = e if field in _EXT_OVERRIDE_FIELDS else p
            original = target.get(field)
            value = _check_override(i, field, v)
            if value is None:
                target.pop(field, None)    # null withdraws the stated value (missing is never zero)
            else:
                target[field] = value
            corrections.append({"id": i, "field": field, "original": original, "corrected": value})
        pilots.append(p)
        if len(e) > 1:          # extension rows carry at least one field besides id
            exts.append(e)
    if corrections:
        flags.append({"code": "CORRECTED_BY_REVIEWER",
                      "message": f"{len(corrections)} value(s) corrected in review; originals are listed in `corrections`."})
    return {"pilots": pilots, "extension_pilots": exts, "flags": flags, "warnings": warnings,
            "unit": parsed["unit"], "periods": parsed["periods"], "format": parsed["format"],
            "corrections": corrections}
