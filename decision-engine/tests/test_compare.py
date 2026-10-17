"""Phase 4 compare: section-2 rule through the engine, ranking, elimination, validation."""
import pytest
from fastapi.testclient import TestClient
from app import compare, main
from app.main import app

c = TestClient(app)
D = main._defaults()["defaults"]

# Worked example from the plan (Note B / section 2): A 2.0L / 1.8 / 0.6, B 5.0 / 4.0 / 1.5, C 1.0 / 0.9 / 0.7.
WORKED = [
    {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000},
    {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000},
    {"name": "C", "setup_cost_inr": 100000, "annual_benefit_inr": 90000, "ai_running_cost_inr": 70000},
]


def post(proposals, budget=None, mode="independent", extension=None):
    body = {"proposals": proposals, "mode": mode}
    if budget is not None:
        body["budget"] = budget
    if extension is not None:
        body["extension"] = extension
    return c.post("/compare", json=body)


def test_worked_example_budget_picks_and_drop_text():
    j = post(WORKED, budget=600000).json()
    assert j["selection"]["selected"] == ["B"] and j["selection"]["budget_left"] == 100000
    b = next(r for r in j["proposals"] if r["name"] == "B")
    assert b["net_annual_inr"] == 250000 and b["payback_months"] == 24.0
    assert b["profit_inr"] == 250000 and b["roi_pct"] == 50.0 and b["breakeven_month"] == 24
    assert b["rank"] == 1 and b["status"] == "recommended" and b["npv_inr"] is not None
    a = next(r for r in j["proposals"] if r["name"] == "A")
    assert a["status"] == "not funded" and a["breakeven_month"] == 20 and a["rank"] is None
    cch = next(r for r in j["proposals"] if r["name"] == "C")
    assert cch["status"] == "dropped" and cch["reason"] == "3-year loss (break-even beyond the horizon)"
    assert "B recommended: highest net benefit within budget (2.5 L/yr net annual savings, break-even 24.0 months)." in j["recommendation"]["text"]
    assert "C dropped: 3-year loss (break-even beyond the horizon)." in j["recommendation"]["text"]
    assert j["token_value_vs_price"]["value_per_1m_tokens"] == 416.67 and j["token_value_vs_price"]["price_per_1m_tokens"] == 250


def test_budget_7_lakh_funds_a_and_b_with_joint_text():
    j = post(WORKED, budget=700000).json()
    assert j["selection"]["selected"] == ["B", "A"] and j["selection"]["total_net_inr"] == 370000
    txt = j["recommendation"]["text"]
    assert txt.startswith("B recommended:") and "Funded together with A: total INR 370,000/yr net savings." in txt


def test_no_budget_funds_every_survivor():
    j = post(WORKED).json()
    assert j["selection"]["budget"] is None and j["selection"]["selected"] == ["B", "A"]
    assert "No budget given" in j["selection"]["message"]


