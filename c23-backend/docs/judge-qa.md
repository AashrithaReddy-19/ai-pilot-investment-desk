# Judge Q&A (45 questions)

## Architecture
1. **Why Go?** The money, budget, overlap and stop rules must be exact, fast and boring. Go gives static types, one binary, cheap concurrency and `math/big` for exact rational arithmetic. Exhaustive search over 2^n subsets is trivial in Go.
2. **Why FastAPI?** The AI/data work (semantic matching, evals, data audit) lives naturally in Python. It is an internal service: only Go calls it.
3. **Why not one language?** The deterministic core and the advisory AI have different failure tolerances. A process boundary with a 1.5 s timeout makes "AI cannot break the money" structurally true, not just a promise.
4. **Why Redis?** It avoids repeated AI round-trips and recomputation. It is optional: keys are content-addressed (fingerprint of every input + rules version), so stale data cannot be served, and a circuit breaker makes a dead Redis cost nothing.
5. **What if Redis is down?** Cache miss, results identical; `/health` shows `redis: degraded`. Proven by an API test and the live smoke test (40/40) with Redis pointed at a dead port.
6. **What if the AI service is down?** Deterministic response returned with `ai_status: degraded` and `ai_explanation: null`; numbers unchanged — also for cached results. Proven by API tests (down/slow/hallucinating) and the live smoke test with the FastAPI process killed.
7. **What if PostgreSQL is down?** `/ready` = 503, DB endpoints return a clean `DATABASE_UNAVAILABLE` (no internals leaked); stateless `/evaluate` and `/optimize` keep working.
8. **How would this scale?** Stateless API behind a load balancer, PostgreSQL as the only state, Redis shared. The optimizer is exact up to 20 candidates; beyond that we would switch to integer programming (the objective and constraints are already explicit).
9. **Why exhaustive optimizer, not linear programming?** C23 has 5–8 pilots: ≤ 256 subsets. Exhaustive search is provably optimal, trivially explainable and deterministic, with a documented tie-break. An LP/MIP solver adds a dependency and opaque tie-breaking for no benefit at this size.
10. **Is the optimizer deterministic?** Yes: candidates sorted by ID, explicit 6-level tie-break; tested with 50 random shuffles of the input.

## Money and rules
11. **Show the 20-month calculation.** Net = 180000 − 60000 = 120000; monthly = 10000; payback = 200000 / 10000 = 20. Computed exactly with rationals; a release-blocking test.
12. **Change running cost to 120000.** Net 60000, monthly 5000, payback 40 months — live via `POST /api/v1/pilots/AI1/scenarios`; the official value is untouched.
13. **Why null ≠ zero?** A missing running cost treated as 0 makes every pilot look better. We report "payback unknown" and REVISE until someone supplies it.
14. **Can I impute a missing cost?** Only in an explicit scenario (`assume_missing_run_cost`), labelled TEAM_ASSUMPTION with the rule shown.
15. **Floating-point errors?** None: money is int64 paise, derived values are `big.Rat`; `0.1 + 0.2 = 0.3` exactly (tested).
16. **How do stop rules work?** Nine rules run every time; all triggered reason codes are returned; the final decision is the most severe (STOP > REVISE > REVIEW > PROCEED). Quality < minimum → STOP; adoption < break-even → STOP; adoption < minimum → REVISE.
17. **Why is quality failure STOP, not REVISE?** A pilot producing unacceptable output should not get more money however good its payback; documented as a team rule, one constant to change.
18. **Why are all official pilots REVIEW?** Nothing has been measured yet. The case says measure adoption and quality before wider investment, so they are fundable only as time-boxed pilots.
19. **Why is AI5 REVISE?** Its readiness label is "low" (a team assumption) — revise readiness before funding.
20. **Can a REVISE pilot be approved?** No. REVISE means fix before committing money, so it blocks portfolio approval and the optimizer excludes it.
21. **Time freed vs cash?** Stored separately (`capacity_value_inr`, `realized_cash_saving_inr`). On the cash basis, time freed counts only recorded cash savings; otherwise REVIEW `TIME_NOT_CASH`.
22. **How do you prevent double counting?** Pilots sharing an overlap group draw on one pool (largest claim). Unresolved → only the largest claim counts and approval is blocked; resolutions are validated so the total can never exceed the pool; the DB itself rejects recognised > own benefit.
23. **Show AI4 + AI5.** Claimed ₹3,60,000, recognised ₹1,80,000, DOUBLE COUNT RISK, NEEDS REVIEW.
24. **What if overlap group is unknown?** OVERLAP_UNKNOWN: never treated as "no overlap"; blocks approval; excluded by the optimizer.
25. **Budget exactly equal to setup?** Allowed (≤). One paisa short → REJECTED. Tested.
26. **Can a client approve something the rules wouldn't allow?** No. Approval is a backend endpoint that recomputes everything first; an over-budget or unresolved portfolio gets 409 APPROVAL_BLOCKED with the blockers (tested and in the smoke test).

