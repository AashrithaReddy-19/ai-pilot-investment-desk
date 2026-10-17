import json
from pathlib import Path
from typing import Any, Optional
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel
from . import core, reports
from . import compare as cmp_mod
from . import ingest
from . import advice, solver, lifecycle
from . import economics, pricing, decision, views
from . import whatif as W
from .version import RULES_VERSION, CONTRACT_VERSION, FORMULA_VERSION

ROOT = Path(__file__).resolve().parent.parent
DATA = ROOT / "data"
app = FastAPI(title="C23 AI Pilot Investment Decision Desk", version=f"rules-{RULES_VERSION}")

def _defaults():
    return json.loads((DATA / "extension.json").read_text())

class Scenario(BaseModel):
    """Everything optional: omit to use the shipped files. Send overrides for judge 'change one input' challenges."""
    fixture: Optional[dict] = None
    extension: Optional[dict] = None   # {pilot_budget_inr, defaults, pilots:[...]}
    budget: Optional[Any] = "__default__"
    mode: str = "independent"
    budget_set: Optional[dict] = None   # A3 budget lines; only read by the economics/decision blocks

def _ctx(s: Scenario):
    fixture = s.fixture or json.loads((DATA / "starting_state.json").read_text())
    ext = s.extension or _defaults()
    d = {**_defaults()["defaults"], **ext.get("defaults", {})}
    budget = ext.get("pilot_budget_inr") if s.budget == "__default__" else s.budget
    if s.mode not in ("independent", "conservative"): raise HTTPException(422, "mode must be independent or conservative")
    return fixture, ext, d, budget, core.load_pilots(fixture, ext.get("pilots", []), d)

@app.get("/health")
def health():
    return {"ok": True, "rules_version": RULES_VERSION, "contract_version": CONTRACT_VERSION,
            "formula_version": FORMULA_VERSION}

@app.get("/defaults")
def defaults():
    return {
        "starting_state": json.loads((DATA / "starting_state.json").read_text()),
        "extension": _defaults(),
        # Visible engine rules the UI can display (team assumptions are labeled, not invented).
        "pipeline_rules": {
            "versions": {"rules_version": RULES_VERSION, "contract_version": CONTRACT_VERSION,
                         "formula_version": FORMULA_VERSION},
            "cash_factor_by_type": core.CASH_FACTOR_BY_TYPE,
            "readiness_factor": core.READINESS_FACTOR,
            "tie_break_order": core.TIE_BREAK_ORDER,
            "horizon_months": core.HORIZON_MONTHS,
            "discount_rate": core.DISCOUNT_RATE,
            "scenario_multipliers": core.SCENARIO_MULTIPLIERS,
            "parse_rules": ingest.PARSE_RULES,
            "advice_rules": {
                "input_weights": advice.INPUT_WEIGHTS,
                "band_edges": advice.BAND_EDGES,
                "outcome_order": advice.OUTCOME_ORDER,
                "observed_grades": ["observed", "calculated"],
                "fixture_cap_rule": "fixture (synthetic) money values cap the evidence band at Low",
                "next_actions": advice.NEXT_ACTIONS,
            },
            "lifecycle_rules": {"checkpoint_outcomes": lifecycle.CHECKPOINT_OUTCOMES,
                                "planned_duration_days": lifecycle.PLANNED_DURATION_DAYS,
                                "plan_statuses": lifecycle.PLAN_STATUSES},
            "solver": {"objectives": core.OBJECTIVES, "max_exact_candidates": core.MAX_EXACT_CANDIDATES,
                       "max_candidates": solver.MAX_CANDIDATES, "node_budget": solver.NODE_BUDGET},
            "whatif_rules": {"pilot_keys": sorted(W.PILOT_KEYS), "org_keys": sorted(W.ORG_KEYS),
                             "sweep_fractions": W.SWEEP_FRACTIONS, "sweep_growth_pct": W.SWEEP_GROWTH_PCT,
                             "sweep_adoption_steps": list(range(0, 101, 10)),
                             "flip_scan_step_of_base": W.FLIP_SCAN_STEP, "flip_adoption_step_pct": W.FLIP_ADOPTION_STEP},
            "compare_rules": {"min_proposals": cmp_mod.MIN_PROPOSALS, "max_proposals": cmp_mod.MAX_PROPOSALS,
                              "drop_rule": "net annual savings <= 0 or break-even beyond the horizon",
                              "selector": "highest total net annual benefit within budget, overlap counted once"},
            "economics_rules": economics.ECONOMICS_RULES,
            "pricing_rules": pricing.PRICING_RULES,
            "decision_rules": {**decision.DECISION_RULES, "actions": decision.DECISION_ACTIONS},
            "view_rules": views.VIEW_RULES,
            "formulas": {
                "net_annual": "annual_benefit − annual_running_cost",
                "cash_flow_t": "benefit × ramp(t) − running cost(t); one-time cost in month 1",
                "breakeven_month": "first month where cumulative cash flow ≥ 0",
                "profit_loss": "Σ cash flows over the horizon",
                "roi": "profit ÷ total one-time cost",
                "npv": "Σ cashflow(t) ÷ (1+r)^(t/12); r = 0 equals profit/loss",
                "ai_run_cost": "monthly tokens × 12 × price per 1M ÷ 1,000,000 + fixed annual",
                "next_fy_cost": "tokens × (1 + usage growth) and price × (1 + price drift); fixed annual unchanged",
                "token_value": "(net annual savings ÷ annual tokens in millions) ÷ price per 1M tokens",
                "inflation_flag": "usage growth ≥ 0 carries AI spend into the next FY",
                "gross_cost_avoidance": "typed annual benefit, or baseline cost × automation share",
                "cashable_savings": "gross × cashable share (default: the benefit type's cash factor)",
                "capacity_value": "gross − cashable",
                "retained_human_cost": "baseline cost − gross (unknown without a baseline)",
                "weighted_score": "Σ weight × criterion normalised 0-100 across the compared set",
                "risk_adjusted_value": "net annual × readiness factor (low 0.6 / medium 0.8 / high 1.0)",
                "evidence_band": "weighted share of decision-driving inputs that are observed or calculated (real user data); "
                                 "≥ 0.75 high, ≥ 0.40 medium, else low; fixture (synthetic) money values cap the band at Low",
            },
        },
    }