def test_ranks_are_dense_and_ties_break_on_id():
    twins = [{"name": t, "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000}
             for t in ("T2", "T1")]
    j = post(twins).json()
    ranks = {r["name"]: r["rank"] for r in j["proposals"] if r["rank"]}
    assert ranks == {"T1": 1, "T2": 2}      # identical numbers → lower id outranks (6-tier order)
    assert j["proposals"][0]["status"] == "recommended"


def test_non_positive_and_overlap_elimination():
    j = post([
        {"name": "Loss", "setup_cost_inr": 100000, "annual_benefit_inr": 50000, "ai_running_cost_inr": 80000},
        {"name": "Big", "setup_cost_inr": 1000000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 50000},
        {"name": "Small", "setup_cost_inr": 50000, "annual_benefit_inr": 120000, "ai_running_cost_inr": 20000},
    ], budget=1000000).json()
    names = {r["name"]: r for r in j["proposals"]}
    assert names["Loss"]["status"] == "dropped" and "no positive net annual savings" in names["Loss"]["reason"]
    assert names["Big"]["status"] == "recommended"       # the only funded survivor
    assert names["Small"]["status"] == "not funded"       # budget fully used by the higher-net pilot
    assert j["recommendation"]["text"].count("dropped") == 1
    assert "Small not funded: budget fully used by higher-ranked pilots." in j["recommendation"]["text"]
    # overlap: equal-priority pair sharing a group keeps only the better one
    j2 = post([
        {"name": "X1", "setup_cost_inr": 100000, "annual_benefit_inr": 200000, "ai_running_cost_inr": 40000, "overlap_group": "chat"},
        {"name": "X2", "setup_cost_inr": 100000, "annual_benefit_inr": 190000, "ai_running_cost_inr": 40000, "overlap_group": "chat"},
    ]).json()
    sel = {r["name"] for r in j2["proposals"] if r["status"] in ("kept", "recommended")}
    assert sel == {"X1"} and any("overlap" in r["reason"] for r in j2["proposals"] if r["name"] == "X2")


def test_conservative_mode_and_null_run_cost():
    j = post([
        {"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": None},
        {"name": "B", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000},
    ], mode="conservative").json()
    rows = {r["name"]: r for r in j["proposals"]}
    assert rows["A"]["flags"] and any(f["code"] == "RUN_COST_ASSUMED" for f in rows["A"]["flags"])
    assert rows["A"]["net_annual_inr"] > rows["B"]["net_annual_inr"] > 0   # assumption beats the priced pilot? A: 180k-36k=144k; B: 180k-60k=120k


def test_missing_tokens_and_price_never_zero():
    ext = {"defaults": {"tokens_per_month": None, "price_per_1m_tokens": None}}
    j = post(WORKED, extension=ext).json()
    tvp = j["token_value_vs_price"]
    assert tvp["value_per_1m_tokens"] is None and tvp["value_vs_price_ratio"] is None
    assert {f["code"] for f in tvp["flags"]} == {"TOKENS_MISSING", "TOKEN_PRICE_MISSING"}


def test_cash_vs_capacity_split():
    j = post([
        {"name": "Cash", "setup_cost_inr": 100000, "annual_benefit_inr": 200000, "ai_running_cost_inr": 50000, "benefit_type": "cashable"},
        {"name": "Time", "setup_cost_inr": 100000, "annual_benefit_inr": 200000, "ai_running_cost_inr": 50000, "benefit_type": "time freed"},
    ]).json()
    rows = {r["name"]: r for r in j["proposals"]}
    assert rows["Cash"]["cash_vs_capacity"] == {"cash_benefit_inr": 200000, "capacity_benefit_inr": 0, "cash_share_pct": 100.0}
    assert rows["Time"]["cash_vs_capacity"]["cash_benefit_inr"] == 60000 and rows["Time"]["cash_vs_capacity"]["capacity_benefit_inr"] == 140000
    assert rows["Time"]["cash_vs_capacity"]["cash_share_pct"] == 30.0


def test_compare_validation_422s():
    assert post([WORKED[0]]).status_code == 422 and "2 to 8" in post([WORKED[0]]).text
    nine = [{"name": f"P{i}", "setup_cost_inr": 1, "annual_benefit_inr": i} for i in range(9)]
    r = post(nine)
    assert r.status_code == 422 and "2 to 8" in r.text
    dup = [WORKED[0], dict(WORKED[0])]
    assert post(dup).status_code == 422 and "duplicate" in post(dup).text
    no_money = [WORKED[0], {"name": "X", "setup_cost_inr": 100000}]
    r = post(no_money)
    assert r.status_code == 422 and "annual_benefit_inr" in r.text
    assert post(WORKED, budget=-5).status_code == 422
    assert post(WORKED, mode="both").status_code == 422
    bad_type = [dict(WORKED[0]), {"name": "Y", "setup_cost_inr": 1, "annual_benefit_inr": 2, "benefit_type": "gold"}]
    assert post(bad_type).status_code == 422 and "benefit_type" in post(bad_type).text
    r = post("nope")
    assert r.status_code == 422


def test_compare_horizon_and_discount_via_extension():
    ext = {"defaults": {"horizon_months": 12, "discount_rate": 0.05}}
    j = post(WORKED, budget=600000, extension=ext).json()
    assert j["recommendation"]["assumptions"]["horizon_months"] == 12
    # B's break-even is month 24 > 12 → no survivor breaks even within the horizon
    days = {r["name"]: r for r in j["proposals"]}
    assert days["B"]["breakeven_beyond_horizon"] is True and days["B"]["status"] == "dropped"


def test_delay_past_horizon_is_a_three_year_loss():
    j = post([
        {"name": "Late", "setup_cost_inr": 100000, "annual_benefit_inr": 240000, "ai_running_cost_inr": 60000,
         "ramp_months": 6, "delay_months": 30},
        {"name": "Now", "setup_cost_inr": 100000, "annual_benefit_inr": 200000, "ai_running_cost_inr": 80000},
    ], budget=200000).json()
    rows = {r["name"]: r for r in j["proposals"]}
    assert rows["Late"]["net_annual_inr"] > 0                                    # steady state is fine...
    assert rows["Late"]["breakeven_month"] is None                               # ...but the ramp+delay never break even in 36 months
    assert rows["Late"]["status"] == "dropped" and "3-year loss" in rows["Late"]["reason"]
    assert rows["Now"]["status"] == "recommended"
    assert "Late dropped: 3-year loss (break-even beyond the horizon)." in j["recommendation"]["text"]
