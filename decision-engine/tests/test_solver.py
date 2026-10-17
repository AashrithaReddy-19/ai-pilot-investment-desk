"""Phase 5.5: branch-and-bound solver — exact agreement, selectable objectives, deterministic cap."""
import copy, json, random
from pathlib import Path
import pytest
from fastapi.testclient import TestClient

from app.core import load_pilots, build_portfolio, view
from app import solver as S
from app.main import app

c = TestClient(app)
D = Path(__file__).resolve().parent.parent / "data"
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())
DEF = EXT["defaults"]
PD = {**DEF}


def brute_force(items, budget):
    """Reference enumeration with the same group rule and tie key (small n only)."""
    from itertools import combinations
    def key(sub):
        return (-sum(it.value for it in sub), sum(it.cost for it in sub),
                [it.order for it in sorted(sub, key=lambda x: x.order)])
    best, bk = None, None
    for r in range(1, len(items) + 1):
        for sub in combinations(items, r):
            if sum(it.cost for it in sub) > budget: continue
            if len({it.group for it in sub}) != len(sub): continue
            k = key(sub)
            if bk is None or k < bk: best, bk = sub, k
    return {it.pid for it in (best or [])}


def test_solver_matches_reference_enumeration_on_seeded_random_small_inputs():
    """The plan's own test: the solver must agree with the exact search on small inputs."""
    rng = random.Random(20260717)
    checked = 0
    for case in range(12):
        n = rng.randint(4, 9)
        items = [S.Item(pid=f"P{i}", order=i, value=rng.randint(10000, 90000),
                        cost=rng.randint(20000, 250000), group=f"g{i}",
                        rank=rng.randint(0, 2), adoption=float(rng.randint(30, 80))) for i in range(n)]
        budget = rng.randint(150000, 700000)
        exp = brute_force(items, budget)
        if not exp:                      # nothing fits yet; raise the budget until something does
            budget += 300000
            exp = brute_force(items, budget)
        got, nodes, capped = S.solve(items, budget)
        assert not capped, f"case {case}: unexpectedly capped after {nodes} nodes"
        assert {it.pid for it in got} == exp
        checked += 1
    assert checked == 12


def _reference(L, budget, mode="independent"):
    """Pre-Phase-5 enumeration using the same core helpers (agreement oracle)."""
    from app.core import _set_key, pilot_key, RANK
    cands = [p for p in L["pilots"] if view(p, mode)["net"] > 0]
    cands.sort(key=lambda p: pilot_key(p, mode))
    best, bk = [], None
    for mask in range(1, 1 << len(cands)):
        s = [p for i, p in enumerate(cands) if mask >> i & 1]
        if sum(p.setup for p in s) > budget: continue
        if len({p.group for p in s}) != len(s): continue
        k = _set_key(s, mode)
        if bk is None or k < bk: best, bk = s, k
    return [p.id for p in best]


def test_build_portfolio_solver_path_matches_reference_enumeration():
    """18 candidates force the generalized path; it must equal the pre-Phase-5 enumeration."""
    rows = copy.deepcopy(FIX["records"]["pilots"])
    for i in range(6, 14):
        row = copy.deepcopy(rows[0])
        row["id"] = f"AI{i}"
        row["overlap_group"] = f"g{i}"
        rows.append(row)
    for i in range(14, 19):                      # 5 fixture + 13 + 5 = 18 candidates
        row = copy.deepcopy(rows[0])
        row["id"] = f"AI{i}"
        row["overlap_group"] = f"g{i}"
        rows.append(row)
    big = load_pilots({"records": {"pilots": rows}},
                      [{"id": f"AI{i}", "adoption_pct": 70, "readiness": "high"} for i in range(1, 19)], PD)
    assert len(big["pilots"]) == 18
    pf = build_portfolio(big["pilots"], 600_000)
    assert set(pf["selected"]) == set(_reference(big, 600_000))


