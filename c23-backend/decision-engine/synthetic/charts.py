"""Comparison charts (matplotlib only, default colour cycle) for the synthetic validation report.

Every plotted value comes from the official fixture, the synthetic dataset, values recalculated by synthetic.rules,
or the advisory model outputs. Nothing is invented. Each chart function returns the arrays it plotted so the
validation step can prove they are finite and non-empty. PNGs carry no timestamps (reproducible bytes).
"""
from __future__ import annotations
import math
from collections import Counter
from fractions import Fraction
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt                                    # noqa: E402
from matplotlib.ticker import FuncFormatter, PercentFormatter      # noqa: E402

from synthetic import rules as R                                   # noqa: E402

SYNTH_FOOTNOTE = "SYNTHETIC test data (dummy records) - not real-world evidence."
DECISIONS = ("PROCEED", "REVISE", "STOP", "REVIEW")
COLOR = {d: f"C{i}" for i, d in enumerate(DECISIONS)}               # default matplotlib cycle, fixed per decision
INR = FuncFormatter(lambda v, _: "INR 0" if v == 0 else f"INR {v / 1e5:.1f}L")      # one unit everywhere: lakh (1L = 100,000)
PCT = PercentFormatter(xmax=100, decimals=0)


def _f(x) -> float:
    return float(x) if isinstance(x, Fraction) else float(x)


def _save(fig, path: Path, footnote: str = SYNTH_FOOTNOTE) -> None:
    fig.text(0.01, 0.005, footnote, fontsize=7, color="0.35", ha="left", va="bottom")
    fig.savefig(path, dpi=110, metadata={"Software": None})
    plt.close(fig)


# For the chart being drawn: one entry per axes, True when it got a title and both axis labels.
_labelled_figs: list = []


def _legend_below(ax, ncol=3):
    """Legend under the axes so it never covers data."""
    ax.legend(loc="upper center", bbox_to_anchor=(0.5, -0.13), ncol=ncol, fontsize=8, frameon=False)


def _labels(ax, title, xlabel, ylabel):
    ax.set_title(title, fontsize=10)
    ax.set_xlabel(xlabel)
    ax.set_ylabel(ylabel)
    _labelled_figs.append(bool(ax.get_title() and ax.get_xlabel() and ax.get_ylabel()))


# ---------------------------------------------------------------- 1
def original_vs_synthetic(fx_rows, syn, path):
    def avgs(rows):
        fins = [R.finance(p["setup_cost_inr"], p["annual_running_cost_inr"], p["annual_benefit_inr"]) for p in rows]
        m = lambda xs: sum(_f(x) for x in xs) / len(xs)
        return {"Setup cost": m([p["setup_cost_inr"] for p in rows]), "Annual running cost": m([p["annual_running_cost_inr"] for p in rows]),
                "Annual benefit": m([p["annual_benefit_inr"] for p in rows]), "Net annual benefit": m([f.net_annual for f in fins]),
                "Payback (months)": m([f.payback_months for f in fins if f.payback_months is not None]), "ROI (%)": m([f.roi_pct for f in fins])}
    o, s = avgs(fx_rows), avgs(syn)
    fig, axes = plt.subplots(1, 3, figsize=(13, 4.6), gridspec_kw={"width_ratios": [3, 1, 1]})
    money = ["Setup cost", "Annual running cost", "Annual benefit", "Net annual benefit"]
    xs = range(len(money))
    axes[0].bar([x - 0.2 for x in xs], [o[k] for k in money], 0.4, label=f"Original = official reference fixture (n={len(fx_rows)})")
    axes[0].bar([x + 0.2 for x in xs], [s[k] for k in money], 0.4, label=f"Synthetic = dummy comparison data (n={len(syn)})")
    axes[0].set_xticks(list(xs), money, fontsize=8)
    axes[0].yaxis.set_major_formatter(INR)
    _labels(axes[0], "Average INR per pilot", "Financial metric", "Average (INR)")
    axes[0].legend(fontsize=8)
    for ax, key, title, fmt in ((axes[1], "Payback (months)", "Average payback", None), (axes[2], "ROI (%)", "Average ROI", PCT)):
        ax.bar(["Original", "Synthetic"], [o[key], s[key]], color=["C0", "C1"])
        _labels(ax, title, "Dataset", key)
        if fmt:
            ax.yaxis.set_major_formatter(fmt)
    fig.suptitle("Original Fixture vs Synthetic Dataset — Financial Comparison\n(different populations: 5 official reference rows vs 120 dummy test rows)", fontsize=11)
    fig.tight_layout(rect=(0, 0.03, 1, 0.92))
    _save(fig, path)
    return {"values": list(o.values()) + list(s.values()), "axes": 3}


