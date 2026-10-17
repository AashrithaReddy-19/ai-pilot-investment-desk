"""Plain-language explanation of a deterministic evaluation (template generator, model "template-explainer").

The text only restates values present in the evaluation; it is then checked by grounding.validate().
No LLM is called: no API key is required, outputs are reproducible, and nothing can be hallucinated.
"""
from __future__ import annotations
from .grounding import validate

MODEL_NAME = "template-explainer"
MODEL_VERSION = "1.0.0"

_DECISION_TEXT = {
    "PROCEED": "meets every c23-rules-v1 check and can proceed",
    "REVIEW": "needs human review before wider investment",
    "REVISE": "must be revised before more money is committed",
    "STOP": "should be stopped under a mandatory rule",
}

_RULE_TEXT = {
    "MISSING_SETUP_COST": "setup cost is unknown, so payback cannot be calculated",
    "MISSING_BENEFIT": "annual benefit is unknown, so net benefit cannot be calculated",
    "MISSING_RUNNING_COST": "annual running cost is unknown (it is never assumed to be zero), so payback cannot be calculated",
    "NEVER_PAYS_BACK": "net annual cash benefit is {actual}, so it never pays back",
    "QUALITY_THRESHOLD_FAILED": "measured quality {actual}% is below the {threshold}% minimum; a quality failure overrides good financials",
    "ADOPTION_BELOW_BREAKEVEN": "measured adoption {actual}% is below the {threshold}% break-even adoption",
    "LOW_ADOPTION": "measured adoption {actual}% is below the {threshold}% minimum",
    "LOW_READINESS": "readiness is low",
    "TIME_NOT_CASH": "the benefit is released time or capacity, which is not counted as cash until a cash saving is recorded",
    "OVERLAP_UNKNOWN": "its overlap group is unknown, which is not treated as 'no overlap'",
    "INSUFFICIENT_EVIDENCE": "actual adoption and quality have not been measured yet, so it should run only as a small time-boxed pilot",
}


def _fmt(v) -> str:
    if v is None:
        return "unknown"
    if isinstance(v, float) and v.is_integer():
        v = int(v)
    return str(v)


def explain(ev: dict) -> str:
    pid, dec = ev["pilot_id"], ev["decision"]
    parts = [f"{pid} {_DECISION_TEXT.get(dec, 'has a decision')}: decision {dec}."]
    s = ((ev.get("calculated_metrics") or {}).get("stated_basis")) or {}
    if s.get("payback_months") is not None:
        parts.append(f"On the stated figures it earns INR {_fmt(s.get('net_annual_benefit_inr'))} net per year "
                     f"(INR {_fmt(s.get('monthly_net_benefit_inr'))} per month) and pays back in {_fmt(s.get('payback_months'))} months.")
    elif s.get("payback_status"):
        parts.append(f"Payback status: {s['payback_status']}.")
    for f in ev.get("failed_rules") or []:
        t = _RULE_TEXT.get(f.get("code"))
        if t:
            parts.append("Because " + t.format(actual=f.get("actual", ""), threshold=f.get("threshold", "")) + ".")
    parts.append("These are calculations on a synthetic fixture and team assumptions, not proven returns.")
    return " ".join(parts)


def explain_checked(ev: dict) -> dict:
    text = explain(ev)
    g = validate(text, ev)
    return {"model_name": MODEL_NAME, "model_version": MODEL_VERSION, "explanation": text, "grounded": g["grounded"],
            "checks": g["checks"], "failures": g["failures"]}