def test_npv_objective_flips_selection_toward_earlier_cash():
    """A has the bigger net but a 6-month delay; B pays back sooner. Net picks A, NPV picks B."""
    base = {"setup_cost_inr": 0, "annual_benefit_inr": 0, "annual_running_cost_inr": 0, "task": "t"}
    rows = [
        {**base, "id": "A", "setup_cost_inr": 160000, "annual_benefit_inr": 190000,
         "annual_running_cost_inr": 60000, "delay_months": 12},
        {**base, "id": "B", "setup_cost_inr": 140000, "annual_benefit_inr": 140000, "annual_running_cost_inr": 20000},
    ]
    L = load_pilots({"records": {"pilots": rows}}, [], PD)
    assert len(L["pilots"]) == 2
    net = build_portfolio(L["pilots"], 250000, d=PD)
    npv = build_portfolio(L["pilots"], 250000, objective="npv", d=PD)
    assert net["selected"] == ["A"]
    assert npv["selected"] == ["B"]
    # the choice is real: B's NPV beats A's at the same horizon and rate
    by = {p.id: p for p in L["pilots"]}
    assert view(by["B"], "independent", PD)["multi_period"]["npv_inr"] > \
           view(by["A"], "independent", PD)["multi_period"]["npv_inr"]


def test_risk_adjusted_objective_rewards_readiness_over_raw_net():
    """A wins on net but is low readiness; B is high readiness. Net picks A, risk_adjusted picks B."""
    base = {"setup_cost_inr": 0, "annual_benefit_inr": 0, "annual_running_cost_inr": 0, "task": "t"}
    rows = [
        {**base, "id": "A", "setup_cost_inr": 150000, "annual_benefit_inr": 210000, "annual_running_cost_inr": 60000},
        {**base, "id": "B", "setup_cost_inr": 140000, "annual_benefit_inr": 140000, "annual_running_cost_inr": 20000},
    ]
    ext = [{"id": "A", "readiness": "low"}, {"id": "B", "readiness": "high"}]
    L = load_pilots({"records": {"pilots": rows}}, ext, PD)
    net = build_portfolio(L["pilots"], 250000, d=PD)
    risk = build_portfolio(L["pilots"], 250000, objective="risk_adjusted", d=PD)
    assert net["selected"] == ["A"] and risk["selected"] == ["B"]
    assert risk["objective_value"] == round(view(L["pilots"][1], "independent")["net"] * 1.0)


def test_node_cap_is_deterministic_and_flagged_on_conflict_pathology():
    """Paired groups + full budget: the include chain keeps hitting conflicts, the optimistic
    (group-blind) bound keeps saying 'worth exploring', so the tree blows to the node budget.
    Flag on and result stable run-to-run."""
    items = [S.Item(pid=f"P{i}", order=i, value=30000, cost=10000 + i, group=f"g{i // 2}", rank=1, adoption=70.0)
             for i in range(40)]
    total = sum(it.cost for it in items)
    r1, n1, cap1 = S.solve(items, total)
    r2, n2, cap2 = S.solve(items, total)
    assert cap1 is cap2 is True          # deterministic traversal hits the cap both times
    assert {it.pid for it in r1} == {it.pid for it in r2}
    assert n1 == n2
    # also visible through build_portfolio (guard against engine-level surprises)
    rows = copy.deepcopy(FIX["records"]["pilots"])
    for i in range(6, 41):               # 5 fixture + 35 = 40 candidates (the cap, not past it)
        row = copy.deepcopy(rows[0])
        row["id"] = f"AI{i}"
        row["overlap_group"] = f"g{i // 2}"
        rows.append(row)
    big = load_pilots({"records": {"pilots": rows}},
                      [{"id": f"AI{i}", "adoption_pct": 70, "readiness": "high"} for i in range(1, 42)], PD)
    total_setup = sum(p.setup for p in big["pilots"])
    pf1 = build_portfolio(big["pilots"], total_setup)
    pf2 = build_portfolio(big["pilots"], total_setup)
    if pf1["solver"]["capped"]:
        assert pf1["selected"] == pf2["selected"]


def test_solve_returns_nothing_when_nothing_fits():
    items = [S.Item(pid="P0", order=0, value=100000, cost=900000, group="g", rank=1, adoption=50.0)]
    got, nodes, capped = S.solve(items, 100000)
    assert got == [] and not capped and nodes <= 2


def test_api_portfolio_risk_adjusted_objective():
    j = c.post("/portfolio", json={"objective": "risk_adjusted", "budget": 600000}).json()
    assert j["objective"] == "risk_adjusted"
    assert j["ok"] is True and j["selected"]
    # cull messages speak the objective's language
    assert all("Risk-adjusted value" in r["reason"] or "Setup" in r["reason"]
               or "Overlap" in r["reason"] or "Not funded" in r["reason"] or "NPV" in r["reason"]
               for r in j["rejected"])
