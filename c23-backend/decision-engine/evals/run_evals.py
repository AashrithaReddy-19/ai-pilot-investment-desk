"""AI evals for the advisory layer. Deterministic engine outputs (from the Go engine) are the ground truth.

Usage (from decision-engine/):  python -m evals.run_evals
Writes reports/ai_evals.json and reports/ai_evals.md; exit code 1 if any gate fails.

Suites
  semantic_overlap      precision/recall/F1 on team-authored dev + holdout pairs (gates fixed before the first run)
  semantic_properties   determinism, symmetry, bounded scores, official AI4/AI5 detected, fallback when text missing
  explanation_grounding every engine evaluation -> explanation must pass the grounding validator and state the decision
  hallucination         adversarial edits (wrong numbers, invented pilot IDs, wrong decision, invented codes, ROI claims)
                        must ALL be rejected; faithful paraphrases (controls) must ALL be accepted
  consistency           same input -> byte-identical output
All figures are SYNTHETIC EVAL results on small team-authored sets, not real-world model performance.
"""
from __future__ import annotations
import copy, json, re, sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from app.ai import explain, grounding, semantic  # noqa: E402

TRAINING_STATUS = ("Predictive training not used because the official fixture is too small and synthetic. AI value is delivered "
                   "through advisory enrichment and deterministic decision support. No model artifacts exist in the repository.")
EVALS = ROOT / "evals"
REPORTS = ROOT / "reports"
GATES = {"holdout_precision_min": 0.8, "holdout_recall_min": 0.7, "grounding_pass_rate": 1.0,
         "hallucination_rejection_rate": 1.0, "control_acceptance_rate": 1.0, "consistency": 1.0}


def metrics(rows: list[dict]) -> dict:
    tp = sum(r["pred"] and r["label"] for r in rows)
    fp = sum(r["pred"] and not r["label"] for r in rows)
    fn = sum(not r["pred"] and r["label"] for r in rows)
    tn = sum(not r["pred"] and not r["label"] for r in rows)
    p = tp / (tp + fp) if tp + fp else 0.0
    r = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * p * r / (p + r) if p + r else 0.0
    return {"n": len(rows), "precision": round(p, 3), "recall": round(r, 3), "f1": round(f1, 3),
            "accuracy": round((tp + tn) / len(rows), 3) if rows else 0.0,
            "confusion": {"tp": tp, "fp": fp, "fn": fn, "tn": tn},
            "errors": [{"a": x["a"], "b": x["b"], "label": x["label"], "similarity": x["sim"]} for x in rows if x["pred"] != x["label"]]}


def eval_semantic() -> dict:
    cases = json.loads((EVALS / "semantic_overlap_cases.json").read_text(encoding="utf-8"))
    out = {}
    for split in ("dev", "holdout"):
        rows = []
        for c in cases[split]:
            r = semantic.compare(c["a"], c["b"])
            rows.append({**c, "pred": r["possible_overlap"], "sim": r["semantic_similarity"]})
        out[split] = metrics(rows)
    out["gate_pass"] = (out["holdout"]["precision"] >= GATES["holdout_precision_min"]
                        and out["holdout"]["recall"] >= GATES["holdout_recall_min"])
    return out


