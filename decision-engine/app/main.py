import json, re, time
from pathlib import Path
from typing import Any, Optional
from fastapi import FastAPI, HTTPException
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel
from . import core, reports

ROOT = Path(__file__).resolve().parent.parent
DATA, SAVED = ROOT / "data", ROOT / "saved"
app = FastAPI(title="C23 AI Pilot Investment Decision Desk", version="1.0")

def _defaults():
    return json.loads((DATA / "extension.json").read_text())

class Scenario(BaseModel):
    """Everything optional: omit to use the shipped files. Send overrides for judge 'change one input' challenges."""
    fixture: Optional[dict] = None
    extension: Optional[dict] = None   # {pilot_budget_inr, defaults, pilots:[...]}
    budget: Optional[Any] = "__default__"
    mode: str = "independent"

def _ctx(s: Scenario):
    fixture = s.fixture or json.loads((DATA / "starting_state.json").read_text())
    ext = s.extension or _defaults()
    d = {**_defaults()["defaults"], **ext.get("defaults", {})}
    budget = ext.get("pilot_budget_inr") if s.budget == "__default__" else s.budget
    if s.mode not in ("independent", "conservative"): raise HTTPException(422, "mode must be independent or conservative")
    return fixture, ext, d, budget, core.load_pilots(fixture, ext.get("pilots", []), d)

@app.get("/health")
def health(): return {"ok": True}

@app.get("/defaults")
def defaults():
    return {
        "starting_state": json.loads((DATA / "starting_state.json").read_text()),
        "extension": _defaults(),
    }

@app.post("/pilots")
def pilots(s: Scenario = Scenario()):
    _, _, d, _, L = _ctx(s)
    return {"pilots": [{**p.__dict__, "independent": core.view(p, "independent"), "conservative": core.view(p, "conservative"),
                        "breakeven_adoption_pct": core.breakeven_adoption_pct(p), "scenarios": core.scenarios(p),
                        "stop_rule": core.evaluate_stop_rule(p, d)} for p in L["pilots"]],
            "invalid": L["invalid"], "warnings": L["warnings"], "tie_break_order": core.TIE_BREAK_ORDER}

@app.post("/portfolio")
def portfolio(s: Scenario = Scenario()):
    _, _, _, budget, L = _ctx(s)
    return {**core.build_portfolio(L["pilots"], budget, s.mode), "invalid": L["invalid"], "warnings": L["warnings"]}

class Manual(Scenario):
    ids: list[str]
@app.post("/portfolio/check")
def manual(s: Manual):
    _, _, _, budget, L = _ctx(s)
    if not isinstance(budget, (int, float)): raise HTTPException(422, "budget missing or invalid")
    return core.check_manual_selection(L["pilots"], s.ids, budget)

class Obs(Scenario):
    observed_adoption_pct: Optional[float] = None
    observed_quality_pct: Optional[float] = None
@app.post("/stoprule/{pid}")
def stoprule(pid: str, s: Obs):
    _, _, d, _, L = _ctx(s)
    p = next((x for x in L["pilots"] if x.id == pid), None)
    if not p: raise HTTPException(404, f"pilot {pid} not found or invalid")
    if s.observed_adoption_pct is not None: p.observed_adoption_pct = s.observed_adoption_pct
    if s.observed_quality_pct is not None: p.observed_quality_pct = s.observed_quality_pct
    return core.evaluate_stop_rule(p, d)

@app.post("/export/csv", response_class=PlainTextResponse)
def export_csv(s: Scenario = Scenario()): return reports.pilots_csv(_ctx(s)[4]["pilots"])

@app.post("/export/evidence", response_class=PlainTextResponse)
def export_evidence(s: Scenario = Scenario()):
    _, _, d, budget, L = _ctx(s)
    return reports.evidence_md(L["pilots"], core.build_portfolio(L["pilots"], budget, s.mode), d, L["invalid"], L["warnings"])

class Save(Scenario):
    name: str
@app.post("/save")
def save(s: Save):
    """Saves starting state + negotiated scope + final evidence, as the brief requires."""
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,40}", s.name): raise HTTPException(422, "name must be 1-40 chars: letters, digits, _ or -")
    fixture, ext, d, budget, L = _ctx(s)
    res = core.build_portfolio(L["pilots"], budget, s.mode)
    doc = {"saved_at": time.strftime("%Y-%m-%dT%H:%M:%S"), "synthetic": True,
           "starting_state": json.loads((DATA / "starting_state.json").read_text()),
           "negotiated_scope": {"fixture_as_used": fixture, "extension": ext, "mode": s.mode},
           "final_evidence": {"portfolio": res, "invalid": L["invalid"], "warnings": L["warnings"],
                              "stop_rules": {p.id: core.evaluate_stop_rule(p, d) for p in L["pilots"]}}}
    SAVED.mkdir(exist_ok=True); (SAVED / f"{s.name}.json").write_text(json.dumps(doc, indent=2))
    return {"saved": s.name}

@app.get("/saved/{name}")
def saved(name: str):
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,40}", name): raise HTTPException(422, "bad name")
    f = SAVED / f"{name}.json"
    if not f.exists(): raise HTTPException(404, "not found")
    return json.loads(f.read_text())
