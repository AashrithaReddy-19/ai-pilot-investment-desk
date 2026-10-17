"""Strict schemas + structured validation for C23 pilot rows.

Validation classifies and reports; it never fills values or alters numbers.
null / absent means UNKNOWN, never zero. A null financial field is *valid but unknown* here;
the missing-value policy (app/pipeline.py) decides what an unknown means for a decision.

Error  => the row is rejected (INVALID) and cannot be evaluated.
Warning => the row is kept; the warning is carried into evidence.
"""
from __future__ import annotations
import math, re
from dataclasses import dataclass, field, asdict
from typing import Any, Optional

ID_RE = re.compile(r"[A-Za-z0-9_-]{1,64}")
LABEL_RE = re.compile(r"[a-z0-9][a-z0-9 _.&-]{0,63}")   # normalised overlap group
READINESS_LABELS = ("low", "medium", "high")
MAX_TEXT = 500


@dataclass(frozen=True)
class FieldSpec:
    name: str
    kind: str                  # id | text | label | money | pct | readiness
    required_key: bool = False  # official core field: key expected in every row
    nullable: bool = True       # null allowed (= unknown)


# Official C23 core fields (fixture.json) followed by the documented extended fields.
PILOT_FIELDS = (
    FieldSpec("id", "id", True, False),
    FieldSpec("task", "text", True, False),
    FieldSpec("setup_cost_inr", "money", True),
    FieldSpec("annual_running_cost_inr", "money", True),
    FieldSpec("annual_benefit_inr", "money", True),
    FieldSpec("benefit_type", "label", True),
    FieldSpec("overlap_group", "label", True),
    FieldSpec("description", "text"),
    FieldSpec("expected_adoption_pct", "pct"),
    FieldSpec("actual_adoption_pct", "pct"),
    FieldSpec("minimum_adoption_pct", "pct"),
    FieldSpec("expected_quality_pct", "pct"),
    FieldSpec("actual_quality_pct", "pct"),
    FieldSpec("minimum_quality_pct", "pct"),
    FieldSpec("data_readiness_pct", "pct"),
    FieldSpec("technical_readiness_pct", "pct"),
    FieldSpec("process_readiness_pct", "pct"),
    FieldSpec("capacity_value_inr", "money"),
    FieldSpec("realized_cash_saving_inr", "money"),
)

# Team extension rows (extension.json). Legacy names are kept in the file; ALIASES maps them.
EXTENSION_FIELDS = (
    FieldSpec("id", "id", True, False),
    FieldSpec("description", "text"),
    FieldSpec("adoption_pct", "pct"),
    FieldSpec("readiness", "readiness"),
    FieldSpec("observed_adoption_pct", "pct"),
    FieldSpec("observed_quality_pct", "pct"),
) + tuple(f for f in PILOT_FIELDS if f.kind == "pct" or f.name in ("capacity_value_inr", "realized_cash_saving_inr"))

EXTENSION_ALIASES = {"adoption_pct": "expected_adoption_pct", "observed_adoption_pct": "actual_adoption_pct",
                     "observed_quality_pct": "actual_quality_pct", "readiness": "readiness_label"}


@dataclass
class Issue:
    code: str
    message: str
    severity: str = "error"          # error | warning
    row: Optional[int] = None        # 0-based index in the input list
    id: Optional[str] = None
    field: Optional[str] = None
    value: Any = None                # JSON-safe repr of the offending value

    def to_dict(self) -> dict:
        return asdict(self)


@dataclass
class ValidatedRow:
    index: int
    id: Optional[str]
    values: dict                     # only spec fields; raw values, unmodified
    issues: list = field(default_factory=list)
    absent: tuple = ()               # required_key fields missing from the row (treated as null)

    @property
    def ok(self) -> bool:
        return not any(i.severity == "error" for i in self.issues)


def safe_value(v: Any) -> Any:
    """JSON-safe representation for evidence (NaN/inf/objects become strings)."""
    if v is None or isinstance(v, (bool, int, str)):
        return v if not isinstance(v, str) else v[:200]
    if isinstance(v, float):
        return v if math.isfinite(v) else repr(v)
    return repr(v)[:200]


def normalize_label(v: str) -> str:
    return re.sub(r"\s+", " ", v.strip().lower())