def eval_semantic_properties(evals: dict) -> dict:
    texts = {"AI1": "Invoice field extraction. Extract invoice fields into the finance ledger instead of manual typing.",
             "AI2": "Order exception triage. Flag and route exceptional orders to the right owner.",
             "AI3": "Weekly catalogue review. Draft the weekly catalogue quality review.",
             "AI4": "Support reply drafting. Draft first replies for support agents to edit.",
             "AI5": "Ticket tagging. Auto-tag incoming support tickets by topic."}
    groups = {"AI1": "finance", "AI2": "ops", "AI3": "catalog", "AI4": "support", "AI5": "support"}
    pilots = [{"id": k, "text": v, "overlap_group": groups[k]} for k, v in texts.items()]
    pairs = semantic.compare_all(pilots)
    flagged = sorted((p["pilot_a"], p["pilot_b"]) for p in pairs if p["possible_overlap"])
    sym = all(semantic.compare(a, b) == semantic.compare(b, a) for a in texts.values() for b in texts.values())
    bounded = all(0.0 <= p["semantic_similarity"] <= 1.0 for p in pairs)
    det = semantic.compare_all(pilots) == pairs and semantic.compare_all(list(reversed(pilots))) == pairs
    fb_same = semantic.compare("", "x", "support", "support")
    fb_diff = semantic.compare("  ", "x", "support", "ops")
    fb_unknown = semantic.compare("", "", None, None)
    fallback = (fb_same["method"] == "overlap_group_fallback" and fb_same["possible_overlap"] and not fb_diff["possible_overlap"]
                and not fb_unknown["possible_overlap"])
    checks = {"official_flags_only_AI4_AI5": flagged == [("AI4", "AI5")], "symmetric": sym, "bounded_0_1": bounded,
              "deterministic_and_order_independent": det, "fallback_to_overlap_group": fallback}
    return {"checks": checks, "official_pairs_flagged": flagged, "gate_pass": all(checks.values())}


def eval_grounding(evals: dict) -> dict:
    rows = []
    for name, ev in sorted(evals.items()):
        r = explain.explain_checked(ev)
        mentions = ev["decision"] in r["explanation"] and ev["pilot_id"] in r["explanation"]
        rows.append({"case": name, "decision": ev["decision"], "grounded": r["grounded"], "mentions_decision": mentions,
                     "failures": r["failures"]})
    ok = sum(r["grounded"] and r["mentions_decision"] for r in rows)
    return {"n": len(rows), "pass_rate": round(ok / len(rows), 3), "rows": rows, "gate_pass": ok == len(rows)}


ADVERSARIAL = [
    ("wrong_number", lambda t, ev: t.replace("in 20 months", "in 18 months") if "in 20 months" in t else t + " It pays back in 7 months."),
    ("invented_pilot", lambda t, ev: t + " It also outperforms AI9."),
    ("wrong_decision", lambda t, ev: t.replace(f"decision {ev['decision']}", "decision " + ("PROCEED" if ev["decision"] != "PROCEED" else "STOP"))),
    ("extra_decision_word", lambda t, ev: t + (" Otherwise STOP." if ev["decision"] != "STOP" else " Otherwise PROCEED.")),
    ("roi_claim", lambda t, ev: t + " This is proven enterprise ROI."),
    ("guarantee", lambda t, ev: t + " Savings are guaranteed."),
    ("certainty", lambda t, ev: t + " The pilot will definitely succeed."),
    ("invented_code", lambda t, ev: t + " Flag: BUDGET_PRE_APPROVED."),
    ("invented_metric", lambda t, ev: t + " It frees 45 hours per week."),
    ("market_claim", lambda t, ev: t + " Demand is a validated market fact."),
]


def controls(ev: dict) -> list[str]:
    pid, d = ev["pilot_id"], ev["decision"]
    s = (ev.get("calculated_metrics") or {}).get("stated_basis") or {}
    out = [f"{pid}: decision {d} ({ev['primary_reason']}).", f"Engine result for {pid} is {d}; results are not proven returns."]
    if s.get("payback_months") is not None:
        out.append(f"{pid} is {d}. Stated payback is {s['payback_months']} months on INR {s['net_annual_benefit_inr']} net per year.")
    return out


