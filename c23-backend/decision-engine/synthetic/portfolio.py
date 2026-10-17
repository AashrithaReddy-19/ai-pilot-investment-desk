"""Overlap accounting and an exact portfolio optimizer that mirrors backend/internal/portfolio.

Overlap (c23-rules-v1): pilots sharing a known overlap_group draw on ONE benefit pool = the largest single claim.
Unresolved -> only the largest claim is recognised (lowest ID on ties) and approval is blocked.

Optimizer: same eligibility, constraints, objective and tie-break order as the Go optimizer
(finance complete, not STOP, not REVISE, overlap group known, positive net on the basis; setup <= budget; at most one
pilot per overlap group; maximise recognised annual net; then faster payback, higher readiness, higher adoption,
lower setup, stable ID order). The Go optimizer is exhaustive and capped at 20 candidates; the synthetic set has
more, so this module solves the same problem exactly with a grouped knapsack DP (checked against brute force and
against the Go optimizer on a 20-pilot subset).
"""
from __future__ import annotations
import re
from fractions import Fraction
from functools import cmp_to_key

from synthetic import rules as R

READINESS_RANK = {"low": 1, "medium": 2, "high": 3}
_NAT = re.compile(r"\d+|\D+")


def nat_key(s: str):
    return [(0, int(t), "") if t.isdigit() else (1, 0, t) for t in _NAT.findall(s)]


def pid(p: dict) -> str:
    return p.get("id") or p.get("record_id")


# ---------------------------------------------------------------- overlap / benefit ledger

def group_pool(claims: dict) -> tuple[Fraction, str]:
    """Pool = largest single claim; primary = that pilot (lowest natural ID on ties)."""
    primary = sorted(claims, key=lambda k: (-claims[k], nat_key(k)))[0]
    return claims[primary], primary


def recognise(claims: dict, resolution: dict | None = None) -> dict:
    """Recognised benefit per pilot for one overlap group. Never exceeds the pool. Raises ValueError on invalid input.

    resolution: None (unresolved) | {"type": "REMOVE_ONE"|"EXCLUDE_SECOND", "primary": id}
              | {"type": "SPLIT_BENEFIT", "shares_pct": {id: pct}} (default equal shares)
              | {"type": "CUSTOM_ALLOCATION", "allocations": {id: inr}}
    """
    claims = {k: Fraction(v) for k, v in claims.items()}
    pool, primary = group_pool(claims)
    if resolution is None:
        return {k: (claims[k] if k == primary else Fraction(0)) for k in claims}
    t = resolution["type"]
    if t in ("REMOVE_ONE", "EXCLUDE_SECOND"):
        keep = resolution.get("primary", primary)
        if keep not in claims:
            raise ValueError("primary pilot is not in the group")
        out = {k: (claims[k] if k == keep else Fraction(0)) for k in claims}
        if t == "REMOVE_ONE":
            out = {keep: out[keep]}                     # the others leave the portfolio entirely
        return out
    if t == "SPLIT_BENEFIT":
        shares = resolution.get("shares_pct") or {k: Fraction(100, len(claims)) for k in claims}
        if set(shares) != set(claims) or any(Fraction(v) < 0 for v in shares.values()) or sum(Fraction(v) for v in shares.values()) != 100:
            raise ValueError("shares must cover every pilot in the group, be >= 0 and sum to 100")
        # pool x share, floored to paise, capped at the pilot's own claim
        return {k: min(claims[k], Fraction(int(pool * Fraction(shares[k]) / 100 * 100), 100)) for k in claims}
    if t == "CUSTOM_ALLOCATION":
        alloc = {k: Fraction(v) for k, v in resolution.get("allocations", {}).items()}
        if set(alloc) != set(claims) or any(v < 0 or v > claims[k] for k, v in alloc.items()) or sum(alloc.values()) > pool:
            raise ValueError("each allocation must be 0..own claim and the total must not exceed the pool")
        return alloc
    raise ValueError(f"unknown resolution type {t!r}")


