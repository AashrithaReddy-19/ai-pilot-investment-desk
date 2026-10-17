"""A3 economics and budgets: hand-calculated split, competitiveness and budget status."""
import pytest
from fastapi.testclient import TestClient

from app import core, economics as E
from app.main import app

c = TestClient(app)


def P(setup=200000, run=60000, benefit=180000, bt="cashable"):
    return core.Pilot("X", "t", "", setup, run, benefit, bt, "g", 70, "high")


def test_split_without_baseline_uses_benefit_type():
    s = E.split(P(bt="time freed"))
    # gross 180,000; time freed cash factor 0.3 → cashable 54,000, capacity 126,000
    assert (s["gross_cost_avoidance_inr"], s["cashable_savings_inr"], s["capacity_value_inr"]) == (180000, 54000, 126000)
    assert s["retained_human_cost_inr"] is None and s["baseline_cost_inr"] is None
    assert s["net_cashable_inr"] == 54000 - 60000
    assert [f["code"] for f in s["flags"]] == ["BASELINE_MISSING"]


def test_split_with_baseline_and_automation_share():
    # baseline 1,000 units × 30 min × INR 600/h × 12 = INR 3,600,000/yr; automation 25% → gross 900,000
    b = {"volume_per_month": 1000, "minutes_per_unit": 30, "loaded_cost_per_hour": 600, "automation_share_pct": 25}
    s = E.split(P(), b, cashable_share_pct=40)
    assert s["baseline_cost_inr"] == 3_600_000 and s["gross_cost_avoidance_inr"] == 900_000
    assert s["cashable_savings_inr"] == 360_000 and s["capacity_value_inr"] == 540_000
    assert s["retained_human_cost_inr"] == 2_700_000
    assert s["cashable_share_source"] == "stated cashable share"


def test_benefit_above_baseline_is_flagged():
    s = E.split(P(benefit=500000), {"annual_cost_inr": 300000})
    assert "BENEFIT_EXCEEDS_BASELINE" in [f["code"] for f in s["flags"]]


def test_competitiveness_ratio_and_per_unit():
    # annualised cost = 200,000 × 12/36 + 60,000 = 126,667; ÷ 180,000 = 0.704 → marginal
    cm = E.competitiveness(P(), 180000, 36)
    assert cm["class"] == "marginal" and cm["ratio"] == pytest.approx(0.704, abs=1e-3)
    # per unit: AI 126,667 / 24,000 units = 5.28; human 6 min × 1,000/h = 100 → 0.053 → highly competitive
    cm2 = E.competitiveness(P(), 180000, 36, {"volume_per_month": 2000, "minutes_per_unit": 6, "loaded_cost_per_hour": 1000})
    assert cm2["class"] == "highly_competitive" and cm2["ratio"] == pytest.approx(0.053, abs=1e-3)
    assert E.competitiveness(P(benefit=0), 0, 36)["class"] is None


def test_budget_status_lines():
    bs = {"implementation_inr": 250000, "first_year_inr": 255000, "recurring_inr_per_year": 50000,
          "total_inr": 400000, "contingency_pct": 10}
    st = E.budget_status(200000, 60000, 36, bs)
    lines = {ln["line"]: ln for ln in st["lines"]}
    assert lines["implementation_inr"]["status"] == "within"            # 200k ≤ 250k (80%)
    assert lines["first_year_inr"]["status"] == "over"                  # 260k > 255k
    assert lines["recurring_inr_per_year"]["status"] == "over"          # 60k > 50k
    # total: (200k + 180k) × 1.1 = 418k > 400k but 380k ≤ 400k → at risk (fits only without contingency)
    assert lines["total_inr"]["status"] == "at_risk" and lines["total_inr"]["need_inr"] == 418000
    assert st["status"] == "over"


def test_budget_status_unknown_without_budget_and_at_risk_above_90pct():
    assert E.budget_status(1, 1, 36, None)["status"] == "unknown"
    assert E.budget_status(95, 0, 36, {"implementation_inr": 100})["status"] == "at_risk"


def test_economics_endpoint_and_validation():
    props = [{"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000,
              "baseline": {"annual_cost_inr": 400000}},
             {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000}]
    j = c.post("/economics", json={"proposals": props, "budget_set": {"total_inr": 2_000_000}}).json()
    a = j["proposals"][0]
    assert a["retained_human_cost_inr"] == 220000 and a["budget"]["status"] == "within"
    assert j["portfolio_budget"]["ids"] == ["A", "B"]
    bad = c.post("/economics", json={"proposals": props, "budget_set": {"total": 1}})
    assert bad.status_code == 422 and "unknown field" in bad.json()["detail"]
    props[0]["baseline"] = {"annual_cost_inr": -1}
    assert c.post("/economics", json={"proposals": props}).status_code == 422


def test_pilots_carry_economics_and_budget_set():
    j = c.post("/pilots", json={"budget_set": {"implementation_inr": 150000}}).json()["pilots"][0]
    assert j["economics"]["budget"]["status"] == "over" and j["economics"]["cashable_savings_inr"] == 180000
    assert c.post("/pilots", json={"budget_set": {"implementation_inr": "x"}}).status_code == 422