def eval_hallucination(evals: dict) -> dict:
    rows, ctrl = [], []
    for name, ev in sorted(evals.items()):
        base = explain.explain(ev)
        for kind, mutate in ADVERSARIAL:
            bad = mutate(base, ev)
            if bad == base:
                continue
            g = grounding.validate(bad, ev)
            rows.append({"case": name, "attack": kind, "rejected": not g["grounded"], "failures": g["failures"]})
        for c in controls(ev):
            ctrl.append({"case": name, "text": c, "accepted": grounding.validate(c, ev)["grounded"]})
    rej = sum(r["rejected"] for r in rows)
    acc = sum(c["accepted"] for c in ctrl)
    by_attack = {}
    for r in rows:
        a = by_attack.setdefault(r["attack"], {"n": 0, "rejected": 0})
        a["n"] += 1
        a["rejected"] += r["rejected"]
    return {"adversarial_n": len(rows), "rejection_rate": round(rej / len(rows), 3), "by_attack": by_attack,
            "missed": [r for r in rows if not r["rejected"]],
            "controls_n": len(ctrl), "control_acceptance_rate": round(acc / len(ctrl), 3),
            "false_rejections": [c for c in ctrl if not c["accepted"]],
            "gate_pass": rej == len(rows) and acc == len(ctrl)}


REQUIRED_SEMANTIC = [
    ("Support reply drafting", "Customer response copilot", "likely"),
    ("Invoice extraction", "Employee onboarding", "unlikely"),
    ("Ticket classification", "Support knowledge recommendation", "possible"),
]


def eval_required_cases(evals: dict) -> dict:
    """Cases named in the product brief, plus explanation-consistency requirements."""
    rows = []
    for a, b, want in REQUIRED_SEMANTIC:
        r = semantic.compare(a, b)
        rows.append({"check": f"semantic band: {a} vs {b}", "expected": want, "actual": r["band"], "pass": r["band"] == want})
    for name, ev in sorted(evals.items()):
        text = explain.explain(ev)
        g = grounding.validate(text, ev)
        if "LOW_ADOPTION" in ev["reason_codes"]:
            ok = "adoption" in text.lower() and g["grounded"]
            rows.append({"check": f"{name}: LOW_ADOPTION explanation mentions adoption and is grounded", "expected": True, "actual": ok, "pass": ok})
        if ev["decision"] == "STOP":
            ok = "STOP" in text and "PROCEED" not in text and "proceed" not in text.lower()
            rows.append({"check": f"{name}: STOP never explained as PROCEED", "expected": True, "actual": ok, "pass": ok})
        # thresholds/costs in the text must come from the evaluation (grounding numbers + units checks)
        ok = g["grounded"]
        rows.append({"check": f"{name}: no invented thresholds, costs or evidence", "expected": True, "actual": ok, "pass": ok})
    return {"n": len(rows), "rows": rows, "gate_pass": all(r["pass"] for r in rows)}


def eval_robustness() -> dict:
    """Out-of-distribution / missing / extreme inputs: no crash, bounded, deterministic, safe fallback."""
    cases = [("", "anything", None, None), ("x" * 2000, "y" * 2000, None, None), ("🚀🚀🚀", "📈", None, None),
             ("12345 67890", "12345", None, None), ("सहायता टिकट", "ग्राहक उत्तर", None, None),
             ("support", "SUPPORT!!!", None, None), ("   ", "", "g", "g"), ("unknown words zzqx", "qqq vvv", None, None)]
    rows = []
    for a, b, ga, gb in cases:
        try:
            r1, r2 = semantic.compare(a, b, ga, gb), semantic.compare(a, b, ga, gb)
            ok = 0.0 <= r1["semantic_similarity"] <= 1.0 and r1 == r2 and r1["band"] in ("likely", "possible", "unlikely")
            rows.append({"input": [a[:20], b[:20]], "band": r1["band"], "pass": ok})
        except Exception as e:  # noqa: BLE001 - an exception IS the failure being measured
            rows.append({"input": [a[:20], b[:20]], "error": repr(e), "pass": False})
    ev = {"pilot_id": "X1", "decision": "REVISE", "primary_reason": "MISSING_RUNNING_COST", "reason_codes": ["MISSING_RUNNING_COST"],
          "failed_rules": [{"code": "MISSING_RUNNING_COST"}, {"code": "SOMETHING_NEW"}], "warnings": [], "calculated_metrics": {}}
    try:
        r = explain.explain_checked(ev)
        rows.append({"input": "evaluation with unknown rule code and no metrics", "band": "n/a", "pass": r["grounded"] and "REVISE" in r["explanation"]})
    except Exception as e:  # noqa: BLE001
        rows.append({"input": "evaluation with unknown rule code", "error": repr(e), "pass": False})
    return {"n": len(rows), "rows": rows, "gate_pass": all(r["pass"] for r in rows)}


