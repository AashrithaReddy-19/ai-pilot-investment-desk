"""Phase 6: checkpoint outcomes, 30-day plan, budget reallocation, decision memo."""
import copy, json
from pathlib import Path
import pytest
from fastapi.testclient import TestClient

from app.core import load_pilots, build_portfolio, view, provenance, breakeven_adoption_pct
from app import lifecycle as LC, reports, main as M
from app.main import app

c = TestClient(app)
D = Path(__file__).resolve().parent.parent / "data"
FIX = json.loads((D / "starting_state.json").read_text())
EXT = json.loads((D / "extension.json").read_text())
DEF = EXT["defaults"]
PD = {**DEF}


def P(**over):
    row = copy.deepcopy(FIX["records"]["pilots"][0])
    row["id"] = "P1"
    row.update(over)
    return row


def REAL(row):
    """Real money (calculated): one-time + recurring cost lines and a benefit driver."""
    return {**row, "one_time_costs": [{"label": "licences", "amount_inr": row["setup_cost_inr"]}],
            "recurring_costs": [{"label": "api", "amount_inr_per_year": row["annual_running_cost_inr"]}],
            "benefit_driver": {"volume_per_month": 5000, "minutes_saved_per_unit": 1.5,
                               "loaded_cost_per_hour": row["annual_benefit_inr"] / (5000 * 1.5 / 60 * 12)}}


def loaded(row, ext=()):
    return load_pilots({"records": {"pilots": [row]}}, list(ext), PD)["pilots"][0]


def cp(row, adoption=None, quality=None, ext=(), revised=False, days=None, real=True):
    p = loaded(REAL(row) if real else row, ext)
    if adoption is not None: p.observed_adoption_pct = adoption
    if quality is not None: p.observed_quality_pct = quality
    return LC.checkpoint(p, PD, revised_before=revised, duration_days=days)


# ---- 6.1 checkpoint outcomes ----

def test_checkpoint_pending_without_real_evidence():
    a = cp(P())
    assert a["checkpoint"] == "pending" and any("not recorded yet" in r for r in a["reasons"])
    b = cp(P(), adoption=70)                       # only one side observed: still pending
    assert b["checkpoint"] == "pending" and "quality" in b["reasons"][0]
    assert b["inputs"]["evidence_band"] and b["inputs"]["target_adoption_pct"]


def test_checkpoint_scale_needs_medium_band_and_passing_gates():
    a = cp(P(), adoption=95, quality=95, ext=[{"id": "P1"}])   # band high (real money), gates pass
    assert a["checkpoint"] == "scale" and a["inputs"]["evidence_band"] in ("high", "medium")


def test_checkpoint_extend_on_low_band_even_when_gates_pass():
    """Gates pass on real measurements, but the money numbers are still fixture (synthetic):
    band stays low => extend instead of scale."""
    a = cp(P(), adoption=95, quality=95, real=False)   # all fixture money; only adoption/quality observed
    assert a["checkpoint"] == "extend"
    assert any("evidence band is low" in r for r in a["reasons"])
    assert a["inputs"]["evidence_band"] == "low"


def test_checkpoint_extend_younger_than_planned_duration():
    a = cp(P(), adoption=95, quality=95, ext=[{"id": "P1"}], days=15)
    assert a["checkpoint"] == "extend" and any("15 of 30" in r for r in a["reasons"])


def test_checkpoint_boundary_is_inclusive_at_targets():
    """Targets are inclusive: exactly at adoption target and quality threshold is a pass."""
    a = cp(P(), adoption=DEF["target_adoption_pct"], quality=DEF["quality_threshold_pct"], ext=[{"id": "P1"}], days=30)
    assert a["checkpoint"] == "scale"


def test_checkpoint_revise_gate_fails_adoption_above_breakeven():
    a = cp(P(), adoption=85, quality=DEF["quality_threshold_pct"] - 10)
    assert a["checkpoint"] == "revise"
    assert any("quality" in r and "below the threshold" in r for r in a["reasons"])


def test_checkpoint_stop_below_breakeven_or_twice_or_dead_net():
    assert cp(P(), adoption=5, quality=95)["checkpoint"] == "stop"          # below break-even
    assert cp(P(), adoption=85, quality=60, revised=True)["checkpoint"] == "stop"   # gate fails again
    assert cp(P(annual_running_cost_inr=500000), adoption=95, quality=95)["checkpoint"] == "stop"  # net <= 0


# ---- 6.2 plan ----

def test_plan_fields_and_cost_limit_math():
    p = loaded(P())
    plan = LC.plan_30d(p, PD, owner="Ops")
    assert plan["pilot_id"] == "P1" and plan["objective"] == p.task and plan["owner"] == "Ops"
    assert plan["adoption_target_pct"] == PD["target_adoption_pct"]
    assert plan["quality_target_pct"] == PD["quality_threshold_pct"]
    assert plan["duration_days"] == LC.PLANNED_DURATION_DAYS == 30
    assert plan["cost_limit_inr"] == round(p.setup) + round(p.run * 30 / 365.0)
    assert plan["stop_rule"] and "break-even" in plan["stop_rule"] and plan["status"] == "planned"


# ---- 6.3 reallocation ----

