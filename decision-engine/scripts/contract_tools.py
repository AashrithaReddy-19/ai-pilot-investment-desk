"""Contract tooling: the example request for every engine endpoint, a JSON Schema inferred from
its response, and a small validator (no third-party dependency).

Used by scripts/build_contracts.py (writes contracts/engine/) and tests/test_contracts.py
(checks the live engine still matches). Schemas allow extra properties on purpose: additive
changes are allowed by the contract rules, removals and type changes are not.
"""
from __future__ import annotations
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
CONTRACTS = ROOT / "contracts"
ENGINE = CONTRACTS / "engine"

# Worked example from the plan (A, B, C), with one proposal carrying evidence and a baseline.
PROPOSALS = [
    {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000,
     "readiness": "high", "adoption_pct": 70, "observed_adoption_pct": 72, "observed_quality_pct": 88,
     "overlap_group": "finance",
     "baseline": {"volume_per_month": 2000, "minutes_per_unit": 6, "loaded_cost_per_hour": 1000}},
    {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000,
     "readiness": "medium", "overlap_group": "support"},
    {"name": "C", "setup_cost_inr": 100000, "annual_benefit_inr": 90000, "ai_running_cost_inr": 70000},
]
PROPOSAL_SET = {"proposals": PROPOSALS, "budget": 700000,
                "budget_set": {"total_inr": 1500000, "implementation_inr": 800000, "contingency_pct": 10},
                "ai_burn_to_date": 1550000}
# [text, filename, mime] as lists: they round-trip through request.json unchanged.
BALANCE_CSV = ["Account,Amount in INR\nAI software licences,1200000\nRent,25000\nCloud AI platform,350000\n", "bs.csv", "text/csv"]
PILOTS_CSV = ["id,task,setup_cost_inr,annual_benefit_inr,annual_running_cost_inr,readiness,adoption_pct\n"
              "P1,Invoice extraction,200000,180000,60000,high,70\nP2,Ticket triage,150000,120000,,medium,\n",
              "pilots.csv", "text/csv"]
HISTORY = [40e6, 43e6, 47e6, 50e6, 52e6, 57e6]
PLANS = [{"name": "pay_as_you_go", "rule": {"type": "per_unit", "unit_price": 0.00025}},
         {"name": "commit_60m", "rule": {"type": "commitment", "committed_units": 60e6, "commit_price": 13000,
                                         "overage_unit_price": 0.0003}},
         {"name": "packages", "rule": {"type": "package", "overage_unit_price": 0.0003,
                                       "packages": [{"name": "10M", "units": 10e6, "price": 2300},
                                                    {"name": "50M", "units": 50e6, "price": 10500}]}}]

# name -> (method, path, body or multipart spec, response kind)
EXAMPLES: dict[str, tuple] = {
    "health": ("GET", "/health", None, "json"),
    "defaults": ("GET", "/defaults", None, "json"),
    "pilots": ("POST", "/pilots", {}, "json"),
    "portfolio": ("POST", "/portfolio", {"budget": 600000, "mode": "conservative"}, "json"),
    "portfolio_check": ("POST", "/portfolio/check", {"ids": ["AI1", "AI2"], "budget": 600000}, "json"),
    "stoprule": ("POST", "/stoprule/AI1", {"observed_adoption_pct": 72, "observed_quality_pct": 85}, "json"),
    "whatif": ("POST", "/whatif", {"pilot_id": "AI1", "overrides": {"observed_adoption_pct": 40}}, "json"),
    "sweep": ("POST", "/sweep", {"pilot_id": "AI1", "parameter": "adoption_pct"}, "json"),
    "compare": ("POST", "/compare", {"proposals": [{k: v for k, v in p.items() if k != "baseline"} for p in PROPOSALS],
                                     "budget": 700000}, "json"),
    "balance_sheet_parse": ("POST", "/balance-sheet/parse", {"multipart": {"file": BALANCE_CSV, "data": {}}}, "json"),
    "balance_sheet_confirm": ("POST", "/balance-sheet/confirm",
                              {"multipart": {"file": BALANCE_CSV, "data": {"rows": "2,4"}}}, "json"),
    "pilots_import_parse": ("POST", "/pilots/import/parse", {"multipart": {"file": PILOTS_CSV, "data": {}}}, "json"),
    "pilots_import_confirm": ("POST", "/pilots/import/confirm",
                              {"multipart": {"file": PILOTS_CSV, "data": {"keep": "P1,P2"}}}, "json"),
    "export_csv": ("POST", "/export/csv", {}, "text"),
    "export_evidence": ("POST", "/export/evidence", {}, "text"),
    "checkpoint": ("POST", "/checkpoint/AI1", {"observed_adoption_pct": 72, "observed_quality_pct": 85,
                                               "duration_days": 30}, "json"),
    "plan": ("POST", "/plan", {"pilot_id": "AI1", "owner": "finance ops"}, "json"),
    "reallocation": ("POST", "/reallocation", {"budget": 600000, "funded_ids": ["AI1", "AI2", "AI4"],
                                               "stopped_ids": ["AI2"]}, "json"),
    "export_memo": ("POST", "/export/memo", {"pilot_id": "AI1", "observed_adoption_pct": 72,
                                             "observed_quality_pct": 85}, "text"),
    "economics": ("POST", "/economics", PROPOSAL_SET, "json"),
    "decision": ("POST", "/decision", PROPOSAL_SET, "json"),
    "views_dashboard": ("POST", "/views/dashboard", PROPOSAL_SET, "json"),
    "views_yearly_savings": ("POST", "/views/yearly-savings", PROPOSAL_SET, "json"),
    "export_yearly_csv": ("POST", "/export/yearly-csv", PROPOSAL_SET, "text"),
    "export_decision_report": ("POST", "/export/decision-report", PROPOSAL_SET, "text"),
    "pricing_quote": ("POST", "/pricing/quote", {"rule": PLANS[2]["rule"], "units": 57e6}, "json"),
    "forecast_usage": ("POST", "/forecast/usage", {"history": HISTORY, "months": 12, "rule": PLANS[0]["rule"]}, "json"),
    "pricing_optimise": ("POST", "/pricing/optimise", {"history": HISTORY, "months": 12, "plans": PLANS}, "json"),
}


