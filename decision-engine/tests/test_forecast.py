"""Hand-calculated token-cost cases (plan section 2 worked example)."""
import pytest
from app import forecast as fc

# Worked example: 50M tokens/month at ₹250 per 1M + ₹20,000 fixed = ₹1,70,000 a year.
# 20% usage growth, 10% price drop ⇒ ₹1,82,000 next FY, inflation flag ON (usage growth ≥ 0).
TOKENS, PRICE, FIXED = 50_000_000, 250, 20_000


def test_run_cost():
    v = fc.ai_run_cost_inr(TOKENS, PRICE, FIXED)
    assert v == pytest.approx(170000)
    # fixed annual defaults to nothing invented
    assert fc.ai_run_cost_inr(TOKENS, PRICE) == pytest.approx(150000)
    # missing tokens or price → None, never zero
    assert fc.ai_run_cost_inr(None, PRICE, FIXED) is None
    assert fc.ai_run_cost_inr(TOKENS, None, FIXED) is None
    assert fc.ai_run_cost_inr("lots", PRICE, FIXED) is None
    # negative inputs are invalid, not numbers
    assert fc.ai_run_cost_inr(-TOKENS, PRICE, FIXED) is None


def test_next_fy():
    """Tokens grow ×1.2, price ×0.9; the ₹20,000 fixed part does not grow. 600M → 720M tokens."""
    nxt = fc.next_fy_cost_inr(TOKENS, PRICE, FIXED, 20, -10)
    assert nxt == pytest.approx(720_000_000 * 225 / 1_000_000 + FIXED)
    assert nxt == pytest.approx(182000)
    # unknown growth/drift leaves them at 1.0 (assumption absent, still a number when tokens/price exist)
    assert fc.next_fy_cost_inr(TOKENS, PRICE, FIXED, None, None) == pytest.approx(150000 + FIXED)  # 170000
    # missing tokens or price → None
    assert fc.next_fy_cost_inr(None, PRICE, FIXED, 20, -10) is None
    assert fc.next_fy_cost_inr(TOKENS, None, FIXED, 20, -10) is None


def test_inflation_flag():
    assert fc.inflation_flag(20) is True
    assert fc.inflation_flag(0) is True     # usage that merely holds flat still carries spend (Note A)
    assert fc.inflation_flag(-10) is False
    assert fc.inflation_flag(None) is None  # unknown → no claim either way


def test_token_value():
    """Agent saving ₹4 L on 600 M tokens/yr ⇒ ₹667 per 1M ≈ 2.7× the ₹250 price."""
    v = fc.value_per_1m_tokens(400000, TOKENS)   # 600M tokens over a year
    assert v == pytest.approx(400000 / 600)
    assert v / PRICE == pytest.approx(2.7, abs=0.05)
    # missing tokens or a zero-token pilot → None
    assert fc.value_per_1m_tokens(400000, None) is None
    assert fc.value_per_1m_tokens(400000, 0) is None
    assert fc.value_per_1m_tokens(None, TOKENS) is None
