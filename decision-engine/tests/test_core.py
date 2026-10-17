import json, copy, pytest
from pathlib import Path
from fastapi.testclient import TestClient
from app.core import *
from app.core import _set_key  # underscore names are skipped by the star import
from app.main import app

D = Path(__file__).resolve().parent.parent / "data"
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())
DEF = EXT["defaults"]

def load(mut=None, ext=None):
    f = copy.deepcopy(FIX)
    if mut: mut(f)
    return load_pilots(f, EXT["pilots"] if ext is None else ext, DEF)
def get(r, i): return next(p for p in r["pilots"] if p.id == i)

# CHECK 1
def test_20_month_payback():
    assert net_annual(180000, 60000) == 120000
    assert payback_months(200000, 180000, 60000) == pytest.approx(20)
    p = get(load(), "AI1")
    assert payback_months(p.setup, p.benefit, p.run) == pytest.approx(FIX["records"]["expected_first_pilot_payback_months"])
def test_never_pays_back():
    assert payback_months(200000, 60000, 60000) is None and payback_months(200000, 50000, 60000) is None

# CHECK 2
def test_budget_and_overlap():
    r = load(); o = build_portfolio(r["pilots"], 600000)
    assert len(o["selected"]) == 3 and o["total_setup"] <= 600000
    assert not ("AI4" in o["selected"] and "AI5" in o["selected"])
    big = build_portfolio(r["pilots"], 5_000_000)
    assert len(big["selected"]) == 4 and big["total_setup"] == 800000
    assert "Overlap" in next(x for x in big["rejected"] if x["id"] == "AI5")["reason"]
def test_manual_selection():
    r = load()["pilots"]
    assert any("Double-count" in p for p in check_manual_selection(r, ["AI4", "AI5"], 600000)["problems"])
    assert any("Over budget" in p for p in check_manual_selection(r, ["AI1", "AI2", "AI3", "AI4"], 600000)["problems"])
    assert check_manual_selection(r, ["AI1", "AI2"], 600000)["ok"]

# CHECK 3
def obs(a, q):
    e = [{**x, "observed_adoption_pct": a, "observed_quality_pct": q} if x["id"] == "AI1" else x for x in EXT["pilots"]]
    return get(load(ext=e), "AI1")
def test_stop_rule():
    assert breakeven_adoption_pct(obs(1, 1)) == pytest.approx(33.333, abs=0.01)
    assert evaluate_stop_rule(obs(20, 90), DEF)["decision"] == "STOP"
    assert evaluate_stop_rule(obs(70, 60), DEF)["decision"] == "REVISE"
    assert evaluate_stop_rule(obs(45, 90), DEF)["decision"] == "REVISE"
    assert evaluate_stop_rule(obs(70, 90), DEF)["decision"] == "GO"
    assert evaluate_stop_rule(obs(None, None), DEF)["decision"] == "REVISE"
    s = evaluate_stop_rule(obs(20, 90), DEF); assert s["threshold"] is not None and s["actual"] == 20

# Tie-break
def test_tie_break_deterministic():
    r = load()["pilots"]
    assert [p.id for p in sorted(r, key=lambda p: pilot_key(p, "independent"))] == ["AI1", "AI4", "AI2", "AI3", "AI5"]
    a = build_portfolio(r, 600000); b = build_portfolio(list(reversed(r)), 600000)
    assert a["selected"] == b["selected"] and set(a["selected"]) == {"AI1", "AI2", "AI4"}
    n = load(ext=[])["pilots"]
    assert build_portfolio(n, 400000)["selected"] == build_portfolio(n[::-1], 400000)["selected"]

# Judge challenges
def test_budget_edges():
    p = load()["pilots"]
    o = build_portfolio(p, 150000); assert o["selected"] == [] and "below the cheapest" in o["message"]
    assert build_portfolio(p, 0)["selected"] == []
    for bad in (-1, None, float("nan"), "6 lakh"): assert "missing or invalid" in build_portfolio(p, bad)["message"]
def test_null_run_cost():
    p = get(load(lambda f: f["records"]["pilots"][0].update(annual_running_cost_inr=None)), "AI1")
    assert p.run == 40000 and "RUN_COST_ASSUMED" in [f["code"] for f in p.flags]
