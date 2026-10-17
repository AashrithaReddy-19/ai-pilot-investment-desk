"""Internal AI service: contract, validation, determinism, fallback, grounding guard."""
import json
from pathlib import Path
import pytest
from fastapi.testclient import TestClient

from app.ai import explain, grounding, semantic
from app.ai_service import app
from evals import run_evals

c = TestClient(app)
EV = json.loads((Path(__file__).resolve().parent.parent / "evals" / "fixtures" / "evaluations.json").read_text())["evaluations"]
PILOTS = [{"id": "AI4", "text": "Support reply drafting. Draft first replies for support agents to edit.", "overlap_group": "support"},
          {"id": "AI5", "text": "Ticket tagging. Auto-tag incoming support tickets by topic.", "overlap_group": "support"},
          {"id": "AI1", "text": "Invoice field extraction into the finance ledger.", "overlap_group": "finance"}]


def test_health():
    j = c.get("/health").json()
    assert j["status"] == "ok" and j["advisory_only"] is True


def test_semantic_contract_matches_go_client():
    j = c.post("/internal/semantic-overlap", json={"pilots": PILOTS}).json()
    assert j["advisory"] is True and j["model_name"] == "lexical-domain-overlap" and j["model_version"]
    pairs = {(p["pilot_a"], p["pilot_b"]): p for p in j["pairs"]}
    assert set(pairs) == {("AI1", "AI4"), ("AI1", "AI5"), ("AI4", "AI5")}
    assert pairs[("AI4", "AI5")]["possible_overlap"] and not pairs[("AI1", "AI4")]["possible_overlap"]
    for p in j["pairs"]:
        assert {"pilot_a", "pilot_b", "semantic_similarity", "possible_overlap", "reason", "method"} <= p.keys()
        assert 0 <= p["semantic_similarity"] <= 1
    assert c.post("/internal/semantic-overlap", json={"pilots": PILOTS}).json() == j   # deterministic


@pytest.mark.parametrize("body", [
    {"pilots": PILOTS[:1]},                                              # need >= 2
    {"pilots": [PILOTS[0], PILOTS[0]]},                                  # duplicate ids
    {"pilots": [{**PILOTS[0], "id": "../x"}, PILOTS[1]]},                # bad id
    {"pilots": [{**PILOTS[0], "text": "x" * 2001}, PILOTS[1]]},          # oversized text
    {"pilots": PILOTS, "extra": 1},                                      # unknown field
    {"pilots": [{**PILOTS[0], "text": 5}, PILOTS[1]]},                   # wrong type
])
def test_semantic_rejects_bad_input(body):
    assert c.post("/internal/semantic-overlap", json=body).status_code == 422


def test_malformed_json():
    r = c.post("/internal/semantic-overlap", content=b"{bad", headers={"content-type": "application/json"})
    assert r.status_code == 422


def test_fallback_when_text_missing():
    j = c.post("/internal/semantic-overlap", json={"pilots": [{**PILOTS[0], "text": ""}, PILOTS[1]]}).json()
    p = j["pairs"][0]
    assert p["method"] == "overlap_group_fallback" and p["possible_overlap"] is True


def test_explain_grounded_for_every_engine_case():
    for name, ev in EV.items():
        j = c.post("/internal/explain", json={"evaluation": ev}).json()
        assert j["grounded"] is True, (name, j["failures"])
        assert ev["decision"] in j["explanation"] and "not proven returns" in j["explanation"]


def test_explain_rejects_bad_evaluation():
    ev = dict(EV["official_AI1"])
    assert c.post("/internal/explain", json={"evaluation": {**ev, "decision": "APPROVE"}}).status_code == 422
    assert c.post("/internal/explain", json={"evaluation": {k: v for k, v in ev.items() if k != "pilot_id"}}).status_code == 422
    assert c.post("/internal/explain", json={}).status_code == 422


@pytest.mark.parametrize("text,failure", [
    ("AI1: decision REVIEW. It pays back in 7 months.", "numbers"),
    ("AI1: decision REVIEW. Unlike AI9 it is fine.", "pilot_ids"),
    ("AI1: decision PROCEED.", "decision"),
    ("AI1: decision REVIEW, maybe STOP.", "decision"),
    ("AI1: decision REVIEW. This is proven ROI.", "claims"),
    ("AI1: decision REVIEW. Returns are guaranteed.", "claims"),
    ("AI1: decision REVIEW. Code MAGIC_APPROVAL.", "reason_codes"),
    ("AI1: decision REVIEW. Saves 20 hours a week.", "units"),
])
def test_grounding_rejects(text, failure):
    g = grounding.validate(text, EV["official_AI1"])
    assert not g["grounded"] and any(f.startswith(failure) for f in g["failures"])


def test_grounding_accepts_faithful_and_negated_claims():
    ev = EV["official_AI1"]
    for t in ("AI1: decision REVIEW (INSUFFICIENT_EVIDENCE). Payback 20 months, INR 120000 net per year.",
              "AI1 is REVIEW; figures are not proven returns."):
        assert grounding.validate(t, ev)["grounded"], grounding.validate(t, ev)


def test_semantic_unit_properties():
    assert semantic.stem("invoices") == "invoice" and semantic.stem("replies") == "reply" and semantic.stem("processes") == "process"
    assert semantic.compare("a b", "c d")["semantic_similarity"] == 0.0
    r = semantic.compare("support tickets", "customer replies", "support", "support")
    assert r["possible_overlap"] and "official overlap_group also matches" in r["reason"]
    assert semantic.compare("", "", None, None)["possible_overlap"] is False


def test_explain_handles_missing_metrics():
    ev = {"pilot_id": "X1", "decision": "REVISE", "primary_reason": "MISSING_RUNNING_COST", "reason_codes": ["MISSING_RUNNING_COST"],
          "failed_rules": [{"code": "MISSING_RUNNING_COST"}], "warnings": [], "calculated_metrics": {}}
    r = explain.explain_checked(ev)
    assert r["grounded"] and "never assumed to be zero" in r["explanation"]


def test_ai_eval_gates_pass(tmp_path, monkeypatch):
    monkeypatch.setattr(run_evals, "REPORTS", tmp_path)
    assert run_evals.main() == 0
    rep = json.loads((tmp_path / "ai_evals.json").read_text(encoding="utf-8"))
    assert rep["all_gates_pass"] and rep["suites"]["hallucination"]["rejection_rate"] == 1.0
    assert rep["suites"]["required_cases"]["gate_pass"] and rep["suites"]["robustness"]["gate_pass"]
    assert "Predictive training not used" in rep["training_status"]
    assert "SYNTHETIC EVAL" in (tmp_path / "ai_evals.md").read_text(encoding="utf-8")


def test_semantic_bands():
    assert semantic.compare("Support reply drafting", "Customer response copilot")["band"] == "likely"
    assert semantic.compare("Invoice extraction", "Employee onboarding")["band"] == "unlikely"
    assert semantic.compare("Ticket classification", "Support knowledge recommendation")["band"] == "possible"
    j = c.post("/internal/semantic-overlap", json={"pilots": PILOTS}).json()
    assert all(p["band"] in ("likely", "possible", "unlikely") for p in j["pairs"])