def overlap_summary(pilots: list, basis: str = R.BASIS_STATED) -> list:
    """Per overlap group: claimed total, pool, double-count that a naive sum would add, members."""
    groups: dict = {}
    for p in pilots:
        g = p.get("overlap_group")
        groups.setdefault(g if g else None, []).append(p)
    out = []
    for g in sorted(groups, key=lambda x: (x is None, x or "")):
        members = sorted(groups[g], key=lambda p: nat_key(pid(p)))
        claims = {pid(p): R.basis_benefit(p, basis) for p in members}
        known = {k: v for k, v in claims.items() if v is not None}
        if g is None:
            out.append({"overlap_group": "UNKNOWN", "pilots": len(members), "ids": [pid(p) for p in members], "claimed_total": None,
                        "pool": None, "recognised_unresolved": None, "double_count_if_summed": None, "primary": None})
            continue
        pool, primary = group_pool(known) if known else (None, None)
        claimed = sum(known.values(), Fraction(0)) if known else None
        out.append({"overlap_group": g, "pilots": len(members), "ids": [pid(p) for p in members], "claimed_total": claimed, "pool": pool,
                    "recognised_unresolved": pool, "double_count_if_summed": (claimed - pool) if known else None, "primary": primary})
    return out


# ---------------------------------------------------------------- optimizer

class _Cand:
    __slots__ = ("id", "group", "setup", "net", "readiness", "adoption")

    def __init__(self, p: dict, ev: R.Evaluation, net: Fraction):
        self.id, self.group = pid(p), p["overlap_group"]
        self.setup = R.frac(p["setup_cost_inr"])
        self.net = net
        self.readiness = READINESS_RANK.get(ev.readiness_level, 0)
        ad = p.get("actual_adoption_pct") if p.get("actual_adoption_pct") is not None else p.get("expected_adoption_pct")
        self.adoption = R.frac(ad) if ad is not None else Fraction(0)


def eligibility(p: dict, ev: R.Evaluation, basis: str) -> tuple[bool, str, str]:
    """(eligible, code, reason) in the Go optimizer's order."""
    if p.get("setup_cost_inr") is None or p.get("annual_running_cost_inr") is None or p.get("annual_benefit_inr") is None:
        return False, "FINANCE_INCOMPLETE", "A critical financial input is unknown."
    if ev.decision == R.STOP:
        return False, "STOP_PILOT_SELECTED", f"Mandatory STOP decision ({ev.primary_reason})."
    if ev.decision == R.REVISE:
        return False, "PILOT_NEEDS_REVISION", f"Must be revised before funding ({ev.primary_reason})."
    if not p.get("overlap_group"):
        return False, "OVERLAP_UNKNOWN", "Overlap group unknown; double counting cannot be ruled out."
    b = R.basis_benefit(p, basis)
    if b is None:
        return False, "TIME_NOT_CASH", f"No cash-recognisable benefit on the {basis} basis."
    if b - R.frac(p["annual_running_cost_inr"]) <= 0:
        return False, "NEVER_PAYS_BACK", f"Net annual benefit on the {basis} basis is not positive."
    return True, "", ""


def _key(sel: tuple) -> dict:
    net = sum((c.net for c in sel), Fraction(0))
    setup = sum((c.setup for c in sel), Fraction(0))
    return {"net": net, "setup": setup, "payback": (setup / net) if net > 0 else None,
            "readiness": sum(c.readiness for c in sel), "adoption": sum(c.adoption for c in sel),
            "ids": sorted((c.id for c in sel), key=nat_key)}


def _cmp(a: dict, b: dict) -> int:
    """Negative if a is better (Go setKey.better order)."""
    if a["net"] != b["net"]:
        return -1 if a["net"] > b["net"] else 1
    if a["payback"] is not None and b["payback"] is not None and a["payback"] != b["payback"]:
        return -1 if a["payback"] < b["payback"] else 1
    for f in ("readiness", "adoption"):
        if a[f] != b[f]:
            return -1 if a[f] > b[f] else 1
    if a["setup"] != b["setup"]:
        return -1 if a["setup"] < b["setup"] else 1
    for x, y in zip(a["ids"], b["ids"]):
        if x != y:
            return -1 if nat_key(x) < nat_key(y) else 1
    return (len(a["ids"]) > len(b["ids"])) - (len(a["ids"]) < len(b["ids"]))