# ---------------------------------------------------------------- 2
def decision_distribution(syn, dv, path):
    series = {"Stored label": Counter(p["decision"] for p in syn),
              "Documented rules (label basis)": Counter(e.decision for e in dv["stated"].values()),
              "Production engine (cash basis)": Counter(e.decision for e in dv["cash"].values())}
    fig, axes = plt.subplots(1, 2, figsize=(13, 4.6))
    w = 0.27
    vals = []
    for j, (name, c) in enumerate(series.items()):
        counts = [c.get(d, 0) for d in DECISIONS]
        pct = [100 * v / len(syn) for v in counts]
        vals += counts + pct
        for ax, ys in ((axes[0], counts), (axes[1], pct)):
            bars = ax.bar([i + (j - 1) * w for i in range(len(DECISIONS))], ys, w, label=name)
            ax.bar_label(bars, labels=[f"{y:.0f}" if ax is axes[0] else f"{y:.0f}%" for y in ys], fontsize=7)
    for ax, ylab in ((axes[0], "Number of pilots"), (axes[1], "Share of pilots")):
        ax.set_xticks(range(len(DECISIONS)), DECISIONS)
        ax.set_ylim(0, ax.get_ylim()[1] * 1.08)
        _labels(ax, "Count" if ax is axes[0] else "Percentage view", "Decision", ylab)
        _legend_below(ax)
    axes[1].yaxis.set_major_formatter(PCT)
    fig.suptitle(f"Synthetic Pilot Decision Distribution (n={len(syn)})", fontsize=11)
    fig.tight_layout(rect=(0, 0.03, 1, 0.94))
    _save(fig, path)
    return {"values": vals, "axes": 2}


# ---------------------------------------------------------------- 3
def adoption(syn, dv, path):
    rows = sorted(syn, key=lambda p: (p["actual_adoption_pct"], p["record_id"]))
    x = list(range(1, len(rows) + 1))
    be = [_f(dv["stated"][p["record_id"]].breakeven_pct) for p in rows]
    exp, act, mn = ([p[k] for p in rows] for k in ("expected_adoption_pct", "actual_adoption_pct", "minimum_adoption_pct"))
    fig, ax = plt.subplots(figsize=(13, 5))
    ax.plot(x, exp, lw=1, label="Expected adoption")
    ax.plot(x, mn, lw=1, ls="--", label="Minimum (target) adoption")
    ax.plot(x, be, lw=1, ls=":", label="Break-even adoption (running / benefit)")
    state = ["Below break-even" if a < b else "Below target (above break-even)" if a < m else "Above target" for a, b, m in zip(act, be, mn)]
    for i, s in enumerate(("Above target", "Below target (above break-even)", "Below break-even")):
        pts = [(xi, a) for xi, a, st in zip(x, act, state) if st == s]
        ax.scatter([p[0] for p in pts], [p[1] for p in pts], s=18, marker="os^"[i], color=f"C{i + 3}", zorder=3, label=f"Actual adoption: {s} ({len(pts)})")
    ax.yaxis.set_major_formatter(PCT)
    _labels(ax, "Expected vs Actual Adoption Across Synthetic Pilots", "Pilots sorted by actual adoption (rank 1 = lowest)", "Adoption")
    _legend_below(ax)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    _save(fig, path)
    return {"values": exp + act + mn + be, "axes": 1}


