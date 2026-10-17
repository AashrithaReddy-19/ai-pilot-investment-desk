"""A1 correctness fixes, A2 formula versioning and the engine version on /health."""
import copy
import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from app import core, lifecycle
from app.main import app
from app.version import RULES_VERSION, CONTRACT_VERSION, FORMULA_VERSION

c = TestClient(app)
D = Path(__file__).resolve().parent.parent / "data"
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())


def test_health_reports_versions():
    j = c.get("/health").json()
    assert j == {"ok": True, "rules_version": RULES_VERSION, "contract_version": CONTRACT_VERSION,
                 "formula_version": FORMULA_VERSION}


def test_no_scientific_notation_in_money_text():
    md = c.post("/export/evidence", json={"budget": 1_500_000}).text
    assert "e+06" not in md and "INR 1,500,000" in md
    r = c.post("/portfolio/check", json={"ids": ["AI1", "AI2", "AI3", "AI4"], "budget": 600000}).json()
    assert "INR 800,000 > INR 600,000" in " ".join(r["problems"])


def test_compare_lakh_text_never_scientific():
    big = [{"name": "Big", "setup_cost_inr": 1e10, "annual_benefit_inr": 2e11, "ai_running_cost_inr": 0},
           {"name": "Small", "setup_cost_inr": 1, "annual_benefit_inr": 2, "ai_running_cost_inr": 0}]
    txt = c.post("/compare", json={"proposals": big}).json()["recommendation"]["text"]
    assert "e+" not in txt and "2,000,000 L/yr" in txt


def test_pilots_honours_request_scenario_multipliers():
    ext = copy.deepcopy(EXT)
    ext["defaults"]["scenario_multipliers"] = {"bad": 0.1}
    sc = c.post("/pilots", json={"extension": ext}).json()["pilots"][0]["scenarios"]
    assert sc[0]["adoption_pct"] == 7.0 and sc[0]["assumptions"]["adoption_multiplier"] == 0.1   # 70 × 0.1
    assert sc[1]["adoption_pct"] == 70.0                                                           # base untouched


def test_observed_zero_adoption_is_a_measurement_not_missing():
    ext = copy.deepcopy(EXT)
    ext["pilots"][0].update(observed_adoption_pct=0, observed_quality_pct=90)
    codes = c.post("/pilots", json={"extension": ext}).json()["pilots"][0]["advice"]["reason_codes"]
    assert "LOW_OBSERVED_ADOPTION" in codes and "NO_OBSERVED_ADOPTION" not in codes


def test_plan_stop_rule_without_breakeven_says_so():
    p = core.Pilot("X", "t", "", 100, 100, 50, "cashable", "g", 50, "high")
    txt = lifecycle.plan_30d(p, EXT["defaults"])["stop_rule"]
    assert "0.0%" not in txt and "never pays back" in txt


def test_more_than_40_candidates_is_a_422_not_a_500():
    fx = copy.deepcopy(FIX)
    fx["records"]["pilots"] = [{"id": f"P{i}", "task": "t", "setup_cost_inr": 1000, "annual_running_cost_inr": 10,
                                "annual_benefit_inr": 1000 + i, "benefit_type": "cashable", "overlap_group": f"g{i}"}
                               for i in range(41)]
    r = c.post("/portfolio", json={"fixture": fx, "extension": {"pilots": []}, "budget": 10_000})
    assert r.status_code == 422 and "max 40" in r.json()["detail"]


def test_defaults_expose_every_rule_block():
    pr = c.get("/defaults").json()["pipeline_rules"]
    for k in ("versions", "whatif_rules", "compare_rules", "economics_rules", "pricing_rules", "decision_rules", "view_rules"):
        assert k in pr, k
    assert pr["solver"]["max_exact_candidates"] == core.MAX_EXACT_CANDIDATES == 16
    assert pr["whatif_rules"]["sweep_fractions"] == [0.5, 0.75, 1.0, 1.25, 1.5]
    assert pr["compare_rules"]["max_proposals"] == 8


def test_multi_period_carries_formula_version_and_breakdown():
    mp = c.post("/pilots", json={}).json()["pilots"][0]["independent"]["multi_period"]
    assert mp["formula_version"] == FORMULA_VERSION
    b = mp["breakdown"]
    assert b["inputs"]["one_time_inr"] == 200000 and b["inputs"]["horizon_months"] == 36
    assert b["formulas"]["net_annual"] == "= INR 180,000 − INR 60,000 = INR 120,000/yr"
    assert b["formulas"]["breakeven_month"] == "first month with cumulative ≥ 0 = month 20"


def test_steady_state_public_checks_unchanged():
    p = c.post("/pilots", json={}).json()["pilots"][0]
    assert p["independent"]["net"] == 120000
    assert p["independent"]["payback_months"] == pytest.approx(20)
    assert p["independent"]["multi_period"]["breakeven_month"] == 20


def test_token_forecast_is_labelled_as_fallback():
    fc = c.post("/pilots", json={}).json()["forecast"]
    assert fc["method"] == "assumed_growth_fallback" and fc["next_fy_cost_inr"] == 182000