# Category cases written as human judgement BEFORE scoring. binary = is it really the same benefit pool?
# ok_bands = advisory answers that are acceptable (never false certainty).
CATEGORY_CASES = [
    ("clear overlap", "Support reply drafting", "Customer response copilot", True, {"likely"}),
    ("same benefit, different wording", "Answer customer emails", "Draft replies to helpdesk enquiries", True, {"likely", "possible"}),
    ("clear non-overlap", "Invoice extraction", "Employee onboarding", False, {"unlikely"}),
    ("same department, different benefit", "Invoice data extraction", "Vendor payment fraud detection", False, {"unlikely", "possible"}),
    ("same department, different benefit", "Draft support replies", "Forecast support staffing levels", False, {"unlikely", "possible"}),
    ("ambiguous", "Ticket classification", "Support knowledge recommendation", None, {"possible"}),
    ("unknown wording", "Quantum flux harmonizer", "Blockchain synergy portal", False, {"unlikely"}),
]


def eval_categories() -> dict:
    rows = []
    for cat, a, b, truth, ok in CATEGORY_CASES:
        r = semantic.compare(a, b)
        binary = None if truth is None else ("TP" if truth and r["possible_overlap"] else "FN" if truth else
                                             "FP" if r["possible_overlap"] else "TN")
        rows.append({"category": cat, "a": a, "b": b, "band": r["band"], "similarity": r["semantic_similarity"],
                     "acceptable_bands": sorted(ok), "binary_outcome": binary, "pass": r["band"] in ok})
    return {"n": len(rows), "rows": rows, "gate_pass": all(r["pass"] for r in rows),
            "binary_false_positives": [r for r in rows if r["binary_outcome"] == "FP"]}


def _explanation_metrics(evals: dict) -> dict:
    """Per-explanation agreement checks against the deterministic engine output."""
    n = len(evals)
    grounded = decision = reason = finance = unsupported = contradiction = 0
    for ev in evals.values():
        text = explain.explain(ev)
        g = grounding.validate(text, ev)
        grounded += g["grounded"]
        words = {d for d in grounding.DECISIONS if re.search(rf"\b{d}\b", text)}
        decision += words == {ev["decision"]}
        contradiction += bool(words - {ev["decision"]})
        unsupported += any(f.startswith("claims") or f.startswith("units") for f in g["failures"])
        # every failed rule that has a plain-language template must be represented in the text
        needed = [explain._RULE_TEXT[f["code"]].split("{")[0].strip()[:30] for f in ev.get("failed_rules") or [] if f["code"] in explain._RULE_TEXT]
        reason += all(n_ in text for n_ in needed)
        s = (ev.get("calculated_metrics") or {}).get("stated_basis") or {}
        if s.get("payback_months") is not None:
            want = [explain._fmt(s.get("net_annual_benefit_inr")), explain._fmt(s.get("monthly_net_benefit_inr")), explain._fmt(s.get("payback_months"))]
            finance += all(w in text for w in want) and not any(f.startswith("numbers") for f in g["failures"])
        else:
            finance += not any(f.startswith("numbers") for f in g["failures"]) and "pays back in" not in text
    pct = lambda k: round(100 * k / n, 1) if n else 0.0
    return {"n": n, "grounding_pct": pct(grounded), "decision_agreement_pct": pct(decision), "reason_code_agreement_pct": pct(reason),
            "financial_number_agreement_pct": pct(finance), "unsupported_claim_rate_pct": pct(unsupported),
            "contradiction_rate_pct": pct(contradiction)}


