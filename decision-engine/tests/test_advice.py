"""Phase 5: evidence bands, gap detector, decision outcomes, next-best actions, head-to-head."""
import copy, json
from pathlib import Path
import pytest
from fastapi.testclient import TestClient

from app.core import load_pilots, build_portfolio
from app import advice, solver, core, main
from app.main import app

c = TestClient(app)
D = Path(__file__).resolve().parent.parent / "data"
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())
DEF = EXT["defaults"]

PD = {**DEF}   # shipped defaults; loader relies on these keys


def P(**over):
    """One pilot row (AI1-shaped) with the given tweaks. Cost lines make inputs 'calculated'."""
    row = copy.deepcopy(FIX["records"]["pilots"][0])
    row["id"] = "P1"
    row.update(over)
    return row


def advise(fixture_rows, ext_rows=None, d=None):
    return advice.advice_for(load_pilots({"records": {"pilots": fixture_rows}},
                                         ext_rows or [], d or PD)["pilots"][0], d or PD)


def test_band_edges_never_a_percentage_output():
    """Band edges both sides: >= 0.75 high, >= 0.40 medium (inclusive), below low; fixture caps at low."""
    assert advice._band(0.749, False) == "medium"
    assert advice._band(0.75, False) == "high"
    assert advice._band(0.3999, False) == "low"
    assert advice._band(0.40, False) == "medium"
    assert advice._band(0.999, True) == "low"          # fixture (synthetic) cap
    assert advice._band(0.0, False) == "low"


def test_shipped_fixture_is_low_and_insufficient():
    """fixture (synthetic) money values cap the band at Low; nothing observed => insufficient_information."""
    a = advise([P()])
    assert a["evidence"]["band"] == "low"
    assert a["evidence"]["band_capped_by_fixture"] is True
    assert a["evidence"]["share"] == 0.0 and a["evidence"]["observed_weight"] == 0
    assert a["outcome"] == "insufficient_information"
    assert [g["input"] for g in a["evidence"]["gaps"]] == [
        "annual_benefit_inr", "adoption_pct", "annual_running_cost_inr", "setup_cost_inr", "quality_pct", "readiness"]
    assert a["actions"] and all(set(x) >= {"code", "action", "owner_hint", "expected_test"} for x in a["actions"])


def test_real_money_moves_band_to_medium_and_outcome_to_fund():
    """Driver + cost lines are 'calculated' (real, user-supplied): 7/11 observed => Medium; outcome fund."""
    a = advise([P(one_time_costs=[{"label": "licences", "amount_inr": 200000}],
                  recurring_costs=[{"label": "api", "amount_inr_per_year": 60000}],
                  benefit_driver={"volume_per_month": 5000, "minutes_saved_per_unit": 1.5, "loaded_cost_per_hour": 100})])
    assert a["evidence"]["band_capped_by_fixture"] is False
    assert a["evidence"]["observed_weight"] == 7 and a["evidence"]["share"] == round(7 / 11, 3)
    assert a["evidence"]["band"] == "medium"
    assert a["outcome"] == "fund"                       # net positive, break-even in horizon
    assert "ADOPTION_ASSUMED" in a["reason_codes"]       # caution actions ride along
    assert all(x["code"] != "FIXTURE_ONLY_EVIDENCE" for x in a["actions"])


def test_observed_adoption_raises_band_to_high():
    a = advise([P(one_time_costs=[{"label": "licences", "amount_inr": 200000}],
                  recurring_costs=[{"label": "api", "amount_inr_per_year": 60000}],
                  benefit_driver={"volume_per_month": 5000, "minutes_saved_per_unit": 1.5, "loaded_cost_per_hour": 100})],
               [{"id": "P1", "observed_adoption_pct": 60, "observed_quality_pct": 90}])
    assert a["evidence"]["observed_weight"] == 10       # 7 money + adoption 2 + quality 1 (readiness never)
    assert a["evidence"]["share"] == round(10 / 11, 3)
    assert a["evidence"]["band"] == "high"
    assert a["outcome"] == "fund"
    assert [g["input"] for g in a["evidence"]["gaps"]] == ["readiness"]