def mk(n=6, groups=None):
    rows = copy.deepcopy(FIX["records"]["pilots"][:n])
    for i, r in enumerate(rows):
        r["overlap_group"] = (groups or {}).get(r["id"], f"g{r['id']}")
    return load_pilots({"records": {"pilots": rows}}, [{"id": r["id"], "adoption_pct": 70, "readiness": "high"}
                                                       for r in rows], PD)["pilots"]


def test_reallocation_frees_locked_and_never_exceeds_budget():
    ps = mk(groups={"AI1": "chat"})
    r = LC.reallocation(ps, 600000, ["AI1", "AI2", "AI4"], ["AI1"])
    assert r["ok"] and r["freed_budget_inr"] == 200000 and r["locked_funded"] == ["AI2", "AI4"]
    assert r["remaining_budget_inr"] == 400000
    rr = r["reallocation"]
    assert rr["ok"] and rr["total_setup"] <= r["remaining_budget_inr"] + 1e-9
    # every suggested pilot must be from the candidate universe, never the funded or stopped ones
    assert not ({"AI1", "AI2", "AI4"} & set(rr["selected"]))


def test_reallocation_never_picks_a_locked_overlap_sibling():
    ps = mk(groups={"AI2": "chat", "AI3": "chat"})   # AI3 shares STILL-FUNDED AI2's group
    r = LC.reallocation(ps, 600000, ["AI1", "AI2", "AI4"], ["AI1"])
    sel = r["reallocation"]["selected"]
    assert "AI3" not in sel, "reallocation double-counted a locked benefit group"
    assert r["reallocation"]["total_setup"] <= 400000


def test_reallocation_stopping_unfunded_pilot_frees_nothing():
    ps = mk()
    r = LC.reallocation(ps, 600000, ["AI1"], ["AI3"])
    assert r["freed_budget_inr"] == 0 and "Nothing was freed" in r["message"]


def test_reallocation_unknown_ids_and_missing_budget():
    ps = mk()
    r = LC.reallocation(ps, 600000, ["AI1"], ["NOPE"])
    assert not r["ok"] and "NOPE" in r["message"]
    r2 = LC.reallocation(ps, None, ["AI1"], ["AI1"])
    assert r2["ok"] is False or r2["message"] == ""
    assert "needs a numeric budget" in r2["message"] and r2["freed_budget_inr"] == 200000


# ---- 6.4 memo ----

def test_memo_contains_every_provenance_tag_and_sections():
    p = loaded(P())
    memo = reports.memo_md(p, PD)
    for src in {v["source"] for v in provenance(p).values() if v["source"] is not None}:
        assert src in memo, f"provenance tag {src!r} missing from memo"
    for header in ("## Recommendation", "## Key numbers", "## Evidence band and gaps",
                   "## Next actions", "## 30-day plan", "## Audit trail"):
        assert header in memo, header
    assert "INSUFFICIENT INFORMATION" in memo
    assert "No plan recorded yet" in memo
    plan = LC.plan_30d(p, PD, owner="Data Eng")
    memo2 = reports.memo_md(p, PD, plan)
    assert plan["stop_rule"] in memo2 and "Data Eng" in memo2 and plan["objective"] in memo2


# ---- endpoints ----

def test_api_checkpoint_keeps_stop_rule_field():
    j = c.post("/checkpoint/AI1", json={"observed_adoption_pct": 90, "observed_quality_pct": 95}).json()
    assert j["checkpoint"] == "extend" and j["stop_rule"]["decision"] == "GO"
    assert c.post("/checkpoint/NOPE", json={}).status_code == 404


def test_api_plan_and_validation():
    j = c.post("/plan", json={"pilot_id": "AI1", "owner": "Ops"}).json()
    assert j["pilot_id"] == "AI1" and j["status"] == "planned" and j["owner"] == "Ops"
    assert c.post("/plan", json={"pilot_id": "NOPE"}).status_code == 422
    assert c.post("/plan", json={"pilot_id": "AI1", "duration_days": 400}).status_code == 422


def test_api_reallocation_validation_and_result():
    j = c.post("/reallocation", json={"budget": 600000, "funded_ids": ["AI1", "AI2", "AI4"], "stopped_ids": ["AI1"]}).json()
    assert j["ok"] and j["freed_budget_inr"] == 200000 and j["reallocation"]["selected"]
    assert c.post("/reallocation", json={"budget": 600000, "funded_ids": ["AI1"], "stopped_ids": ["AI1"], "objective": "sharpe"}).status_code == 422
    assert c.post("/reallocation", json={"budget": -5, "funded_ids": [], "stopped_ids": []}).status_code == 422


def test_api_memo_endpoint():
    t = c.post("/export/memo", json={"pilot_id": "AI1"})
    assert t.status_code == 200 and t.headers["content-type"].startswith("text/markdown")
    assert "Decision memo — AI1" in t.text
    assert c.post("/export/memo", json={"pilot_id": "NOPE"}).status_code == 404


def test_api_pilots_carry_checkpoint_and_defaults_expose_lifecycle_rules():
    j = c.post("/pilots", json={}).json()
    assert all(p["checkpoint"]["checkpoint"] in LC.CHECKPOINT_OUTCOMES for p in j["pilots"])
    lcr = c.get("/defaults").json()["pipeline_rules"]["lifecycle_rules"]
    assert lcr["checkpoint_outcomes"] == LC.CHECKPOINT_OUTCOMES and lcr["planned_duration_days"] == 30
    assert "plan_statuses" in lcr
