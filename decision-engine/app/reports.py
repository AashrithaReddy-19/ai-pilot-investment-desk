"""CSV + evidence markdown. Keeps observed (fixture) and assumed (team) separate."""
import csv, io
from .core import view, evaluate_stop_rule, scenarios, breakeven_adoption_pct, provenance, multi_period, TIE_BREAK_ORDER
from . import advice, lifecycle as LC

def pilots_csv(pilots: list) -> str:
    buf = io.StringIO(); w = csv.writer(buf)
    w.writerow(["id", "task", "group", "setup_inr", "run_inr", "benefit_inr", "net_independent", "payback_mo_independent",
                "net_conservative", "payback_mo_conservative", "adoption_pct", "readiness", "flags"])
    for p in pilots:
        a, b = view(p, "independent"), view(p, "conservative")
        w.writerow([p.id, p.task, p.group, p.setup, p.run, p.benefit,
                    round(a["net"]),
                    "" if a["payback_months"] is None else round(a["payback_months"], 1),
                    round(b["net"]),
                    "" if b["payback_months"] is None else round(b["payback_months"], 1),
                    p.adoption_pct, p.readiness, ";".join(f["code"] for f in p.flags)])
    return buf.getvalue()

def evidence_md(pilots: list, result: dict, defaults: dict, invalid: list, warnings: list) -> str:
    L = ["# C23 Evidence Report", "", "> Synthetic data. Inputs are assumptions. No claim of live revenue or production readiness.", "",
         "## Result", f"- Mode: {result['mode']}", f"- Budget: INR {result['budget']:g}", f"- {result['message']}",
         f"- Selected: {', '.join(result['selected']) or 'none'}", f"- Total net annual benefit: INR {result['total_net']:g}",
         f"- Tie-break order: {' > '.join(TIE_BREAK_ORDER)}", "", "## Rejected / not funded"]
    L += [f"- {r['id']}: {r['reason']}" for r in result["rejected"]] or ["- none"]
    L += ["", "## Pilot table (observed fixture figures vs team assumptions)", "",
          "| Pilot | Setup | Run/yr | Benefit/yr | Payback (indep.) | Adoption* | Readiness* | Breakeven adoption | Flags |", "|---|---|---|---|---|---|---|---|---|"]
    for p in pilots:
        v, be = view(p, "independent"), breakeven_adoption_pct(p)
        pb = "never" if v["payback_months"] is None else f"{v['payback_months']:.1f} mo"
        L.append(f"| {p.id} {p.task} | {p.setup:g} | {p.run:g} | {p.benefit:g} | {pb} | {p.adoption_pct:g}% | {p.readiness} | "
                 f"{'n/a' if be is None else f'{be:.1f}%'} | {', '.join(f['code'] for f in p.flags) or '-'} |")
    L += ["", "\\* team-authored assumption, not in the fixture.", "", "## Money over time (multi-period view)",
          "Horizon and discount rate are stated inputs; the one-time cost lands in month 1.", "",
          "| Pilot | Profit over horizon | ROI | Break-even | NPV (indep.) |", "|---|---|---|---|---|"]
    for p in pilots:
        mp = view(p, "independent", defaults)["multi_period"]
        be = "beyond horizon" if mp["breakeven_month"] is None else f"{mp['breakeven_month']} mo"
        roi = "n/a" if mp["roi_pct"] is None else f"{mp['roi_pct']:.1f}%"
        L.append(f"| {p.id} | INR {mp['profit_inr']:,.0f} | {roi} | {be} | INR {mp['npv_inr']:,.0f} |")
    L += ["", "## Provenance of the decision-driving numbers"]
    for p in pilots:
        prov = provenance(p, defaults)
        L.append(f"- {p.id}: setup {prov['setup_inr']['source']}, run cost {prov['annual_running_cost_inr']['source']}, "
                 f"benefit {prov['annual_benefit_inr']['source']}, adoption {prov['adoption_pct']['source']}"
                 + (f", observed adoption {prov['observed_adoption_pct']['value']}" if prov['observed_adoption_pct']['value'] is not None else ""))
    L += ["", "## Stop-rule status"]
    for p in pilots:
        s = evaluate_stop_rule(p, defaults); L.append(f"- {p.id}: **{s['decision']}**: {s['reasons'][0]}")
    L += ["", "## What-if scenarios (conservative mode)"]
    for p in pilots:
        L.append(f"- {p.id}: " + "; ".join(f"{s['scenario']} adoption {s['adoption_pct']}% -> net {s['net_annual']}, payback "
                                              f"{'never' if s['payback_months'] is None else str(s['payback_months']) + ' mo'}" for s in scenarios(p)))
    if invalid: L += ["", "## Invalid inputs (excluded)"] + [f"- {i['id']}: {'; '.join(i['errors'])}" for i in invalid]
    if warnings: L += ["", "## Warnings"] + [f"- {w}" for w in warnings]
    return "\n".join(L) + "\n"

