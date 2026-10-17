"""Hand-calculated cases for the multi-period cash-flow engine (plan section 2 formulas)."""
import pytest
from app import cashflow as cf


def test_ramp_and_delay():
    """Ramp/delay are additive extensions of the same pilots; None must never be invented as 0."""
    # AI1 steady state: setup 200000, net/yr 120000 -> 10000/mo net, breakeven = month 20 (worked example A)
    flows = cf.monthly_cash_flows(200000, 180000 / 12, 60000 / 12, 36)
    assert len(flows) == 36 and flows[0] == pytest.approx(-190000)
    cum = cf.cumulative(flows)
    assert cf.breakeven_month_from_cumulative(cum) == 20
    assert cum[-1] == pytest.approx(160000)  # 36 × 10000 − 200000


def test_ramp_cases():
    """ramp = 6: month 1 earns 1/6 of benefit, month 6 reaches full benefit."""
    assert cf.ramp_factor(1, 6, 0) == pytest.approx(1 / 6)
    assert cf.ramp_factor(3, 6, 0) == pytest.approx(3 / 6)
    assert cf.ramp_factor(6, 6, 0) == 1.0
    assert cf.ramp_factor(7, 6, 0) == 1.0
    # ramp 0/None means full benefit from day one (default)
    assert cf.ramp_factor(1, 0, 0) == 1.0
    # delay: nothing before month delay+1, full after ramp completes
    assert cf.ramp_factor(1, 6, 2) == 0.0
    assert cf.ramp_factor(2, 6, 2) == 0.0
    assert cf.ramp_factor(3, 6, 2) == pytest.approx(1 / 6)
    assert cf.ramp_factor(8, 6, 2) == 1.0


def test_delay_worsens_breakeven():
    """With the run cost charged from month 1 and a 6-month ramping delay, break-even lands on month 33."""
    flows = cf.monthly_cash_flows(200000, 180000 / 12, 60000 / 12, 36, ramp_months=6, delay_months=6)
    cum = cf.cumulative(flows)
    assert cf.breakeven_month_from_cumulative(cum) == 33
    # Hand check: months 1–6 = −200000 one-time − 6×5000 run. Months 7–12 ramp (21/6 of full benefit);
    # then 24 full months of net 10000. Run cost is charged in all 36 months.
    expected = -200000 - 5000 * 36 + ((1 + 2 + 3 + 4 + 5 + 6) / 6) * 15000 + 24 * 15000
    assert cum[-1] == pytest.approx(expected)  # 32500
    assert cf.profit_loss(flows) == pytest.approx(expected)


def test_profit_roi():
    """Profit over horizon = Σ flows; ROI = profit ÷ one-time cost. C loses 40%."""
    flows_a = cf.monthly_cash_flows(200000, 180000 / 12, 60000 / 12, 36)
    pa = cf.profit_loss(flows_a)
    assert pa == pytest.approx(160000)                       # worked example A: 3-yr profit 1.6 L
    assert cf.roi_pct(pa, 200000) == pytest.approx(80)       # 80%
    flows_c = cf.monthly_cash_flows(100000, 90000 / 12, 70000 / 12, 36)
    pc = cf.profit_loss(flows_c)
    assert pc == pytest.approx(-40000)                       # worked example C: −0.4 L
    assert cf.roi_pct(pc, 100000) == pytest.approx(-40)      # −40%


def test_roi_undefined_for_free_pilot():
    """A pilot with no one-time cost has no ROI denominator — None, never invented."""
    assert cf.roi_pct(100000, 0) is None
    assert cf.roi_pct(-1000, 0) is None


def test_npv_zero_rate_equals_profit():
    """Discount rate 0 ⇒ NPV == profit/loss (formula table)."""
    flows = [-100000, 12000, 15000, -3000, 9000]
    assert cf.npv(flows, 0.0) == pytest.approx(cf.profit_loss(flows))
    flows = cf.monthly_cash_flows(200000, 15000, 5000, 36)
    assert cf.npv(flows, 0.0) == pytest.approx(cf.profit_loss(flows))


def test_npv_positive_rate_is_less():
    flows = cf.monthly_cash_flows(200000, 180000 / 12, 60000 / 12, 36)
    profit = cf.profit_loss(flows)
    npv10 = cf.npv(flows, 0.10)
    assert npv10 < profit
    # the value must equal Σ discounted flows exactly as the formula defines
    assert npv10 == pytest.approx(sum(f / 1.1 ** ((t + 1) / 12) for t, f in enumerate(flows)))


def test_npv_hand_case_monthly():
    """One hand-checked row: single +12000 flow at month 1, r=10% ⇒ 12000 ÷ 1.1^(1/12)."""
    assert cf.npv([12000], 0.10) == pytest.approx(12000 / 1.1 ** (1 / 12))


def test_breakeven_beyond_horizon_is_none():
    """C never reaches positive cumulative inside a 36-month horizon -> None, not invented."""
    flows = cf.monthly_cash_flows(100000, 90000 / 12, 70000 / 12, 36)
    assert cf.breakeven_month_from_cumulative(cf.cumulative(flows)) is None
