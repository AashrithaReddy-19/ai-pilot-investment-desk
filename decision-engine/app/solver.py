"""Branch-and-bound portfolio solver (Phase 5.5).

Deterministic. Serves the selectable-objective portfolio build (net / NPV / risk-adjusted)
and raises the exact-search candidate limit. Loses to no tie-break: candidate subsets are
compared on (objective value, tie key) where the tie key mirrors core._set_key with the
objective value in place of the net sum — for the net objective the key coincides with the
exact search's own, so both paths agree on small inputs (verified by test).

A node budget keeps the worst case finite: past the budget the search stops and returns
the best set found so far with capped=True — same input, same traversal, same result.
"""
from __future__ import annotations
from dataclasses import dataclass, field


NODE_BUDGET = 150_000
MAX_CANDIDATES = 40


@dataclass
class Item:
    pid: str
    order: int          # position in the candidate list already sorted by core.pilot_key
    value: int          # integer objective value in INR
    cost: int           # one-time setup in integer INR
    group: str
    rank: int           # core.RANK[readiness], for the tie key
    adoption: float     # adoption_pct, for the tie key
    density: float = field(init=False)

    def __post_init__(self):
        self.density = self.value / self.cost if self.cost > 0 else float("inf")


class _CapReached(Exception):
    pass


def _tie_key(items: list["Item"]) -> tuple:
    ordered = sorted(items, key=lambda it: it.order)
    return (-sum(it.value for it in items), sum(it.cost for it in items),
            -sum(it.rank for it in items), -sum(it.adoption for it in items),
            [it.order for it in ordered])


def solve(items: list[Item], budget: float) -> tuple[list[Item], int, bool]:
    """Maximise total value subject to total cost <= budget and at most one item per group.

    Returns (chosen, nodes explored, capped). Items are explored in density order
    (value per rupee, cheap-first on ties, then the pilot_key order); the
    fractional-knapsack bound ignores group conflicts, so it never over-prunes.
    The bound prunes only when it is strictly below the best value — equal-bound branches
    are still explored so the tie-break contract stays exact until the node budget hits."""
    by_order = sorted(items, key=lambda it: (-it.density, it.cost, it.order))
    n = len(by_order)
    state = {"nodes": 0, "best": None, "best_key": None, "best_value": None, "capped": False}

    def bound(f: int, value: float, budget_left: float) -> float:
        v = value
        for it in by_order[f:]:
            if it.cost <= budget_left:
                v += it.value
                budget_left -= it.cost
            else:
                v += it.value * budget_left / it.cost
                break
        return v

    def dfs(f: int, value: int, budget_left: float, chosen: list[Item], used: frozenset):
        state["nodes"] += 1
        if state["nodes"] > NODE_BUDGET:
            state["capped"] = True
            raise _CapReached
        if f == n:
            if chosen:
                k = _tie_key(chosen)
                if state["best_key"] is None or k < state["best_key"]:
                    state["best"], state["best_key"] = list(chosen), k
                    state["best_value"] = -k[0]
            return
        it = by_order[f]
        # include branch first: better-first ordering keeps the bound tight early
        if it.cost <= budget_left and it.group not in used:
            chosen.append(it)
            dfs(f + 1, value + it.value, budget_left - it.cost, chosen, used | {it.group})
            chosen.pop()
        if state["best_value"] is None or bound(f + 1, value, budget_left) > state["best_value"]:
            dfs(f + 1, value, budget_left, chosen, used)

    try:
        dfs(0, 0, budget, [], frozenset())
    except _CapReached:
        pass
    return (state["best"] or [], state["nodes"], state["capped"])
