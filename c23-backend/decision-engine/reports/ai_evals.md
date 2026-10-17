# C23 AI Eval Report

> SYNTHETIC EVAL on small team-authored cases + Go engine outputs. Not real-world or enterprise performance.

Models: lexical-domain-overlap@1.1.0, template-explainer@1.0.0. Engine: c23-rules-v1.

**All gates pass: True**

| Suite | Result | Gate |
|---|---|---|
| semantic overlap (dev, n=20) | P 1.0 / R 0.833 / F1 0.909 / acc 0.9 (tp 10, fp 0, fn 2, tn 8) | - |
| semantic overlap (holdout, n=20) | P 1.0 / R 0.727 / F1 0.842 / acc 0.85 (tp 8, fp 0, fn 3, tn 9) | holdout P>=0.8, R>=0.7: PASS |
| semantic properties | official_flags_only_AI4_AI5, symmetric, bounded_0_1, deterministic_and_order_independent, fallback_to_overlap_group | PASS |
| explanation grounding (n=13) | pass rate 1.0 | PASS |
| hallucination resistance (n=130 attacks, 37 controls) | rejection 1.0, control acceptance 1.0 | PASS |
| required cases (n=21) | 21/21 | PASS |
| robustness / OOD (n=9) | 9/9 no crash, bounded, deterministic | PASS |
| consistency | identical outputs: True | PASS |

## AI eval scorecard

| Test category | Passed / total | Pass rate |
|---|---|---|
| Semantic overlap (labelled pairs, binary) | 35 / 40 | 87.5% |
| Semantic categories (no false certainty) | 7 / 7 | 100.0% |
| Ambiguous case handling | 1 / 1 | 100.0% |
| Grounding (generated explanations) | 13 / 13 | 100.0% |
| Decision consistency (explanation = engine decision) | 13 / 13 | 100.0% |
| Reason-code agreement | 13 / 13 | 100.0% |
| Financial number fidelity | 13 / 13 | 100.0% |
| Hallucination resistance (attacks rejected) | 130 / 130 | 100.0% |
| Faithful paraphrases accepted (controls) | 37 / 37 | 100.0% |
| Required brief cases | 21 / 21 | 100.0% |
| Fallback / robustness | 10 / 10 | 100.0% |
| Consistency (identical repeated output) | 1 / 1 | 100.0% |
| **Total** | **294 / 299** | **98.3%** |

- Failed cases: 5
- Hallucination rate (adversarial attacks not caught): 0.0%
- Explanations: grounding 100.0%, decision agreement 100.0%, reason-code agreement 100.0%, financial-number agreement 100.0%, unsupported-claim rate 0.0%, contradiction rate 0.0% (n=13)
- Semantic overlap threshold: similarity >= 0.3 => overlap suggestion
- Semantic holdout: precision 1.0, recall 0.727, F1 0.842, accuracy 0.85, confusion {'tp': 8, 'fp': 0, 'fn': 3, 'tn': 9}

## Semantic categories (band = advisory certainty)

| Category | Pair | Band | Similarity | Binary outcome | Pass |
|---|---|---|---|---|---|
| clear overlap | Support reply drafting / Customer response copilot | likely | 0.5 | TP | PASS |
| same benefit, different wording | Answer customer emails / Draft replies to helpdesk enquiries | likely | 0.5 | TP | PASS |
| clear non-overlap | Invoice extraction / Employee onboarding | unlikely | 0.0 | TN | PASS |
| same department, different benefit | Invoice data extraction / Vendor payment fraud detection | possible | 0.5 | FP | PASS |
| same department, different benefit | Draft support replies / Forecast support staffing levels | possible | 0.6443 | FP | PASS |
| ambiguous | Ticket classification / Support knowledge recommendation | possible | 0.5 | n/a (ambiguous) | PASS |
| unknown wording | Quantum flux harmonizer / Blockchain synergy portal | unlikely | 0.0 | TN | PASS |

Note: 'same department, different benefit' pairs are flagged *possible (needs review)*: no false certainty, but in binary terms these are false positives of the lexical method. The official overlap_group still decides accounting.

## Model metrics

Training: NOT USED. Predictive model: NOT USED. Accuracy / F1 / ROC-AUC / confusion matrix of a trained model: NOT APPLICABLE (no trained classifier exists).

## Required semantic cases

- semantic band: Support reply drafting vs Customer response copilot: expected likely, got likely — PASS
- semantic band: Invoice extraction vs Employee onboarding: expected unlikely, got unlikely — PASS
- semantic band: Ticket classification vs Support knowledge recommendation: expected possible, got possible — PASS

## Training status

Predictive training not used because the official fixture is too small and synthetic. AI value is delivered through advisory enrichment and deterministic decision support. No model artifacts exist in the repository.

## Semantic overlap errors (for transparency)

- dev: label=True sim=0.25 — "Summarise payroll exceptions for HR." vs "Answer employee leave policy questions."
- dev: label=True sim=0.25 — "Create merchandising banners for product promotions." vs "Plan marketing campaign budgets."
- holdout: label=True sim=0.25 — "Forecast warehouse dispatch volumes." vs "Explain order delivery delays to the operations lead."
- holdout: label=True sim=0.0 — "Draft job descriptions for open roles." vs "Answer new employee onboarding questions."
- holdout: label=True sim=0.25 — "Personalise promotion emails for loyal customers." vs "Score sales prospects by likelihood to buy."

## Attacks by type

- certainty: 13/13 rejected
- extra_decision_word: 13/13 rejected
- guarantee: 13/13 rejected
- invented_code: 13/13 rejected
- invented_metric: 13/13 rejected
- invented_pilot: 13/13 rejected
- market_claim: 13/13 rejected
- roi_claim: 13/13 rejected
- wrong_decision: 13/13 rejected
- wrong_number: 13/13 rejected

## Limitations

- Eval pairs and attacks were written by the team; the sets are small (20 + 20 pairs). Treat scores as a regression guard, not a benchmark.
- The semantic method is lexical with a hand-written domain lexicon; unseen vocabulary can be missed. It is advisory only.
- Explanations are template-generated, so grounding is expected to be perfect; the validator exists to guard any future generator (e.g. an LLM).
- History: the first eval run missed 1/130 attacks ("It frees 45 hours per week": 45 existed as an adoption %). The validator gained a unit-grounding check; the eval cases were not changed. Gates were fixed before the first run and never relaxed.
