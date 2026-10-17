# Running the backend

The backend is usable on its own through HTTP (see [api-contract.md](api-contract.md)). No frontend is required.

```
client ──> Go + Gin API :8080 ──> PostgreSQL (source of truth, migrations embedded in the Go binary)
                              ──> Redis (optional cache)
                              ──> FastAPI AI service :8001 (optional, advisory only)
```

## Folder structure (backend)

```
backend/                 Go module (Go 1.24+)
  cmd/api                public API server
  cmd/devstack           local stack without Docker (embedded PostgreSQL + cache + API)
  cmd/evidence           judge checks -> reports/judge_evidence.md, eval fixtures for Python
  cmd/bench              latency benchmark
  internal/money finance decision rules portfolio scenario domain fixture   deterministic engine (no I/O)
  internal/store (+migrations/*.sql)  cache  aiclient  service  httpapi  config
  reports/               judge evidence, benchmark, coverage, smoke-test results
decision-engine/         Python: internal AI service (app/ai_service.py, app/ai/*), AI evals (evals/),
                         data audit/pipeline (scripts/, app/schemas.py, app/pipeline.py), immutable data (data/raw/),
                         legacy v0 prototype (app/main.py, app/core.py)
scripts/                 start/stop (Windows), smoke tests (PowerShell + bash)
docs/                    architecture, business rules, API contract, judge material
docker-compose.yml       postgres, redis, ai, api
```

## Option A — Docker (recommended where Docker is available)

```bash
cp .env.example .env            # set POSTGRES_PASSWORD
docker compose up --build -d    # postgres, redis, ai, api; migrations + official fixture seeded automatically
docker compose ps               # all healthy
curl -s localhost:8080/health
BASE_URL=http://localhost:8080 bash scripts/smoke_test.sh
docker compose down             # stop (add -v to delete the database volume)
```
Outage rehearsal: `docker compose stop ai` (→ `ai_status: degraded`, numbers unchanged), `docker compose stop redis`
(→ `redis: degraded`), `docker compose start ai redis` (recovers). **Status: written and YAML-validated, NOT executed on the
development machine (Docker not installed).**

## Option B — Windows without Docker (tested)

Prerequisites: Go 1.24+ on PATH (or the portable Go in `%LOCALAPPDATA%\c23-tools\go`), Python 3.12+.
```powershell
cd decision-engine; py -3.13 -m venv .venv; .venv\Scripts\pip install -r requirements.txt; cd ..   # once
powershell -ExecutionPolicy Bypass -File scripts\start_backend.ps1 -Fresh    # embedded PostgreSQL + API :8080 + cache + AI :8001
powershell -ExecutionPolicy Bypass -File scripts\smoke_test.ps1              # 40 checks
powershell -ExecutionPolicy Bypass -File scripts\stop_backend.ps1
```
Flags: `-Fresh` (wipe dev DB), `-NoAI` (AI unreachable → degraded), `-NoCache`, `-RedisDown` (cache at a dead port).
The first start downloads PostgreSQL binaries (~30 MB) into `%LOCALAPPDATA%\c23desk-embedded-postgres`.
Logs: `%TEMP%\c23-backend\`.

## Option C — any OS, your own PostgreSQL / Redis

```bash
cd decision-engine && pip install -r requirements.txt && uvicorn app.ai_service:app --port 8001 &
cd backend
DATABASE_URL=postgres://c23:pw@localhost:5432/c23?sslmode=disable REDIS_URL=redis://localhost:6379/0 \
AI_SERVICE_URL=http://127.0.0.1:8001 SEED_OFFICIAL=true go run ./cmd/api
```
Or `go run ./cmd/devstack -redis` for an embedded PostgreSQL on any OS.

## Environment variables

See [`.env.example`](../.env.example) — every variable read by the backend is listed with its default and meaning:
`PORT`, `DATABASE_URL`, `REDIS_URL` (empty = cache off), `AI_SERVICE_URL` (empty = AI off), `AI_TIMEOUT_MS`, `CORS_ORIGINS`
(exact origins, no `*`), `RAW_DATA_DIR`, `MAX_BODY_BYTES`, `AUTO_MIGRATE`, `SEED_OFFICIAL`, `API_KEY` (optional write
protection via `X-API-Key`), `LOG_LEVEL`, `TEST_DATABASE_URL` (tests). Tool-only: `REPORT_DIR`, `EVAL_FIXTURE_DIR` (cmd/evidence).
Secrets are redacted in startup logs; no secret is committed.

## Migrations

Embedded SQL in `backend/internal/store/migrations/`, applied in order at startup when `AUTO_MIGRATE=true` and recorded in
`schema_migrations` (idempotent; tested). Schema: pilots, pilot_financials, pilot_metrics, portfolios, portfolio_pilots
(benefit ledger), overlap_resolutions, decisions, scenario_runs, audit_logs, model_predictions — see
[backend-architecture.md](backend-architecture.md).

## Load data

`POST /api/v1/import/official` (SHA-256 verified C23 fixture, labelled OFFICIAL_FIXTURE), or `SEED_OFFICIAL=true`, or
`POST /api/v1/import` for your own fixture-shaped data (all-or-nothing validation, labelled TEAM_ASSUMPTION).

## Health

| URL | Meaning |
|---|---|
| `http://localhost:8080/health` | always 200; per-component status (database / redis / ai_service) |
| `http://localhost:8080/ready` | 200 only when PostgreSQL is reachable (use for orchestration) |
| `http://localhost:8001/health` | AI service liveness (internal) |

