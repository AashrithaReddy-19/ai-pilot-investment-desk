"""Phase 3: named scenarios, what-if overrides, sensitivity sweep, verdict flips."""
import copy, pytest
from fastapi.testclient import TestClient
from app.main import app
from app import core, whatif as W

D = __import__("pathlib").Path(__file__).resolve().parent.parent / "data"
import json
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())
c = TestClient(app)


def ext_row(**over):
    """One extension override row for AI1, other rows untouched."""
    rows = copy.deepcopy(EXT["pilots"])
    rows[0] = {**rows[0], **over}
    return rows


# ---- named, editable scenarios ----

def test_scenarios_carry_explicit_assumptions():
    p = core.load_pilots(FIX, EXT["pilots"], core.DEF)["pilots"][0]
    out = core.scenarios(p, core.DEF)
    assert [s["scenario"] for s in out] == ["bad", "base", "good"]
    assert out[0]["assumptions"] == {"adoption_multiplier": 0.5, "multiplies": "adoption_pct",
                                     "held_at_base": ["annual_benefit_inr", "annual_running_cost_inr", "readiness", "setup"]}
    assert out[0]["adoption_pct"] == out[1]["adoption_pct"] * 0.5     # bad = half of base

def test_scenario_multipliers_are_editable():
    p = core.load_pilots(FIX, EXT["pilots"], core.DEF)["pilots"][0]
    d = {**core.DEF, "scenario_multipliers": {"bad": 0.7, "good": 2.0}}
    out = {s["scenario"]: s for s in core.scenarios(p, d)}
    assert out["bad"]["adoption_pct"] == pytest.approx(p.adoption_pct * 0.7)
    assert out["good"]["adoption_pct"] == 100.0                       # capped, never invented past 100
    assert out["base"]["adoption_pct"] == p.adoption_pct              # unlisted names keep the shipped value

def test_defaults_expose_scenario_multipliers():
    j = c.get("/defaults").json()
    assert j["pipeline_rules"]["scenario_multipliers"] == {"bad": 0.5, "base": 1.0, "good": 1.25}


# ---- what-if ----

def test_pass_through_pilots_still_has_multi_period_and_forecast():
    j = c.post("/pilots", json={}).json()
    assert j["pilots"][0]["independent"].get("multi_period") and j.get("forecast")


def test_whatif_base_vs_changed_with_deltas():
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"setup_cost_inr": 100000}}).json()
    assert r["base"]["net"] == 120000 and r["changed"]["net"] == 120000       # net does not depend on setup
    assert r["base"]["payback_months"] == 20.0 and r["changed"]["payback_months"] == 10.0
    assert r["deltas"]["payback_months"] == pytest.approx(-10.0)
    assert r["deltas"]["profit_inr"] == 100000                               # 260000 − 160000
    assert r["changed"]["stop_rule"]["decision"] == "REVISE"
    assert r["overrides_applied"] == {"setup_cost_inr": 100000}


def test_whatif_mutates_no_stored_data():
    fixture, ext = copy.deepcopy(FIX), copy.deepcopy(EXT)
    r = c.post("/whatif", json={"pilot_id": "AI1",
                                "overrides": {"adoption_pct": 95, "annual_benefit_inr": 999999,
                                              "horizon_months": 12, "discount_rate": 0.05}}).json()
    assert r["changed"]["benefit_inr"] == 999999
    assert fixture == FIX and ext == EXT                                     # byte-identical after the call
    # the next call at the shipped default is unaffected
    assert c.post("/pilots", json={}).json()["pilots"][0]["adoption_pct"] == 70


def test_whatif_benefit_override_beats_the_driver():
    fixture = copy.deepcopy(FIX)
    fixture["records"]["pilots"][0]["benefit_driver"] = {"volume_per_month": 400, "minutes_saved_per_unit": 30, "loaded_cost_per_hour": 200}
    r = c.post("/whatif", json={"fixture": fixture, "pilot_id": "AI1", "overrides": {"annual_benefit_inr": 300000}}).json()
    assert r["changed"]["benefit_inr"] == 300000        # the driver is stripped with a clear precedence rule
    assert r["base"]["benefit_inr"] == 480000           # driver base = 400 × 30 ÷ 60 × 200 × 12


def test_whatif_run_cost_null_reverts_to_the_assumption():
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"annual_running_cost_inr": None}}).json()
    assert r["base"]["net"] == 120000
    assert r["changed"]["net"] == 140000         # recorded run withdrawn → assumed 20% of setup = 40000


def test_whatif_budget_and_mode_change_the_portfolio():
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"budget": 100000, "mode": "conservative"}}).json()
    assert r["base"]["portfolio"]["selected"]                      # independent at default budget funds AI1
    assert r["changed"]["portfolio"]["selected"] == []             # conservative + tiny budget funds nothing
    assert r["mode"] == "conservative" and r["deltas"]["net"] < 0  # negative conservative net 180000×0.7×0.3 − 60000