def scorecard(suites: dict, evals: dict) -> dict:
    so, h, rc, rb, cat = suites["semantic_overlap"], suites["hallucination"], suites["required_cases"], suites["robustness"], suites["categories"]
    em = _explanation_metrics(evals)
    labelled = so["dev"]["n"] + so["holdout"]["n"]
    labelled_ok = round(so["dev"]["accuracy"] * so["dev"]["n"]) + round(so["holdout"]["accuracy"] * so["holdout"]["n"])
    fallback_rows = [r for r in rb["rows"]] + [{"pass": suites["semantic_properties"]["checks"]["fallback_to_overlap_group"]}]
    amb = [r for r in cat["rows"] if r["category"] == "ambiguous"]
    rows = [
        ("Semantic overlap (labelled pairs, binary)", labelled_ok, labelled),
        ("Semantic categories (no false certainty)", sum(r["pass"] for r in cat["rows"]), cat["n"]),
        ("Ambiguous case handling", sum(r["pass"] for r in amb), len(amb)),
        ("Grounding (generated explanations)", round(em["grounding_pct"] * em["n"] / 100), em["n"]),
        ("Decision consistency (explanation = engine decision)", round(em["decision_agreement_pct"] * em["n"] / 100), em["n"]),
        ("Reason-code agreement", round(em["reason_code_agreement_pct"] * em["n"] / 100), em["n"]),
        ("Financial number fidelity", round(em["financial_number_agreement_pct"] * em["n"] / 100), em["n"]),
        ("Hallucination resistance (attacks rejected)", h["adversarial_n"] - len(h["missed"]), h["adversarial_n"]),
        ("Faithful paraphrases accepted (controls)", h["controls_n"] - len(h["false_rejections"]), h["controls_n"]),
        ("Required brief cases", sum(r["pass"] for r in rc["rows"]), rc["n"]),
        ("Fallback / robustness", sum(r["pass"] for r in fallback_rows), len(fallback_rows)),
        ("Consistency (identical repeated output)", int(suites["consistency"]["identical_outputs"]), 1),
    ]
    table = [{"category": c, "passed": p, "total": t, "pass_rate_pct": round(100 * p / t, 1) if t else 0.0} for c, p, t in rows]
    total, passed = sum(t for _, _, t in rows), sum(p for _, p, _ in rows)
    return {"table": table, "total_cases": total, "passed": passed, "failed": total - passed,
            "pass_rate_pct": round(100 * passed / total, 1), "explanation_metrics": em,
            "hallucination_rate_pct": round(100 * len(h["missed"]) / h["adversarial_n"], 1),
            "semantic_threshold": semantic.THRESHOLD,
            "semantic_holdout": {k: so["holdout"][k] for k in ("precision", "recall", "f1", "accuracy", "confusion")},
            "semantic_dev": {k: so["dev"][k] for k in ("precision", "recall", "f1", "accuracy", "confusion")}}


def eval_consistency(evals: dict) -> dict:
    same = all(explain.explain_checked(copy.deepcopy(ev)) == explain.explain_checked(ev) for ev in evals.values())
    return {"identical_outputs": same, "gate_pass": same}