def test_missing_required():
    r = load(lambda f: f["records"]["pilots"][1].update(setup_cost_inr=None))
    assert r["invalid"][0]["id"] == "AI2" and "AI2" not in build_portfolio(r["pilots"], 5_000_000)["selected"]
    def m(f):
        del f["records"]["pilots"][0]["annual_benefit_inr"]; f["records"]["pilots"][1]["setup_cost_inr"] = "2 lakh"; f["records"]["pilots"][2]["setup_cost_inr"] = -5
    assert [i["id"] for i in load(m)["invalid"]] == ["AI1", "AI2", "AI3"]
def test_raised_cost():
    r = load(lambda f: f["records"]["pilots"][0].update(annual_running_cost_inr=200000))
    o = build_portfolio(r["pilots"], 5_000_000)
    assert "AI1" not in o["selected"] and "never pays back" in next(x for x in o["rejected"] if x["id"] == "AI1")["reason"]
    assert evaluate_stop_rule(get(r, "AI1"), DEF)["decision"] == "STOP"
def test_time_freed():
    r = load(lambda f: f["records"]["pilots"][0].update(benefit_type="time freed"))
    assert "TIME_NOT_CASH" in [f["code"] for f in get(r, "AI1").flags]
    assert "AI1" not in build_portfolio(r["pilots"], 5_000_000, "conservative")["selected"]
def test_conservative_never_beats_independent():
    p = load()["pilots"]
    assert build_portfolio(p, 600000, "conservative")["total_net"] <= build_portfolio(p, 600000)["total_net"]
def test_misc():
    assert "OVERLAP_UNKNOWN" in [f["code"] for f in get(load(lambda f: f["records"]["pilots"][4].update(overlap_group=None)), "AI5").flags]
    assert "duplicate" in " ".join(load(lambda f: f["records"]["pilots"][1].update(id="AI1"))["invalid"][0]["errors"])
    assert load_pilots({}, [], DEF)["invalid"][0]["id"] == "(file)"
    e = [{**x, "adoption_pct": 150} if x["id"] == "AI1" else x for x in EXT["pilots"]] + [{"id": "AI99", "adoption_pct": 50, "readiness": "high"}]
    r = load(ext=e); assert get(r, "AI1").adoption_pct == 50 and len(r["warnings"]) == 2
    assert build_portfolio([], 600000)["ok"] is False

# ---- Phase 0 regression tests ----

def test_non_cashable_benefit_type_is_not_selected():
    """Defect 1: 'non-cashable' must NOT match the 'cashable' substring and return cash_factor=1.0."""
    from app.core import cash_factor
    assert cash_factor("non-cashable") == 0.0, "non-cashable must have cash factor 0, not 1.0"
    assert cash_factor("cashable") == 1.0
    assert cash_factor("cashable assumption") == 1.0
    # A pilot with non-cashable benefit type should never be selected (net <= 0 in conservative mode)
    r = load(lambda f: f["records"]["pilots"][0].update(benefit_type="non-cashable"))
    p = get(r, "AI1")
    assert "BENEFIT_TYPE_NON_CASHABLE" in [flag["code"] for flag in p.flags]
    # In conservative mode, realized_benefit = benefit * adoption * readiness * 0.0 = 0
    from app.core import realized_benefit
    assert realized_benefit(p) == 0.0
    port = build_portfolio(r["pilots"], 5_000_000, "conservative")
    assert "AI1" not in port["selected"]

def test_unknown_benefit_type_is_flagged_not_crashed():
    """Defect 1 (related): unrecognised benefit_type must flag and fall back, not match 'cashable'."""
    r = load(lambda f: f["records"]["pilots"][0].update(benefit_type="definitely-cashable"))
    p = get(r, "AI1")
    flags = [f["code"] for f in p.flags]
    assert "BENEFIT_TYPE_UNKNOWN" in flags

def test_portfolio_message_no_scientific_notation():
    """Defect 2: %g formatting prints 1.5e+06 for 1500000. Verify messages use comma-separated integers."""
    pilots = load()["pilots"]
    msg = build_portfolio(pilots, 1_500_000)["message"]
    assert "e+" not in msg and "e-" not in msg, f"scientific notation in message: {msg!r}"
    assert "1,500,000" in msg or "1500000" in msg, f"expected full number in message: {msg!r}"