def _pilot_decision(p, d, bs):
    dec = decision.decision_for(p, d, bs)
    return dec.pop("economics"), dec


@app.post("/pilots")
def pilots(s: Scenario = Scenario()):
    _, _, d, _, L = _ctx(s)
    try:
        bs = economics.validate_budget_set(s.budget_set)
    except economics.EconomicsError as exc:
        raise HTTPException(422, str(exc))
    out = []
    for p in L["pilots"]:
        econ, dec = _pilot_decision(p, d, bs)
        out.append({**p.__dict__, "independent": core.view(p, "independent", d), "conservative": core.view(p, "conservative", d),
                    "provenance": core.provenance(p, d),
                    "breakeven_adoption_pct": core.breakeven_adoption_pct(p), "scenarios": core.scenarios(p, d),
                    "advice": advice.advice_for(p, d),
                    "economics": econ, "decision": dec,
                    "checkpoint": lifecycle.checkpoint(p, d),
                    "stop_rule": core.evaluate_stop_rule(p, d)})
    return {"pilots": out,
            "forecast": core.forecast_block(d),
            "invalid": L["invalid"], "warnings": L["warnings"], "tie_break_order": core.TIE_BREAK_ORDER}

class PortfolioRequest(Scenario):
    objective: str = "net"

@app.post("/portfolio")
def portfolio(s: PortfolioRequest = PortfolioRequest()):
    _, _, d, budget, L = _ctx(s)
    try:
        result = core.build_portfolio(L["pilots"], budget, s.mode, s.objective, d)
    except ValueError as exc:
        raise HTTPException(422, str(exc))
    return {**result, "invalid": L["invalid"], "warnings": L["warnings"]}

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

# ---------- Phase 3: what-if + sensitivity sweep ----------

class WhatIfRequest(Scenario):
    pilot_id: Optional[str] = None
    overrides: dict = {}

def _apply_overrides(fixture: dict, ext: dict, overrides: dict):
    try:
        return W.apply(fixture, ext, W.validate(overrides))
    except W.OverrideError as exc:
        raise HTTPException(422, str(exc))

