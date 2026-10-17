"""Pricing rules, usage forecasting with a backtest, and the package optimiser (A4).

Pure functions, no I/O. Deterministic: same input, same numbers, same choice.

Pricing rule shapes (one JSON object each, validated by `validate_rule`):
- per_unit    {"type": "per_unit", "unit_price"}
- tiered      {"type": "tiered", "tiers": [{"up_to": N|null, "unit_price"}], "tier_mode": "graduated"|"volume"}
- credit      {"type": "credit", "credits_per_unit", "price_per_credit", "included_credits"?}
- commitment  {"type": "commitment", "committed_units", "commit_price", "overage_unit_price"}
- package     {"type": "package", "packages": [{"name", "units", "price"}], "overage_unit_price"?}
Every rule also accepts "fixed_monthly_inr" (platform fee) and "minimum_monthly_inr"
(minimum commitment: the bill is never below it). Quantities are units per month.

Usage forecast: with at least MIN_HISTORY monthly points, three methods (naive last value,
linear trend, compound growth) are fitted on the history minus a hold-out tail and scored by
MAPE on that tail; the lowest MAPE wins (ties by METHOD_ORDER). Low/high bands are
base × (1 ∓ max(winning MAPE, BAND_FLOOR)). With less history, the forecast falls back to the
assumed annual usage growth and says so in `method` — the fallback is labelled, never silent.
"""
from __future__ import annotations
import math
from typing import Any, Optional

RULE_TYPES = ["per_unit", "tiered", "credit", "commitment", "package"]
METHOD_ORDER = ["naive", "linear_trend", "compound_growth"]
MIN_HISTORY = 4
HOLDOUT_MAX = 3
BAND_FLOOR = 0.10
FALLBACK_METHOD = "assumed_growth_fallback"
MAX_PACKAGE_COUNT = 50          # most packages of one kind a month's bill may combine
MAX_PACKAGE_KINDS = 4           # keeps the exhaustive mix search small (51^3 leaves at worst)

PRICING_RULES = {
    "rule_types": RULE_TYPES,
    "modifiers": {"fixed_monthly_inr": "added every month", "minimum_monthly_inr": "bill = max(bill, minimum)"},
    "tier_modes": {"graduated": "each unit pays its own tier's price",
                   "volume": "every unit pays the price of the tier the total falls in"},
    "package_rule": "cheapest mix of whole packages (plus overage, if priced) that covers the month's units",
    "package_limits": {"max_kinds": MAX_PACKAGE_KINDS, "max_count_per_kind": MAX_PACKAGE_COUNT},
    "forecast": {"methods": METHOD_ORDER, "min_history_months": MIN_HISTORY, "holdout_max_months": HOLDOUT_MAX,
                 "selection": "lowest hold-out MAPE; ties by method order",
                 "band": f"base × (1 ∓ max(MAPE, {BAND_FLOOR}))",
                 "fallback": f"{FALLBACK_METHOD}: last known usage × (1 + annual growth)^(m/12), labelled"},
    "optimiser": "plan with the lowest base-scenario annual cost; also reports the plan with the lowest "
                 "worst-case (high-scenario) cost and the regret of each plan",
}


class PricingError(ValueError):
    """Bad pricing or forecast input: shown as a 422."""


def _num(x: Any) -> bool:
    return isinstance(x, (int, float)) and not isinstance(x, bool) and math.isfinite(x)


def _nonneg(v: Any, where: str) -> float:
    if not _num(v) or v < 0:
        raise PricingError(f"{where} must be a non-negative number, got {v!r}.")
    return float(v)


# ---------------------------------------------------------------- rules