# ---------------------------------------------------------------- 4
def quality(syn, path):
    rows = sorted(syn, key=lambda p: (p["actual_quality_pct"], p["record_id"]))
    x = list(range(1, len(rows) + 1))
    exp, act, mn = ([p[k] for p in rows] for k in ("expected_quality_pct", "actual_quality_pct", "minimum_quality_pct"))
    fig, ax = plt.subplots(figsize=(13, 5))
    ax.plot(x, exp, lw=1, label="Expected quality")
    ax.plot(x, mn, lw=1, ls="--", label="Minimum quality")
    fail = [(xi, a) for xi, a, m in zip(x, act, mn) if a < m]
    ok = [(xi, a) for xi, a, m in zip(x, act, mn) if a >= m]
    ax.scatter([p[0] for p in ok], [p[1] for p in ok], s=18, color="C2", zorder=3, label=f"Actual quality: pass ({len(ok)})")
    ax.scatter([p[0] for p in fail], [p[1] for p in fail], s=26, marker="x", color="C3", zorder=3, label=f"Actual quality: below minimum -> STOP ({len(fail)})")
    ax.yaxis.set_major_formatter(PCT)
    _labels(ax, "Expected vs Actual Quality Across Synthetic Pilots", "Pilots sorted by actual quality (rank 1 = lowest)", "Quality")
    _legend_below(ax, ncol=4)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    _save(fig, path)
    return {"values": exp + act + mn, "axes": 1}


# ---------------------------------------------------------------- 5
def readiness(syn, dv, path):
    groups = {"All pilots": syn, **{d: [p for p in syn if p["decision"] == d] for d in ("PROCEED", "REVISE", "STOP")}}
    measures = (("Data", "data_readiness_pct"), ("Technical", "technical_readiness_pct"), ("Process", "process_readiness_pct"), ("Overall (mean)", None))
    fig, ax = plt.subplots(figsize=(12, 4.8))
    w, vals = 0.2, []
    for j, (label, key) in enumerate(measures):
        ys = []
        for rows in groups.values():
            v = [_f(dv["stated"][p["record_id"]].readiness_pct) if key is None else p[key] for p in rows]
            ys.append(sum(v) / len(v))
        vals += ys
        bars = ax.bar([i + (j - 1.5) * w for i in range(len(groups))], ys, w, label=label)
        ax.bar_label(bars, labels=[f"{y:.0f}" for y in ys], fontsize=7)
    ax.axhline(50, color="0.3", ls="--", lw=1, label="Low-readiness threshold (overall < 50, TEAM rule)")
    ax.set_xticks(range(len(groups)), [f"{g} (n={len(r)})" for g, r in groups.items()])
    ax.yaxis.set_major_formatter(PCT)
    _labels(ax, "Readiness by Decision Outcome (stored decision labels)", "Pilot group", "Average readiness")
    _legend_below(ax, ncol=5)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    _save(fig, path)
    return {"values": vals, "axes": 1}


# ---------------------------------------------------------------- 6
def roi_vs_payback(syn, dv, path):
    fig, ax = plt.subplots(figsize=(11, 5.6))
    vals = []
    for d, marker in (("PROCEED", "o"), ("REVISE", "s"), ("STOP", "x")):
        pts = [(_f(dv["stated"][p["record_id"]].stated.payback_months), _f(dv["stated"][p["record_id"]].stated.roi_pct)) for p in syn if p["decision"] == d]
        vals += [v for pt in pts for v in pt]
        ax.scatter([p[0] for p in pts], [p[1] for p in pts], s=26, marker=marker, color=COLOR[d], label=f"{d} (stored label, n={len(pts)})", alpha=0.75)
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.xaxis.set_major_formatter(FuncFormatter(lambda v, _: f"{v:g}"))
    ax.yaxis.set_major_formatter(FuncFormatter(lambda v, _: f"{v:g}%"))
    _labels(ax, "ROI vs Payback Across Synthetic Pilots (recalculated, stated benefit; log-log)", "Simple payback (months, log scale)", "Simple annual ROI (log scale)")
    ax.text(0.98, 0.95, "ROI x payback = 1200 for every pilot\n(both are set by net / setup), so points lie on one curve;\n"
            "short payback = high ROI. Decisions are spread along it:\nquality, adoption and readiness gates decide, not position.",
            transform=ax.transAxes, ha="right", va="top", fontsize=8, color="0.25")
    _legend_below(ax)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    _save(fig, path)
    return {"values": vals, "axes": 1}