@app.post("/whatif")
def whatif(s: WhatIfRequest):
    try:
        overrides = W.validate(s.overrides)
    except W.OverrideError as exc:
        raise HTTPException(422, str(exc))
    fixture1, ext1, d1, budget1, L1 = _ctx(s)
    p1 = next((x for x in L1["pilots"] if x.id == s.pilot_id), None) if s.pilot_id else None
    if p1 is None:
        raise HTTPException(422, f"pilot_id must name a valid pilot, got {s.pilot_id!r}.")
    fixture2, ext2 = _apply_overrides(fixture1, ext1, overrides)
    s2 = Scenario(fixture=fixture2, extension=ext2, budget=overrides.get("budget", s.budget), mode=overrides.get("mode", s.mode))
    _, _, d2, budget2, L2 = _ctx(s2)
    p2 = next((x for x in L2["pilots"] if x.id == s.pilot_id), None)
    if p2 is None:
        raise HTTPException(422, f"pilot {s.pilot_id} became invalid after applying overrides: {L2['invalid']}")

    def block(p, d, budget, L, mode):
        nums = W.pilot_numbers(p, mode, d)
        pf = core.build_portfolio(L["pilots"], budget, mode)
        sr = core.evaluate_stop_rule(p, d)
        return {**nums, "verdict": sr["decision"], "stop_rule": sr,
                "portfolio": {"selected": pf["selected"], "total_net": round(pf["total_net"]),
                              "budget_left": round(pf["budget_left"]), "ok": pf["ok"], "message": pf["message"]}}
    base, changed = block(p1, d1, budget1, L1, s.mode), block(p2, d2, budget2, L2, s2.mode)
    NUM_KEYS = ("benefit_inr", "net", "payback_months", "profit_inr", "roi_pct", "npv_inr", "breakeven_month")
    deltas = {k: (changed[k] - base[k] if isinstance(base[k], (int, float)) and isinstance(changed[k], (int, float)) else None)
              for k in NUM_KEYS}
    out = {"pilot_id": s.pilot_id, "mode": s2.mode, "overrides_applied": overrides,
           "base": base, "changed": changed, "deltas": deltas}
    if base["verdict"] != changed["verdict"]:
        out["verdict_flip"] = {"from": base["verdict"], "to": changed["verdict"]}
        pilot_ov = [k for k in overrides if k in W.PILOT_KEYS]
        if len(pilot_ov) == 1 and pilot_ov[0] in W.FLIP_FIELDS:
            f = pilot_ov[0]
            attr = {"observed_adoption_pct": "observed_adoption_pct", "annual_benefit_inr": "benefit",
                    "annual_running_cost_inr": "run", "setup_cost_inr": "setup"}[f]
            def reload(value):
                ofx, oex = W.apply(fixture1, ext1, {f: value})
                _, _, _, _, LL = _ctx(Scenario(fixture=ofx, extension=oex, budget=s.budget, mode=s.mode))
                return LL["pilots"]
            out["flip_statement"] = W.flip_scan(f, reload, base["verdict"], changed["verdict"], getattr(p1, attr), s.pilot_id, d1)
        elif len(pilot_ov) > 1:
            out["flip_statement"] = None
            out["flip_note"] = "Threshold scan needs exactly one pilot-level override; the flip is reported without one."
    return out

class SweepRequest(Scenario):
    pilot_id: Optional[str] = None
    parameter: str = ""

