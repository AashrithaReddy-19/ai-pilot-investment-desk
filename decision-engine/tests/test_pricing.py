"""A4 pricing rules, backtested usage forecast and the package optimiser (hand-calculated)."""
import pytest
from fastapi.testclient import TestClient

from app import pricing as PR
from app.main import app

c = TestClient(app)


def bill(rule, units):
    return PR.monthly_bill(PR.validate_rule(rule), units)["total_inr"]


def test_per_unit_with_fixed_fee_and_minimum():
    assert bill({"type": "per_unit", "unit_price": 2, "fixed_monthly_inr": 100}, 50) == 200
    assert bill({"type": "per_unit", "unit_price": 2, "minimum_monthly_inr": 500}, 50) == 500   # topped up
    assert bill({"type": "per_unit", "unit_price": 2, "minimum_monthly_inr": 500}, 300) == 600


def test_tiered_graduated_vs_volume():
    tiers = [{"up_to": 100, "unit_price": 2}, {"up_to": 1000, "unit_price": 1}, {"up_to": None, "unit_price": 0.5}]
    # graduated: 100 × 2 + 900 × 1 + 500 × 0.5 = 1,350; volume: all 1,500 at 0.5 = 750
    assert bill({"type": "tiered", "tiers": tiers}, 1500) == 1350
    assert bill({"type": "tiered", "tiers": tiers, "tier_mode": "volume"}, 1500) == 750
    assert bill({"type": "tiered", "tiers": tiers, "tier_mode": "volume"}, 100) == 200      # boundary inclusive


def test_credit_and_commitment_overage():
    # 1,000 units × 3 credits = 3,000 − 500 included = 2,500 × 0.4 = 1,000
    assert bill({"type": "credit", "credits_per_unit": 3, "price_per_credit": 0.4, "included_credits": 500}, 1000) == 1000
    rule = {"type": "commitment", "committed_units": 1000, "commit_price": 800, "overage_unit_price": 1.2}
    assert bill(rule, 900) == 800 and bill(rule, 1100) == 800 + 120


def test_package_cheapest_mix():
    rule = {"type": "package", "overage_unit_price": 12,
            "packages": [{"name": "S", "units": 10, "price": 90}, {"name": "L", "units": 100, "price": 700}]}
    assert bill(rule, 0) == 0
    assert bill(rule, 5) == 60          # 5 overage × 12 beats a 90 package
    assert bill(rule, 95) == 700        # one L beats 9 S + 5 overage (870)
    assert bill(rule, 130) == 970       # L + 3 S
    no_overage = {k: v for k, v in rule.items() if k != "overage_unit_price"}
    assert bill(no_overage, 5) == 90


@pytest.mark.parametrize("rule,msg", [
    ({"type": "nope"}, "type must be"),
    ({"type": "tiered", "tiers": [{"up_to": 10, "unit_price": 1}]}, "open-ended"),
    ({"type": "tiered", "tiers": [{"up_to": 10, "unit_price": 1}, {"up_to": 5, "unit_price": 1}, {"up_to": None, "unit_price": 1}]}, "increasing"),
    ({"type": "per_unit", "unit_price": -1}, "non-negative"),
    ({"type": "package", "packages": [{"name": f"p{i}", "units": 1, "price": 1} for i in range(5)]}, "1 to 4"),
])
def test_rule_validation(rule, msg):
    with pytest.raises(PR.PricingError, match=msg):
        PR.validate_rule(rule)


def test_backtest_picks_exact_linear_and_compound_series():
    lin = PR.forecast_usage([100, 110, 120, 130, 140, 150], 3)
    assert lin["method"] == "linear_trend" and lin["base"] == [160, 170, 180]
    assert lin["band_pct"] == 10.0 and lin["low"][0] == 144 and lin["high"][0] == 176   # floor band 10%
    geo = PR.forecast_usage([100, 200, 400, 800, 1600, 3200], 2)
    assert geo["method"] == "compound_growth" and geo["base"] == [6400, 12800]
    flat = PR.forecast_usage([50, 50, 50, 50], 1)
    assert flat["method"] == "naive"                                                    # tie → method order


def test_short_history_falls_back_and_says_so():
    fc = PR.forecast_usage([1000], 12, annual_growth_pct=20)
    assert fc["method"] == PR.FALLBACK_METHOD and fc["base"][-1] == pytest.approx(1200)
    assert "FORECAST_FALLBACK" in [f["code"] for f in fc["flags"]]
    none = PR.forecast_usage(None, 12)
    assert none["base"] is None and none["flags"][-1]["code"] == "USAGE_MISSING"


def test_optimiser_recommended_vs_robust_choice():
    fc = {"low": [80], "base": [100], "high": [200], "flags": []}
    plans = [{"name": "payg", "rule": {"type": "per_unit", "unit_price": 1}},
             {"name": "commit", "rule": {"type": "commitment", "committed_units": 200, "commit_price": 120,
                                         "overage_unit_price": 2}}]
    o = PR.optimise(plans, fc)
    # payg: 80/100/200; commit: 120/120/120 → cheapest base payg, cheapest high commit
    assert o["recommended"] == "payg" and o["robust_choice"] == "commit"
    rows = {r["name"]: r for r in o["plans"]}
    assert rows["commit"]["regret_base_inr"] == 20 and rows["payg"]["regret_high_inr"] == 80
    with pytest.raises(PR.PricingError, match="duplicate"):
        PR.optimise(plans + plans[:1], fc)


def test_endpoints():
    q = c.post("/pricing/quote", json={"rule": {"type": "per_unit", "unit_price": 0.5}, "units": 10}).json()
    assert q["total_inr"] == 5 and q["effective_unit_price"] == 0.5
    assert c.post("/pricing/quote", json={"rule": {"type": "x"}, "units": 1}).status_code == 422
    fc = c.post("/forecast/usage", json={"history": [100, 110, 120, 130, 140, 150], "months": 2,
                                          "rule": {"type": "per_unit", "unit_price": 2}}).json()
    assert fc["cost"]["base"]["annual_inr"] == (160 + 170) * 2
    o = c.post("/pricing/optimise", json={"history": [100, 110, 120, 130], "months": 1,
                                          "plans": [{"name": "a", "rule": {"type": "per_unit", "unit_price": 1}}]}).json()
    assert o["recommended"] == "a" and o["forecast"]["method"] in PR.METHOD_ORDER
    assert c.post("/forecast/usage", json={"months": 0}).status_code == 422
