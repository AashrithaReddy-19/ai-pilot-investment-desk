"""Independent, exact re-implementation of c23-rules-v1 (docs/business-rules.md, backend/internal/decision).

All arithmetic uses Fraction (no floats). None means unknown, never zero. Two benefit bases:

  stated : annual_benefit_inr as supplied (the basis of the official public check and of the formulas in the
           synthetic dataset's derived columns)
  cash   : production default. cashable -> annual_benefit_inr; any other benefit type -> realized_cash_saving_inr
           only (time freed / capacity / revenue assumptions are not cash). This is what the Go engine decides on.

Thresholds are the documented TEAM defaults (rules.Default() in Go); a pilot's own minimum_*_pct overrides them.
"""
from __future__ import annotations
from dataclasses import dataclass, field
from fractions import Fraction

RULES_VERSION = "c23-rules-v1"
BASIS_STATED, BASIS_CASH = "stated", "cash"

# TEAM thresholds (backend/internal/rules/rules.go Default(); extension.json defaults). Not invented here.
DEFAULT_MIN_ADOPTION_PCT = Fraction(60)
DEFAULT_MIN_QUALITY_PCT = Fraction(80)
LOW_READINESS_BELOW_PCT = Fraction(50)
HIGH_READINESS_FROM_PCT = Fraction(75)

PROCEED, REVIEW, REVISE, STOP = "PROCEED", "REVIEW", "REVISE", "STOP"
SEVERITY = {PROCEED: 0, REVIEW: 1, REVISE: 2, STOP: 3}

# Reason codes in priority order (rules.Priority) and the decision each forces (rules.DecisionFor).
PRIORITY = ["MISSING_SETUP_COST", "MISSING_BENEFIT", "MISSING_RUNNING_COST", "NEVER_PAYS_BACK", "QUALITY_THRESHOLD_FAILED",
            "ADOPTION_BELOW_BREAKEVEN", "LOW_ADOPTION", "LOW_READINESS", "TIME_NOT_CASH", "OVERLAP_UNKNOWN", "INSUFFICIENT_EVIDENCE"]
FORCES = {"MISSING_SETUP_COST": REVISE, "MISSING_BENEFIT": REVISE, "MISSING_RUNNING_COST": REVISE,
          "NEVER_PAYS_BACK": STOP, "QUALITY_THRESHOLD_FAILED": STOP, "ADOPTION_BELOW_BREAKEVEN": STOP,
          "LOW_ADOPTION": REVISE, "LOW_READINESS": REVISE,
          "TIME_NOT_CASH": REVIEW, "OVERLAP_UNKNOWN": REVIEW, "INSUFFICIENT_EVIDENCE": REVIEW}
ALL_PASSED = "ALL_RULES_PASSED"

READINESS_FIELDS = ("data_readiness_pct", "technical_readiness_pct", "process_readiness_pct")


def frac(v) -> Fraction | None:
    """Exact Fraction from an int/str/float input; None stays None (unknown)."""
    if v is None:
        return None
    if isinstance(v, bool):
        raise TypeError("boolean is not a number")
    return Fraction(str(v)) if isinstance(v, float) else Fraction(v)


def fmt(x: Fraction | None, places: int = 2) -> str | None:
    """Render like Go money.FormatRat: round half away from zero, trim trailing zeros. None -> None."""
    if x is None:
        return None
    scale = 10 ** places
    q = abs(x) * scale
    n = int(q) + (1 if q - int(q) >= Fraction(1, 2) else 0)
    s = f"{n // scale}.{n % scale:0{places}d}" if places else str(n)
    if "." in s:
        s = s.rstrip("0").rstrip(".")
    return "-" + s if x < 0 and s != "0" else s


def canonical_benefit_type(raw: str | None) -> str:
    """Mirror of domain.CanonicalBenefitType: 'time' wins over 'cash'; anything else is 'other'."""
    if raw is None or not raw.strip():
        return "unknown"
    t = raw.lower()
    if "time" in t:
        return "time_freed"
    if "cash" in t:
        return "cashable"
    return "other"


def basis_benefit(p: dict, basis: str) -> Fraction | None:
    if basis == BASIS_STATED:
        return frac(p.get("annual_benefit_inr"))
    if canonical_benefit_type(p.get("benefit_type")) == "cashable":
        return frac(p.get("annual_benefit_inr"))
    return frac(p.get("realized_cash_saving_inr"))


@dataclass(frozen=True)
class Finance:
    net_annual: Fraction | None
    monthly_net: Fraction | None
    payback_months: Fraction | None
    payback_status: str
    roi_pct: Fraction | None


