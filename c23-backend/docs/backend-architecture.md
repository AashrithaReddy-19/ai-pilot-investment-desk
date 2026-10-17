# C23 Backend Architecture

## One-sentence summary

A **deterministic Go engine** decides everything that involves money, budget, overlap and stop rules; an **optional
Python AI service** adds advisory text and similarity hints that can fail at any time without changing a single number.

```
     (any API client; no frontend in this deliverable)
                    |
                    v
        Go + Gin public API  (backend/)  ----------------->  FastAPI internal AI service (decision-engine/app/ai_service.py)
        |  validation, finance, decisions,      1.5 s timeout,      semantic overlap (advisory), grounded explanations
        |  overlap ledger, optimizer, scenarios, response validated, never decides anything
        |  audit, approval                       degrades safely
        |
        +--> PostgreSQL  (source of truth: pilots, portfolios, ledger, decisions, scenarios, audit, predictions)
        +--> Redis       (optional cache-aside; content-addressed keys; circuit breaker)
```

## Client integration

Clients talk **only** to the Go API, never to the FastAPI service, and should not re-implement business calculations:
totals, payback, decisions, overlap accounting and approval all come from API responses. Read-only helper endpoints exist
for client screens: `GET /api/v1/dashboard`, `POST /api/v1/preview/portfolio` (assess a selection without saving),
`POST /api/v1/preview/optimize`, `GET /api/v1/scenario-presets`, `GET /api/v1/activity` (see api-contract.md).

## Why two languages

| Concern | Owner | Reason |
|---|---|---|
| Public REST API, validation, finance, decisions, portfolio, overlap, audit, persistence | **Go** | Exact integer money (paise) + `big.Rat`, static types, one binary, fast exhaustive search, strong concurrency story |
| Advisory semantic overlap, plain-language explanations, AI evals, data audit/preprocessing scripts | **Python** | Data/AI ecosystem; the team's existing data pipeline and legacy prototype live here |

The Python service is **internal**: the Go API is the only caller. It is not a second business API.

## Layer A — deterministic engine (`backend/internal/`)

| Package | Responsibility |
|---|---|
| `money` | Nullable exact INR (`int64` paise) and percent (hundredths); strict JSON-number parsing; unknown ≠ 0 (using an unknown as a number panics) |
| `domain` | Pilot model, strict parser (all field errors collected, unknown fields and duplicate keys rejected), provenance labels, scenario overrides |
| `finance` | Pure formulas: net, monthly net, payback (+status), ROI, break-even adoption, sums, budget remaining |
| `rules` | `c23-rules-v1`: thresholds, reason codes, priorities, decision severities |
| `decision` | Per-pilot rules engine → decision, all reason codes, failed rules with numbers, metrics, explanation |
| `portfolio` | Assessment (budget, overlap ledger, totals, status) and exhaustive optimizer |
| `scenario` | What-if overrides on a copy; base pilot never modified |
| `fixture` | Load fixture/extension shapes; SHA-256 verification of the official files |

None of these packages performs I/O. They are covered at 95.4% by unit, property and judge tests.

## Layer B — orchestration and infrastructure

| Package | Responsibility |
|---|---|
| `store` | PostgreSQL via pgx; embedded SQL migrations; every mutation + its audit row in one transaction; optimistic locking; read-only retry on transient errors |
| `cache` | Redis cache-aside; 200 ms timeouts; circuit breaker (5 s bypass after a failure); content-addressed keys |
| `aiclient` | HTTP client to FastAPI: 1.5 s timeout, context cancellation, 1 MiB response cap, response validation (unknown pilot IDs, out-of-range scores, ungrounded text → rejected); live status memoised 3 s so cache hits report the AI service's *current* state |
| `service` | Wires engine + store + cache + AI; maps errors to a stable error contract |
| `httpapi` | Gin router and middleware (request ID, structured logs, body limit, panic recovery, optional API key, CORS allowlist, security headers) |

## Failure behaviour

| Failure | Behaviour | Tested |
|---|---|---|
| AI service down / slow / returns garbage / hallucinates IDs | Deterministic response returned, `ai_status: degraded`, suggestions dropped — also on cache hits (cached AI text withheld while the service is down) | `TestDemo7_AIOutageCoreStillWorks`, `TestRedTeamCacheHitDuringAIOutageReportsDegraded`, live drill (`smoke_test_ai_down.json`), benchmark |
| Redis down | Cache miss, breaker opens, `/health` shows `redis: degraded`, results identical | `TestRedisOutageCoreStillWorks`, `cache` tests |
| PostgreSQL down | `/ready` → 503; DB endpoints → 503 `DATABASE_UNAVAILABLE` (no internals leaked); stateless `/api/v1/evaluate` and `/api/v1/optimize` keep working | `TestDatabaseOutage`, `TestConnectionFailure` |
| Concurrent edits | Optimistic locking → `409 VERSION_CONFLICT`; exactly one concurrent approval wins | `TestConcurrentUpdatesOnlyOneWins`, `TestRedTeamApprovalRaceAndStaleness` |
| Official data tampered | Import refuses (SHA-256 mismatch against `data/raw/MANIFEST.json`) | `TestTamperedRawFileRejected` |

## Database (`backend/internal/store/migrations/0001_init.sql`)

`pilots`, `pilot_financials` (BIGINT paise, NULL = unknown, CHECK ≥ 0), `pilot_metrics` (0..10000 hundredths of a percent),
`portfolios`, `portfolio_pilots` (**benefit attribution ledger**; CHECK recognised ≤ basis benefit), `overlap_resolutions`,
`decisions` (full input + calculation snapshot + `rules_version`), `scenario_runs`, `audit_logs` (before/after, actor,
request ID; same transaction as the change), `model_predictions` (advisory AI output, never read by the engine).
Constraints back up API validation: even a bug that bypassed validation cannot store negative money or an unsafe ID.

Migrations are owned by Go (embedded, applied in order, recorded in `schema_migrations`, idempotent). There is no
second migration system: Python does not touch the database.

## Redis key space

`pilot:{id}:evaluation:v1:{fingerprint}` (5 min), `portfolio:{id}:summary:v1:{fingerprint}` (2 min),
`optimization:v1:{fingerprint}` (2 min), `semantic:{fingerprint}:v1` (5 min). The fingerprint hashes every input (pilot
records incl. version, peers, budget, basis, thresholds, `rules_version`), so a changed input can never hit an old entry.
Writes also invalidate by prefix. Audit logs and decisions are never cached.

## AI decisions (what we deliberately did NOT build)

- **No adoption-risk ML model.** There is no real adoption data; a model trained on synthetic rows we generate would only
  re-learn our generator's formula and its "accuracy" would be meaningless. Five fixture rows cannot train anything.
- **No sentence-transformers.** For 5–8 short descriptions a ~400 MB model adds startup cost and version-dependent
  non-determinism for no demonstrated gain. The lexical-domain method is transparent, deterministic and evaluated.
- **No LLM call.** Explanations are template-generated from engine output and pass a grounding validator. The validator
  is built to guard a future LLM generator (it is tested against 130 adversarial hallucinations).

## Legacy prototype

`decision-engine/app/main.py` + `core.py` is the original v0 prototype (19 tests, unchanged and passing). It is
superseded by the Go API for all business decisions. Known intentional differences in `c23-rules-v1`: quality failure is
STOP (v0: REVISE); unknown overlap group is never treated as standalone (v0 did); missing running cost is not imputed
unless a scenario explicitly opts in (v0 always imputed 20%).
