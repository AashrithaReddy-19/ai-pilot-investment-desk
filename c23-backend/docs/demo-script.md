# API demo script (≈ 6 minutes) — every number is produced live by the engine

## Start (once, before judges arrive)

**Docker:** `docker compose up --build -d` → API http://localhost:8080 (official fixture auto-seeded).
**Without Docker (tested on Windows):**
```powershell
powershell -ExecutionPolicy Bypass -File scripts\start_backend.ps1 -Fresh    # embedded PostgreSQL + API :8080 + cache + AI :8001
```
Check `http://localhost:8080/health`: database, redis and ai_service must all be `ok`.

PowerShell helper used below (curl works the same way, see docs/judge-tests.md):
```powershell
function Api($m, $p, $b) { if ($b) { Invoke-RestMethod -Method $m "http://localhost:8080$p" -ContentType 'application/json' -Body $b } else { Invoke-RestMethod -Method $m "http://localhost:8080$p" } }
```

## 30-second opener
"Companies have many AI ideas but no auditable way to decide which pilot deserves money. We evaluate economics,
adoption, quality, readiness, overlap and stop conditions with a deterministic engine. AI can explain. AI cannot change the money."

| # | Call | Say | Expected response |
|---|---|---|---|
| 1 Official reference | `Api POST /api/v1/pilots/AI1/evaluate` | "200000 setup, 60000 running, 180000 benefit." | `stated_basis`: net 120000, monthly 10000, payback **20**; `field_sources` say OFFICIAL_FIXTURE |
| 2 Changed input | `Api POST /api/v1/pilots/AI1/scenarios '{"overrides":{"annual_running_cost_inr":120000}}'` | "Judge changes one input, same rule." | baseline 20 → scenario **40** months, net 60000, monthly 5000; `GET /api/v1/pilots/AI1` still shows 60000 |
| 3 Overlap | `Api POST /api/v1/portfolios '{"name":"pair","budget_inr":600000,"pilot_ids":["AI4","AI5"]}'` | "Both save support-team hours." | `DOUBLE_COUNT_RISK`, claimed 360000, recognised **180000**, status NEEDS_REVIEW; approve → 409 APPROVAL_BLOCKED |
| 3b Resolve | `Api POST /api/v1/portfolios/{id}/overlaps/support/resolve '{"resolution_type":"CUSTOM_ALLOCATION","allocations_inr":{"AI4":120000,"AI5":60000},"version":1}'` | "A reviewer splits the pool; the ledger records it." | overlap RESOLVED, recognised 180000; still blocked by AI5 `PILOT_NEEDS_REVISION` (low readiness) |
| 4 Low adoption | scenarios `{"overrides":{"actual_adoption_pct":20,"actual_quality_pct":90}}` and `{"preset":"adoption_down_30"}` | "Break-even adoption is 33.33%." | 20% → **STOP** `ADOPTION_BELOW_BREAKEVEN`; 49% → **REVISE** `LOW_ADOPTION` |
| 5 Failed quality | scenario `{"preset":"quality_down_10"}` | "Quality stops a pilot even when payback is 20 months." | **STOP** `QUALITY_THRESHOLD_FAILED`, payback still 20 |
| 6 Missing cost | scenario `{"overrides":{"annual_running_cost_inr":null}}` | "Null means unknown, never zero." | `payback_months: null`, `NOT_CALCULABLE`, **REVISE** `MISSING_RUNNING_COST` |
| 7 AI down | stop the AI service (`docker compose stop ai`, or kill uvicorn), repeat call 1 | "The AI service is gone." | same numbers and decision, `"ai_status":"degraded"`, `ai_explanation: null` |
| 8 Budget reduced | `Api POST /api/v1/preview/optimize '{"budget_inr":100000}'` (also 600000, 400000) | "Budget cut." | 600000 → AI1, AI2, AI4 · 400000 → AI1, AI4 · 100000 → **empty** |
| Close | create AI1+AI2+AI4 portfolio → approve → `GET /api/v1/portfolios/{id}` → `GET /api/v1/audit/portfolio/{id}` | "Every change is auditable." | APPROVED survives reload; audit shows create + approve with before/after |

Or run all of it at once: `powershell -ExecutionPolicy Bypass -File scripts\smoke_test.ps1` (40 checks).

## If something fails live
- Database down: stateless `POST /api/v1/evaluate` and `/api/v1/optimize` still answer (see docs/judge-tests.md).
- Evidence on disk: `backend/reports/judge_evidence.md`, `backend/reports/smoke_test*.json`, `decision-engine/reports/ai_evals.md`.