@app.post("/sweep")
def sweep(s: SweepRequest):
    if s.parameter not in W.SWEEP_PARAMS:
        raise HTTPException(422, f"parameter must be one of {sorted(W.SWEEP_PARAMS)}.")
    if s.parameter in ("adoption_pct", "annual_benefit_inr", "annual_running_cost_inr") and not s.pilot_id:
        raise HTTPException(422, f"pilot_id is required to sweep {s.parameter}.")
    fixture1, ext1, d1, budget, L1 = _ctx(s)
    p1 = next((x for x in L1["pilots"] if x.id == s.pilot_id), None) if s.pilot_id else None
    if s.pilot_id and p1 is None:
        raise HTTPException(422, f"pilot {s.pilot_id!r} not found or invalid.")
    ORG_PARAMS = ("price_per_1m_tokens", "annual_usage_growth_pct")
    pilot_base = {"adoption_pct": p1.adoption_pct if p1 else None, "annual_benefit_inr": p1.benefit if p1 else None,
                  "annual_running_cost_inr": p1.run if p1 else None}
    d_dict = {"price_per_1m_tokens": d1.get("price_per_1m_tokens"), "annual_usage_growth_pct": None}
    base_value = d_dict.get(s.parameter, pilot_base.get(s.parameter))
    flags, steps = [], []
    ladder = W.sweep_steps(base_value, s.parameter)
    if s.parameter == "price_per_1m_tokens" and not W._numok(d1.get("tokens_per_month")):
        flags.append({"code": "TOKENS_MISSING", "message": "Monthly AI token usage not recorded; the sweep axis has no numbers."})
    if s.parameter == "annual_usage_growth_pct" and (not W._numok(d1.get("tokens_per_month")) or not W._numok(d1.get("price_per_1m_tokens"))):
        flags.append({"code": "TOKENS_MISSING" if not W._numok(d1.get("tokens_per_month")) else "TOKEN_PRICE_MISSING",
                      "message": "Token usage or price not recorded; the sweep axis has no numbers."})
    for v in ladder:
        fixture2, ext2 = _apply_overrides(fixture1, ext1, {s.parameter: v})
        _, _, d2, _, L2 = _ctx(Scenario(fixture=fixture2, extension=ext2, budget=s.budget, mode=s.mode))
        if s.parameter in ORG_PARAMS:
            fc = core.forecast_block(d2)
            steps.append({"value": v, "ai_run_cost_inr": fc["ai_run_cost_inr"], "next_fy_cost_inr": fc["next_fy_cost_inr"],
                          "inflation_flag": fc["inflation_flag"]})
        else:
            p2 = next((x for x in L2["pilots"] if x.id == s.pilot_id), None)
            if p2 is None:
                continue
            steps.append({"value": v, **W.pilot_numbers(p2, s.mode, d1)})
    series = [st["net" if s.parameter not in ORG_PARAMS else "next_fy_cost_inr"] for st in steps]
    better_is_lower = s.parameter in ("annual_running_cost_inr",) + ORG_PARAMS
    out = {"parameter": s.parameter, "pilot_id": s.pilot_id, "mode": s.mode,
           "steps": steps, "direction": "down_improves" if better_is_lower else "up_improves",
           "flags": flags}
    out["monotonic"] = (W.monotonic_ok(series, better_is_lower)
                        if len(series) >= 2 and all(W._numok(x) for x in series) else None)
    if not ladder and not flags:
        out["flags"] = [{"code": "SWEEP_BASE_MISSING",
                         "message": f"{s.parameter} has no recorded base value; nothing to sweep."}]
    return out

@app.post("/export/csv", response_class=PlainTextResponse)
def export_csv(s: Scenario = Scenario()): return reports.pilots_csv(_ctx(s)[4]["pilots"])

@app.post("/export/evidence", response_class=PlainTextResponse)
def export_evidence(s: Scenario = Scenario()):
    _, _, d, budget, L = _ctx(s)
    return reports.evidence_md(L["pilots"], core.build_portfolio(L["pilots"], budget, s.mode), d, L["invalid"], L["warnings"])


# ---------- Phase 6: checkpoint, plan tracker, reallocation, memo ----------

class CheckpointRequest(Scenario):
    observed_adoption_pct: Optional[float] = None
    observed_quality_pct: Optional[float] = None
    revised_before: bool = False
    duration_days: Optional[int] = None

@app.post("/checkpoint/{pid}")
def checkpoint_now(pid: str, s: CheckpointRequest):
    _, _, d, _, L = _ctx(s)
    p = next((x for x in L["pilots"] if x.id == pid), None)
    if not p: raise HTTPException(404, f"pilot {pid} not found or invalid")
    if s.observed_adoption_pct is not None: p.observed_adoption_pct = s.observed_adoption_pct
    if s.observed_quality_pct is not None: p.observed_quality_pct = s.observed_quality_pct
    cp = lifecycle.checkpoint(p, d, revised_before=s.revised_before, duration_days=s.duration_days)
    return {"pilot_id": p.id, **cp, "stop_rule": core.evaluate_stop_rule(p, d)}

class PlanRequest(Scenario):
    pilot_id: str = ""
    duration_days: Optional[int] = None
    owner: Optional[str] = None