def validate_rule(rule: Any, where: str = "rule") -> dict:
    if not isinstance(rule, dict):
        raise PricingError(f"{where} must be an object.")
    t = rule.get("type")
    if t not in RULE_TYPES:
        raise PricingError(f"{where}.type must be one of {RULE_TYPES}, got {t!r}.")
    out: dict = {"type": t}
    for mod in ("fixed_monthly_inr", "minimum_monthly_inr"):
        if rule.get(mod) is not None:
            out[mod] = _nonneg(rule[mod], f"{where}.{mod}")
    if t == "per_unit":
        out["unit_price"] = _nonneg(rule.get("unit_price"), f"{where}.unit_price")
    elif t == "tiered":
        tiers = rule.get("tiers")
        if not isinstance(tiers, list) or not tiers:
            raise PricingError(f"{where}.tiers must be a non-empty array.")
        clean, last = [], 0.0
        for i, tr in enumerate(tiers):
            if not isinstance(tr, dict):
                raise PricingError(f"{where}.tiers[{i}] must be an object.")
            up = tr.get("up_to")
            price = _nonneg(tr.get("unit_price"), f"{where}.tiers[{i}].unit_price")
            if up is None:
                if i != len(tiers) - 1:
                    raise PricingError(f"{where}.tiers[{i}]: only the last tier may be open-ended (up_to null).")
            else:
                up = _nonneg(up, f"{where}.tiers[{i}].up_to")
                if up <= last:
                    raise PricingError(f"{where}.tiers must have strictly increasing up_to values.")
                last = up
            clean.append({"up_to": up, "unit_price": price})
        if clean[-1]["up_to"] is not None:
            raise PricingError(f"{where}: the last tier must be open-ended (up_to null) so every quantity has a price.")
        mode = rule.get("tier_mode", "graduated")
        if mode not in ("graduated", "volume"):
            raise PricingError(f"{where}.tier_mode must be graduated or volume.")
        out.update(tiers=clean, tier_mode=mode)
    elif t == "credit":
        out["credits_per_unit"] = _nonneg(rule.get("credits_per_unit"), f"{where}.credits_per_unit")
        out["price_per_credit"] = _nonneg(rule.get("price_per_credit"), f"{where}.price_per_credit")
        out["included_credits"] = _nonneg(rule.get("included_credits", 0), f"{where}.included_credits")
    elif t == "commitment":
        out["committed_units"] = _nonneg(rule.get("committed_units"), f"{where}.committed_units")
        out["commit_price"] = _nonneg(rule.get("commit_price"), f"{where}.commit_price")
        out["overage_unit_price"] = _nonneg(rule.get("overage_unit_price"), f"{where}.overage_unit_price")
    elif t == "package":
        pk = rule.get("packages")
        if not isinstance(pk, list) or not pk or len(pk) > MAX_PACKAGE_KINDS:
            raise PricingError(f"{where}.packages must be an array of 1 to {MAX_PACKAGE_KINDS} packages.")
        clean = []
        for i, k in enumerate(pk):
            if not isinstance(k, dict) or not isinstance(k.get("name"), str) or not k["name"]:
                raise PricingError(f"{where}.packages[{i}] needs a name.")
            units = _nonneg(k.get("units"), f"{where}.packages[{i}].units")
            if units <= 0:
                raise PricingError(f"{where}.packages[{i}].units must be positive.")
            clean.append({"name": k["name"], "units": units, "price": _nonneg(k.get("price"), f"{where}.packages[{i}].price")})
        out["packages"] = sorted(clean, key=lambda k: (k["units"], k["price"], k["name"]))
        if rule.get("overage_unit_price") is not None:
            out["overage_unit_price"] = _nonneg(rule["overage_unit_price"], f"{where}.overage_unit_price")
    return out