# ---------------------------------------------------------------- 7
def cash_vs_capacity(syn, path):
    types = sorted({p["benefit_type"] for p in syn}, key=lambda t: (t not in ("time freed", "capacity value"), t))
    series = (("Stated annual benefit", "annual_benefit_inr"), ("Capacity value", "capacity_value_inr"), ("Realised cash saving", "realized_cash_saving_inr"))
    fig, ax = plt.subplots(figsize=(12, 5))
    w, vals = 0.27, []
    for j, (label, key) in enumerate(series):
        ys = [sum(p[key] for p in syn if p["benefit_type"] == t) for t in types]
        vals += ys
        ax.bar([i + (j - 1) * w for i in range(len(types))], ys, w, label=label)
    ax.set_xticks(range(len(types)), [f"{t}\n(n={sum(p['benefit_type'] == t for p in syn)}; " + ("counted as cash" if R.canonical_benefit_type(t) == "cashable" else "NOT cash") + ")" for t in types], fontsize=8)
    ax.yaxis.set_major_formatter(INR)
    _labels(ax, "Capacity Value vs Realized Cash Saving — time freed is not automatically cash saved", "Benefit type (totals across synthetic pilots)", "Total per year (INR)")
    ax.legend(fontsize=8)
    fig.tight_layout(rect=(0, 0.03, 1, 1))
    _save(fig, path)
    return {"values": vals, "axes": 1}


# ---------------------------------------------------------------- 8
STRESS_CASES = (("Base", 1, 1), ("Benefit -10%", Fraction(9, 10), 1), ("Benefit -30%", Fraction(7, 10), 1),
                ("Running +20%", 1, Fraction(6, 5)), ("Running +50%", 1, Fraction(3, 2)), ("Running +100%", 1, 2))


def stress(fx_rows, path):
    p = fx_rows[0]
    rows = []
    for name, bf, rf in STRESS_CASES:
        fin = R.finance(p["setup_cost_inr"], R.frac(p["annual_running_cost_inr"]) * rf, R.frac(p["annual_benefit_inr"]) * bf)
        rows.append((name, _f(fin.payback_months), _f(fin.net_annual), _f(fin.roi_pct)))
    fig, axes = plt.subplots(1, 3, figsize=(14, 4.6))
    names = [r[0] for r in rows]
    for ax, idx, title, ylab, fmt in ((axes[0], 1, "Base vs Stress — Payback", "Simple payback (months)", None),
                                      (axes[1], 2, "Base vs Stress — Net Annual Benefit", "Net annual benefit (INR)", INR),
                                      (axes[2], 3, "Base vs Stress — ROI", "Simple annual ROI", PCT)):
        ys = [r[idx] for r in rows]
        bars = ax.bar(names, ys, color=["C0"] + ["C1"] * (len(rows) - 1))
        ax.bar_label(bars, labels=[f"{y:.1f}" if idx == 1 else (f"{y / 1e5:.2f}L" if idx == 2 else f"{y:.0f}%") for y in ys], fontsize=7)
        ax.tick_params(axis="x", labelrotation=30, labelsize=8)
        _labels(ax, title, "Scenario", ylab)
        if fmt:
            ax.yaxis.set_major_formatter(fmt)
    fig.suptitle(f"Official reference case {p['id']} (setup 200000, running 60000, benefit 180000) under stress — Running +100% = 40-month changed-input case", fontsize=10)
    fig.tight_layout(rect=(0, 0.03, 1, 0.93))
    _save(fig, path, "Official C23 reference fixture inputs (synthetic teaching fixture) with illustrative scenario multipliers - not a forecast.")
    return {"values": [v for r in rows for v in r[1:]], "axes": 3, "rows": rows}