@app.post("/plan")
def plan(s: PlanRequest):
    _, _, d, _, L = _ctx(s)
    p = next((x for x in L["pilots"] if x.id == s.pilot_id), None)
    if not p:
        raise HTTPException(422, "pilot_id must name a valid pilot.")
    if s.duration_days is not None and (not isinstance(s.duration_days, int) or not 1 <= s.duration_days <= 365):
        raise HTTPException(422, "duration_days must be a whole number between 1 and 365.")
    return lifecycle.plan_30d(p, d, duration_days=s.duration_days, owner=s.owner)

class ReallocRequest(Scenario):
    budget: Optional[Any] = None
    funded_ids: list[str] = []
    stopped_ids: list[str] = []
    objective: str = "net"

@app.post("/reallocation")
def realloc(s: ReallocRequest):
    if s.objective not in core.OBJECTIVES:
        raise HTTPException(422, f"objective must be one of: {', '.join(core.OBJECTIVES)}.")
    if s.budget is not None and (not isinstance(s.budget, (int, float)) or isinstance(s.budget, bool) or s.budget < 0):
        raise HTTPException(422, "budget must be a non-negative number or null (null = nothing to free against).")
    _, _, d, _, L = _ctx(s)
    try:
        return lifecycle.reallocation(L["pilots"], s.budget, s.funded_ids, s.stopped_ids, s.mode, s.objective, d)
    except ValueError as exc:
        raise HTTPException(422, str(exc))

class MemoRequest(Scenario):
    pilot_id: str = ""
    plan: Optional[dict] = None
    observed_adoption_pct: Optional[float] = None
    observed_quality_pct: Optional[float] = None
    revised_before: bool = False
    duration_days: Optional[int] = None

@app.post("/export/memo")
def export_memo(s: MemoRequest):
    _, _, d, _, L = _ctx(s)
    p = next((x for x in L["pilots"] if x.id == s.pilot_id), None)
    if not p:
        raise HTTPException(404, f"pilot {s.pilot_id!r} not found or invalid")
    if s.observed_adoption_pct is not None: p.observed_adoption_pct = s.observed_adoption_pct
    if s.observed_quality_pct is not None: p.observed_quality_pct = s.observed_quality_pct
    cp = lifecycle.checkpoint(p, d, revised_before=s.revised_before, duration_days=s.duration_days)
    return PlainTextResponse(reports.memo_md(p, d, s.plan, cp), media_type="text/markdown; charset=utf-8")


# ---------- Phase 4: balance sheet, proposal import, compare ----------

class CompareRequest(BaseModel):
    proposals: list[dict]
    budget: Optional[float] = None      # None = no budget constraint (every survivor is funded)
    mode: str = "independent"
    extension: Optional[dict] = None   # only defaults (horizon, discount rate, tokens, price) are read


def _defaults_block(ext: Optional[dict]) -> dict:
    return {**_defaults()["defaults"], **(ext or {}).get("defaults", {})}


@app.post("/compare")
def compare_v(s: CompareRequest):
    if s.mode not in ("independent", "conservative"):
        raise HTTPException(422, "mode must be independent or conservative.")
    if s.budget is not None and (not isinstance(s.budget, (int, float)) or isinstance(s.budget, bool) or s.budget < 0):
        raise HTTPException(422, "budget must be a non-negative number or null (null = no budget constraint).")
    try:
        clean = cmp_mod.validate(s.proposals)
        return cmp_mod.run(clean, s.budget, s.mode, _defaults_block(s.extension))
    except cmp_mod.CompareError as exc:
        raise HTTPException(422, str(exc))


async def _upload(file: UploadFile) -> bytes:
    raw = await file.read()
    if not raw:
        raise HTTPException(422, "The uploaded file is empty.")
    return raw


@app.post("/balance-sheet/parse")
async def balance_sheet_parse(file: UploadFile = File(...), unit: str = Form("auto"),
                              amount_column: Optional[str] = Form(None), period: str = Form("auto")):
    try:
        return ingest.parse_balance_sheet(await _upload(file), unit, amount_column, period)
    except ingest.ParseError as exc:
        raise HTTPException(422, str(exc))


