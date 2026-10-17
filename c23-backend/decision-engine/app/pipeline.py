"""Data preparation: validate -> merge team extension -> missing-value policy -> normalise -> provenance.

Pure functions, no I/O. Raw numbers are never altered; normalisation only touches labels and the
original label is kept alongside. Every imputed value is a TEAM_ASSUMPTION with a flag.

Missing-value policy (null = unknown, never zero):
  setup_cost_inr / annual_benefit_inr null  -> unknown in ALL modes; row INCOMPLETE (decision REVISE)
  annual_running_cost_inr null              -> STRICT: unknown, INCOMPLETE, MISSING_RUN_COST
                                               SCENARIO: setup * assumed_run_cost_pct_of_setup / 100, RUN_COST_ASSUMED
  expected_adoption_pct null                -> STRICT: unknown; SCENARIO: defaults.assumed_adoption_pct, ADOPTION_ASSUMED
  readiness_label null                      -> STRICT: unknown; SCENARIO: "low" (or defaults.assumed_readiness), READINESS_ASSUMED
  actual_adoption_pct / actual_quality_pct  -> NOT_MEASURED in all modes (never imputed)
  overlap_group null/blank                  -> OVERLAP_UNKNOWN in all modes; group stays null (NOT "no overlap")
No financial value is ever mean/median-imputed.
"""
from __future__ import annotations
from dataclasses import dataclass, field
from typing import Any, Literal, Optional, Union
from pydantic import BaseModel, ConfigDict

from . import provenance as P
from .schemas import (EXTENSION_ALIASES, EXTENSION_FIELDS, PILOT_FIELDS, READINESS_LABELS, FieldSpec, Issue,
                      check_value, normalize_label, safe_value, validate_budget, validate_rows)

PIPELINE_VERSION = "1.0.0"
STRICT, SCENARIO = "strict", "scenario"
MODES = (STRICT, SCENARIO)
DEFAULT_ASSUMED_READINESS = "low"   # matches legacy core.load_pilots behaviour

Num = Optional[Union[int, float]]

FLAG_TEXT = {
    "MISSING_SETUP_COST": "Setup cost unknown (null). Cannot compute payback; decision REVISE until supplied.",
    "MISSING_BENEFIT": "Annual benefit unknown (null). Cannot compute net benefit; decision REVISE until supplied.",
    "MISSING_RUN_COST": "Annual running cost unknown (null). STRICT mode: no reliable payback; decision REVISE.",
    "RUN_COST_ASSUMED": "Annual running cost unknown; SCENARIO mode ASSUMED a team default (see assumptions).",
    "RUN_COST_NOT_IMPUTABLE": "Running cost unknown and could not be assumed (setup unknown or default invalid).",
    "ADOPTION_EXPECTATION_UNKNOWN": "Expected adoption not supplied.",
    "ADOPTION_ASSUMED": "Expected adoption not supplied; SCENARIO mode ASSUMED a team default.",
    "ADOPTION_NOT_MEASURED": "Actual adoption not measured yet (null, not zero).",
    "QUALITY_NOT_MEASURED": "Actual quality not measured yet (null, not zero).",
    "READINESS_UNKNOWN": "Readiness label not supplied.",
    "READINESS_ASSUMED": "Readiness label not supplied; SCENARIO mode ASSUMED a team default.",
    "OVERLAP_UNKNOWN": "Overlap group unknown. NOT treated as 'no overlap'; must be confirmed before portfolio approval.",
    "BENEFIT_TYPE_UNKNOWN": "Benefit type unknown; cannot be counted as cash.",
    "BENEFIT_TYPE_UNRECOGNISED": "Benefit type not recognised as cashable or time freed; kept as 'other'.",
    "TIME_NOT_CASH": "Benefit is time freed (capacity), not cash removed. Not counted as cash savings by default.",
}


class CleanPilot(BaseModel):
    """One prepared pilot. Numbers are exactly as supplied (or a flagged TEAM_ASSUMPTION)."""
    model_config = ConfigDict(extra="forbid", frozen=True)
    id: str
    task: str
    description: Optional[str] = None
    setup_cost_inr: Num = None
    annual_running_cost_inr: Num = None
    annual_benefit_inr: Num = None
    benefit_type: Literal["cashable", "time_freed", "other", "unknown"]
    benefit_type_raw: Optional[str] = None
    overlap_group: Optional[str] = None
    overlap_group_raw: Optional[str] = None
    overlap_status: Literal["KNOWN", "UNKNOWN"]
    expected_adoption_pct: Num = None
    actual_adoption_pct: Num = None
    minimum_adoption_pct: Num = None
    expected_quality_pct: Num = None
    actual_quality_pct: Num = None
    minimum_quality_pct: Num = None
    data_readiness_pct: Num = None
    technical_readiness_pct: Num = None
    process_readiness_pct: Num = None
    readiness_label: Optional[Literal["low", "medium", "high"]] = None
    readiness_label_raw: Optional[str] = None
    capacity_value_inr: Num = None
    realized_cash_saving_inr: Num = None
    adoption_status: Literal["MEASURED", "NOT_MEASURED"]
    quality_status: Literal["MEASURED", "NOT_MEASURED"]
    data_status: Literal["COMPLETE", "INCOMPLETE"]
    flags: list[str]
    row_source: str = P.OFFICIAL_FIXTURE
    field_sources: dict[str, str]
    assumptions: list[dict]
    prep_mode: Literal["strict", "scenario"]