def finance(setup, running, benefit) -> Finance:
    """Net = benefit - running; monthly = net / 12; payback = setup / monthly (only if monthly > 0); ROI = net / setup * 100."""
    setup, running, benefit = frac(setup), frac(running), frac(benefit)
    if running is None or benefit is None:
        return Finance(None, None, None, "NOT_CALCULABLE", None)
    net = benefit - running
    monthly = net / 12
    if setup is None:
        payback, status = None, "NOT_CALCULABLE"
    elif monthly <= 0:
        payback, status = None, "NEVER_PAYS_BACK"
    elif setup == 0:
        payback, status = Fraction(0), "IMMEDIATE"
    else:
        payback, status = setup / monthly, "CALCULABLE"
    roi = None if setup is None or setup == 0 else net / setup * 100
    return Finance(net, monthly, payback, status, roi)


def breakeven_adoption_pct(running, benefit) -> Fraction | None:
    """Adoption at which benefit (linear in adoption) covers running cost = running / benefit * 100."""
    running, benefit = frac(running), frac(benefit)
    if running is None or benefit is None or benefit == 0:
        return None
    return running / benefit * 100


def readiness(p: dict) -> tuple[Fraction | None, str]:
    """Mean of supplied readiness pct values -> low (< 50) / medium / high (>= 75); else the label; else unknown."""
    vals = [frac(p.get(k)) for k in READINESS_FIELDS if p.get(k) is not None]
    if vals:
        avg = sum(vals) / len(vals)
        level = "low" if avg < LOW_READINESS_BELOW_PCT else "high" if avg >= HIGH_READINESS_FROM_PCT else "medium"
        return avg, level
    label = p.get("readiness_label")
    return None, label if label else "unknown"


@dataclass
class Evaluation:
    pilot_id: str
    basis: str
    decision: str
    primary_reason: str
    reason_codes: list = field(default_factory=list)
    stated: Finance | None = None
    on_basis: Finance | None = None
    basis_benefit: Fraction | None = None
    breakeven_pct: Fraction | None = None
    readiness_pct: Fraction | None = None
    readiness_level: str = "unknown"
    min_adoption: Fraction = DEFAULT_MIN_ADOPTION_PCT
    min_quality: Fraction = DEFAULT_MIN_QUALITY_PCT


def evaluate(p: dict, basis: str = BASIS_CASH) -> Evaluation:
    """Apply R1-R9; final decision = most severe triggered rule (STOP > REVISE > REVIEW > PROCEED)."""
    pid = p.get("id") or p.get("record_id")
    setup, running, benefit = frac(p.get("setup_cost_inr")), frac(p.get("annual_running_cost_inr")), frac(p.get("annual_benefit_inr"))
    b = basis_benefit(p, basis)
    min_ad = frac(p.get("minimum_adoption_pct")) if p.get("minimum_adoption_pct") is not None else DEFAULT_MIN_ADOPTION_PCT
    min_q = frac(p.get("minimum_quality_pct")) if p.get("minimum_quality_pct") is not None else DEFAULT_MIN_QUALITY_PCT
    actual_ad, actual_q = frac(p.get("actual_adoption_pct")), frac(p.get("actual_quality_pct"))
    on_basis = finance(setup, running, b)
    be = breakeven_adoption_pct(running, b)
    ready_pct, ready_level = readiness(p)
    codes = []
    if setup is None: codes.append("MISSING_SETUP_COST")
    if benefit is None: codes.append("MISSING_BENEFIT")
    if running is None: codes.append("MISSING_RUNNING_COST")
    if on_basis.net_annual is not None and on_basis.net_annual <= 0: codes.append("NEVER_PAYS_BACK")
    if actual_q is not None and actual_q < min_q: codes.append("QUALITY_THRESHOLD_FAILED")
    if actual_ad is not None:
        if be is not None and actual_ad < be: codes.append("ADOPTION_BELOW_BREAKEVEN")
        if actual_ad < min_ad: codes.append("LOW_ADOPTION")
    if ready_level == "low": codes.append("LOW_READINESS")
    if canonical_benefit_type(p.get("benefit_type")) != "cashable" and p.get("realized_cash_saving_inr") is None: codes.append("TIME_NOT_CASH")
    if not p.get("overlap_group"): codes.append("OVERLAP_UNKNOWN")
    if actual_ad is None or actual_q is None: codes.append("INSUFFICIENT_EVIDENCE")
    codes.sort(key=PRIORITY.index)
    decision = max((FORCES[c] for c in codes), key=SEVERITY.__getitem__, default=PROCEED)
    primary = next((c for c in codes if FORCES[c] == decision), ALL_PASSED)
    return Evaluation(pid, basis, decision, primary, codes, finance(setup, running, benefit), on_basis, b, be,
                      ready_pct, ready_level, min_ad, min_q)