## Tests, evals, coverage, benchmark

```bash
cd backend
go vet ./... && go build ./...
go test ./...                                  # 113 tests: unit, property, real PostgreSQL, API, red-team, outage
go test ./... -coverpkg=./internal/... -coverprofile=reports/go_coverage.out && go tool cover -func=reports/go_coverage.out
go run ./cmd/evidence                          # 15 judge checks -> reports/judge_evidence.md (exit 1 on failure)
go run ./cmd/bench -inprocess -n 100 -ai http://127.0.0.1:8001   # needs the AI service running for the "AI online" rows

cd ../decision-engine
python -m pytest -q --cov=app --cov=scripts --cov=evals         # 140 tests
python -m evals.run_evals                                       # AI evals -> reports/ai_evals.{json,md}
ruff check .
```

## Judge challenge examples (curl)

```bash
curl -s -X POST localhost:8080/api/v1/pilots/AI1/evaluate                                   # 120000 / 10000 / 20 months
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -H 'Content-Type: application/json' \
     -d '{"overrides":{"annual_running_cost_inr":120000}}'                                  # 60000 / 5000 / 40 months
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"annual_running_cost_inr":null}}'   # null payback, REVISE
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"actual_adoption_pct":20,"actual_quality_pct":90}}'  # STOP
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -d '{"overrides":{"actual_adoption_pct":70,"actual_quality_pct":60}}'  # STOP (quality)
curl -s -X POST localhost:8080/api/v1/preview/portfolio -d '{"budget_inr":600000,"pilot_ids":["AI1","AI2","AI3","AI4"]}'        # REJECTED
curl -s -X POST localhost:8080/api/v1/portfolios -d '{"name":"pair","budget_inr":600000,"pilot_ids":["AI4","AI5"]}'             # DOUBLE_COUNT_RISK
curl -s -X POST localhost:8080/api/v1/preview/optimize -d '{"budget_inr":100000}'                                              # empty portfolio
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `/ready` 503 | PostgreSQL unreachable: check `DATABASE_URL`; stateless `/api/v1/evaluate` and `/api/v1/optimize` still work |
| `ai_status: degraded` | AI service down or slow (>1.5 s): start `uvicorn app.ai_service:app --port 8001`; numbers are unaffected |
| `redis: degraded` | Redis unreachable: results unaffected; the circuit breaker retries every 5 s |
| devstack: "process already listening on port 55432" | an earlier devstack database is still running: `scripts\stop_backend.ps1` |
| 409 `CONFLICT` on import | data already imported (idempotent; use `-Fresh` for a clean database) |