@dataclass
class PreparedData:
    mode: str
    pilots: list                     # list[CleanPilot], input order preserved
    rejected: list                   # [{"id", "row", "issues": [Issue dict]}]
    issues: list                     # file-level issues and row warnings (Issue)
    budget: Num = None
    budget_issue: Optional[Issue] = None
    defaults: dict = field(default_factory=dict)

    def summary(self) -> dict:
        return {"mode": self.mode, "valid": len(self.pilots), "rejected": len(self.rejected),
                "complete": sum(p.data_status == "COMPLETE" for p in self.pilots),
                "incomplete": sum(p.data_status == "INCOMPLETE" for p in self.pilots)}


def canonical_benefit_type(raw: Optional[str]) -> str:
    if raw is None or not raw.strip():
        return "unknown"
    t = raw.lower()
    if "time" in t:            # checked first, as in core.cash_factor: "cashable time" is still time
        return "time_freed"
    if "cash" in t:
        return "cashable"
    return "other"


def _defaults(ext_meta: dict) -> tuple[dict, list]:
    d, issues = {}, []
    raw = ext_meta.get("defaults") if isinstance(ext_meta.get("defaults"), dict) else {}
    for k in ("target_adoption_pct", "quality_threshold_pct", "assumed_run_cost_pct_of_setup", "assumed_adoption_pct"):
        v = raw.get(k)
        if v is None:
            continue
        bad = check_value(FieldSpec(k, "pct"), v)
        if bad:
            issues.append(Issue(bad[0], f"extension defaults: {bad[1]}", field=f"defaults.{k}", value=safe_value(v)))
        else:
            d[k] = v
    ar = raw.get("assumed_readiness", DEFAULT_ASSUMED_READINESS)
    if isinstance(ar, str) and normalize_label(ar) in READINESS_LABELS:
        d["assumed_readiness"] = normalize_label(ar)
    else:
        issues.append(Issue("INVALID_ENUM", "extension defaults: assumed_readiness must be low/medium/high",
                            field="defaults.assumed_readiness", value=safe_value(ar)))
    return d, issues


def prepare(fixture_rows: Any, extension_rows: Any = None, extension_meta: Optional[dict] = None,
            mode: str = STRICT, budget: Any = "__from_extension__") -> PreparedData:
    if mode not in MODES:
        raise ValueError(f"mode must be one of {MODES}")
    extension_meta = extension_meta or {}
    defaults, issues = _defaults(extension_meta)
    if budget == "__from_extension__":
        budget = extension_meta.get("pilot_budget_inr")
    budget_issue = validate_budget(budget)

    rows, file_issues = validate_rows(fixture_rows, PILOT_FIELDS)
    issues += file_issues
    ext_rows, ext_file_issues = validate_rows(extension_rows if extension_rows is not None else [], EXTENSION_FIELDS)
    issues += [i for i in ext_file_issues if i.code != "EMPTY_LIST"]
    ids = {r.id.lower() for r in rows if r.id}
    ext_by: dict = {}
    for er in ext_rows:
        for i in er.issues:
            i.field = f"extension.{i.field}" if i.field else "extension"
        if not er.id or er.id.lower() not in ids:
            issues.append(Issue("ORPHAN_EXTENSION_ROW", f"extension row {safe_value(er.id)!r} matches no pilot; ignored",
                                "warning", er.index, er.id))
            issues += [i for i in er.issues if i.severity == "warning"]
            continue
        ext_by.setdefault(er.id.lower(), []).append(er)

    pilots, rejected = [], []
    for r in rows:
        ers = ext_by.get(r.id.lower(), []) if r.id else []
        row_issues = list(r.issues) + [i for er in ers for i in er.issues]
        if len(ers) > 1:
            row_issues.append(Issue("DUPLICATE_EXTENSION_ROW", f"{len(ers)} extension rows for {r.id}", row=r.index, id=r.id,
                                    field="extension.id"))
        if any(i.severity == "error" for i in row_issues):
            rejected.append({"id": r.id or "(no id)", "row": r.index,
                             "issues": [i.to_dict() for i in row_issues if i.severity == "error"]})
            issues += [i for i in row_issues if i.severity == "warning"]
            continue
        issues += row_issues
        pilots.append(_clean(r, ers[0] if ers else None, defaults, mode, issues))
    return PreparedData(mode, pilots, rejected, issues, budget if budget_issue is None else None, budget_issue, defaults)