def test_too_many_candidates_returns_422_not_500():
    """Defect 3: >40 candidates raise ValueError which must become a 422, not a 500.
    Phase 5.5: the solver raised the exact-search limit from 16 to 40, so 17+ must now
    be served by the branch-and-bound path (result identical to the exact search for net)."""
    import copy
    big = copy.deepcopy(FIX)
    for i in range(6, 42):                      # 5 fixture + 36 = 41 candidates
        row = copy.deepcopy(big["records"]["pilots"][0])
        row["id"] = f"AI{i}"
        row["overlap_group"] = f"group{i}"      # each in its own group so none are excluded for overlap
        big["records"]["pilots"].append(row)
    ext_rows = [{"id": f"AI{i}", "adoption_pct": 70, "readiness": "high"} for i in range(1, 42)]
    L = load_pilots(big, ext_rows, DEF)
    assert len(L["pilots"]) == 41
    # Direct call: above the solver cap it raises
    with pytest.raises(ValueError, match="Too many candidates"):
        build_portfolio(L["pilots"], 99_999_999)
    # Through the API it must be 422, not 500
    resp = c.post("/portfolio", json={"fixture": big,
                                      "extension": {**EXT, "pilots": ext_rows, "pilot_budget_inr": 99_999_999}})
    assert resp.status_code == 422, f"expected 422 for too many candidates, got {resp.status_code}"

def _exact_reference(L, budget, mode="independent"):
    """The pre-Phase-5 enumeration (same core helpers), kept as the agreement oracle for the solver."""
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

def test_portfolio_beyond_exact_limit_solved_by_branch_and_bound():
    """Phase 5.5: 21 candidates (above the legacy 16-candidate exact search) solve within
    budget, keep overlap groups pairwise-exclusive, and match the exact oracle."""
    import copy
    big = copy.deepcopy(FIX)
    for i in range(6, 22):
        row = copy.deepcopy(big["records"]["pilots"][0])
        row["id"] = f"AI{i}"
        row["overlap_group"] = f"group{i}"
        row["setup_cost_inr"] = 100000 + i * 1000
        big["records"]["pilots"].append(row)
    ext_rows = [{"id": f"AI{i}", "adoption_pct": 70, "readiness": "high"} for i in range(1, 22)]
    L = load_pilots(big, ext_rows, DEF)
    assert len(L["pilots"]) == 21
    pf = build_portfolio(L["pilots"], 600_000)
    assert pf["ok"] and pf["total_net"] > 0 and pf["total_setup"] <= 600_000
    by = {p.id: p for p in L["pilots"]}
    assert len({by[i].group for i in pf["selected"]}) == len(pf["selected"])
    assert set(pf["selected"]) == set(_exact_reference(L, 600_000))

def test_csv_rounding_consistent():
    """Defect 5: net_independent was unrounded; net_conservative was rounded. Both must match."""
    import csv, io
    from app.reports import pilots_csv
    pilots = load()["pilots"]
    text = pilots_csv(pilots)
    reader = csv.DictReader(io.StringIO(text))
    for row in reader:
        ni, nc = row["net_independent"], row["net_conservative"]
        # Both must be integers (rounded), not floats like 120000.0
        assert "." not in ni, f"net_independent not rounded: {ni!r}"
        assert "." not in nc, f"net_conservative not rounded: {nc!r}"

# API
c = TestClient(app)
def test_api_portfolio_default():
    j = c.post("/portfolio", json={}).json(); assert j["ok"] and len(j["selected"]) == 3
def test_api_judge_challenge_budget():
    j = c.post("/portfolio", json={"budget": 100000}).json(); assert j["selected"] == [] and "below the cheapest" in j["message"]
def test_api_bad_budget_type_no_500():
    assert c.post("/portfolio", json={"budget": "abc"}).status_code == 200
def test_api_bad_mode():
    assert c.post("/portfolio", json={"mode": "x"}).status_code == 422