def check_value(spec: FieldSpec, v: Any) -> Optional[tuple[str, str]]:
    """Returns (code, message) if v violates spec, else None. v is never None here."""
    k = spec.kind
    if k in ("money", "pct"):
        if isinstance(v, bool) or not isinstance(v, (int, float)):
            return "NOT_NUMERIC", f"{spec.name} must be a number, got {type(v).__name__} {safe_value(v)!r}"
        if not math.isfinite(v):
            return "NOT_FINITE", f"{spec.name} must be finite, got {v!r}"
        if k == "money" and v < 0:
            return "NEGATIVE", f"{spec.name} must be >= 0, got {v:g}"
        if k == "pct" and not 0 <= v <= 100:
            return "OUT_OF_RANGE", f"{spec.name} must be between 0 and 100, got {v:g}"
        return None
    if not isinstance(v, str):
        return "NOT_TEXT", f"{spec.name} must be text, got {type(v).__name__}"
    s = v.strip()
    if k == "id":
        if not ID_RE.fullmatch(s):
            return "BAD_ID", f"id must be 1-64 chars of letters, digits, _ or -, got {safe_value(v)!r}"
    elif k == "text":
        if not s:
            return "EMPTY", f"{spec.name} must not be empty"
        if len(s) > MAX_TEXT:
            return "TOO_LONG", f"{spec.name} longer than {MAX_TEXT} chars"
    elif k == "label":
        if s and spec.name == "overlap_group" and not LABEL_RE.fullmatch(normalize_label(s)):
            return "BAD_LABEL", f"overlap_group must be 1-64 chars of letters, digits, space, _ . & -, got {safe_value(v)!r}"
        if len(s) > 100:
            return "TOO_LONG", f"{spec.name} longer than 100 chars"
    elif k == "readiness":
        if normalize_label(s) not in READINESS_LABELS:
            return "INVALID_ENUM", f"readiness must be one of {', '.join(READINESS_LABELS)}, got {safe_value(v)!r}"
    return None


def validate_rows(rows: Any, specs=PILOT_FIELDS, allowed_extra: tuple = ()) -> tuple[list, list]:
    """Validate a list of row objects. Returns (validated_rows, file_level_issues).
    Duplicate IDs (case-insensitive) reject EVERY occurrence, so the outcome is order-independent."""
    if not isinstance(rows, list):
        return [], [Issue("FILE_SHAPE", "pilot rows must be a JSON array")]
    file_issues = [] if rows else [Issue("EMPTY_LIST", "no pilot rows supplied", "warning")]
    by_name = {s.name: s for s in specs}
    out: list[ValidatedRow] = []
    for i, r in enumerate(rows):
        if not isinstance(r, dict):
            out.append(ValidatedRow(i, None, {}, [Issue("NOT_AN_OBJECT", f"row {i} is {type(r).__name__}, expected object", row=i)]))
            continue
        rid = r.get("id").strip() if isinstance(r.get("id"), str) else None
        vr = ValidatedRow(i, rid, {})
        absent = []
        for s in specs:
            if s.name not in r:
                if not s.nullable:
                    vr.issues.append(Issue("REQUIRED_MISSING", f"{s.name} is required", row=i, id=rid, field=s.name))
                elif s.required_key:
                    absent.append(s.name)
                    vr.issues.append(Issue("FIELD_ABSENT", f"{s.name} key absent; treated as unknown (null), never zero",
                                           "warning", i, rid, s.name))
                continue
            v = r[s.name]
            if v is None:
                if not s.nullable:
                    vr.issues.append(Issue("REQUIRED_MISSING", f"{s.name} is required and cannot be null", row=i, id=rid, field=s.name))
                vr.values[s.name] = None
                continue
            bad = check_value(s, v)
            if bad:
                vr.issues.append(Issue(bad[0], bad[1], row=i, id=rid, field=s.name, value=safe_value(v)))
            else:
                vr.values[s.name] = v
        for k in r:
            if k not in by_name:
                if k in allowed_extra:
                    vr.values[k] = r[k]
                else:
                    vr.issues.append(Issue("UNKNOWN_FIELD", f"field {k!r} is not in the schema; dropped", "warning", i, rid, k))
        vr.absent = tuple(absent)
        out.append(vr)
    groups: dict = {}
    for vr in out:
        if vr.id and ID_RE.fullmatch(vr.id):
            groups.setdefault(vr.id.lower(), []).append(vr)
    for dupes in groups.values():
        if len(dupes) > 1:
            where = ", ".join(f"row {d.index}" for d in dupes)
            for d in dupes:
                d.issues.append(Issue("DUPLICATE_ID", f"id {d.id!r} appears more than once ({where}); all occurrences rejected",
                                      row=d.index, id=d.id, field="id", value=d.id))
    return out, file_issues


def validate_budget(v: Any) -> Optional[Issue]:
    """Budget must be a known, finite, non-negative number. null is unknown => cannot build a portfolio."""
    if v is None:
        return Issue("BUDGET_MISSING", "pilot budget is unknown (null); cannot build a portfolio", field="pilot_budget_inr")
    bad = check_value(FieldSpec("pilot_budget_inr", "money"), v)
    return Issue(bad[0], bad[1], field="pilot_budget_inr", value=safe_value(v)) if bad else None