def _best(sels: list) -> tuple:
    return min(sels, key=cmp_to_key(lambda a, b: _cmp(_key(a), _key(b))))


def optimize(pilots: list, evals: dict, budget, basis: str = R.BASIS_CASH) -> dict:
    """Exact optimum. `evals` maps id -> Evaluation (decisions used for eligibility, like the Go optimizer)."""
    budget = R.frac(budget)
    ps = sorted(pilots, key=lambda p: nat_key(pid(p)))
    cands, excluded = [], []
    for p in ps:
        ok, code, why = eligibility(p, evals[pid(p)], basis)
        if not ok:
            excluded.append({"pilot_id": pid(p), "code": code, "reason": why})
            continue
        cands.append(_Cand(p, evals[pid(p)], R.basis_benefit(p, basis) - R.frac(p["annual_running_cost_inr"])))
    groups: dict = {}
    for c in cands:
        groups.setdefault(c.group, []).append(c)
    # DP over groups: state = total setup -> selections tied on (net, readiness, adoption) at that exact setup.
    # Within one setup value, payback and setup are fixed, so this partial order is exact; ties are all kept.
    states = {Fraction(0): [()]}
    for g in sorted(groups):
        nxt: dict = {}
        for setup, sels in states.items():
            for sel in sels:
                for opt in [None] + groups[g]:
                    s = setup + (opt.setup if opt else 0)
                    if s > budget:
                        continue
                    new = sel + ((opt,) if opt else ())
                    cur = nxt.get(s)
                    if cur is None:
                        nxt[s] = [new]
                        continue
                    k_new, k_cur = _key(new), _key(cur[0])
                    t_new = (k_new["net"], k_new["readiness"], k_new["adoption"])
                    t_cur = (k_cur["net"], k_cur["readiness"], k_cur["adoption"])
                    if t_new > t_cur:
                        nxt[s] = [new]
                    elif t_new == t_cur:
                        cur.append(new)
        states = nxt
    best = _best([sel for sels in states.values() for sel in sels])
    chosen = {c.id for c in best}
    by_group = {c.group: c.id for c in best}
    for c in cands:
        if c.id in chosen:
            continue
        if c.group in by_group:
            excluded.append({"pilot_id": c.id, "code": "DOUBLE_COUNT_RISK",
                             "reason": f"Shares benefit group {c.group!r} with selected {by_group[c.group]}; the benefit is counted once."})
        elif c.setup > budget:
            excluded.append({"pilot_id": c.id, "code": "BUDGET_EXCEEDED", "reason": f"Setup INR {R.fmt(c.setup)} alone exceeds the budget."})
        else:
            excluded.append({"pilot_id": c.id, "code": "NOT_FUNDED", "reason": "Not funded: the budget is better used by the selected pilots."})
    excluded.sort(key=lambda e: nat_key(e["pilot_id"]))
    k = _key(best)
    return {"basis": basis, "budget": budget, "selected": k["ids"], "total_setup": k["setup"], "net_annual": k["net"],
            "payback_months": (k["payback"] * 12) if k["payback"] is not None else None, "excluded": excluded,
            "candidates": len(cands)}


def brute_force(pilots: list, evals: dict, budget, basis: str = R.BASIS_CASH) -> list:
    """Exhaustive search (small inputs only) used to verify `optimize`."""
    from itertools import combinations
    budget = R.frac(budget)
    cands = [_Cand(p, evals[pid(p)], R.basis_benefit(p, basis) - R.frac(p["annual_running_cost_inr"]))
             for p in sorted(pilots, key=lambda p: nat_key(pid(p))) if eligibility(p, evals[pid(p)], basis)[0]]
    feasible = [()]
    for n in range(1, len(cands) + 1):
        for sel in combinations(cands, n):
            if sum(c.setup for c in sel) <= budget and len({c.group for c in sel}) == len(sel):
                feasible.append(sel)
    return _key(_best(feasible))["ids"]