def test_api_stoprule():
    j = c.post("/stoprule/AI1", json={"observed_adoption_pct": 20, "observed_quality_pct": 90}).json(); assert j["decision"] == "STOP"
    assert c.post("/stoprule/NOPE", json={}).status_code == 404
def test_api_exports():
    assert "AI1" in c.post("/export/csv", json={}).text
    ev = c.post("/export/evidence", json={}).text; assert "Synthetic data" in ev and "Stop-rule" in ev

# ---- Phase 2 tests: multi-period, provenance, forecast ----

def test_steady_state_output_unchanged():
    """Steady state must equal today's output: the flat keys of view() must be byte-identical in shape."""
    p = get(load(), "AI1")
    assert list(view(p, "conservative").keys()) == ["id", "benefit", "net", "payback_months"]
    v = view(p, "conservative")
    assert v["payback_months"] == pytest.approx(payback_months(p.setup, realized_benefit(p), p.run))
    assert view(p, "independent")["payback_months"] == pytest.approx(FIX["records"]["expected_first_pilot_payback_months"])
    with_def = view(p, "independent", DEF)
    for k in ("id", "benefit", "net", "payback_months"):
        assert with_def[k] == view(p, "independent")[k]

def test_multi_period_matches_worked_example():
    """Independent AI1: break-even 20 months, 3-year profit 1.6 L, ROI 80% (plan worked example A)."""
    mp = view(get(load(), "AI1"), "independent", DEF)["multi_period"]
    assert mp["breakeven_month"] == 20
    assert mp["profit_inr"] == 160000
    assert mp["roi_pct"] == 80.0
    assert mp["npv_inr"] < mp["profit_inr"]          # positive rate must discount below the plain sum
    assert mp["cumulative"][-1] == 160000 and len(mp["cumulative"]) == 36
    # r = 0 must equal profit/loss
    d0 = {**DEF, "discount_rate": 0}
    mp0 = view(get(load(), "AI1"), "independent", d0)["multi_period"]
    assert mp0["npv_inr"] == mp0["profit_inr"] == 160000

def test_multi_period_beyond_horizon_is_none():
    """C-style pilot: break-even past the horizon is reported as None + flag, never invented."""
    pilot = get(load(), "AI3")  # same 20-month pilot; shrink the horizon instead to test the flag
    mp = view(pilot, "independent", {**DEF, "horizon_months": 12})["multi_period"]
    assert mp["breakeven_month"] is None and mp["breakeven_beyond_horizon"] is True
    assert mp["profit_inr"] < 0

def test_cost_lines_replace_fixture_figures():
    """Supplied cost lines replace the fixture setup/run; bad lines are ignored with a warning."""
    def mut(f):
        f["records"]["pilots"][0].update(one_time_costs=[
            {"label": "platform licence", "amount_inr": 120000},
            {"label": "fine tuning", "amount_inr": 80000},
            {"label": "bad row", "amount_inr": "two lakh"},
        ])
        f["records"]["pilots"][0].update(recurring_costs=[{"label": "vendor", "amount_inr_per_year": 96000}])
    e = [{**x} for x in EXT["pilots"]]
    L = load(mut, ext=e)
    p = get(L, "AI1")
    assert p.setup == 200000 and p.run == 96000
    assert any("ignored a malformed cost line" in w for w in L["warnings"])
    # a supplied recurring cost kills the run-cost assumption
    assert "RUN_COST_ASSUMED" not in [f["code"] for f in p.flags]
    # extension may supply the lines instead of the fixture row
    e[0]["one_time_costs"] = [{"amount_inr": 400000}]
    p2 = get(load(ext=e), "AI1")
    assert p2.setup == 400000
    assert build_portfolio([p2], 300000)["selected"] == []  # cost lines raised the setup above this budget

def test_benefit_driver_computes_benefit():
    """benefit_driver = volume × minutes ÷ 60 × loaded rate × 12; incomplete driver warns and uses the typed figure."""
    def mut(f):
        f["records"]["pilots"][0].update(benefit_driver={
            "volume_per_month": 400, "minutes_saved_per_unit": 30, "loaded_cost_per_hour": 200})
    p = get(load(mut), "AI1")
    assert p.benefit == pytest.approx(400 * 30 / 60 * 200 * 12)  # 480000/yr
    assert get(load(mut, ext=[{**x, "benefit_driver": {"volume_per_month": 400}} for x in EXT["pilots"]]), "AI1").benefit == 180000