# ---------------------------------------------------------------- 9
def portfolio(results, path):
    rows = [r for r in results if r["mode"].startswith("production")]
    budgets = [r["budget_inr"] for r in rows]
    setup = [float(r["total_setup_inr"]) for r in rows]
    remain = [float(r["remaining_budget_inr"]) for r in rows]
    net = [float(r["annual_net_benefit_inr"]) for r in rows]
    n = [r["selected_count"] for r in rows]
    labels = [f"INR {b / 1e5:.0f}L" for b in budgets]
    fig, axes = plt.subplots(1, 2, figsize=(13, 4.8))
    xs = list(range(len(labels)))
    axes[0].bar(xs, setup, label="Selected setup cost")
    axes[0].bar(xs, remain, bottom=setup, label="Remaining budget")
    axes[0].hlines(budgets, [x - 0.45 for x in xs], [x + 0.45 for x in xs], colors="0.2", linestyles="--", label="Budget (setup must stay at or below)")
    axes[0].set_xticks(xs, labels)
    axes[0].yaxis.set_major_formatter(INR)
    _labels(axes[0], "Setup cost vs budget", "Budget", "INR")
    _legend_below(axes[0])
    bars = axes[1].bar(labels, net, color="C2", label="Recognised annual net benefit (cash basis)")
    axes[1].bar_label(bars, labels=[f"{k} pilot{'s' if k != 1 else ''}" for k in n], fontsize=8)
    axes[1].yaxis.set_major_formatter(INR)
    _labels(axes[1], "Recognised annual net benefit and number of selected pilots", "Budget", "INR per year")
    axes[1].set_ylim(0, max(net) * 1.12)
    _legend_below(axes[1])
    fig.suptitle("Portfolio Performance by Budget (production rules: cash basis, no STOP/REVISE, one pilot per overlap group)", fontsize=11)
    fig.tight_layout(rect=(0, 0.03, 1, 0.94))
    _save(fig, path)
    return {"values": setup + remain + net + n, "axes": 2}


# ---------------------------------------------------------------- 10
def ml_vs_rules(advisory, path):
    decs = [d for d in ("PROCEED", "REVISE", "STOP") if any(a["deterministic_decision"] == d for a in advisory)]
    data = [[a["adoption_success_probability"] for a in advisory if a["deterministic_decision"] == d] for d in decs]
    fig, axes = plt.subplots(1, 2, figsize=(13, 4.8))
    axes[0].boxplot(data, tick_labels=[f"{d} (n={len(v)})" for d, v in zip(decs, data)])
    axes[0].set_ylim(0, 1.05)
    _labels(axes[0], "Out-of-fold adoption-success probability by decision", "Deterministic decision (production engine)", "Adoption success probability")
    bands = ("LOW", "MEDIUM", "HIGH")
    vals = [v for d in data for v in d]
    w = 0.27
    for j, b in enumerate(bands):
        ys = [sum(a["deterministic_decision"] == d and a["adoption_risk_band"] == b for a in advisory) for d in decs]
        vals += ys
        bars = axes[1].bar([i + (j - 1) * w for i in range(len(decs))], ys, w, label=f"{b} adoption risk")
        axes[1].bar_label(bars, fontsize=7)
    axes[1].set_xticks(range(len(decs)), decs)
    _labels(axes[1], "Risk-band count by decision", "Deterministic decision (production engine)", "Number of pilots")
    axes[1].legend(fontsize=8)
    fig.suptitle("ML Adoption Risk vs Deterministic Decision — ML is advisory only; final investment decisions are deterministic", fontsize=11)
    fig.tight_layout(rect=(0, 0.03, 1, 0.94))
    _save(fig, path)
    return {"values": vals, "axes": 2}