def _package_bill(units: float, packages: list, overage: Optional[float]) -> tuple[float, list]:
    """Cheapest whole-package mix covering `units` (plus priced overage). Exhaustive over the
    counts of all but the largest package (each capped at MAX_PACKAGE_COUNT); the largest
    package's count is solved in closed form. Deterministic tie order: cost, fewer packages, mix."""
    *small, big = packages
    best: Optional[tuple] = None

    def consider(cost: float, mix: tuple):
        nonlocal best
        key = (round(cost, 6), sum(mix), mix)
        if best is None or key < best[0]:
            best = (key, cost, mix)

    def leaf(covered: float, cost: float, mix: tuple):
        rem = max(0.0, units - covered)
        full = math.ceil(rem / big["units"]) if rem else 0
        options = {full}
        if overage is not None:
            options |= {0, math.floor(rem / big["units"])}
        for n in options:
            if n > MAX_PACKAGE_COUNT:
                continue
            short = max(0.0, rem - n * big["units"])
            if short and overage is None:
                continue
            consider(cost + n * big["price"] + short * (overage or 0.0), mix + (n,))

    def rec(i: int, covered: float, cost: float, mix: tuple):
        if i == len(small):
            leaf(covered, cost, mix)
            return
        k = small[i]
        need = math.ceil((units - covered) / k["units"]) if units > covered else 0
        for n in range(0, min(need, MAX_PACKAGE_COUNT) + 1):
            if best is not None and cost + n * k["price"] > best[1]:
                break
            rec(i + 1, covered + n * k["units"], cost + n * k["price"], mix + (n,))

    rec(0, 0.0, 0.0, ())
    if best is None:
        raise PricingError(f"No package mix covers {units:,.0f} units within {MAX_PACKAGE_COUNT} packages each "
                           "and no overage price is set.")
    _, cost, mix = best
    lines = [{"label": f"{n} × package {k['name']} ({k['units']:,.0f} units)", "amount_inr": n * k["price"]}
             for n, k in zip(mix, packages) if n]
    covered = sum(n * k["units"] for n, k in zip(mix, packages))
    if units > covered:
        lines.append({"label": f"overage {units - covered:,.0f} units × INR {overage:g}", "amount_inr": (units - covered) * overage})
    return cost, lines


def monthly_bill(rule: dict, units: float) -> dict:
    """Bill for one month at `units`, with every line that makes it up."""
    if not _num(units) or units < 0:
        raise PricingError(f"units must be a non-negative number, got {units!r}.")
    t, lines = rule["type"], []
    if t == "per_unit":
        lines.append({"label": f"{units:,.0f} units × INR {rule['unit_price']:g}", "amount_inr": units * rule["unit_price"]})
    elif t == "tiered":
        if rule["tier_mode"] == "volume":
            tier = next(tr for tr in rule["tiers"] if tr["up_to"] is None or units <= tr["up_to"])
            lines.append({"label": f"{units:,.0f} units × INR {tier['unit_price']:g} (volume tier)",
                          "amount_inr": units * tier["unit_price"]})
        else:
            lo = 0.0
            for tr in rule["tiers"]:
                hi = math.inf if tr["up_to"] is None else tr["up_to"]
                q = max(0.0, min(units, hi) - lo)
                if q > 0:
                    lines.append({"label": f"{q:,.0f} units × INR {tr['unit_price']:g} (tier to "
                                           f"{'∞' if tr['up_to'] is None else f'{hi:,.0f}'})",
                                  "amount_inr": q * tr["unit_price"]})
                lo = hi
                if units <= hi:
                    break
    elif t == "credit":
        credits = units * rule["credits_per_unit"]
        paid = max(0.0, credits - rule["included_credits"])
        lines.append({"label": f"{credits:,.0f} credits − {rule['included_credits']:,.0f} included = {paid:,.0f} × INR "
                               f"{rule['price_per_credit']:g}", "amount_inr": paid * rule["price_per_credit"]})
    elif t == "commitment":
        lines.append({"label": f"commitment {rule['committed_units']:,.0f} units", "amount_inr": rule["commit_price"]})
        over = max(0.0, units - rule["committed_units"])
        if over:
            lines.append({"label": f"overage {over:,.0f} units × INR {rule['overage_unit_price']:g}",
                          "amount_inr": over * rule["overage_unit_price"]})
    elif t == "package":
        _, lines = _package_bill(units, rule["packages"], rule.get("overage_unit_price"))
    if rule.get("fixed_monthly_inr"):
        lines.append({"label": "fixed monthly fee", "amount_inr": rule["fixed_monthly_inr"]})
    subtotal = sum(ln["amount_inr"] for ln in lines)
    minimum = rule.get("minimum_monthly_inr")
    if minimum is not None and subtotal < minimum:
        lines.append({"label": f"top-up to the minimum commitment INR {minimum:,.0f}", "amount_inr": minimum - subtotal})
    total = sum(ln["amount_inr"] for ln in lines)
    return {"units": units, "total_inr": round(total, 2),
            "lines": [{**ln, "amount_inr": round(ln["amount_inr"], 2)} for ln in lines],
            "effective_unit_price": None if units == 0 else round(total / units, 6)}