@app.post("/balance-sheet/confirm")
async def balance_sheet_confirm(file: UploadFile = File(...), rows: str = Form(...), unit: str = Form("auto"),
                                amount_column: Optional[str] = Form(None), defaults: str = Form(""),
                                period: str = Form("auto")):
    raw = await _upload(file)
    try:
        row_ids = [int(x.strip()) for x in rows.split(",") if x.strip()]
        d = {**_defaults_block(None), **(json.loads(defaults) if defaults and defaults.strip() else {})}
    except ValueError:
        raise HTTPException(422, "rows must be a comma-separated list of candidate row numbers and "
                                 "defaults (when sent) must be a JSON object.")
    if not isinstance(d, dict):
        raise HTTPException(422, "defaults (when sent) must be a JSON object of token assumptions.")
    try:
        return ingest.confirm_balance_sheet(raw, row_ids, unit, amount_column, d, period)
    except ingest.ParseError as exc:
        raise HTTPException(422, str(exc))


@app.post("/pilots/import/parse")
async def pilots_import_parse(file: UploadFile = File(...), unit: str = Form("auto"), period: str = Form("auto")):
    try:
        return ingest.parse_pilots_csv(await _upload(file), unit, period)
    except ingest.ParseError as exc:
        raise HTTPException(422, str(exc))


@app.post("/pilots/import/confirm")
async def pilots_import_confirm(file: UploadFile = File(...), keep: str = Form(...), unit: str = Form("auto"),
                                period: str = Form("auto")):
    raw = await _upload(file)
    keep_ids = [x.strip() for x in keep.split(",") if x.strip()]
    fixture_ids = [p["id"] for p in json.loads((DATA / "starting_state.json").read_text())["records"]["pilots"]]
    try:
        return ingest.confirm_pilots_csv(raw, keep_ids, unit, fixture_ids, period)
    except ingest.ParseError as exc:
        raise HTTPException(422, str(exc))


# ---------- A3/A6/A7: economics, decision, dashboard and yearly views over a proposal set ----------

_INPUT_ERRORS = (cmp_mod.CompareError, economics.EconomicsError, decision.DecisionError, pricing.PricingError)


class ProposalSet(BaseModel):
    """Proposals in the /compare shape. Omit `proposals` to use the shipped fixture + extension pilots."""
    proposals: Optional[list[dict]] = None
    budget: Optional[Any] = "__default__"   # "__default__" = the extension's pilot budget; null = no budget
    mode: str = "independent"
    extension: Optional[dict] = None
    budget_set: Optional[dict] = None
    weights: Optional[dict] = None
    ai_burn_to_date: Optional[float] = None


def _fixture_proposals() -> list[dict]:
    fx = json.loads((DATA / "starting_state.json").read_text())["records"]["pilots"]
    ext = {e["id"]: e for e in _defaults()["pilots"]}
    out = []
    for p in fx:
        e = ext.get(p["id"], {})
        row = {"name": p["id"], "setup_cost_inr": p["setup_cost_inr"], "annual_benefit_inr": p["annual_benefit_inr"],
               "ai_running_cost_inr": p.get("annual_running_cost_inr"), "benefit_type": p.get("benefit_type") or "unknown",
               "readiness": e.get("readiness") or "low", "adoption_pct": e.get("adoption_pct", 100),
               "observed_adoption_pct": e.get("observed_adoption_pct"), "observed_quality_pct": e.get("observed_quality_pct"),
               "money_source": core.PROVENANCE_FIXTURE}
        if p.get("overlap_group"):
            row["overlap_group"] = p["overlap_group"]
        out.append(row)
    return out


def _proposal_ctx(s: ProposalSet):
    if s.mode not in ("independent", "conservative"):
        raise HTTPException(422, "mode must be independent or conservative.")
    d = _defaults_block(s.extension)
    budget = _defaults().get("pilot_budget_inr") if s.budget == "__default__" else s.budget
    if budget is not None and (not isinstance(budget, (int, float)) or isinstance(budget, bool) or budget < 0):
        raise HTTPException(422, "budget must be a non-negative number or null (null = no budget constraint).")
    try:
        clean = cmp_mod.validate(s.proposals if s.proposals is not None else _fixture_proposals())
        bs = economics.validate_budget_set(s.budget_set)
        extras = {}
        for p in clean:
            ex = {}
            if "baseline" in p:
                ex["baseline"] = economics.validate_baseline(p["baseline"], f"{p['name']}.baseline")
            if "cashable_share_pct" in p:
                ex["cashable_share_pct"] = p["cashable_share_pct"]
            extras[p["name"]] = ex
        decision.validate_weights(s.weights)
    except _INPUT_ERRORS as exc:
        raise HTTPException(422, str(exc))
    return clean, d, budget, bs, extras