def test_stage_outcome_when_obvious_but_thin():
    """Run cost real, everything else fixture: observed 2/11 => Low => stage (not fund)."""
    a = advise([P(recurring_costs=[{"label": "api", "amount_inr_per_year": 60000}])])
    assert a["evidence"]["observed_weight"] == 2 and a["evidence"]["band"] == "low"
    assert a["outcome"] == "stage"


def test_defer_outcome_when_breakeven_beyond_horizon():
    a = advise([P(one_time_costs=[{"label": "build", "amount_inr": 3000000}])])
    assert a["outcome"] == "defer"
    assert a["evidence"]["observed_weight"] == 2        # real setup line only
    assert "BREAK_EVEN_BEYOND_HORIZON" in a["reason_codes"]
    assert "BREAK_EVEN_BEYOND_HORIZON" in {x["code"] for x in a["actions"]}


def test_reject_outcome_and_boundary():
    """net <= 0 on the independent lens is a reject; net == 0 boundary is a reject too (not fund)."""
    a = advise([P(annual_running_cost_inr=500000)])
    assert a["outcome"] == "reject"
    assert "NET_NOT_POSITIVE" in {x["code"] for x in a["actions"]}
    b = advise([P(annual_benefit_inr=60000, annual_running_cost_inr=60000)])
    assert b["outcome"] == "reject"


def test_low_observed_adoption_reason_code():
    """Observed adoption below break-even adoption carries its own reason code and action."""
    a = advise([P()], [{"id": "P1", "observed_adoption_pct": 5}])
    assert "LOW_OBSERVED_ADOPTION" in a["reason_codes"]
    act = next(x for x in a["actions"] if x["code"] == "LOW_OBSERVED_ADOPTION")
    assert act["action"] == "short user-training experiment" and act["owner_hint"] == "org lead"


def test_every_reason_code_has_an_action_table_row():
    for code, a in advice.NEXT_ACTIONS.items():
        assert a["action"] and a["owner_hint"] and a["expected_test"], code
    # every reason code the engine emits as reasons resolves in the table
    for emitted in ("RUN_COST_ASSUMED", "ADOPTION_ASSUMED", "CONSERVATIVE_DIES",
                    "NO_OBSERVED_ADOPTION", "NO_OBSERVED_QUALITY", "LOW_READINESS", "LOW_OBSERVED_ADOPTION",
                    "BREAK_EVEN_BEYOND_HORIZON", "FIXTURE_ONLY_EVIDENCE", "NET_NOT_POSITIVE", "NON_CASHABLE"):
        assert emitted in advice.NEXT_ACTIONS, emitted


def test_every_non_fund_outcome_has_at_least_one_action_across_shaped_pilots():
    shapes = [
        advise([P()]),                                                       # insufficient_information
        advise([P(annual_running_cost_inr=500000)]),                         # reject
        advise([P(one_time_costs=[{"label": "b", "amount_inr": 3000000}])]),  # defer (setup real)
        advise([P(recurring_costs=[{"label": "api", "amount_inr": 60000}])]),  # stage
        advise([P(one_time_costs=[{"label": "licences", "amount_inr": 5000000}],
                  benefit_driver={"volume_per_month": 10, "minutes_saved_per_unit": 1.0, "loaded_cost_per_hour": 50})]),
    ]
    for a in shapes:
        if a["outcome"] != "fund":
            assert a["actions"], f"{a['outcome']} returned no action"


