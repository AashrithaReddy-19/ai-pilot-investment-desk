"""Grounding validator: any generated explanation must be fully supported by the deterministic evaluation.

Checks (all must pass):
  numbers      every number in the text exists in the evaluation (after stripping pilot IDs / rules version)
  units        no quantity uses a unit the engine never produces (hours, days, people...): the engine emits INR, months, %
  pilot_ids    every pilot ID mentioned is the evaluated pilot or a listed peer
  decision     the evaluation's decision is stated; no other decision word appears
  reason_codes every UPPER_SNAKE reason code mentioned is one the engine produced
  claims       no unsupported certainty/ROI claims ("proven", "guaranteed", "will definitely", ...)

Designed to guard ANY text generator (template today, an optional LLM later): unsupported text is rejected
and the caller falls back to the deterministic explanation.
"""
from __future__ import annotations
import re
from decimal import Decimal, InvalidOperation
from typing import Any

DECISIONS = ("PROCEED", "REVISE", "STOP", "REVIEW")
ID_RE = re.compile(r"\b[A-Z]{1,5}\d{1,4}\b")
CODE_RE = re.compile(r"\b[A-Z]{3,}(?:_[A-Z]+)+\b")
NUM_RE = re.compile(r"(?<![A-Za-z0-9_.])-?\d+(?:,\d{3})*(?:\.\d+)?")
UNIT_RE = re.compile(r"\d\s*(hours?|hrs?|days?|weeks?|minutes?|years? of|FTEs?|people|employees|customers|tickets|users|staff|headcount)\b", re.I)
BANNED = [
    re.compile(p, re.I) for p in (
        r"(?<!not )(?<!not yet )\bproven\b", r"\bguarantee(d|s)?\b", r"\bwill definitely\b", r"\bcertain(ly)? to\b",
        r"\bvalidated market\b", r"\breal enterprise roi\b", r"\brisk[- ]free\b", r"\bno risk\b", r"\b100% (sure|certain)\b",
    )
]


def _numbers(obj: Any, acc: set[Decimal]) -> set[Decimal]:
    if isinstance(obj, bool) or obj is None:
        return acc
    if isinstance(obj, (int, float)):
        try:
            d = Decimal(str(obj))
            acc.update({d, d.quantize(Decimal("0.01")), d.quantize(Decimal("1"))})
        except InvalidOperation:
            pass
    elif isinstance(obj, str):
        for m in NUM_RE.findall(ID_RE.sub(" ", obj.replace("c23-rules-v1", " "))):
            try:
                acc.add(Decimal(m.replace(",", "")))
            except InvalidOperation:
                pass
    elif isinstance(obj, dict):
        for v in obj.values():
            _numbers(v, acc)
    elif isinstance(obj, list):
        for v in obj:
            _numbers(v, acc)
    return acc


def _peer_ids(ev: dict) -> set[str]:
    ids = {ev.get("pilot_id", "")}
    for w in ev.get("warnings") or []:
        ids.update(ID_RE.findall(w))
    return ids


def validate(text: str, ev: dict) -> dict:
    """Returns {"grounded": bool, "checks": [...passed], "failures": [...]}."""
    failures, passed = [], []
    allowed_nums = _numbers(ev, set()) | {Decimal(0), Decimal(12), Decimal(100)}  # 12 months/yr, percent base
    stripped = ID_RE.sub(" ", text.replace("c23-rules-v1", " "))
    bad_nums = []
    for m in NUM_RE.findall(stripped):
        try:
            d = Decimal(m.replace(",", ""))
        except InvalidOperation:
            continue
        if d not in allowed_nums and d.quantize(Decimal("0.01")) not in allowed_nums:
            bad_nums.append(m)
    (failures if bad_nums else passed).append("numbers" if not bad_nums else f"numbers: unsupported {sorted(set(bad_nums))}")

    # A number can exist in the evaluation yet be re-used with a unit the engine never produces
    # ("45 hours per week" when 45 was an adoption %). The engine only emits INR, months and percent.
    units = UNIT_RE.findall(text)
    (failures if units else passed).append("units" if not units else f"units: engine never produces {sorted({u.lower() for u in units})}")

    ids = set(ID_RE.findall(text)) - _peer_ids(ev)
    (failures if ids else passed).append("pilot_ids" if not ids else f"pilot_ids: unknown {sorted(ids)}")

    dec = ev.get("decision")
    words = {d for d in DECISIONS if re.search(rf"\b{d}\b", text)}
    if dec not in words or words - {dec}:
        failures.append(f"decision: expected only {dec}, found {sorted(words)}")
    else:
        passed.append("decision")

    codes = set(CODE_RE.findall(text)) - set(ev.get("reason_codes") or []) - {"NOT_MEASURED", "NOT_CALCULABLE", "NEVER_PAYS_BACK",
                                                                              "ALL_RULES_PASSED", "DOUBLE_COUNT_RISK", "TEAM_ASSUMPTION",
                                                                              "OFFICIAL_FIXTURE", "CALCULABLE", "IMMEDIATE"}
    codes -= {ev.get("primary_reason", "")}
    (failures if codes else passed).append("reason_codes" if not codes else f"reason_codes: invented {sorted(codes)}")

    hits = [p.pattern for p in BANNED if p.search(text)]
    (failures if hits else passed).append("claims" if not hits else f"claims: unsupported certainty {hits}")
    return {"grounded": not failures, "checks": passed, "failures": failures}
