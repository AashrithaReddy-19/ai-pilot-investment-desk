# Judge Challenge Guide

Every row below is an automated test **and** can be reproduced live with curl. Generated evidence:
`backend/reports/judge_evidence.md` (`go run ./cmd/evidence`, exits non-zero if any check fails).

| Demo / case | Change | Expected (c23-rules-v1) | Automated test |
|---|---|---|---|
| DEMO 1 | none (AI1 200000 / 60000 / 180000) | net 120000, monthly 10000, payback 20 | `TestOfficialReference20MonthPayback`, `TestDemo1_*` |
| DEMO 2 / A | running 60000 → 120000 | net 60000, monthly 5000, payback 40 | `TestJudgeChangedRunningCost40Months`, `TestDemo2_*` |
| DEMO 6 / B | running → null | payback null (`NOT_CALCULABLE`), REVISE `MISSING_RUNNING_COST` | `TestCaseB_*`, `TestDemo6_*` |
| C | budget 600000, select AI1–AI4 (800000) | REJECTED `BUDGET_EXCEEDED`, remaining −200000 | `TestCaseC_*` |
| DEMO 3 / D | select AI4 + AI5 | `DOUBLE_COUNT_RISK`, recognised 180000 (not 360000), NEEDS_REVIEW | `TestCaseD_*`, `TestDemo3_*` |
| – | resolve AI4 120000 / AI5 60000 | recognised 180000; still NEEDS_REVIEW (AI5 low readiness) → revise AI5 → VALID → APPROVED | `TestDemo3_*`, `TestOverlapResolutions` |
| DEMO 4 / E | adoption 70 → 20 | STOP `ADOPTION_BELOW_BREAKEVEN` (break-even 33.33%) | `TestCaseE_*` |
| – | adoption 45 | REVISE `LOW_ADOPTION` | `TestLowAdoptionAboveBreakevenRevises` |
| DEMO 5 / F | quality 90 → 60 | STOP `QUALITY_THRESHOLD_FAILED` even with 20-month payback | `TestCaseF_*` |
| G | running 200000 | net −20000, `NEVER_PAYS_BACK`, STOP | `TestCaseG_*` |
| DEMO 8 | budget 600000 → 400000 → 200000 → 100000 | AI1,AI2,AI4 → AI1,AI4 → AI1 → empty | `TestDemo8_BudgetReduced`, `TestOptimizerBudgetEdges`, live smoke test |
| – | presets running +25% / benefit −20% / adoption −30% / quality −10% | 22.86 months / net 84000 / REVISE / STOP | `TestPresetsUseTheNormalEngine` |
| – | benefit_type → "time freed" | REVIEW `TIME_NOT_CASH`; cash basis unknown; excluded on cash basis | `TestTimeFreedIsNotCash` |
| – | overlap_group → null | `OVERLAP_UNKNOWN`, blocks approval | `TestOverlapUnknownAndSharedGroup` |
| DEMO 7 | AI service down / slow / hallucinating | same numbers, `ai_status: degraded`, < 1.5 s | `TestDemo7_AIOutageCoreStillWorks` |
| – | Redis down | same numbers, `redis: degraded` in /health | `TestRedisOutageCoreStillWorks` |
| – | PostgreSQL down | 503 on DB endpoints; stateless endpoints still answer | `TestDatabaseOutage` |

## Live API smoke test (real stack, no mocks)

`powershell -ExecutionPolicy Bypass -File scripts/start_backend.ps1 -Fresh` then `scripts/smoke_test.ps1`
(or `bash scripts/smoke_test.sh` against Docker) drives the running backend through every check above: health/ready,
import, 20 and 40 months, adoption, quality, missing cost, negative economics, time freed, budget, AI4+AI5 overlap and its
resolution, unknown overlap, optimizer determinism, budget reduced, save/approve/reload, scenarios, decisions, audit,
export, validation and security cases. Run with all services up (40 checks), FastAPI killed (`-ExpectAI degraded`) and
Redis unavailable (`start_backend.ps1 -RedisDown`, `-ExpectCache degraded`). Results: `backend/reports/smoke_test*.json`.

## Live commands (API on :8080 with the official fixture seeded)

```bash
# DEMO 1
curl -s -X POST localhost:8080/api/v1/pilots/AI1/evaluate
# DEMO 2 (any field can be changed the same way; the pilot itself is not modified)
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -H 'Content-Type: application/json' \
  -d '{"name":"judge","overrides":{"annual_running_cost_inr":120000}}'
# DEMO 4 / 5 / 6
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"actual_adoption_pct":20,"actual_quality_pct":90}}'
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"actual_adoption_pct":70,"actual_quality_pct":60}}'
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"annual_running_cost_inr":null}}'
# DEMO 3
curl -s -X POST localhost:8080/api/v1/portfolios -d '{"name":"support pair","budget_inr":600000,"pilot_ids":["AI4","AI5"]}'
# C: over budget
curl -s -X POST localhost:8080/api/v1/portfolios -d '{"name":"too big","budget_inr":600000,"pilot_ids":["AI1","AI2","AI3","AI4"]}'
# Any judge-supplied data, no database needed
curl -s -X POST localhost:8080/api/v1/optimize -d '{"budget_inr":100000,"pilots":[...]}'
# DEMO 7: stop the AI service (docker compose stop ai) and repeat DEMO 1 -> "ai_status":"degraded", same numbers
```
(PowerShell: use `Invoke-RestMethod -Method Post -ContentType 'application/json' -Body '...'`.)