def test_whatif_verdict_flip_and_statement():
    e = ext_row(observed_quality_pct=90)                                     # quality already recorded
    r = c.post("/whatif", json={"extension": {**EXT, "pilots": e}, "pilot_id": "AI1",
                                "overrides": {"observed_adoption_pct": 90}}).json()
    assert r["base"]["verdict"] == "REVISE" and r["changed"]["verdict"] == "GO"
    assert r["verdict_flip"] == {"from": "REVISE", "to": "GO"}
    # GO needs observed adoption ≥ max(breakeven 33.3%, target 60%); the first GO step is 60
    assert r["flip_statement"] == "REVISE becomes GO when observed adoption reaches 60%."


def test_whatif_flip_without_statement_when_multi_override():
    r = c.post("/whatif", json={"pilot_id": "AI1",
                                "overrides": {"observed_adoption_pct": 90, "observed_quality_pct": 90}}).json()
    assert r["verdict_flip"] == {"from": "REVISE", "to": "GO"}
    assert r["flip_statement"] is None and "one pilot-level override" in r["flip_note"]


def test_whatif_cost_benefit_flip_statement():
    """A fully endorsed pilot (obs 95/90) flips GO→STOP as the rising run cost pushes the
    required breakeven adoption past the observed 95% (at 174,000 = 290% of base)."""
    e = ext_row(observed_adoption_pct=95, observed_quality_pct=90)
    r = c.post("/whatif", json={"extension": {**EXT, "pilots": e}, "pilot_id": "AI1",
                                "overrides": {"annual_running_cost_inr": 300000}}).json()
    assert r["base"]["verdict"] == "GO" and r["changed"]["verdict"] == "STOP"
    assert "GO becomes STOP when annual_running_cost_inr reaches INR 174,000 (290% of base)." == r["flip_statement"]


def test_whatif_unknown_override_and_bad_values_are_422():
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"draft_cost": 1}})
    assert r.status_code == 422 and "Unknown override key" in r.json()["detail"]
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"readiness": "turbo"}})
    assert r.status_code == 422
    r = c.post("/whatif", json={"pilot_id": "AI1", "overrides": {"adoption_pct": 120}})
    assert r.status_code == 422 and "0..100" in r.json()["detail"]
    r = c.post("/whatif", json={"pilot_id": "NOPE", "overrides": {}})
    assert r.status_code == 422 and "valid pilot" in r.json()["detail"]


# ---- sensitivity sweep ----

def test_sweep_adoption_monotonic_up():
    # adoption only reshapes the conservative (realized) benefit; sweep in that mode
    r = c.post("/sweep", json={"pilot_id": "AI1", "parameter": "adoption_pct", "mode": "conservative"}).json()
    assert [s["value"] for s in r["steps"]] == list(range(0, 101, 10))
    assert r["direction"] == "up_improves" and r["monotonic"] is True
    assert r["steps"][0]["net"] < r["steps"][-1]["net"]


def test_sweep_benefit_and_run_cost_ladders():
    b = c.post("/sweep", json={"pilot_id": "AI1", "parameter": "annual_benefit_inr"}).json()
    assert [s["value"] for s in b["steps"]] == [90000, 135000, 180000, 225000, 270000]
    assert b["monotonic"] is True and b["direction"] == "up_improves"
    r = c.post("/sweep", json={"pilot_id": "AI1", "parameter": "annual_running_cost_inr"}).json()
    assert [s["value"] for s in r["steps"]] == [30000, 45000, 60000, 75000, 90000]
    assert r["direction"] == "down_improves" and r["monotonic"] is True


def test_sweep_token_price_missing_flags_no_numbers():
    e = {**EXT, "defaults": {**EXT["defaults"], "price_per_1m_tokens": None, "tokens_per_month": None}}
    r = c.post("/sweep", json={"extension": e, "pilot_id": "AI1", "parameter": "price_per_1m_tokens"}).json()
    assert r["steps"] == [] and [f["code"] for f in r["flags"]] == ["TOKENS_MISSING"]
    assert r["monotonic"] is None                                          # unknown, not invented


def test_sweep_growth_steps_reproduce_forecast():
    r = c.post("/sweep", json={"parameter": "annual_usage_growth_pct"}).json()
    assert [s["value"] for s in r["steps"]] == W.SWEEP_GROWTH_PCT
    flat = next(s for s in r["steps"] if s["value"] == -10)
    assert flat["next_fy_cost_inr"] == 141500     # 600M × 0.9 tokens × ₹225 + ₹20,000 fixed
    assert flat["inflation_flag"] is False        # falling usage carries no AI-spend inflation


def test_sweep_bad_parameter_is_422():
    assert c.post("/sweep", json={"parameter": "vibes"}).status_code == 422
    assert c.post("/sweep", json={"parameter": "adoption_pct"}).status_code == 422  # needs pilot_id
