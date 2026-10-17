"""CSV + evidence markdown. Keeps observed (fixture) and assumed (team) separate."""
import csv, io
from .core import view, evaluate_stop_rule, scenarios, breakeven_adoption_pct, TIE_BREAK_ORDER

def pilots_csv(pilots: list) -> str:
    buf = io.StringIO(); w = csv.writer(buf)
    w.writerow(["id", "task", "group", "setup_inr", "run_inr", "benefit_inr", "net_independent", "payback_mo_independent",
                "net_conservative", "payback_mo_conservative", "adoption_pct", "readiness", "flags"])
    for p in pilots:
        a, b = view(p, "independent"), view(p, "conservative")
        w.writerow([p.id, p.task, p.group, p.setup, p.run, p.benefit, a["net"],
                    "" if a["payback_months"] is None else round(a["payback_months"], 1),
                    round(b["net"]), "" if b["payback_months"] is None else round(b["payback_months"], 1),
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
    L += ["", "\\* team-authored assumption, not in the fixture.", "", "## Stop-rule status"]
    for p in pilots:
        s = evaluate_stop_rule(p, defaults); L.append(f"- {p.id}: **{s['decision']}**: {s['reasons'][0]}")
    L += ["", "## What-if scenarios (conservative mode)"]
    for p in pilots:
        L.append(f"- {p.id}: " + "; ".join(f"{s['scenario']} adoption {s['adoption_pct']}% -> net {s['net_annual']}, payback "
                                              f"{'never' if s['payback_months'] is None else str(s['payback_months']) + ' mo'}" for s in scenarios(p)))
    if invalid: L += ["", "## Invalid inputs (excluded)"] + [f"- {i['id']}: {'; '.join(i['errors'])}" for i in invalid]
    if warnings: L += ["", "## Warnings"] + [f"- {w}" for w in warnings]
    return "\n".join(L) + "\n"