def run() -> dict:
    fx = json.loads((EVALS / "fixtures" / "evaluations.json").read_text(encoding="utf-8"))
    evals = fx["evaluations"]
    rep = {"label": "SYNTHETIC EVAL on small team-authored cases + Go engine outputs. Not real-world or enterprise performance.",
           "engine_rules_version": fx["rules_version"], "gates": GATES,
           "models": {"semantic": f"{semantic.MODEL_NAME}@{semantic.MODEL_VERSION}", "explain": f"{explain.MODEL_NAME}@{explain.MODEL_VERSION}"},
           "suites": {"semantic_overlap": eval_semantic(), "semantic_properties": eval_semantic_properties(evals),
                      "explanation_grounding": eval_grounding(evals), "hallucination": eval_hallucination(evals),
                      "required_cases": eval_required_cases(evals), "robustness": eval_robustness(),
                      "categories": eval_categories(), "consistency": eval_consistency(evals)},
           "training_status": TRAINING_STATUS,
           "model_metrics": {"training": "NOT USED", "predictive_model": "NOT USED", "accuracy_f1_roc_auc_confusion_matrix": "NOT APPLICABLE",
                             "reason": "No trained classifier exists; the semantic method is an untrained lexical rule evaluated on labelled pairs."}}
    rep["all_gates_pass"] = all(s["gate_pass"] for s in rep["suites"].values())
    rep["scorecard"] = scorecard(rep["suites"], evals)
    return rep