CHARTS = (
    ("original_vs_synthetic_financials.png", "Original vs Synthetic Financials"),
    ("decision_distribution.png", "Decision Distribution"),
    ("adoption_comparison.png", "Adoption Comparison"),
    ("quality_comparison.png", "Quality Comparison"),
    ("readiness_comparison.png", "Readiness Comparison"),
    ("roi_vs_payback.png", "ROI vs Payback"),
    ("cash_vs_capacity.png", "Cash vs Capacity"),
    ("stress_test_comparison.png", "Stress Test Comparison"),
    ("portfolio_budget_comparison.png", "Portfolio Budget Comparison"),
    ("ml_vs_rule_comparison.png", "ML vs Rules"),
)

README = {
    "original_vs_synthetic_financials.png": ("Original fixture (5 rows) and synthetic CSV (120 rows); net, payback and ROI recalculated from inputs.",
        "Average setup, running cost, benefit, net benefit, payback and ROI for each dataset.",
        "The synthetic set is costlier and more varied than the five identical fixture rows; on average it pays back in a similar time.",
        "Different populations (official reference vs dummy test data); averages are not comparable as market evidence."),
    "decision_distribution.png": ("Stored synthetic labels; decisions recalculated with the documented rules on the label basis and by the production engine (cash basis).",
        "PROCEED / REVISE / STOP / REVIEW counts and shares for the three decision sources.",
        "The production engine is stricter than the labels because non-cash benefit is not counted as cash.",
        "Synthetic labels come from a generator; the distribution says nothing about real pilot success rates."),
    "adoption_comparison.png": ("Synthetic expected / actual / minimum adoption; break-even adoption recalculated (running / benefit x 100).",
        "All 120 pilots sorted by actual adoption, with each pilot's actual value marked by state.",
        "Pilots below break-even lose money at their adoption (STOP); below target but above break-even need revising (REVISE).",
        "Dummy adoption values; no real usage data."),
    "quality_comparison.png": ("Synthetic expected / actual / minimum quality.",
        "All 120 pilots sorted by actual quality; failures (actual < minimum) marked with x.",
        "Every quality failure is a STOP regardless of the financial case.",
        "Dummy quality values; no real evaluation data."),
    "readiness_comparison.png": ("Synthetic data / technical / process readiness; overall = mean of the three (recalculated).",
        "Average readiness for all pilots and per stored decision, with the 50% low-readiness threshold.",
        "Readiness barely separates the classes in this dataset; low readiness means fix readiness first, not a failed idea.",
        "The 50% threshold is a TEAM assumption; synthetic values only."),
    "roi_vs_payback.png": ("Synthetic inputs; ROI and payback recalculated on the stated benefit; colour = stored decision.",
        "Relationship between payback duration and simple annual ROI, one point per pilot.",
        "ROI x payback = 1200 exactly for every pilot (ROI = 100 x net / setup, payback = 12 x setup / net), so all points lie on one curve: "
        "shorter payback always means higher ROI. Stored decisions are spread along the curve because quality, adoption and readiness gates decide, not finance alone.",
        "Synthetic test data only. This is not real market performance evidence."),
    "cash_vs_capacity.png": ("Synthetic annual_benefit_inr, capacity_value_inr and realized_cash_saving_inr, totalled by benefit type.",
        "Stated benefit vs capacity value vs realised cash saving for each benefit type.",
        "For time freed and capacity value only a fraction is realised as cash; the engine counts only that fraction.",
        "Totals of dummy values; the realised share is generator output, not observed savings."),
    "stress_test_comparison.png": ("Official fixture AI1 inputs with the benefit / running-cost multipliers applied (recalculated exactly).",
        "Payback, net annual benefit and ROI for the base case and five stress scenarios.",
        "Doubling the running cost halves the net benefit and doubles payback to 40 months (the official changed-input check).",
        "Single reference pilot; multipliers are illustrative scenarios, not forecasts."),
    "portfolio_budget_comparison.png": ("Exact optimizer results on the 120 synthetic pilots (production rules, cash basis) for INR 2L / 4L / 6L / 8L.",
        "Selected setup vs remaining budget, recognised annual net benefit and number of pilots per budget.",
        "Setup never exceeds the budget; net benefit rises with budget with diminishing additions.",
        "Portfolio of dummy pilots; benefits are assumptions."),
    "ml_vs_rule_comparison.png": ("Out-of-fold probabilities from the advisory adoption-success model; production engine decisions.",
        "Probability distribution and risk-band counts per deterministic decision.",
        "Highlights pilots whose adoption may need validation; the rule decision never changes.",
        "ML is advisory only and trained/evaluated on synthetic data; final investment decisions are deterministic."),
}