def test_ramp_and_delay_flow_through():
    """ramp_months / delay_months reach the monthly engine: delay 5 × {1/5..5/5} ramp pushes break-even past 20."""
    e = [{**x, "ramp_months": 5, "delay_months": 5} if x["id"] == "AI1" else x for x in EXT["pilots"]]
    mp = view(get(load(ext=e), "AI1"), "independent", DEF)["multi_period"]
    # months 1-5: no benefit; 6-10: ramp (1/5..5/5) of 15000 gross minus 5000 run charged every month; then full
    expected_profit = -200000 - 5000 * 36 + 15000 * (0.2 + 0.4 + 0.6 + 0.8 + 1.0) + 15000 * 26
    assert mp["profit_inr"] == round(expected_profit)  # 55000
    assert mp["breakeven_month"] > 20

def test_provenance_sources():
    """Every decision-driving value carries source + note; fixture values are never observed."""
    e = [{**x, "observed_adoption_pct": 58} if x["id"] == "AI1" else x for x in EXT["pilots"]]
    prov = provenance(get(load(ext=e), "AI1"))
    assert prov["setup_inr"]["source"] == "fixture (synthetic)"
    assert prov["annual_running_cost_inr"]["source"] == "fixture (synthetic)"
    assert prov["benefit_type"]["source"] == "fixture (synthetic)"
    assert prov["adoption_pct"]["source"] == "assumed (team)"
    assert prov["observed_adoption_pct"]["source"] == "observed" and prov["observed_adoption_pct"]["value"] == 58
    assert prov["observed_quality_pct"]["source"] is None  # unknown is not a source, not zero
    assert f"= INR 180,000 − INR 60,000 = INR 120,000/yr" in prov["net_independent"]["note"]
    assert prov["net_independent"]["source"] == "calculated"
    # assumed run cost shows as assumed (team), never as observed
    p = get(load(lambda f: f["records"]["pilots"][0].update(annual_running_cost_inr=None)), "AI1")
    assert provenance(p)["annual_running_cost_inr"]["source"] == "assumed (team)"

def test_forecast_block_numbers_and_flags():
    """Token forecast from the shipped defaults reproduces the worked example; removing inputs yields flags, no numbers."""
    from app.main import app as _a  # endpoint path
    j = c.post("/pilots", json={}).json()
    f = j["forecast"]
    assert f["ai_run_cost_inr"] == 170000 and f["next_fy_cost_inr"] == 182000 and f["inflation_flag"] is True
    # extension without token inputs → flags + None
    # explicit nulls nullify the shipped token assumptions (missing is never zero)
    stripped = {**EXT, "defaults": {**{k: v for k, v in DEF.items() if not k.startswith(("tokens", "price", "ai_fixed"))},
                                   "tokens_per_month": None, "price_per_1m_tokens": None}}
    j2 = c.post("/pilots", json={"extension": stripped}).json()
    f2 = j2["forecast"]
    assert f2["ai_run_cost_inr"] is None and f2["next_fy_cost_inr"] is None and f2["inflation_flag"] is True
    codes = [x["code"] for x in f2["flags"]]
    assert codes == ["TOKENS_MISSING", "TOKEN_PRICE_MISSING"]
    # per-pilot token value likewise never invented
    assert j2["pilots"][0]["independent"]["multi_period"]["value_per_1m_tokens"] is None

def test_defaults_expose_pipeline_rules():
    j = c.get("/defaults").json()
    pr = j["pipeline_rules"]
    assert pr["cash_factor_by_type"]["time freed"] == 0.3
    assert pr["horizon_months"] == 36 and abs(pr["discount_rate"] - 0.10) < 1e-9
    assert "npv" in pr["formulas"]

def test_api_pilots_carry_multiperiod_and_provenance():
    j = c.post("/pilots", json={}).json()
    p = j["pilots"][0]
    assert p["independent"]["multi_period"]["breakeven_month"] == 20
    assert p["provenance"]["adoption_pct"]["source"] == "assumed (team)"
    assert j["forecast"]["ai_run_cost_inr"] == 170000