@app.post("/economics")
def economics_v(s: ProposalSet = ProposalSet()):
    clean, d, _, bs, extras = _proposal_ctx(s)
    try:
        L = cmp_mod.to_pilots(clean, d)
    except _INPUT_ERRORS as exc:
        raise HTTPException(422, str(exc))
    rows = [{"id": p.id, **economics.economics_for(p, d, bs, extras[p.id].get("baseline"),
                                                    extras[p.id].get("cashable_share_pct"))} for p in L["pilots"]]
    return {"proposals": rows, "portfolio_budget": economics.portfolio_budget(L["pilots"], [p.id for p in L["pilots"]], d, bs),
            "budget_set": bs, "rules": economics.ECONOMICS_RULES, "warnings": L["warnings"]}


@app.post("/decision")
def decision_v(s: ProposalSet = ProposalSet()):
    clean, d, _, bs, extras = _proposal_ctx(s)
    try:
        L = cmp_mod.to_pilots(clean, d)
        return {**decision.decide_set(L["pilots"], d, bs, extras, s.weights), "warnings": L["warnings"]}
    except _INPUT_ERRORS as exc:
        raise HTTPException(422, str(exc))


@app.post("/views/dashboard")
def dashboard_v(s: ProposalSet = ProposalSet()):
    clean, d, budget, bs, _ = _proposal_ctx(s)
    try:
        return views.dashboard(clean, budget, s.mode, d, bs, s.ai_burn_to_date, s.weights)
    except _INPUT_ERRORS as exc:
        raise HTTPException(422, str(exc))


def _yearly(s: ProposalSet) -> dict:
    clean, d, budget, _, extras = _proposal_ctx(s)
    try:
        funded = cmp_mod.run(clean, budget, s.mode, d)["selection"]["selected"]
        return views.yearly_view(cmp_mod.to_pilots(clean, d)["pilots"], d, s.mode, extras, funded)
    except _INPUT_ERRORS as exc:
        raise HTTPException(422, str(exc))


@app.post("/views/yearly-savings")
def yearly_v(s: ProposalSet = ProposalSet()):
    return _yearly(s)


@app.post("/export/yearly-csv", response_class=PlainTextResponse)
def yearly_csv_v(s: ProposalSet = ProposalSet()):
    return PlainTextResponse(reports.yearly_csv(_yearly(s)), media_type="text/csv; charset=utf-8")


@app.post("/export/decision-report", response_class=PlainTextResponse)
def decision_report_v(s: ProposalSet = ProposalSet()):
    return PlainTextResponse(reports.decision_report_md(dashboard_v(s)), media_type="text/markdown; charset=utf-8")


# ---------- A4: pricing rules, usage forecast, package optimiser ----------

class QuoteRequest(BaseModel):
    rule: dict
    units: float


@app.post("/pricing/quote")
def pricing_quote(s: QuoteRequest):
    try:
        return pricing.monthly_bill(pricing.validate_rule(s.rule), s.units)
    except pricing.PricingError as exc:
        raise HTTPException(422, str(exc))


class ForecastRequest(BaseModel):
    history: Optional[list] = None            # monthly usage, oldest first
    months: int = 12
    annual_usage_growth_pct: Optional[float] = None
    last_known_units: Optional[float] = None
    rule: Optional[dict] = None               # optional: price the low/base/high usage with this rule


@app.post("/forecast/usage")
def forecast_usage_v(s: ForecastRequest):
    try:
        fc = pricing.forecast_usage(s.history, s.months, s.annual_usage_growth_pct, s.last_known_units)
        if s.rule is not None and fc["base"] is not None:
            rule = pricing.validate_rule(s.rule)
            fc["cost"] = {sc: pricing.cost_of(rule, fc[sc]) for sc in ("low", "base", "high")}
        return fc
    except pricing.PricingError as exc:
        raise HTTPException(422, str(exc))


class OptimiseRequest(ForecastRequest):
    plans: list


@app.post("/pricing/optimise")
def pricing_optimise(s: OptimiseRequest):
    try:
        fc = pricing.forecast_usage(s.history, s.months, s.annual_usage_growth_pct, s.last_known_units)
        return {**pricing.optimise(s.plans, fc), "forecast": {k: fc[k] for k in ("method", "note", "band_pct", "base", "low", "high")}}
    except pricing.PricingError as exc:
        raise HTTPException(422, str(exc))
