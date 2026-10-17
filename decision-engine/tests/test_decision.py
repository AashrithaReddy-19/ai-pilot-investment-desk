"""A6 decision engine: every outcome path, eligibility, weighted ranking, why A over B,
what would reverse it, and the set-level answer including no_clear_winner."""
import pytest
from fastapi.testclient import TestClient

from app import decision as DC
from app.main import app

c = TestClient(app)
OBS = {"observed_adoption_pct": 80, "observed_quality_pct": 90}
FULL = {"readiness": "high", "money_source": "observed", "overlap_group": "g",
        "baseline": {"volume_per_month": 2000, "minutes_per_unit": 6, "loaded_cost_per_hour": 1000}}


def prop(name, setup, benefit, run, **kw):
    return {"name": name, "setup_cost_inr": setup, "annual_benefit_inr": benefit, "ai_running_cost_inr": run, **kw}


def decide(props, **body):
    r = c.post("/decision", json={"proposals": props, **body})
    assert r.status_code == 200, r.text
    return r.json()


def outcome(j, name):
    return j["decisions"][name]["outcome"]


def test_every_outcome_path():
    props = [
        prop("Reject", 100000, 50000, 100000),                                           # net −50k, cut 50% > 30%
        prop("Reneg", 100000, 90000, 100000),                                            # net −10k, cut 10%
        prop("Uncomp", 300000, 110000, 20000),                                           # (100k + 20k)/110k = 1.09
        prop("Defer", 600000, 120000, 20000, **{**FULL, "overlap_group": "d"}),          # break-even 72 months
        prop("Evidence", 200000, 180000, 60000),                                         # nothing observed
        prop("Limited", 200000, 180000, 60000, **OBS),                                   # observed but band low
        prop("Invest", 200000, 180000, 60000, **{**FULL, **OBS, "overlap_group": "i"}),
    ]
    j = decide(props)
    assert {n: outcome(j, n) for n in [p["name"] for p in props]} == {
        "Reject": "reject", "Reneg": "renegotiate", "Uncomp": "renegotiate", "Defer": "defer",
        "Evidence": "gather_evidence", "Limited": "limited_pilot", "Invest": "invest"}
    assert j["overall"]["outcome"] == "invest" and j["overall"]["best"] == "Invest"


def test_non_cashable_and_over_budget():
    props = [prop("Cap", 100000, 200000, 10000, benefit_type="non-cashable"),
             prop("Big", 900000, 900000, 10000, **{**FULL, **OBS})]
    j = decide(props, budget_set={"implementation_inr": 500000})
    assert outcome(j, "Cap") == "reject" and "NON_CASHABLE" in j["decisions"]["Cap"]["reason_codes"]
    assert outcome(j, "Big") == "defer" and "OVER_BUDGET" in j["decisions"]["Big"]["reason_codes"]
    gates = {g["gate"]: g["status"] for g in j["decisions"]["Big"]["eligibility"]["gates"]}
    assert gates == {"NET_POSITIVE": "pass", "BREAKEVEN_IN_HORIZON": "pass", "BUDGET_FIT": "fail", "BENEFIT_HAS_CASH": "pass"}


def test_data_readiness_levels():
    j = decide([prop("Bare", 200000, 180000, None), prop("Full", 200000, 180000, 60000, **FULL)])
    assert j["decisions"]["Bare"]["data_readiness"]["level"] == "partial"     # run assumed, no baseline, low readiness... 4/7
    assert j["decisions"]["Full"]["data_readiness"]["level"] == "ready"
    assert "run cost stated" in j["decisions"]["Bare"]["data_readiness"]["missing"]