# ---------------------------------------------------------------- forecast

def _validate_history(history: Any) -> list:
    if history is None:
        return []
    if not isinstance(history, list):
        raise PricingError("history must be an array of monthly usage numbers (oldest first).")
    return [_nonneg(v, f"history[{i}]") for i, v in enumerate(history)]


def _fit(method: str, train: list):
    """Return f(k) giving the forecast k months after the last training point (k >= 1)."""
    n, last = len(train), train[-1]
    if method == "naive":
        return lambda k: last
    if method == "linear_trend":
        xs = range(n)
        mx, my = (n - 1) / 2, sum(train) / n
        sxx = sum((x - mx) ** 2 for x in xs)
        slope = 0.0 if sxx == 0 else sum((x - mx) * (y - my) for x, y in zip(xs, train)) / sxx
        icpt = my - slope * mx
        return lambda k: max(0.0, icpt + slope * (n - 1 + k))
    first = train[0]
    if first <= 0 or last <= 0:
        return None                     # growth is undefined from or to zero usage
    g = (last / first) ** (1 / (n - 1)) - 1 if n > 1 else 0.0
    return lambda k: last * (1 + g) ** k


def _mape(actual: list, pred: list) -> Optional[float]:
    pairs = [(a, p) for a, p in zip(actual, pred) if a > 0]
    if not pairs:
        return None
    return sum(abs(a - p) / a for a, p in pairs) / len(pairs)