def memo_md(pilot, defaults: dict, plan: dict = None, checkpoint_out: dict = None) -> str:
    """Phase 6.4 — one-page decision memo for one pilot: recommendation, key numbers with
    provenance tags, evidence band, gaps, next action, 30-day plan and the audit trail."""
    p = pilot
    ev = advice.advice_for(p, defaults)
    v = view(p, "independent", defaults)
    mp = v["multi_period"]
    prov = provenance(p, defaults)
    cp = checkpoint_out or LC.checkpoint(p, defaults)
    st = evaluate_stop_rule(p, defaults)
    be_ad = breakeven_adoption_pct(p)
    inr = lambda x: f"INR {x:,.0f}"
    L = [f"# Decision memo — {p.id} ({p.task})", "",
         "> Synthetic data. Inputs are assumptions. No claim of live revenue or production readiness.", "",
         "## Recommendation",
         f"- Decision: **{ev['outcome'].replace('_', ' ').upper()}** — {ev['reasons'][0]}",
         f"- Stop rule: **{st['decision']}** — {st['reasons'][0]}",
         f"- Checkpoint: **{cp['checkpoint'].upper()}** — {cp['reasons'][0]}",
         f"- Money lens: independent mode; conservative net {inr(view(p, 'conservative')['net'])}/yr.", ""]
    L += ["## Key numbers (with provenance tags)", "",
          "| Number | Value | Provenance tag | Detail |", "|---|---|---|---|"]
    rows = [
        ("setup_cost_inr", p.setup, prov["setup_inr"]),
        ("annual_running_cost_inr", p.run, prov["annual_running_cost_inr"]),
        ("annual_benefit_inr", p.benefit, prov["annual_benefit_inr"]),
        ("net_annual_inr", round(v["net"]), prov["net_independent"]),
        ("payback_months", v["payback_months"], prov["payback_months_independent"]),
        ("profit_inr", mp["profit_inr"], {"source": prov["net_independent"]["source"], "note": "multi-period profit over the horizon"}),
        ("npv_inr", mp["npv_inr"], {"source": prov["net_independent"]["source"], "note": "NPV at the request discount rate"}),
    ]
    for name, val, pr in rows:
        shown = "never (past the horizon)" if (name == "payback_months" and val is None) else inr(val) if isinstance(val, (int, float)) else str(val)
        L.append(f"| {name} | {shown} | {pr['source']} | {pr.get('note', '')} |")
    tags = {pr["source"] for _, _, pr in rows}
    for extra in ("adoption_pct", "readiness", "observed_adoption_pct", "observed_quality_pct"):
        tags.add(prov[extra]["source"])
        L.append(f"| {extra} | {prov[extra]['value'] if prov[extra]['value'] is not None else 'not recorded'} "
                 f"| {prov[extra]['source']} | {prov[extra].get('note', '')} |")
    if be_ad is not None:
        L.append(f"| breakeven_adoption_pct | {be_ad:.1f}% | calculated | adoption where net turns positive |")
    L.append("")
    L += ["## Evidence band and gaps",
          f"- Band: **{ev['evidence']['band']}** (observed weight {ev['evidence']['observed_weight']}/{ev['evidence']['total_weight']}"
          + ("; band capped by fixture (synthetic) values" if ev['evidence']['band_capped_by_fixture'] else "") + ")."]
    L += [f"- Gap ({g['impact']}): {g['input']} — {g['why']}" for g in ev["evidence"]["gaps"]] or ["- No gaps."]
    L += ["", "## Next actions"]
    L += [f"- **{a['code']}**: {a['action']} (owner hint: {a['owner_hint']}; expected test: {a['expected_test']})"
          for a in ev["actions"]] or ["- None: the pilot is on the fund path."]
    L += ["", "## 30-day plan"]
    if plan:
        L += [f"- Objective: {plan.get('objective', p.task)} (status: {plan.get('status', 'planned')}",
              f"- Owner: {plan.get('owner') or 'not assigned'}",
              f"- Adoption target: {plan.get('adoption_target_pct')}%; quality target: {plan.get('quality_target_pct')}%",
              f"- Cost limit: {inr(plan.get('cost_limit_inr', 0))} ({plan.get('cost_limit_note', '')})",
              f"- Duration: {plan.get('duration_days')} days; stop rule: {plan.get('stop_rule')}"]
    else:
        L += ["- No plan recorded yet — generate one at `POST /api/v1/plans`."]
    L += ["", "## Audit trail",
          f"- Pilot flags: {'; '.join(f['code'] for f in p.flags) or 'none'}.",
          f"- Provenance tags present: {'; '.join(sorted(t for t in tags if t))}.",
          f"- Checkpoint inputs: adoption {cp['inputs']['observed_adoption_pct']}, quality {cp['inputs']['observed_quality_pct']}, "
          f"duration {cp['inputs']['duration_days']} days, revised before: {cp['inputs']['revised_before']}.",
          f"- Tie-break order: {' > '.join(TIE_BREAK_ORDER)}.",
          "- Overrides and hypothesis rejections follow the policy; actuals come from observations, never invented."]
    return "\n".join(L) + "\n"