def test_no_clear_winner_when_scores_are_close():
    a = prop("A", 200000, 180000, 60000, **{**FULL, **OBS, "overlap_group": "a"})
    b = prop("B", 200000, 180000, 60000, **{**FULL, **OBS, "overlap_group": "b"})
    j = decide([a, b])
    assert j["overall"]["outcome"] == "no_clear_winner" and j["overall"]["contenders"] == ["A", "B"]
    assert j["overall"]["next_actions"][0]["code"] == "NO_CLEAR_WINNER"
    assert j["why_a_over_b"][0]["reasons"] == ["identical on every criterion; the 6-tier tie-break order decides"]


def test_weighted_ranking_hand_check_and_weight_override():
    a = prop("A", 200000, 180000, 60000, **{**FULL, **OBS, "overlap_group": "a"})          # net 120k, payback 20
    b = prop("B", 200000, 260000, 60000, **{**FULL, **OBS, "overlap_group": "b", "readiness": "medium"})  # net 200k
    j = decide([a, b])
    rk = {r["id"]: r for r in j["ranking"]}
    # B tops net/NPV/payback (0.30+0.20+0.15 = 65 points); A tops readiness (10); both tie on evidence + cash (25 each)
    assert rk["B"]["score"] == pytest.approx(90.0) and rk["A"]["score"] == pytest.approx(35.0)
    assert j["ranking"][0]["id"] == "B" and j["overall"]["best"] == "B"
    j2 = decide([a, b], weights={"net_annual": 0, "npv": 0, "payback": 0, "readiness": 1})
    assert j2["ranking"][0]["id"] == "A"
    assert c.post("/decision", json={"proposals": [a, b], "weights": {"vibes": 1}}).status_code == 422
    assert c.post("/decision", json={"proposals": [a, b], "weights": {k: 0 for k in DC.RANKING_WEIGHTS}}).status_code == 422


def test_why_a_over_b_names_the_gate_and_criteria():
    j = decide([prop("Good", 200000, 180000, 60000, **{**FULL, **OBS}), prop("Bad", 100000, 50000, 100000)])
    pair = j["why_a_over_b"][0]
    assert pair["over"] == "Good" and pair["reasons"][0].startswith("Bad fails a mandatory eligibility gate")
    assert any(r.startswith("net annual benefit: INR 120,000 vs INR -50,000") for r in pair["reasons"])


def test_what_would_reverse_points_at_real_flips():
    p = prop("Reneg", 100000, 90000, 100000)
    j = decide([p, prop("Other", 200000, 180000, 60000)])
    rev = j["decisions"]["Reneg"]["what_would_reverse"]
    run = next(r for r in rev if r["field"] == "annual_running_cost_inr" and r["to_value"] < 100000)
    assert run["new_outcome"] != "renegotiate" and run["to_value"] <= 90000      # net must turn positive
    # re-running at the reported value really flips the outcome
    j2 = decide([{**p, "ai_running_cost_inr": run["to_value"]}, prop("Other", 200000, 180000, 60000)])
    assert outcome(j2, "Reneg") == run["new_outcome"]
    assert rev == sorted(rev, key=lambda r: (abs(r["change_pct"]), r["field"]))


def test_shipped_fixture_needs_evidence_and_keeps_legacy_advice():
    j = c.post("/decision", json={}).json()
    assert j["overall"]["outcome"] == "gather_evidence" and j["overall"]["next_actions"]
    pilots = c.post("/pilots", json={}).json()["pilots"]
    assert pilots[0]["advice"]["outcome"] == "insufficient_information"          # legacy field untouched
    assert pilots[0]["decision"]["outcome"] == "gather_evidence"
    assert "economics" not in pilots[0]["decision"]                              # economics lives beside it


def test_user_proposal_money_is_assumed_not_fixture():
    j = decide([prop("A", 200000, 180000, 60000, **OBS), prop("B", 1, 2, 0)])
    assert j["decisions"]["A"]["evidence"]["band_capped_by_fixture"] is False
    assert c.post("/decision", json={"proposals": [prop("A", 1, 2, 0, money_source="guess"), prop("B", 1, 2, 0)]}).status_code == 422