def validate(path: Path, info: dict) -> dict:
    """File exists, opens, is non-empty and not blank; all plotted values finite; axes labelled (checked at plot time)."""
    from PIL import Image
    res = {"chart": path.name, "exists": path.exists()}
    try:
        with Image.open(path) as im:
            im.load()
            res["size"] = list(im.size)
            extrema = im.convert("L").getextrema()
            res["opens"], res["not_blank"] = True, extrema[0] != extrema[1]
    except Exception as e:                                          # pragma: no cover - reported, not raised
        res["opens"], res["not_blank"], res["error"] = False, False, str(e)
    vals = [float(v) for v in info.get("values", [])]
    res["points_plotted"] = len(vals)
    res["all_finite"] = bool(vals) and all(math.isfinite(v) for v in vals)
    res["axes_labelled"] = info.get("labelled", False)
    res["status"] = "PASS" if res["exists"] and res["opens"] and res["not_blank"] and res["all_finite"] and res["axes_labelled"] else "FAIL"
    return res


def generate(out_dir: Path, ctx: dict) -> list:
    out_dir.mkdir(parents=True, exist_ok=True)
    jobs = {
        "original_vs_synthetic_financials.png": lambda p: original_vs_synthetic(ctx["fx_rows"], ctx["syn"], p),
        "decision_distribution.png": lambda p: decision_distribution(ctx["syn"], ctx["dv"], p),
        "adoption_comparison.png": lambda p: adoption(ctx["syn"], ctx["dv"], p),
        "quality_comparison.png": lambda p: quality(ctx["syn"], p),
        "readiness_comparison.png": lambda p: readiness(ctx["syn"], ctx["dv"], p),
        "roi_vs_payback.png": lambda p: roi_vs_payback(ctx["syn"], ctx["dv"], p),
        "cash_vs_capacity.png": lambda p: cash_vs_capacity(ctx["syn"], p),
        "stress_test_comparison.png": lambda p: stress(ctx["fx_rows"], p),
        "portfolio_budget_comparison.png": lambda p: portfolio(ctx["portfolio_results"], p),
        "ml_vs_rule_comparison.png": (lambda p: ml_vs_rules(ctx["advisory"], p)) if ctx.get("advisory") else None,
    }
    results = []
    for name, label in CHARTS:
        path = out_dir / name
        fn = jobs[name]
        if fn is None:
            results.append({"chart": name, "label": label, "status": "FAIL", "error": "advisory model output not available"})
            continue
        _labelled_figs.clear()
        info = fn(path)
        info["labelled"] = bool(_labelled_figs) and all(_labelled_figs) and len(_labelled_figs) == info["axes"]
        results.append({"label": label, **validate(path, info)})
    write_readme(out_dir, results)
    return results


def write_readme(out_dir: Path, results: list) -> None:
    L = ["# Synthetic validation charts", "",
         "Generated by `python -m synthetic.run` (matplotlib, default colour cycle). Every value comes from the official fixture, the",
         "synthetic dataset, exact recalculations, or the advisory model outputs. **All charts except the reference-case stress chart show",
         "SYNTHETIC test data, which is not real-world evidence.**", ""]
    status = {r["chart"]: r["status"] for r in results}
    for name, label in CHARTS:
        data, shows, interp, limit = README[name]
        L += [f"## {label}", "", f"![{label}]({name})", "", f"- **File:** `{name}` ({status.get(name, 'NOT GENERATED')})",
              f"- **Data used:** {data}", f"- **Shows:** {shows}", f"- **Interpretation:** {interp}", f"- **Limitation:** {limit}", ""]
    (out_dir / "README.md").write_text("\n".join(L), encoding="utf-8")