def test_head_to_head_adjacent_pairs_and_overlap():
    body = {"proposals": [
        {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000},
        {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000},
    ], "budget": None}
    j = c.post("/compare", json=body).json()
    h2h = j["head_to_head"]
    assert len(h2h) == 1 and h2h[0]["over"] == "B" and h2h[0]["under"] == "A"
    joined = " | ".join(h2h[0]["reasons"])
    assert "net annual savings INR 250,000/yr vs INR 120,000/yr" in joined
    assert "payback 24 vs 20 months (4 months faster: A)" in joined
    assert "same readiness (medium)" in joined
    # shared overlap group: the eliminated competitor gets its own pair against the winner
    body2 = {"proposals": [
        {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000,
         "overlap_group": "chat"},
        {"name": "X", "setup_cost_inr": 150000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 80000,
         "overlap_group": "docs"},
        {"name": "C", "setup_cost_inr": 100000, "annual_benefit_inr": 95000, "ai_running_cost_inr": 50000,
         "overlap_group": "chat"},
    ]}
    j2 = c.post("/compare", json=body2).json()
    over = next(p for p in j2["head_to_head"] if p["over"] == "A" and p["under"] == "C")
    assert any("shared overlap group" in r and '"chat"' in r for r in over["reasons"])
    # a budget that funds only B leaves A payable but unfunded — that pair must be explained too
    body3 = {"proposals": [
        {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000},
        {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000},
    ], "budget": 600000}
    j3 = c.post("/compare", json=body3).json()
    assert j3["selection"]["selected"] == ["B"]
    p3 = next(p for p in j3["head_to_head"] if p["over"] == "B" and p["under"] == "A")
    assert any("net annual savings INR 250,000/yr vs INR 120,000/yr" in r for r in p3["reasons"])
    # same input twice: identical output (deterministic)
    assert c.post("/compare", json=body2).json()["head_to_head"] == j2["head_to_head"]


def test_api_pilots_carries_advice():
    j = c.post("/pilots", json={}).json()
    ai1 = next(p for p in j["pilots"] if p["id"] == "AI1")
    assert ai1["advice"]["evidence"]["band"] == "low"
    assert ai1["advice"]["outcome"] == "insufficient_information"
    assert ai1["advice"]["actions"]


def test_api_portfolio_objective_npv_and_invalid():
    j = c.post("/portfolio", json={}).json()                      # default objective: net, legacy path
    assert j["objective"] == "net" and j["ok"]
    j2 = c.post("/portfolio", json={"objective": "npv", "budget": 600000}).json()
    assert j2["objective"] == "npv" and j2["ok"] and j2["selected"]
    assert c.post("/portfolio", json={"objective": "sharpe"}).status_code == 422


def test_api_defaults_expose_advice_and_solver_rules():
    j = c.get("/defaults").json()
    rules = j["pipeline_rules"]
    assert rules["advice_rules"]["input_weights"]["annual_benefit_inr"] == 3
    assert rules["advice_rules"]["band_edges"] == {"medium": 0.4, "high": 0.75}
    assert "next_actions" in rules["advice_rules"] and rules["advice_rules"]["next_actions"]
    assert rules["solver"]["objectives"] == ["net", "npv", "risk_adjusted"]
    assert rules["solver"]["max_candidates"] == 40 and rules["solver"]["max_exact_candidates"] == 16
    assert "evidence_band" in rules["formulas"] and "risk_adjusted_value" in rules["formulas"]


def test_node_cap_flag_is_deterministic():
    """The pathological all-fit case binds exhaustively; the node cap stops it with a flag and a stable result."""
    rows = [P(id=f"P{i}", setup_cost_inr=10000 + i, annual_benefit_inr=30000, annual_running_cost_inr=10000) for i in range(40)]
    ext = [{"id": f"P{i}", "adoption_pct": 70, "readiness": "high"} for i in range(40)]
    L = load_pilots({"records": {"pilots": rows}}, ext, PD)
    total = sum(p.setup for p in L["pilots"])
    pf1 = build_portfolio(L["pilots"], total)      # everything fits: bound stops pruning early
    pf2 = build_portfolio(L["pilots"], total)
    assert pf1["ok"]
    if pf1["solver"]["capped"]:
        assert pf1["selected"] == pf2["selected"] and pf1["solver"]["nodes"] == pf2["solver"]["nodes"]
    else:
        assert pf1["selected"] == pf2["selected"]