## AI
27. **Why not a pure LLM?** LLMs can't be trusted with arithmetic, budgets or stop rules, and their answers vary. We use AI where being wrong is cheap: suggestions and explanations, both checked.
28. **Why no ML training?** There is no real training data. Five synthetic rows can't train anything, and a model trained on data we generate would only learn our generator. We report this instead of a meaningless accuracy.
29. **Why not train on five records?** Any metric would be noise; it would be fake evidence.
30. **What AI does the product use?** (1) Semantic overlap suggestions with a certainty band (likely / possible / unlikely) — advisory, the official group decides accounting. (2) Plain-language explanations of the engine's decision, validated by a grounding checker.
31. **How do you prevent hallucination?** Every explanation must pass: all numbers exist in the evaluation, no unsupported units, only known pilot IDs, exactly the engine's decision word, only produced reason codes, no "proven ROI/guaranteed" claims. The Go client also rejects ungrounded or malformed AI responses.
32. **Why AI evals?** Tests prove code paths; evals measure behaviour. 130 adversarial hallucinations (wrong numbers, invented pilots, wrong decision, ROI claims) — 100% rejected; 37 faithful paraphrases — 100% accepted.
33. **Did an eval ever fail?** Yes: the first run missed "frees 45 hours per week" (45 existed as an adoption %). We fixed the validator with unit grounding, not the eval set. Recorded in the report.
34. **Ambiguous overlap?** "Ticket classification" vs "Support knowledge recommendation" returns *possible — needs human review*, not fabricated certainty.
35. **Why not sentence-transformers?** 5–8 short descriptions; a ~400 MB model adds startup cost and non-determinism without proven gain. The lexical method is transparent and evaluated (holdout precision 1.0, recall 0.73); we state its limits.

## Evidence and honesty
36. **What is synthetic?** The official C23 fixture (declared synthetic) and our eval cases. Results are calculations, not proven ROI.
37. **What is real?** The engine, its tests, the measured benchmark and the live smoke-test runs. Interviews: **not yet done** — marked as manual work, not fabricated.
38. **How is evidence tracked?** Every field carries a provenance label (Official / Team assumption / Observed / Calculated / Unknown) returned as `field_sources` on every pilot; editing an official value turns it into a team assumption.
39. **"Why did AI2 become REVISE yesterday?"** Every evaluation is stored with the full input snapshot and rules version; every change has a before/after audit row with actor and request ID.
40. **Is the official data protected?** `data/raw` is SHA-256 verified against a manifest at import; a tampered file is refused (tested).
41. **Security?** Strict validation (no "2 lakh", NaN, duplicate JSON keys, unknown fields), parameterised SQL, ID patterns, 1 MiB body limit, CORS allowlist, optional API key, no secrets in logs, CSV/Markdown injection neutralised, security headers.
42. **Performance?** Local benchmark, 100 concurrent requests: evaluate p50 ≈ 24–29 ms without AI, ≈ 120–190 ms with AI explanations; 0% errors in all 8 configurations (backend/reports/benchmark.md). Not a production-scale claim.
43. **Did Docker / CI run?** Written and syntax-validated; **not executed** on our machine (no Docker or git installed). We say so rather than claim it.
44. **What did each team member build?** See `docs/contribution-matrix.md` (to be completed with real names and evidence).
45. **What would you build next?** Real interviews → set thresholds from evidence; measured adoption from tool logs; an optional LLM explainer behind the existing grounding validator.