def markdown(r: dict) -> str:
    s = r["suites"]
    L = ["# C23 AI Eval Report", "", f"> {r['label']}", "", f"Models: {r['models']['semantic']}, {r['models']['explain']}. "
         f"Engine: {r['engine_rules_version']}.", "", f"**All gates pass: {r['all_gates_pass']}**", "",
         "| Suite | Result | Gate |", "|---|---|---|"]
    so = s["semantic_overlap"]
    for split in ("dev", "holdout"):
        m = so[split]
        L.append(f"| semantic overlap ({split}, n={m['n']}) | P {m['precision']} / R {m['recall']} / F1 {m['f1']} / acc {m['accuracy']} "
                 f"(tp {m['confusion']['tp']}, fp {m['confusion']['fp']}, fn {m['confusion']['fn']}, tn {m['confusion']['tn']}) | "
                 + ("holdout P>=0.8, R>=0.7: " + ("PASS" if so["gate_pass"] else "FAIL") if split == "holdout" else "-") + " |")
    sp = s["semantic_properties"]
    L.append(f"| semantic properties | {', '.join(k for k, v in sp['checks'].items() if v)} | {'PASS' if sp['gate_pass'] else 'FAIL'} |")
    g = s["explanation_grounding"]
    L.append(f"| explanation grounding (n={g['n']}) | pass rate {g['pass_rate']} | {'PASS' if g['gate_pass'] else 'FAIL'} |")
    h = s["hallucination"]
    L.append(f"| hallucination resistance (n={h['adversarial_n']} attacks, {h['controls_n']} controls) | rejection {h['rejection_rate']}, "
             f"control acceptance {h['control_acceptance_rate']} | {'PASS' if h['gate_pass'] else 'FAIL'} |")
    rc = s["required_cases"]
    L.append(f"| required cases (n={rc['n']}) | {sum(r['pass'] for r in rc['rows'])}/{rc['n']} | {'PASS' if rc['gate_pass'] else 'FAIL'} |")
    rb = s["robustness"]
    L.append(f"| robustness / OOD (n={rb['n']}) | {sum(r['pass'] for r in rb['rows'])}/{rb['n']} no crash, bounded, deterministic | {'PASS' if rb['gate_pass'] else 'FAIL'} |")
    L.append(f"| consistency | identical outputs: {s['consistency']['identical_outputs']} | {'PASS' if s['consistency']['gate_pass'] else 'FAIL'} |")
    sc = r["scorecard"]
    L += ["", "## AI eval scorecard", "", "| Test category | Passed / total | Pass rate |", "|---|---|---|"]
    L += [f"| {x['category']} | {x['passed']} / {x['total']} | {x['pass_rate_pct']}% |" for x in sc["table"]]
    L += [f"| **Total** | **{sc['passed']} / {sc['total_cases']}** | **{sc['pass_rate_pct']}%** |", "",
          f"- Failed cases: {sc['failed']}",
          f"- Hallucination rate (adversarial attacks not caught): {sc['hallucination_rate_pct']}%",
          f"- Explanations: grounding {sc['explanation_metrics']['grounding_pct']}%, decision agreement {sc['explanation_metrics']['decision_agreement_pct']}%, "
          f"reason-code agreement {sc['explanation_metrics']['reason_code_agreement_pct']}%, financial-number agreement "
          f"{sc['explanation_metrics']['financial_number_agreement_pct']}%, unsupported-claim rate {sc['explanation_metrics']['unsupported_claim_rate_pct']}%, "
          f"contradiction rate {sc['explanation_metrics']['contradiction_rate_pct']}% (n={sc['explanation_metrics']['n']})",
          f"- Semantic overlap threshold: similarity >= {sc['semantic_threshold']} => overlap suggestion",
          f"- Semantic holdout: precision {sc['semantic_holdout']['precision']}, recall {sc['semantic_holdout']['recall']}, F1 {sc['semantic_holdout']['f1']}, "
          f"accuracy {sc['semantic_holdout']['accuracy']}, confusion {sc['semantic_holdout']['confusion']}",
          "", "## Semantic categories (band = advisory certainty)", "", "| Category | Pair | Band | Similarity | Binary outcome | Pass |", "|---|---|---|---|---|---|"]
    L += [f"| {x['category']} | {x['a']} / {x['b']} | {x['band']} | {x['similarity']} | {x['binary_outcome'] or 'n/a (ambiguous)'} | {'PASS' if x['pass'] else 'FAIL'} |"
          for x in r["suites"]["categories"]["rows"]]
    if r["suites"]["categories"]["binary_false_positives"]:
        L += ["", "Note: 'same department, different benefit' pairs are flagged *possible (needs review)*: no false certainty, "
              "but in binary terms these are false positives of the lexical method. The official overlap_group still decides accounting."]
    L += ["", "## Model metrics", "", "Training: NOT USED. Predictive model: NOT USED. Accuracy / F1 / ROC-AUC / confusion matrix of a trained model: "
          "NOT APPLICABLE (no trained classifier exists)."]
    L += ["", "## Required semantic cases", ""] + [f"- {r['check']}: expected {r['expected']}, got {r['actual']} — {'PASS' if r['pass'] else 'FAIL'}"
                                                   for r in rc["rows"] if r["check"].startswith("semantic")]
    L += ["", "## Training status", "", r["training_status"]]
    L += ["", "## Semantic overlap errors (for transparency)", ""]
    for split in ("dev", "holdout"):
        for e in so[split]["errors"]:
            L.append(f"- {split}: label={e['label']} sim={e['similarity']} — \"{e['a']}\" vs \"{e['b']}\"")
    if not so["dev"]["errors"] and not so["holdout"]["errors"]:
        L.append("- none")
    L += ["", "## Attacks by type", ""] + [f"- {k}: {v['rejected']}/{v['n']} rejected" for k, v in sorted(h["by_attack"].items())]
    L += ["", "## Limitations", "",
          "- Eval pairs and attacks were written by the team; the sets are small (20 + 20 pairs). Treat scores as a regression guard, not a benchmark.",
          "- The semantic method is lexical with a hand-written domain lexicon; unseen vocabulary can be missed. It is advisory only.",
          "- Explanations are template-generated, so grounding is expected to be perfect; the validator exists to guard any future generator (e.g. an LLM).",
          "- History: the first eval run missed 1/130 attacks (\"It frees 45 hours per week\": 45 existed as an adoption %). The validator gained a "
          "unit-grounding check; the eval cases were not changed. Gates were fixed before the first run and never relaxed."]
    return "\n".join(L) + "\n"


def main() -> int:
    r = run()
    REPORTS.mkdir(exist_ok=True)
    (REPORTS / "ai_evals.json").write_text(json.dumps(r, indent=2, ensure_ascii=False) + "\n", encoding="utf-8", newline="\n")
    (REPORTS / "ai_evals.md").write_text(markdown(r), encoding="utf-8", newline="\n")
    print(markdown(r))
    return 0 if r["all_gates_pass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
