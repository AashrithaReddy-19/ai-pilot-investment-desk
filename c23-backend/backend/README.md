# C23 Go API (`backend/`)

Go 1.24+, Gin, pgx (PostgreSQL), go-redis. Deterministic rules engine `c23-rules-v1`.

## Layout

```
cmd/api        public API server (env config; -healthcheck flag for containers)
cmd/devstack   local stack without Docker: embedded PostgreSQL + optional Redis-protocol cache + API
cmd/evidence   runs judge challenges through the engine -> reports/judge_evidence.{md,json}
cmd/bench      latency benchmark (remote URL or -inprocess with 4 configurations)
internal/money finance decision rules portfolio scenario domain fixture   pure engine (no I/O)
internal/store (migrations/) cache aiclient service httpapi config        infrastructure
```

## Run

```bash
go run ./cmd/devstack                         # embedded PostgreSQL :55432, API :8080, official fixture seeded
go run ./cmd/devstack -redis -fresh           # + cache, wipe dev DB first
DATABASE_URL=postgres://... go run ./cmd/api  # against any PostgreSQL (AUTO_MIGRATE=true applies migrations)
```
Configuration: see `../.env.example` (PORT, DATABASE_URL, REDIS_URL, AI_SERVICE_URL, AI_TIMEOUT_MS, CORS_ORIGINS,
RAW_DATA_DIR, MAX_BODY_BYTES, AUTO_MIGRATE, SEED_OFFICIAL, API_KEY). Secrets are redacted in startup logs.

## Migrate / import

Migrations are embedded SQL (`internal/store/migrations`) applied on startup (`AUTO_MIGRATE=true`) and recorded in
`schema_migrations`. Import the verified official fixture with `SEED_OFFICIAL=true` or `POST /api/v1/import/official`;
other data via `POST /api/v1/import` (all-or-nothing).

## Test

```bash
go test ./...                     # DB tests start embedded PostgreSQL (first run downloads binaries) or use TEST_DATABASE_URL
go test ./... -p 1                # required when TEST_DATABASE_URL points at one shared database
```

Results at handover: 106 Go test functions pass (0 fail); coverage 88.3% of `internal/` overall, 95.4% of the core engine.