def backtest(history: list) -> dict:
    hold = min(HOLDOUT_MAX, max(1, len(history) // 3))
    train, test = history[:-hold], history[-hold:]
    scores = []
    for m in METHOD_ORDER:
        f = _fit(m, train) if len(train) >= 2 else None
        if f is None:
            scores.append({"method": m, "mape": None, "usable": False})
            continue
        pred = [f(k) for k in range(1, hold + 1)]
        mape = _mape(test, pred)
        scores.append({"method": m, "mape": None if mape is None else round(mape, 4), "usable": mape is not None,
                       "holdout_predictions": [round(x, 2) for x in pred]})
    usable = [s for s in scores if s["usable"]]
    winner = min(usable, key=lambda s: (s["mape"], METHOD_ORDER.index(s["method"]))) if usable else None
    return {"holdout_months": hold, "holdout_actual": test, "scores": scores,
            "winner": None if winner is None else winner["method"],
            "winner_mape": None if winner is None else winner["mape"]}


def forecast_usage(history: Any, months: int = 12, annual_growth_pct: Any = None,
                   last_known: Any = None) -> dict:
    """Monthly usage forecast for the next `months` with low/base/high scenarios."""
    if not isinstance(months, int) or isinstance(months, bool) or not 1 <= months <= 60:
        raise PricingError("months must be a whole number between 1 and 60.")
    hist = _validate_history(history)
    flags: list = []
    if len(hist) >= MIN_HISTORY:
        bt = backtest(hist)
        if bt["winner"] is not None:
            f = _fit(bt["winner"], hist)
            base = [f(k) for k in range(1, months + 1)]
            band = max(bt["winner_mape"], BAND_FLOOR)
            return _scenarios(base, band, bt["winner"], bt, flags,
                              f"{bt['winner']} won the backtest (MAPE {bt['winner_mape']:.1%} on the last "
                              f"{bt['holdout_months']} months)")
        flags.append({"code": "BACKTEST_UNUSABLE", "message": "History has no positive hold-out months; using the fallback."})
    start = hist[-1] if hist else (last_known if _num(last_known) and last_known >= 0 else None)
    if start is None:
        return {"method": None, "months": months, "base": None, "low": None, "high": None, "backtest": None,
                "band_pct": None, "note": None,
                "flags": flags + [{"code": "USAGE_MISSING", "message": "No usage history or current usage: no forecast."}]}
    g = annual_growth_pct if _num(annual_growth_pct) else None
    if g is None:
        flags.append({"code": "GROWTH_MISSING", "message": "No usage growth assumption: flat usage assumed (labelled)."})
        g = 0.0
    flags.append({"code": "FORECAST_FALLBACK",
                  "message": f"Fewer than {MIN_HISTORY} months of history: forecast uses the assumed growth "
                             f"{g:g}%/yr, not a fitted trend."})
    base = [start * (1 + g / 100) ** (k / 12) for k in range(1, months + 1)]
    return _scenarios(base, max(abs(g) / 100, BAND_FLOOR) if g else BAND_FLOOR, FALLBACK_METHOD, None, flags,
                      f"assumed growth {g:g}%/yr from the last known usage {start:,.0f}/month")


def _scenarios(base: list, band: float, method: str, bt: Optional[dict], flags: list, note: str) -> dict:
    return {"method": method, "months": len(base), "note": note, "band_pct": round(band * 100, 1),
            "base": [round(x, 2) for x in base],
            "low": [round(x * (1 - band), 2) for x in base],
            "high": [round(x * (1 + band), 2) for x in base],
            "backtest": bt, "flags": flags}


# ---------------------------------------------------------------- cost of a forecast + optimiser

def cost_of(rule: dict, usage: list) -> dict:
    bills = [monthly_bill(rule, u) for u in usage]
    return {"annual_inr": round(sum(b["total_inr"] for b in bills), 2),
            "monthly_inr": [b["total_inr"] for b in bills]}


def optimise(plans: Any, fc: dict) -> dict:
    """Price every plan against the low/base/high usage scenarios and pick one."""
    if not isinstance(plans, list) or not plans:
        raise PricingError("plans must be a non-empty array of {name, rule}.")
    if fc.get("base") is None:
        return {"plans": [], "recommended": None, "robust_choice": None, "flags": fc.get("flags", []),
                "note": "No usage forecast: plans cannot be priced."}
    names, rows = set(), []
    for i, pl in enumerate(plans):
        if not isinstance(pl, dict) or not isinstance(pl.get("name"), str) or not pl["name"]:
            raise PricingError(f"plans[{i}] needs a name.")
        if pl["name"] in names:
            raise PricingError(f"duplicate plan name {pl['name']!r}.")
        names.add(pl["name"])
        rule = validate_rule(pl.get("rule"), f"plans[{i}].rule")
        rows.append({"name": pl["name"], "rule": rule,
                     **{sc: cost_of(rule, fc[sc])["annual_inr"] for sc in ("low", "base", "high")}})
    best_base = min(r["base"] for r in rows)
    best_high = min(r["high"] for r in rows)
    for r in rows:
        r["regret_base_inr"] = round(r["base"] - best_base, 2)
        r["regret_high_inr"] = round(r["high"] - best_high, 2)
    rec = min(rows, key=lambda r: (r["base"], r["high"], r["name"]))
    robust = min(rows, key=lambda r: (r["high"], r["base"], r["name"]))
    note = (f"{rec['name']} is cheapest on base usage (INR {rec['base']:,.0f}/yr)."
            + ("" if robust["name"] == rec["name"] else
               f" {robust['name']} is cheapest if usage runs high (INR {robust['high']:,.0f}/yr vs "
               f"{rec['high']:,.0f}); choose it if overrun risk matters more than the base saving."))
    return {"plans": [{k: v for k, v in r.items() if k != "rule"} for r in rows],
            "recommended": rec["name"], "robust_choice": robust["name"], "note": note, "flags": fc.get("flags", [])}