def call(client, method: str, path: str, body):
    if method == "GET":
        return client.get(path)
    if isinstance(body, dict) and "multipart" in body:
        text, fname, mime = body["multipart"]["file"]
        return client.post(path, files={"file": (fname, text.encode(), mime)}, data=body["multipart"]["data"])
    return client.post(path, json=body)


# ---------------------------------------------------------------- schema inference + validation

def _type(v: Any) -> str:
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "boolean"
    if isinstance(v, (int, float)):
        return "number"
    if isinstance(v, str):
        return "string"
    if isinstance(v, list):
        return "array"
    return "object"


# Objects keyed by proposal id rather than by field name: one value schema for every key.
ID_KEYED = {"decisions"}


def infer(v: Any, key: str = "") -> dict:
    t = _type(v)
    if t == "object" and key in ID_KEYED and v:
        vals = [infer(x) for x in v.values()]
        item = vals[0]
        for x in vals[1:]:
            item = merge(item, x)
        return {"type": "object", "additionalProperties": item, "x-keyed-by": "proposal id"}
    if t == "object":
        return {"type": "object", "properties": {k: infer(x, k) for k, x in v.items()}, "required": sorted(v)}
    if t == "array":
        out: dict = {"type": "array"}
        if v:
            item = infer(v[0])
            for x in v[1:]:
                item = merge(item, infer(x))
            out["items"] = item
        return out
    return {"type": t}


def _types(s: dict) -> set:
    t = s.get("type")
    return set(t) if isinstance(t, list) else {t}


def merge(a: dict, b: dict) -> dict:
    ta, tb = _types(a), _types(b)
    types = sorted(ta | tb)
    out: dict = {"type": types[0] if len(types) == 1 else types}
    if "object" in ta and "object" in tb:
        pa, pb = a.get("properties", {}), b.get("properties", {})
        out["properties"] = {k: merge(pa[k], pb[k]) if k in pa and k in pb else (pa.get(k) or pb[k]) for k in sorted(set(pa) | set(pb))}
        out["required"] = sorted(set(a.get("required", [])) & set(b.get("required", [])))
    elif "object" in ta or "object" in tb:
        src = a if "object" in ta else b
        out["properties"], out["required"] = src.get("properties", {}), []   # null-or-object: keys optional
    if "array" in ta and "array" in tb and ("items" in a or "items" in b):
        out["items"] = merge(a["items"], b["items"]) if "items" in a and "items" in b else (a.get("items") or b["items"])
    elif "array" in ta | tb and ("items" in a or "items" in b):
        out["items"] = a.get("items") or b.get("items")
    return out


def validate(v: Any, schema: dict, where: str = "$") -> list:
    """Errors (empty = valid). Extra properties are allowed; missing required keys and type
    changes are errors. A schema type 'null' observed in the example also accepts any value,
    since null there means 'unknown in this example', which the engine allows for any field."""
    errs: list = []
    allowed = _types(schema)
    t = _type(v)
    if "null" in allowed and len(allowed) == 1:
        return errs
    if t not in allowed and not (t == "null" and "null" in allowed):
        if t == "null":
            return errs if schema.get("nullable_ok", True) else [f"{where}: null not allowed"]
        return [f"{where}: expected {sorted(allowed)}, got {t}"]
    if t == "object":
        for k in schema.get("required", []):
            if k not in v:
                errs.append(f"{where}: missing required key {k!r}")
        for k, sub in schema.get("properties", {}).items():
            if k in v:
                errs += validate(v[k], sub, f"{where}.{k}")
        if isinstance(schema.get("additionalProperties"), dict):
            for k, x in v.items():
                errs += validate(x, schema["additionalProperties"], f"{where}.{k}")
    elif t == "array" and "items" in schema:
        for i, x in enumerate(v):
            errs += validate(x, schema["items"], f"{where}[{i}]")
    return errs
