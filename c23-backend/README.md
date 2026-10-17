# C23 — AI Pilot Investment Decision Desk (backend)

> **Location:** this backend lives in `c23-backend/`, side by side with the team's existing code at the repository root
> (which is left unchanged). **Run every command below from inside `c23-backend/`.** CI: `.github/workflows/c23-backend.yml`.

A backend that helps a department head choose bounded AI pilots under a budget with conservative, explainable economics:
exact payback, no double-counted benefits, explicit PROCEED / REVIEW / REVISE / STOP rules, and a full audit trail.
**AI can explain. AI cannot change the money.**

> Data is the **synthetic** C23 teaching fixture plus documented team assumptions. Results are calculations on stated
> assumptions — not proven ROI, not validated market demand. No real interviews have been recorded yet.

```
client ──> Go + Gin API :8080 (deterministic engine, c23-rules-v1) ──> PostgreSQL  (source of truth)
                                                                   ──> Redis       (optional cache)
                                                                   ──> FastAPI :8001 (optional, advisory AI)
```

## Start / stop

**Docker** (`docker compose` v2):
```bash
cp .env.example .env
docker compose up --build -d     # postgres, redis, ai, api — migrations + official fixture seeded
docker compose down              # stop (add -v to delete data)
```

**Windows without Docker** (tested end to end):
```powershell
cd decision-engine; py -3.13 -m venv .venv; .venv\Scripts\pip install -r requirements.txt; cd ..    # once
powershell -ExecutionPolicy Bypass -File scripts\start_backend.ps1 -Fresh
powershell -ExecutionPolicy Bypass -File scripts\stop_backend.ps1
```

Health: `http://localhost:8080/health` (per component) · `http://localhost:8080/ready` (DB reachable) · `http://localhost:8001/health` (AI).
Full guide (env vars, migrations, any-OS commands, troubleshooting): **[docs/running-the-backend.md](docs/running-the-backend.md)**.

## Try it (curl)

```bash
curl -s -X POST localhost:8080/api/v1/pilots/AI1/evaluate                          # net 120000, monthly 10000, payback 20
curl -s -X POST localhost:8080/api/v1/pilots/AI1/scenarios -H 'Content-Type: application/json' \
     -d '{"overrides":{"annual_running_cost_inr":120000}}'                         # net 60000, monthly 5000, payback 40
curl -s -X POST localhost:8080/api/v1/portfolios -H 'Content-Type: application/json' \
     -d '{"name":"pair","budget_inr":600000,"pilot_ids":["AI4","AI5"]}'            # DOUBLE_COUNT_RISK, recognised 180000
```
Every route, body, response and error: **[docs/api-contract.md](docs/api-contract.md)** (real captured examples in `docs/api-examples/`).

## Test

```bash
powershell -File scripts/smoke_test.ps1     # or: BASE_URL=http://localhost:8080 bash scripts/smoke_test.sh   (against a running backend)
cd backend && go vet ./... && go test ./... && go run ./cmd/evidence
cd decision-engine && python -m pytest -q && python -m evals.run_evals && ruff check .
```
Database tests start a real embedded PostgreSQL automatically (or use `TEST_DATABASE_URL` with `go test -p 1 ./...`).

## Documentation

| Topic | Document |
|---|---|
| Run, configure, migrate, troubleshoot | [docs/running-the-backend.md](docs/running-the-backend.md) |
| API contract | [docs/api-contract.md](docs/api-contract.md) |
| Architecture, schema, Redis strategy, AI role, failure behaviour | [docs/backend-architecture.md](docs/backend-architecture.md) |
| Formulas, null policy, stop rules, overlap ledger, approval, optimizer | [docs/business-rules.md](docs/business-rules.md) |
| Judge challenges and evidence | [docs/judge-tests.md](docs/judge-tests.md), `backend/reports/judge_evidence.md`, [docs/judge-qa.md](docs/judge-qa.md) |
| AI evals | `decision-engine/reports/ai_evals.md` |
| Benchmark, coverage, smoke test | `backend/reports/benchmark.md`, `backend/reports/coverage.md`, `backend/reports/smoke_test*.json` |
| Business evidence (hypotheses, interviews pending) | [docs/business/](docs/business/), [docs/interviews/](docs/interviews/README.md), [docs/product-change.md](docs/product-change.md) |

Any future client (web, mobile, scripts) integrates through the Go API only; it never calls the AI service directly.

## Known limitations

- Synthetic fixture; adoption/quality not measured, so every official pilot is at best REVIEW (pre-pilot).
- Thresholds (60% adoption, 80% quality, 50/75 readiness), readiness labels, the 20% assumed run cost and the
  "overlap pool = largest claim" rule are team assumptions, centralised in `backend/internal/rules`.
- No predictive model training (fixture too small and synthetic). Semantic overlap is lexical and advisory
  (holdout recall 0.727; same-department pairs are flagged "possible" — binary false positives).
- `X-Actor` is an audit label, not authentication; optional `API_KEY` protects writes; no rate limiting.
- Docker stack and GitHub Actions CI are written and syntax-validated but **not executed** (no Docker or git on the dev machine).