def _clean(r, er, defaults: dict, mode: str, issues: list) -> CleanPilot:
    v = dict(r.values)
    src = {k: (P.OFFICIAL_FIXTURE if v.get(k) is not None else P.UNKNOWN) for k in v}
    for k in r.absent:
        v[k] = None
        src[k] = P.UNKNOWN
    if er is not None:
        for k, val in er.values.items():
            if k == "id":
                continue
            target = EXTENSION_ALIASES.get(k, k)
            if v.get(target) is not None:
                if val is not None and val != v[target]:
                    issues.append(Issue("EXTENSION_CONFLICT", f"{target}: fixture value kept, extension value {val!r} ignored",
                                        "warning", r.index, r.id, target, safe_value(val)))
                continue
            v[target] = val
            src[target] = P.UNKNOWN if val is None else (P.OBSERVED if target.startswith("actual_") else P.TEAM_ASSUMPTION)

    flags, assumptions = [], []

    def assume(fld, value, rule):
        v[fld] = value
        src[fld] = P.TEAM_ASSUMPTION
        assumptions.append({"field": fld, "value": value, "rule": rule, "source": P.TEAM_ASSUMPTION})

    setup, run, benefit = v.get("setup_cost_inr"), v.get("annual_running_cost_inr"), v.get("annual_benefit_inr")
    if setup is None: flags.append("MISSING_SETUP_COST")
    if benefit is None: flags.append("MISSING_BENEFIT")
    if run is None:
        pct = defaults.get("assumed_run_cost_pct_of_setup")
        if mode == SCENARIO and setup is not None and pct is not None:
            assume("annual_running_cost_inr", setup * pct / 100, f"setup_cost_inr * {pct:g}% (extension.defaults.assumed_run_cost_pct_of_setup)")
            flags.append("RUN_COST_ASSUMED")
        else:
            flags.append("MISSING_RUN_COST")
            if mode == SCENARIO:
                flags.append("RUN_COST_NOT_IMPUTABLE")

    if v.get("expected_adoption_pct") is None:
        if mode == SCENARIO and defaults.get("assumed_adoption_pct") is not None:
            assume("expected_adoption_pct", defaults["assumed_adoption_pct"], "extension.defaults.assumed_adoption_pct")
            flags.append("ADOPTION_ASSUMED")
        else:
            flags.append("ADOPTION_EXPECTATION_UNKNOWN")

    rl_raw = v.pop("readiness_label", None)
    rl = normalize_label(rl_raw) if isinstance(rl_raw, str) and rl_raw.strip() else None
    if rl is None:
        if mode == SCENARIO and defaults.get("assumed_readiness"):
            rl = defaults["assumed_readiness"]
            src["readiness_label"] = P.TEAM_ASSUMPTION
            assumptions.append({"field": "readiness_label", "value": rl, "rule": "defaults.assumed_readiness (legacy default 'low')",
                                "source": P.TEAM_ASSUMPTION})
            flags.append("READINESS_ASSUMED")
        else:
            src["readiness_label"] = P.UNKNOWN
            flags.append("READINESS_UNKNOWN")

    if v.get("actual_adoption_pct") is None: flags.append("ADOPTION_NOT_MEASURED")
    if v.get("actual_quality_pct") is None: flags.append("QUALITY_NOT_MEASURED")

    bt_raw = v.get("benefit_type")
    bt = canonical_benefit_type(bt_raw)
    flags += {"unknown": ["BENEFIT_TYPE_UNKNOWN"], "other": ["BENEFIT_TYPE_UNRECOGNISED"], "time_freed": ["TIME_NOT_CASH"]}.get(bt, [])

    og_raw = v.get("overlap_group")
    og = normalize_label(og_raw) if isinstance(og_raw, str) and og_raw.strip() else None
    if og is None:
        flags.append("OVERLAP_UNKNOWN")

    incomplete = any(f in flags for f in ("MISSING_SETUP_COST", "MISSING_BENEFIT", "MISSING_RUN_COST"))
    numeric = {k: v.get(k) for k in ("expected_adoption_pct", "actual_adoption_pct", "minimum_adoption_pct", "expected_quality_pct",
                                      "actual_quality_pct", "minimum_quality_pct", "data_readiness_pct", "technical_readiness_pct",
                                      "process_readiness_pct", "capacity_value_inr", "realized_cash_saving_inr")}
    desc = v.get("description")
    return CleanPilot(
        id=r.id, task=v["task"].strip(), description=desc.strip() if isinstance(desc, str) else None,
        setup_cost_inr=setup, annual_running_cost_inr=v.get("annual_running_cost_inr"), annual_benefit_inr=benefit,
        benefit_type=bt, benefit_type_raw=bt_raw, overlap_group=og, overlap_group_raw=og_raw,
        overlap_status="KNOWN" if og else "UNKNOWN", readiness_label=rl, readiness_label_raw=rl_raw,
        adoption_status="NOT_MEASURED" if v.get("actual_adoption_pct") is None else "MEASURED",
        quality_status="NOT_MEASURED" if v.get("actual_quality_pct") is None else "MEASURED",
        data_status="INCOMPLETE" if incomplete else "COMPLETE", flags=flags,
        field_sources=dict(sorted(src.items())), assumptions=assumptions, prep_mode=mode, **numeric)
