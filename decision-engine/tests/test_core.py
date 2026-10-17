import json, copy, pytest
from pathlib import Path
from fastapi.testclient import TestClient
from app.core import *
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
def test_api_exports_and_save(tmp_path, monkeypatch):
    import app.main as m; monkeypatch.setattr(m, "SAVED", tmp_path)
    assert "AI1" in c.post("/export/csv", json={}).text
    ev = c.post("/export/evidence", json={}).text; assert "Synthetic data" in ev and "Stop-rule" in ev
    assert c.post("/save", json={"name": "run1"}).status_code == 200
    d = c.get("/saved/run1").json(); assert {"starting_state", "negotiated_scope", "final_evidence"} <= d.keys()
    assert c.post("/save", json={"name": "../etc"}).status_code == 422
    assert c.get("/saved/..%2Fx").status_code in (404, 422)
