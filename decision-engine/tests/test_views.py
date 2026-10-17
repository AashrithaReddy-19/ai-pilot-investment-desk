"""A7 views: the yearly savings view and the dashboard, plus the cross-checks that the dashboard,
the yearly view, the compare rows and the report all carry the same numbers."""
from fastapi.testclient import TestClient

from app import views
from app.main import app

c = TestClient(app)
# Worked example from the plan: A 2.0/1.8/0.6 L, B 5.0/4.0/1.5 L, C 1.0/0.9/0.7 L, r = 0, 36 months.
EX = [{"name": "A", "setup_cost_inr": 200000, "annual_benefit_inr": 180000, "ai_running_cost_inr": 60000},
      {"name": "B", "setup_cost_inr": 500000, "annual_benefit_inr": 400000, "ai_running_cost_inr": 150000},
      {"name": "C", "setup_cost_inr": 100000, "annual_benefit_inr": 90000, "ai_running_cost_inr": 70000}]
BODY = {"proposals": EX, "budget": 700000, "extension": {"defaults": {"discount_rate": 0}}}


def test_yearly_view_hand_calculated():
    j = c.post("/views/yearly-savings", json=BODY).json()
    a = next(r for r in j["proposals"] if r["id"] == "A")
    assert [y["net_inr"] for y in a["years"]] == [-80000, 120000, 120000]            # 180k − 60k − 200k, then 120k
    assert [y["cumulative_inr"] for y in a["years"]] == [-80000, 40000, 160000]
    assert a["totals"]["net_inr"] == a["profit_inr"] == 160000 and a["matches_profit"]
    assert j["portfolio"]["ids"] == ["B", "A"]                                         # budget 7 L funds A + B
    assert [y["net_inr"] for y in j["portfolio"]["years"]] == [-330000, 370000, 370000]


def test_yearly_view_with_ramp_and_delay_splits_the_year():
    p = [{**EX[0], "ramp_months": 4, "delay_months": 2}, EX[1]]
    a = next(r for r in c.post("/views/yearly-savings", json={**BODY, "proposals": p}).json()["proposals"] if r["id"] == "A")
    # year 1 benefit: months 3-12 earn 15k × (1/4, 2/4, 3/4, 1 × 7) = 15k × 8.5 = 127,500
    assert a["years"][0]["benefit_inr"] == 127500 and a["matches_profit"]


def test_yearly_csv_rows():
    txt = c.post("/export/yearly-csv", json=BODY).text.splitlines()
    assert txt[0].startswith("id,year,months,benefit_inr")
    assert "A,1,12,180000,180000,0,60000,200000,-80000,-80000" in txt
    assert txt[-1].startswith("PORTFOLIO,3,")


def test_dashboard_numbers_agree_everywhere():
    d = c.post("/views/dashboard", json=BODY).json()
    k = {x["key"]: x["value"] for x in d["kpis"]}
    assert k["total_investment_inr"] == 700000 and k["net_annual_savings_inr"] == 370000
    assert k["profit_inr"] == 410000 and k["npv_inr"] == 410000                      # r = 0: NPV equals profit
    assert k["roi_pct"] == 58.6 and k["breakeven_month"] == 23                        # 410k/700k; −700k + 23 × 30,833
    assert all(x["ok"] for x in d["consistency"])
    cmp = c.post("/compare", json=BODY).json()
    assert cmp["selection"]["total_net_inr"] == k["net_annual_savings_inr"]
    yv = c.post("/views/yearly-savings", json=BODY).json()
    assert sum(y["net_inr"] for y in yv["portfolio"]["years"]) == k["profit_inr"]
    rep = c.post("/export/decision-report", json=BODY).text
    assert "INR 370,000" in rep and "INR 410,000" in rep and "✗" not in rep


def test_dashboard_states_charts_and_links():
    d = c.post("/views/dashboard", json=BODY).json()
    states = {p["id"]: p["state"] for p in d["proposals"]}
    assert states == {"A": "recommended", "B": "recommended", "C": "not_viable"}
    assert set(views.PROPOSAL_STATES) >= set(states.values())
    order = d["charts"]["series_order"]
    assert order == [p["id"] for p in d["proposals"]]
    for name in ("cumulative_cash_flow", "yearly_savings"):
        assert [s["id"] for s in d["charts"][name]["series"]] == order
    for name in ("roi_breakeven", "cash_vs_capacity"):
        assert [r["id"] for r in d["charts"][name]["rows"]] == order
    assert len(d["charts"]["cumulative_cash_flow"]["portfolio"]) == 36
    assert d["header"]["rules_version"] and d["links"]["decision_report"] == "/export/decision-report"
    assert d["summary"]["headline"] and d["summary"]["read_more"].startswith(d["recommendation"]["reasons"][0])


def test_dashboard_risks_and_burn():
    d = c.post("/views/dashboard", json={**BODY, "ai_burn_to_date": 1234567}).json()
    k = {x["key"]: x for x in d["kpis"]}
    assert k["ai_burn_to_date_inr"]["value"] == 1234567 and k["ai_burn_to_date_inr"]["source"].startswith("observed")
    gate = next(r for r in d["risks"] if r["code"] == "GATE_BREAKEVEN_IN_HORIZON")
    assert gate["ids"] == ["C"] and gate["severity"] == "high"
    assert d["risks"] == sorted(d["risks"], key=lambda r: ({"high": 0, "medium": 1, "low": 2}[r["severity"]], r["code"]))


def test_dashboard_defaults_to_the_shipped_fixture():
    d = c.post("/views/dashboard", json={}).json()
    assert d["header"]["proposal_count"] == 5 and d["recommendation"]["outcome"] == "gather_evidence"
    assert all(x["ok"] for x in d["consistency"])
