# C23 AI Pilot Investment Decision Desk

> An enterprise decision desk and quantitative analytics platform for evaluating, prioritizing, ranking, and making stop-rule decisions on AI pilot initiatives.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [System Components](#system-components)
  - [Decision Engine (FastAPI)](#decision-engine-fastapi)
  - [Backend API Gateway (Go Gin)](#backend-api-gateway-go-gin)
  - [Frontend Web Dashboard (React / Vite)](#frontend-web-dashboard-react--vite)
- [Project Directory Structure](#project-directory-structure)
- [Prerequisites](#prerequisites)
- [Quick Start Guide](#quick-start-guide)
  - [1. Database Setup](#1-database-setup)
  - [2. Start the Decision Engine](#2-start-the-decision-engine)
  - [3. Start the Backend API](#3-start-the-backend-api)
- [Automated Scripts Reference](#automated-scripts-reference)
  - [Startup Scripts (`start.sh` & `start.ps1`)](#startup-scripts-startsh--startps1)
  - [Integration Test Scripts (`test.sh` & `test.ps1`)](#integration-test-scripts-testsh--testps1)
- [Running Unit & Integration Tests](#running-unit--integration-tests)
- [AWS Deployment with OpenTofu](#aws-deployment-with-opentofu)
- [Knowledge Graph & Architecture Map](#knowledge-graph--architecture-map)
- [API Reference](#api-reference)
  - [Health & Readiness](#health--readiness)
  - [Evaluation & Analytics](#evaluation--analytics)
  - [Portfolios Management](#portfolios-management)
  - [Observations & Stop-Rule History](#observations--stop-rule-history)
  - [MBA Interviews](#mba-interviews)
  - [Proposals](#proposals)
- [Configuration Reference](#configuration-reference)
  - [Backend Configuration (`backend/.env`)](#backend-configuration-backendenv)
  - [Decision Engine Configuration (`decision-engine/.env`)](#decision-engine-configuration-decision-engineenv)
- [Database Schema & Migrations](#database-schema--migrations)
- [Troubleshooting & FAQ](#troubleshooting--faq)

---

## Overview

The **C23 AI Pilot Investment Decision Desk** is a dual-service architecture designed to eliminate guesswork from AI capital allocation. It evaluates incoming AI use cases across organizational units (finance, customer support, operations, catalog quality), stress-tests their assumptions against multiple adoption scenarios, enforces group-overlap constraints, computes financial payback periods, and generates auditable decision records.

### Key Capabilities

1. **Deterministic Quantitative Finance**:
   - Net Annual Benefit (`benefit - running cost`).
   - Payback Period in months (`setup cost / net benefit * 12`).
   - Breakeven Adoption Percentage.
   - Three-point scenario sensitivity modeling (`bad`, `base`, `good`).
2. **Dual-Mode Optimization**:
   - **`independent`**: Baseline optimistic projection without cross-dependency haircuts.
   - **`conservative`**: Haircuts non-cashable time savings by 70% (0.30 multiplier), factors organizational readiness (`low`: 0.6, `medium`: 0.8, `high`: 1.0), and enforces adoption curve scaling.
3. **Knapsack Portfolio Selection with Overlap Rejection**:
   - Maximizes net returns under a hard capital budget (e.g., INR 600,000).
   - Rejects overlapping initiatives targeting the same benefit pool (e.g., duplicate support ticket automations).
   - Strict 6-tier tie-breaking hierarchy: `net benefit` $\rightarrow$ `payback speed` $\rightarrow$ `readiness` $\rightarrow$ `adoption` $\rightarrow$ `setup cost` $\rightarrow$ `lexical ID`.
4. **Dynamic Stop-Rule Engine**:
   - Continuously evaluates observed pilot adoption and quality against breakeven thresholds.
   - Yields deterministic recommendations: `CONTINUE`, `REVISE`, or `STOP`.
5. **Fail-Open Deterministic Caching**:
   - Redis cache keyed by SHA-256 hash of canonical JSON requests and rules versioning.
   - Resilient fail-open design: if Redis is unavailable or slow, requests bypass the cache seamlessly.

---

## Architecture

```mermaid
flowchart TD
    subgraph Client ["Clients & Integration"]
        Browser["Web UI / Dashboard (Vite/React)"]
        Curl["curl / CLI Scripts (test.sh)"]
    end

    subgraph Backend ["Backend Gateway (Go Gin :8080)"]
        Router["HTTP Router & Middleware"]
        Handlers["API Handlers (/api/v1)"]
        Store["Store Layer (pgxpool)"]
        CacheClient["Cache Layer (go-redis v9)"]
        RulesClient["Rules HTTP Client"]
    end

    subgraph DecisionEngine ["Decision Engine (Python FastAPI :8000)"]
        FastAPI["FastAPI Application"]
        Core["core.py (Pure Finance & Logic)"]
        Reports["reports.py (CSV / Markdown Exporters)"]
        DataFiles["starting_state.json & extension.json"]
    end

    subgraph Storage ["Infrastructure"]
        Postgres[("PostgreSQL 14+\nDatabase: c23")]
        Redis[("Redis 6+\nPort: 6379")]
    end

    Browser -->|HTTP| Router
    Curl -->|HTTP| Router
    Router --> Handlers

    Handlers -->|Store & Fetch| Store
    Handlers -->|Check Cache| CacheClient
    Handlers -->|Proxy / Compute| RulesClient

    Store -->|SQL Queries & Migrations| Postgres
    CacheClient -->|Rules Response Cache| Redis
    RulesClient -->|POST /pilots, /portfolio| FastAPI

    FastAPI --> Core
    FastAPI --> Reports
    FastAPI --> DataFiles
```

---

## System Components

### Decision Engine (FastAPI)
- **Directory**: [`decision-engine/`](decision-engine)
- **Runtime**: Python 3.10+ (tested on Python 3.14)
- **Framework**: FastAPI, Uvicorn, Pydantic
- **Role**: Pure financial modeling, sensitivity analysis, knapsack portfolio optimization, CSV/Markdown dossier generation.
- **Port**: `8000`
- **Swagger Docs**: `http://localhost:8000/docs`

### Backend API Gateway (Go Gin)
- **Directory**: [`backend/`](backend)
- **Runtime**: Go 1.25+ (required by `backend/go.mod`; tested on Go 1.27)
- **Framework**: Gin, pgx v5, go-redis v9, goose v3
- **Role**: CORS, transaction management, persistence of portfolios, proposals, audit evaluations, qualitative interviews, and caching.
- **Port**: `8080`

### Frontend Web Dashboard (React / Vite)
- **Directory**: [`frontend/`](frontend)
- **Runtime**: Node.js 20+ (CI uses 22)
- **Framework**: React 18, TypeScript, Vite, Zustand
- **Role**: Single-page application for real-time pilot portfolio evaluation, budget constraints, qualitative interviews, health telemetry, and decision auditing. The operating screens (Phase 7) are:
  - **Decision workspace** (`/`): pilot table, assumptions, portfolio builder, stop-rule panel, advice and next-best actions per pilot.
  - **Portfolio optimization** (`/optimize`): the same branch-and-bound solver, standalone.
  - **Compare proposals** (`/compare`): Phase 4 flow — balance-sheet upload with an AI-spend row picker, next-FY token plan, proposal CSV import review, the three-proposal comparison table with KPI cards, cash-flow chart, ROI/break-even bars and the recommendation with the burn beside it.
  - **Balance sheet** (`/balance-sheet`): the AI-spend parser and confirm flow on its own screen.
  - **What-if and sweep** (`/what-if`): base-vs-changed for one pilot plus the one-variable sensitivity ladder.
  - **Evidence and decision** (`/evidence`): per-pilot evidence band, advice outcome, stop rule and Phase 6 checkpoint, with the dossier/CSV downloads.
  - **Pilot tracker** (`/tracker`): 30-day plans, expected-vs-actual gaps, status moves (active/revise/stopped/completed) and the reallocation recommendation.
  - **Decision memo** (`/memo`): the one-page markdown memo with provenance tags, plan section and audit trail.
  - Plus the records screens: saved portfolios, proposals (versioned inputs + evidence), observations and stop rules, interviews.
- **Port**: `5173`
- **URL**: `http://localhost:5173`

---

## Project Directory Structure

```text
fynd/
├── .github/
│   └── workflows/
│       └── ci.yml                      # GitHub Actions CI pipeline (DE, Backend & E2E tests)
├── backend/
│   ├── cmd/
│   │   └── api/
│   │       └── main.go                 # Backend entrypoint (server, migrations and bootstrap CLI)
│   ├── db/
│   │   └── init.sql                    # Initial PostgreSQL user & database setup
│   ├── internal/
│   │   ├── cache/                      # Redis fail-open cache implementation
│   │   ├── config/                     # Environment configuration loader
│   │   ├── httpapi/                    # Gin router, handlers, middleware, auth/roles/rate limit, uploads, workspace views
│   │   ├── jobs/                       # Upload processing worker (claims queued uploads, retries with backoff)
│   │   ├── migrations/                 # Goose embedded SQL migrations (00001 to 00012)
│   │   ├── model/                      # Shared Go data structs
│   │   ├── rules/                      # HTTP client talking to Decision Engine
│   │   └── store/                      # PostgreSQL pgx store & migration runners
│   ├── .env.example                    # Backend environment template
│   ├── .golangci.yml                   # Go linter configuration
│   ├── go.mod                          # Go module dependencies
│   ├── go.sum                          # Go module checksums
│   ├── start.sh                        # Backend Linux/macOS startup script
│   └── start.ps1                       # Backend Windows PowerShell startup script
│
├── decision-engine/
│   ├── app/
│   │   ├── core.py                     # Quantitative finance, scoring, knapsack logic
│   │   ├── cashflow.py                 # Multi-period cash flow (ramp, delay, NPV, break-even)
│   │   ├── forecast.py                 # Token cost model (PWM)
│   │   ├── whatif.py                   # Base-vs-changed overrides, flip scan, sweeps
│   │   ├── ingest.py                   # CSV ingest: balance sheet + proposals (Phase 4)
│   │   ├── compare.py                  # Section-2 proposal comparison rule (Phase 4)
│   │   ├── advice.py                   # Evidence bands, outcomes, next-best actions (Phase 5)
│   │   ├── solver.py                   # Branch-and-bound portfolio solver (Phase 5.5)
│   │   ├── lifecycle.py                # Checkpoint outcomes, 30-day plan, reallocation (Phase 6)
│   │   ├── main.py                     # FastAPI routes & payload schemas
│   │   └── reports.py                  # CSV & Markdown report formatters
│   ├── data/
│   │   ├── starting_state.json         # Base pilot fixture dataset
│   │   └── extension.json              # Extension schema and default overrides
│   ├── tests/
│   │   ├── test_core.py                # Engine core tests (gate stays constant)
│   │   ├── test_cashflow.py            # Multi-period conventions tests
│   │   ├── test_forecast.py            # Token model tests
│   │   ├── test_whatif.py              # Phase 3 what-if / sweep tests
│   │   ├── test_ingest.py              # Phase 4 CSV ingest parsers
│   │   ├── test_compare.py             # Phase 4 comparison rule
│   │   ├── test_advice.py              # Phase 5 evidence / advice tests
│   │   ├── test_solver.py              # Phase 5.5 solver tests
│   │   └── test_lifecycle.py           # Phase 6 checkpoint / plan / reallocation / memo tests
│   ├── .env.example                    # Decision engine environment template
│   ├── pytest.ini                      # Pytest discovery and pythonpath configuration
│   ├── requirements.txt                # Python dependencies (fastapi, uvicorn, etc.)
│   ├── start.sh                        # Decision Engine Linux/macOS startup script
│   └── start.ps1                       # Decision Engine Windows PowerShell startup script
│
├── frontend/
│   ├── e2e/
│   │   └── screens.spec.ts             # Playwright browser checks for the operating screens
│   ├── src/
│   │   ├── api/                        # Split-by-resource API client: http.ts core + core/workspace/observations/compare/portfolios/history/lifecycle/proposals/interviews modules composed into one flat `api` barrel (client.ts)
│   │   ├── components/                 # UI components, layout shell, modals, fields
│   │   ├── domain/                     # Quantitative calculation & currency formatters
│   │   ├── features/
│   │   │   ├── compare/                # Phase 4 balance sheet + comparison (Compare, BalancePanel)
│   │   │   ├── lifecycle/              # Phase 6/7 screens: WhatIf, EvidenceDecision, Tracker, Memo
│   │   │   ├── portfolios/             # Saved portfolios and the export buttons
│   │   │   ├── proposals/              # Versioned proposals + evidence
│   │   │   ├── observations/ interviews/ # Records screens
│   │   │   ├── workspace/              # Decision workspace stages
│   │   │   └── system/                 # Checks page, about/limitations page
│   │   ├── pages/SimplePages.tsx       # Route page shells (Head + feature composition)
│   │   └── state/                      # Zustand workspace & theme state stores
│   ├── index.html                      # Single page application entry HTML
│   ├── package.json                    # NPM scripts (dev/build/preview/typecheck/test/test:e2e)
│   ├── playwright.config.ts            # Playwright e2e configuration (BASE_URL)
│   ├── tsconfig.json                   # TypeScript configuration
│   ├── start.sh                        # Frontend Linux/macOS startup script
│   ├── start.ps1                       # Frontend Windows PowerShell startup script
│   └── vite.config.ts                  # Vite build, dev server and preview proxy configuration
│
├── graphify-out/
│   ├── graph.html                      # Interactive 2D/3D knowledge graph visualizer
│   ├── graph.json                      # GraphRAG dataset (1,359 nodes, 3,722 edges, 69 communities)
│   └── GRAPH_REPORT.md                 # Knowledge graph report & community detection audit
│
├── main.tf                             # OpenTofu root configuration (AWS EC2 & automated deploy)
├── variables.tf                        # OpenTofu input variables definition
├── outputs.tf                          # OpenTofu output endpoints (IP, API URLs, SSH)
├── tofu.tfvars.example                 # OpenTofu variables configuration template
├── .gitignore                          # Git ignore rules
├── LICENSE                             # License file
├── test.sh                             # Automated API integration tests (Bash)
├── test.ps1                            # Automated API integration tests (PowerShell)
└── README.md                           # Documentation
```

---

## Prerequisites

Before running the application, ensure the following tools are installed:

| Tool | Minimum Version | Description |
| :--- | :--- | :--- |
| **Go** | 1.25+ | Backend server runtime |
| **Node.js** | 20+ | Frontend build and dev server |
| **Python** | 3.10+ | Decision engine runtime |
| **PostgreSQL** | 14+ | Primary database |
| **Redis** | 6.0+ | Deterministic caching engine |
| **curl** & **jq** | Any recent | For API testing and scripts |

### OS Package Installation Examples

#### Ubuntu / Debian
```bash
sudo apt update
sudo apt install -y golang-go python3 python3-venv python3-pip postgresql redis-server curl jq
```

#### macOS (Homebrew)
```bash
brew install go python postgresql redis curl jq
brew services start postgresql
brew services start redis
```

#### Windows
Install via [winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/):
```powershell
winget install GoLang.Go Python.Python.3.12 PostgreSQL.PostgreSQL Redis
```

---

## Quick Start Guide

### 1. Database Setup

1. Run the initial database setup script as the PostgreSQL superuser to create the `c23` role and databases:
   ```bash
   sudo -u postgres psql -f backend/db/init.sql
   ```
2. By default, `backend/.env.example` sets:
   ```env
   DATABASE_URL=postgres://c23:CHANGE_ME@localhost:5432/c23?sslmode=disable
   ```
   *(If you customized the password in `init.sql`, update `backend/.env` accordingly).*

3. Run migrations:
   ```bash
   cd backend
   go run ./cmd/api migrate up
   ```

### 2. Start the Decision Engine

Open **Terminal 1** and run:

```bash
# Linux / macOS
cd decision-engine
./start.sh

# Windows (PowerShell)
cd decision-engine
.\start.ps1
```

> **What `start.sh` does automatically:**
> - Verifies Python 3 runtime.
> - Copies `.env.example` to `.env` if `.env` does not exist.
> - Detects or creates a Python virtual environment (`.venv`).
> - Installs missing packages from `requirements.txt`.
> - Checks if port `8000` is free.
> - Starts the FastAPI server: `uvicorn app.main:app --reload --port 8000`.

### 3. Start the Backend API

Open **Terminal 2** and run:

```bash
# Linux / macOS
cd backend
./start.sh

# Windows (PowerShell)
cd backend
.\start.ps1
```

> **What `start.sh` does automatically:**
> - Verifies Go compiler.
> - Copies `.env.example` to `.env` if `.env` does not exist.
> - Downloads all Go module dependencies (`go mod download`).
> - Verifies if Redis is running on `localhost:6379`; if down, automatically attempts to start the Redis server/daemon.
> - Checks PostgreSQL connectivity on port `5432`.
> - Checks if Decision Engine is online on `http://localhost:8000`.
> - Checks if port `8080` is available.
> - Starts the Gin backend: `go run ./cmd/api`.

### 4. Start the Frontend

Open **Terminal 3** and run:

```bash
# Linux / macOS
cd frontend
./start.sh

# Windows (PowerShell)
cd frontend
.\start.ps1
```

> **What `start.sh` does automatically:**
> - Verifies Node.js 20+ and npm.
> - Runs `npm ci` when `node_modules` is missing (only needed on the first run). `npm ci` installs exactly what `package-lock.json` pins and never rewrites it.
> - Checks the Go gateway on `:8080` (the Vite proxy target) and warns when it is not up yet.
> - Checks if port `5173` is available (Vite picks the next free port when taken).
> - Starts the Vite dev server: `npm run dev` → `http://localhost:5173`.

The browser talks to the Go gateway only (`/api`, `/health`, `/ready` proxied), so the page and API share one origin — no CORS. Set `VITE_API_BASE` to point the built bundle at a deployed gateway instead.

---

## Automated Scripts Reference

### Startup Scripts (`start.sh` & `start.ps1`)

All three services — `backend/`, `decision-engine/` and `frontend/` — feature self-healing startup scripts that execute pre-flight checks, install any missing prerequisites, and provide a clear status report:

```text
=======================================================
            BACKEND PRE-FLIGHT CHECKS                  
=======================================================
[INFO] Verifying and downloading Go modules ...

Check Results:
  [PASS] Go Runtime: go1.26.4
  [PASS] Environment File: backend/.env is present
  [PASS] Dependencies: Go modules verified and downloaded
  [PASS] Redis Service: Running on port 6379 (PONG)
  [PASS] PostgreSQL: Accepting connections on localhost:5432
  [PASS] Decision Engine: Reachable on port 8000
  [PASS] Port 8080: Available

=======================================================
        ALL CHECKS PASSED - STARTING APP               
=======================================================
Command: go run ./cmd/api
Backend API URL: http://localhost:8080
```

If any check fails (e.g. Go missing, Python venv missing), the script exits with code `1` and prints an actionable checklist under **"What is left to install / fix"**.

---

### Integration Test Scripts (`test.sh` & `test.ps1`)

Located at the repository root, [`test.sh`](test.sh) and [`test.ps1`](test.ps1) run automated end-to-end integration tests using `curl` and `jq`.

```bash
# Linux / macOS
./test.sh

# Windows (PowerShell)
.\test.ps1
```

#### Tests Executed:
1. **Readiness Check**: Verifies PostgreSQL, FastAPI, and Redis connectivity:
   ```bash
   curl -s http://localhost:8080/ready | jq
   ```
2. **Dual-Mode Evaluation**: Computes independent and conservative scenarios side-by-side:
   ```bash
   curl -s -X POST http://localhost:8080/api/v1/evaluate -d '{}' | jq
   ```
3. **Save Portfolio**: Persists a portfolio with constraints to PostgreSQL:
   ```bash
   curl -s -X POST http://localhost:8080/api/v1/portfolios \
     -H "Content-Type: application/json" \
     -d '{"name":"run_1","mode":"independent","budget":600000}' | jq
   ```
4. **30-Day Plan (Phase 6)**: Generates and stores the engine plan for AI1 (`POST /api/v1/plans`).
5. **Observation (Phase 6)**: Records adoption/quality actuals with the auto decision (`POST /api/v1/pilots/AI1/observations`).
6. **Plan Progress (Phase 6)**: Expected-vs-actual gaps and the checkpoint verdict re-run (`GET /api/v1/plans/AI1` → +12/+5 pp, `extend`).
7. **Checkpoint Preview (Phase 6)**: Outcome with the stop rule carried beside it (`POST /api/v1/checkpoint/AI1`).
8. **Plan Status Moves (Phase 6)**: `revise` (arms the "fails again after a revise" rule) then `stopped` (frees the budget).
9. **Reallocation (Phase 6)**: Free the stopped pilot's 200000 as a recommendation only (`POST /api/v1/reallocation` → `"accepted":false`).
10. **Decision Memo (Phase 6)**: One-page markdown with the stored plan, provenance tags and audit trail (`POST /api/v1/export/memo`).

#### Guaranteed Data Cleanup:
Any data created during the test run is guaranteed to be deleted before the script exits (using `trap ... EXIT` in Bash and `try { ... } finally { ... }` in PowerShell):
- The portfolio `run_1` is deleted via `DELETE /api/v1/portfolios/run_1` (cascading to all portfolio pilot items).
- The deletion is independently verified (`404 Not Found`).
- Any evaluation audit records created during the test run in the `evaluations` table are pruned from PostgreSQL.
- The Phase 6 rows created during the run are pruned as well: the `pilot_plans` row for AI1 (deleted by pilot id — the test itself replaces any same-pilot plan), and the `observations` / `decisions` rows with ids above the pre-recorded maxima.

---

## Running Unit & Integration Tests

### 1. Decision Engine Unit Tests
Runs 131 automated tests covering financial arithmetic, the cash-flow conventions (ramp, delay, NPV, break-even), scenarios, what-if overrides and sweeps, the CSV ingest parsers (balance sheet + proposal import), the section-2 comparison rule, the Phase 5 advice/solver modules, the exact-search agreement and the Phase 6 lifecycle (checkpoint outcomes, 30-day plan, reallocation, decision memo):
```bash
cd decision-engine
./venv/bin/python -m pytest -q
# counts: test_core + test_cashflow + test_forecast + test_whatif + test_ingest + test_compare + test_advice + test_solver + test_lifecycle = 131

# or with active virtualenv:
python -m pytest -q
```

### 2. Backend Unit Tests
Runs Go unit tests across cache, config, router, rules client, and store:
```bash
cd backend
go test -v ./internal/...
```

### 3. Full Integration Test Suite
```bash
./test.sh
```

### 3. Frontend Unit Tests (Vitest)
Runs the pure-formatter and domain tests:
```bash
cd frontend
npm test
```

### 4. Browser End-to-End Checks (Playwright)
Walks the operating screens in a real browser — including the public check (AI1 net 1,20,000/yr on the evidence screen), the what-if base-vs-changed table and sweep ladder, the tracker plan/progress/reallocation loop and the rendered memo. Needs the full stack (Postgres, Redis, engine on :8000, gateway on :8080) and the Vite server on :5173:
```bash
cd frontend
npm run test:e2e
```

### 5. Continuous Integration (GitHub Actions)
Automated testing is orchestrated via [`.github/workflows/ci.yml`](.github/workflows/ci.yml) on push, pull requests, and manual dispatch:
- **`test-decision-engine`**: Installs dependencies and executes all Pytest tests for the Python decision engine (`python -m pytest -v`, 131 at the time of writing).
- **`test-backend`**: Starts PostgreSQL 16 and Redis 7 service containers and runs all Go unit tests and store integration tests with race detection (`go test -v -race ./internal/...`).
- **`test-integration`**: Boots live PostgreSQL and Redis services, executes database migrations (`go run ./cmd/api migrate up`), starts both the Decision Engine and Gin API Gateway in the background, runs [`test.sh`](test.sh) (all 10 checks), and verifies guaranteed cleanup.
- **`test-frontend`**: Typechecks (tsc), builds the production bundle and runs the Vitest unit tests.
- **`test-browser-e2e`**: Builds the frontend, serves it with `vite preview`, boots the full stack behind it and runs the Playwright browser checks (`frontend/e2e/`).

---

## AWS Deployment with OpenTofu

The repository includes complete OpenTofu Infrastructure-as-Code to provision an AWS EC2 instance and automatically deploy the full C23 platform (Go Gin Backend, Python Decision Engine, PostgreSQL, and Redis).

### Architecture Overview

OpenTofu provisions:
- **Compute**: An EC2 instance (Ubuntu 24.04 LTS, `t3.small` with 20 GB gp3 storage).
- **Networking & Security**: A dedicated security group opening port `8080` (Go Gin API Gateway). Port `22` (SSH) opens only for the CIDRs in `allowed_ssh_cidr`, and port `8000` (FastAPI Decision Engine) only for the CIDRs in `decision_engine_public_cidr`. Both default to closed: the gateway reaches the engine on `localhost`.
- **Automated Cloud-Init (`user_data`)**:
  - Automatically installs Go, Python 3, PostgreSQL, and Redis on boot.
  - Configures PostgreSQL role `c23` and databases `c23` & `c23_test`.
  - Clones the repository to `/opt/c23`.
  - Sets up the Python virtual environment and enables systemd service `c23-decision-engine.service`.
  - Compiles the Go binary, applies Goose database migrations, and enables `c23-backend.service`.
  - Restarts and monitors all services with systemd.

### Deployment Steps

1. **Configure Variables**:
   Copy the example variables file:
   ```bash
   cp tofu.tfvars.example tofu.tfvars
   ```
   Optionally configure your SSH public key, preferred AWS region, or database password.

2. **Provide AWS Credentials**:
   Export your AWS credentials via environment variables or configure via AWS CLI:
   ```bash
   export AWS_ACCESS_KEY_ID="your-access-key-id"
   export AWS_SECRET_ACCESS_KEY="your-secret-access-key"
   export AWS_REGION="us-east-1"
   ```

3. **Initialize and Deploy**:
   ```bash
   tofu init
   tofu plan
   tofu apply
   ```

4. **Access the Application**:
   When deployment completes, OpenTofu outputs your live endpoints:
   ```bash
   tofu output
   ```
   - **Backend API**: `http://<PUBLIC_IP>:8080/health`
   - **Readiness Check**: `http://<PUBLIC_IP>:8080/ready`
   - **Decision Engine API Docs**: `http://<PUBLIC_IP>:8000/docs` (only when `decision_engine_public_cidr` includes your IP)
   - **SSH Access**: `ssh ubuntu@<PUBLIC_IP>` (only when `allowed_ssh_cidr` includes your IP)
   - **Browser origins**: set `allowed_origins` (comma-separated, exact origins) when a frontend on another origin calls the gateway

5. **Teardown**:
   To destroy all provisioned AWS resources and stop charges:
   ```bash
   tofu destroy
   ```

---

## Knowledge Graph & Architecture Map

An automated, persistent knowledge graph of the entire repository is maintained in [`graphify-out/`](graphify-out):

- **Interactive Visualizer**: Open [`graphify-out/graph.html`](graphify-out/graph.html) directly in any browser for an interactive visualization of all 69 communities, god nodes, cross-package bridges, and data flows.
- **GraphRAG Dataset**: [`graphify-out/graph.json`](graphify-out/graph.json) provides the complete knowledge graph (1,359 nodes, 3,722 edges) for GraphRAG indexing, network analytics, and automated queries.
- **Audit & Cohesion Report**: [`graphify-out/GRAPH_REPORT.md`](graphify-out/GRAPH_REPORT.md) details community cohesion scores, centrality measurements, knowledge gaps, and architectural insights.

---

## API Reference

Base URL: `http://localhost:8080`

### Health & Readiness

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/health` | Basic liveness probe (returns `{"ok": true}`). |
| `GET` | `/ready` | Deep readiness probe checking PostgreSQL, Redis, and FastAPI rules engine. |

#### Example `/ready` Response:
```json
{
  "ready": true,
  "checks": {
    "postgres": "ok",
    "redis": "ok",
    "rules_engine": "ok"
  }
}
```

---

### Evaluation & Analytics

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/defaults` | Shipped starting state, extension configurations, and the visible `pipeline_rules` (cash factors, readiness factors, tie-break order, horizon, discount rate and every formula). |
| `POST` | `/api/v1/pilots` | Evaluates all pilots with payback periods, stop rules, scenarios, a multi-period cash-flow view (`independent.multi_period` / `conservative.multi_period`), a per-value `provenance` map, an organisation-level `forecast` block — plus the Phase 5 `advice` block per pilot (evidence band + input list, gaps by impact, decision outcome with reasons / what-would-change-it / next-best actions) and the Phase 6 `checkpoint` lens per pilot (`scale` / `extend` / `revise` / `stop` / `pending`; `pending` until real adoption **and** quality are recorded). |
| `POST` | `/api/v1/portfolio` | Runs knapsack optimization under a given budget and mode. Optional `objective`: `net` (default, unchanged public behaviour incl. the legacy exact search for ≤ 16 candidates), `npv` (discounted horizon flows) or `risk_adjusted` (net × readiness factor). Above 16 candidates a deterministic branch-and-bound solver runs (cap 40, node budget, `solver` meta with `nodes` / `capped`); > 40 is a 422, not a 500. The objective's value sum lands in `objective_value` on the generalized path. |
| `POST` | `/api/v1/portfolio/check` | Validates a manual pilot selection list against budget and conflicts. |
| `POST` | `/api/v1/evaluate` | Computes **both** `independent` and `conservative` portfolios simultaneously and records evaluation history. |
| `GET` | `/api/v1/evaluations` | Lists the evaluation audit log (newest first). Optional `limit`, clamped to 1–50 (default 20). |
| `GET` | `/api/v1/evaluations/:id` | Retrieves one audit row with the request and both answers as recorded. |
| `POST` | `/api/v1/stoprule/:id` | Previews stop-rule decision for a pilot given simulated adoption & quality. |
| `POST` | `/api/v1/whatif` | Base vs changed comparison for one pilot. Overrides (all optional): `setup_cost_inr`, `annual_running_cost_inr` (or explicit `null` to withdraw the recorded figure), `annual_benefit_inr`, `adoption_pct`, `readiness`, `observed_adoption_pct` / `observed_quality_pct`, `ramp_months`, `delay_months`, `budget`, `mode`, `horizon_months`, `discount_rate`, `tokens_per_month`, `price_per_1m_tokens`, `annual_usage_growth_pct`, `annual_price_drift_pct`. Returns base, changed, deltas and, when the stop-rule verdict flips, `verdict_flip` plus a `flip_statement` (e.g. "REVISE becomes GO when observed adoption reaches 60%."). Unknown keys and out-of-range values are 422s. Overrides never mutate stored data. |
| `POST` | `/api/v1/sweep` | One-variable sensitivity sweep. `parameter` ∈ `adoption_pct` (0–100 by 10), `annual_benefit_inr` / `annual_running_cost_inr` (0.5×–1.5× the base value in five steps), `price_per_1m_tokens` (same fractions of the recorded price), `annual_usage_growth_pct` (−20%…+30%). Returns the deterministic step ladder with `direction` and a `monotonic` check; sweeping an input the scenario does not know yields a flag and no numbers. Pilot-level sweeps need `pilot_id`. |
| `POST` | `/api/v1/balance-sheet/parse` | **Multipart upload** (`file`; optional `unit` ∈ auto/rupees/thousands/lakh/crore; optional `amount_column`; optional `sheet` for a workbook; optional `pages` such as `3-5,9` for a PDF). The file may be a **CSV**, an **Excel** workbook (`.xlsx`, `.xls`) or a **PDF** (see *Balance-sheet documents* below). A CSV reaches the engine byte for byte as before (UTF-8 with BOM, ≤ 2 MB); the engine auto-detects the money unit from the amount-column header, handles bracketed negatives, thousands separators and blanks, and returns AI-spend **candidates** (rows matched by the keywords ai / software / cloud / licence / token). Nothing is guessed: `ai_burn_to_date` stays `null` until confirm; no matching row yields the `BALANCE_NO_AI_ROW` gap flag. Nothing is stored. Parser rules are visible in `/api/v1/defaults` → `pipeline_rules.parse_rules`. The gateway adds to the engine's answer: `extraction` (format, sheet and sheet list, pages and table pages, left-out columns, currency, stated unit, notes), a `source` (sheet and row, or page, plus the line as read) and a `reason` on every candidate, and the flags `UNIT_ASSUMED`, `AMOUNT_COLUMN_CHOSEN`, `AMOUNT_MISSING` and, for documents, `UNIT_AMBIGUOUS`, `FORMULA_NO_VALUE`, `AMOUNT_SUFFIX_CR_DR`, `PDF_PAGES_NO_TEXT`, `PDF_COLUMNS_SHIFTED`. |
| `POST` | `/api/v1/balance-sheet/confirm` | **Multipart upload** of the same file (and the same `sheet` / `pages` / `unit` / `amount_column`) plus `rows` (candidate row numbers the user confirmed). The document is read the same way, so the row numbers still point at the same rows. Returns `ai_burn_to_date` (observed sum; a confirmed row without a readable amount is flagged, not zeroed) beside the next-FY AI forecast from the token assumptions (`next_fy` dashboard copy: run cost, next-FY cost, inflation flag); each confirmation also carries its `source`. Nothing is stored. |
| `POST` | `/api/v1/pilots/import/parse` | **Multipart upload** of a proposals CSV. Flexible headers (id / task / setup / savings / running cost / benefit type / adoption / readiness / ramp / delay / overlap group — full alias table in `/defaults`), per-row error review (`line`, `field`, `reason`), duplicates blocked. Returns the mapped rows plus `extension_pilots` (adoption/readiness) for review; nothing imported yet. |
| `POST` | `/api/v1/pilots/import/confirm` | **Multipart upload** of the same file plus `keep` (ids). Returns the confirmed rows in the engine's city-pilot shape (`pilots` for `fixture`, `extension_pilots` for `extension`), ready for `/pilots`, `/portfolio` or `/compare`; ids colliding with the shipped fixture are warned. Persistence stays in Go and Postgres. |
| `POST` | `/api/v1/compare` | Sections-2 three-proposal comparison, 2–8 proposals. Every proposal is scored by the same cash-flow engine; the plan's rule applies: proposals with non-positive net annual savings **or** break-even beyond the horizon are dropped ("3-year loss"), then the combination with the highest total net annual benefit within budget (greedy, ties by the 6-tier order) is funded; `overlap_group` makes proposals mutually exclusive. Returns per-proposal cash flow, ROI, profit, NPV, break-even, cash-vs-capacity split and rank, a **deterministic template recommendation** (e.g. "B recommended: highest net benefit within budget (2.5 L/yr net annual savings, break-even 24.0 months). … C dropped: 3-year loss (break-even beyond the horizon)."), the `head_to_head` explanations (why the pilot above outranks the one below: net, payback, readiness, overlap), the token-value-vs-price lens for the funded set and the assumptions echo. Without a budget every survivor is funded. Defaults: `mode` independent, horizon 36, r = 10% (overridable via `extension.defaults`). |
| `POST` | `/api/v1/export/csv` | Exports evaluated pilot metrics as downloadable CSV (`pilots.csv`). |
| `POST` | `/api/v1/export/evidence`| Exports markdown decision evidence dossier (`evidence.md`). |
| `POST` | `/api/v1/export/memo` | One-page markdown decision memo (`memo.md`, Phase 6): recommendation (advice outcome + stop rule + checkpoint), key numbers **with provenance tags**, evidence band and gaps, next actions, the 30-day plan and the audit trail. The gateway embeds the stored pilot plan and the latest observed actuals automatically; the engine remains stateless. |

#### Example `/api/v1/evaluate`:
```bash
curl -s -X POST http://localhost:8080/api/v1/evaluate \
  -H "Content-Type: application/json" \
  -d '{"budget": 600000}' | jq
```

---

### Balance-sheet documents (PDF, Excel, CSV)

`/balance-sheet/parse` and `/balance-sheet/confirm` accept real financial reports, not only CSV. The gateway reads a PDF, XLSX or XLS into the plain CSV the engine already understands and forwards that, so the **engine stays the only place that reads money**: it applies the unit, sums the confirmed rows and decides which rows look like AI spend. The gateway copies each figure as the document prints it (it never multiplies, rounds or re-groups an amount), records where every row came from, and refuses, with a reason, whatever it cannot read. A file is identified by its bytes, not its name.

| Format | How it is read | Source shown |
| :--- | :--- | :--- |
| **CSV** | Unchanged: the engine's own parser. | CSV line number |
| **XLSX / XLS** | Every sheet is read (numbers as the exact stored text, dates as dates, formulas by their stored result, hidden sheets marked). The sheet that looks like the statement is chosen; the others are listed so the reviewer can switch (`sheet`). A "Note" or "Ref" column is left out because note numbers are not amounts. | Sheet and row |
| **PDF** (text-based) | Positioned text is grouped into lines and cells, the amount columns are found by alignment, a table that runs over several pages stays one table, repeated headings and page footers are skipped, wrapped descriptions are joined to their amounts, and narrative pages are ignored. `pages` narrows a long report. | Page |

What it will not do, by design:

- **Scanned (image-only) PDFs are refused** (`scanned_document`). There is no OCR on the server, so a picture of a statement cannot be read; upload a text-based PDF or the spreadsheet the figures came from. A PDF whose fonts cannot be decoded is refused the same way. Pages without text inside an otherwise readable PDF are skipped and reported (`PDF_PAGES_NO_TEXT`).
- **Units are never assumed silently.** A unit stated in a title or column heading (lakh, crore, thousands, rupees) is passed to the engine in the column heading; none stated means the engine's default of rupees and a `UNIT_ASSUMED` flag; two different units mean no unit is applied and `UNIT_AMBIGUOUS`. Millions and billions are refused (`unit_not_supported`), and so is any currency other than INR (`currency_not_supported`).
- **Several amount columns** (this year and last year) are all kept; the engine uses the first and `AMOUNT_COLUMN_CHOSEN` names the others so the reviewer can choose (`amount_column`).
- A CSV row **wider than its header** (typically an unquoted `1,20,000`) makes the engine's parser fail with a bare server error; the gateway turns that into `csv_row_too_wide` naming the row.
- Reading a PDF is heuristic. It works on tables whose columns line up; a table that is drawn irregularly, or text drawn as pictures, can be read wrongly or missed. That is why nothing is counted until a person ticks the row, and why each row shows its source line.

Refusals use the usual error envelope with a request id. Codes: `empty_file`, `unsupported_format`, `file_encrypted`, `file_corrupt`, `scanned_document`, `no_table_found`, `sheet_not_found`, `invalid_pages`, `too_many_pages`, `document_too_large`, `currency_not_supported`, `unit_not_supported`, `extraction_timeout`, `csv_row_too_wide`, plus `payload_too_large` (413) and `busy` (429, with `Retry-After`). No document content is logged: only format, size, row count, duration and request id. Limits are configurable (`DOCUMENT_MAX_BYTES`, `DOCUMENT_MAX_PAGES`, `DOCUMENT_TIMEOUT_SECONDS`, `DOCUMENT_CONCURRENCY`; see the configuration table).

### Money over time, provenance and the token forecast (multi-period view)

Every pilot in `/api/v1/pilots` additionally carries:

- **`independent.multi_period` / `conservative.multi_period`** — profit over the horizon, ROI, NPV, break-even month (`null` + `breakeven_beyond_horizon: true` when it falls past the horizon, never invented), the cumulative monthly series, and the per-pilot `value_per_1m_tokens` with its `value_vs_price_ratio`.
- **`provenance`** — source of every decision-driving value: `fixture (synthetic)`, `observed`, `assumed (team)`, or `calculated`, each with a one-line substituted formula, e.g. `= INR 180,000 − INR 60,000 = INR 120,000/yr`.
- Top-level **`forecast`** — `ai_run_cost_inr`, `next_fy_cost_inr` and the `inflation_flag` (on when usage growth ≥ 0). Missing tokens or price yields `TOKENS_MISSING` / `TOKEN_PRICE_MISSING` flags and **no number** — missing is never zero.

Conventions (all stated in `/api/v1/defaults` → `pipeline_rules.formulas`): months are numbered from 1; the one-time cost lands in month 1; benefits start after `delay_months` and ramp linearly over `ramp_months` (`active/ramp_months`, capped at 1.0; 0 = full benefit from day one); the running cost is charged from month 1 while the pilot exists (waiting is not free); break-even is the first month with cumulative cash flow ≥ 0; NPV discounts month *t* by `(1+r)^(t/12)`, so a 0% rate makes NPV equal profit/loss.

**CSV money conventions (ingest)**: commas are thousands separators (Indian and western groupings both fine), the decimal point is `.` only, `(1,00,000)` is a negative, inline suffixes (`2.5 cr`, `1.2 L`, `50k`) win over the column's detected unit, and blanks (`—`, `n/a`) are missing — never zero. Named scenarios keep their multipliers (`bad ×0.5, base ×1.0, good ×1.25`); compare requests share the same horizon, discount and token assumptions through `extension.defaults`.

Editable per request through `extension.defaults` (shipped values are team-authored assumptions; send explicit `null` to withdraw one):

```json
{
  "horizon_months": 36, "discount_rate": 0.10,
  "tokens_per_month": 50000000, "price_per_1m_tokens": 250, "ai_fixed_cost_inr_per_year": 20000,
  "annual_usage_growth_pct": 20, "annual_price_drift_pct": -10
}
```

Pilots also accept additive inputs on the fixture or extension rows: `one_time_costs` / `recurring_costs` cost lines (replace the fixture setup / assumed run when supplied), a `benefit_driver` (`volume_per_month` × `minutes_saved_per_unit` × `loaded_cost_per_hour`, annualised), and `ramp_months` / `delay_months`. Malformed lines are dropped with a warning; nothing is silently zeroed.

**Named what-if scenarios** (`bad` / `base` / `good` on each pilot) multiply only the adoption assumption — benefit, cost lines, readiness and setup are held at base. The shipped spread `{"bad": 0.5, "base": 1.0, "good": 1.25}` is visible config in `/api/v1/defaults` → `pipeline_rules.scenario_multipliers` and editable per request through `extension.defaults.scenario_multipliers`; each scenario carries an `assumptions` block stating exactly that.

### Evidence strength, decision outcomes, next-best actions and why-A-over-B (Phase 5)

Each pilot in `/api/v1/pilots` carries an `advice` block:

- **Evidence band, never a confidence percentage** — the band is the weighted share of decision-driving inputs (`setup`, `run cost`, `benefit`, `adoption`, `quality`, `readiness`; weights visible in `/defaults` → `pipeline_rules.advice_rules.input_weights`) whose provenance is `observed` or `calculated` (real, user-supplied numbers). `assumed (team)` and `fixture (synthetic)` do not count. Band: share ≥ 0.75 → **high**, ≥ 0.40 → **medium**, else **low** (edges visible in `advice_rules.band_edges`). `fixture (synthetic)` money values **cap the band at Low**. The per-input list shows each input's source and weight; `gaps` lists every un-observed input ordered by impact on the decision.
- **Decision outcome** (`reject` / `insufficient_information` / `defer` / `stage` / `fund`, rule order fixed and visible in `advice_rules.outcome_order`) — reject on net ≤ 0; insufficient information when nothing at all is backed by real data; defer when break-even falls beyond the horizon; stage when it is payable but the band is Low; fund otherwise. Every outcome carries `reasons`, `what_would_change_it` and `reason_codes`; a pilot that is payable on the full-benefit view but negative under the conservative haircut gets the `CONSERVATIVE_DIES` caution instead of a false yes.
- **Next-best actions** — every non-fund outcome resolves to at least one action from the data-driven rule table in `/defaults` → `advice_rules.next_actions` (code → action, owner hint, expected test): unknown run cost → get vendor pricing (finance), no observed adoption → run one adoption spot-check (org lead), low readiness → clean the data first (data engineering), below-break-even adoption after measurement → short user-training experiment, and so on.

`/compare` responses carry `head_to_head`: adjacent ranked pairs ("why the pilot above outranks the one below") plus each funded pilot against the first same-group competitor it eliminated, with sentences over net, payback, readiness and the shared overlap group. Deterministic — same input, same sentences.

The portfolio solver (5.5) keeps the public contract: `net` objective with ≤ 16 candidates is the byte-identical legacy exact search; above that (or for the `npv` / `risk_adjusted` objectives) a deterministic branch-and-bound runs with the same tie-break contract, capped at 40 candidates (`pipeline_rules.solver` in `/defaults`); past the node budget it returns the best set found with `solver.capped: true` — same input, same traversal, same result.

### Pilot checkpoint, 30-day plan, reallocation and the decision memo (Phase 6)

The lifecycle rides on the stop rule — GO / REVISE / STOP stays authoritative; the checkpoint is an additional lens computed from the plan's own targets and the pilot's real evidence.

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/checkpoint/:id` | Checkpoint outcome for one pilot: `scale` (gates pass, evidence band at least medium, pilot ran the planned days), `extend` (gates pass but band low — e.g. money still fixture (synthetic) — or pilot younger than 30 days), `revise` (one gate fails while adoption stays at or above break-even; once), `stop` (below break-even, net ≤ 0, or a gate fails again after a revise) or `pending` (not both observed values recorded yet). The answer always carries the stop-rule `stop_rule` field beside it. Body accepts `observed_adoption_pct` / `observed_quality_pct`, `revised_before`, `duration_days` and any scenario fields; stored plan state and the latest observation fill the rest. Preview only — nothing is written. |
| `POST` | `/api/v1/plans` | Generates the 30-day checkpoint plan from the engine (`/plan`) and records it: objective, owner, adoption/quality targets, cost limit (setup + running pro-rated over the duration), duration, stop rule, status `planned`. Regenerating replaces the current plan (one plan per pilot). |
| `GET` | `/api/v1/plans` | Lists every recorded plan (newest update first). |
| `GET` | `/api/v1/plans/:id` | Tracker view: expected from the plan, **actual from the latest linked observation**, the adoption/quality gaps, and the checkpoint verdict re-run with actuals (`verdict: null` when the pilot is not in the loaded scenario — GET has no scenario body). |
| `POST` | `/api/v1/plans/:id/status` | Moves the plan through `planned` → `active` → `revise` / `stopped` / `completed`. Status `revise` is what makes the next checkpoint count as "gate fails again after a revise" (`revised_before`). |
| `POST` | `/api/v1/reallocation` | On stop: frees the stopped pilot's reservation, keeps every other funded pilot locked (their spend and groups are protected — a candidate sharing a locked group is excluded), and computes the next best use of the freed budget with the same deterministic optimizer. Scenario source: named saved `portfolio_name`, an `evaluation_id`, or the newest evaluation that funded this pilot. Ranks by net only (any other `objective` is a 422). A pure recommendation: `accepted: false`, nothing is written — accepting means saving the updated portfolio via `POST /api/v1/portfolios`. Only pilots that hold a budget can free one (404 when no evaluation or portfolio funded this pilot). |
| `POST` | `/api/v1/export/memo` | See the export table above — one-page decision memo from `reports.py`. |

Checkpoint rules are visible in `/api/v1/defaults` → `pipeline_rules.lifecycle_rules` (outcomes, planned duration 30 days, plan statuses). The plan's cost limit uses the money formulas already in the defaults (`setup + running × days/365`), never a hidden number.

### Portfolios Management

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/portfolios` | Saves a portfolio result with negotiated scope and pilot line items. |
| `GET` | `/api/v1/portfolios` | Lists all saved portfolios with summary statistics. |
| `GET` | `/api/v1/portfolios/:name` | Retrieves a full saved portfolio by unique name. |
| `DELETE` | `/api/v1/portfolios/:name` | Deletes a saved portfolio and cascades to its pilot line items. |

#### Example Save Portfolio:
```bash
curl -s -X POST http://localhost:8080/api/v1/portfolios \
  -H "Content-Type: application/json" \
  -d '{
    "name": "q4_approved_plan",
    "mode": "conservative",
    "budget": 600000
  }' | jq
```

---

### Observations & Stop-Rule History

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/pilots/:id/observations` | Records observed real-world adoption % and quality %; evaluates and stores stop-rule decision. Accepts an optional `proposal_id` to link the row to a stored proposal. |
| `GET` | `/api/v1/pilots/:id/history` | Fetches historical timeline of observations and resulting decisions for a pilot. |
| `GET` | `/api/v1/history` | Merged, newest-first timeline of evaluations and stop-rule decisions. Optional `limit`, clamped to 1–50 (default 25). Decision events carry `verdict`, `previous_verdict` (the pilot's earlier stop-rule outcome) and the observed values; evaluation events carry mode, budget, the funded set and a `change_summary` diff against the previous evaluation ("funded AI4; dropped AI2; budget INR 600000 → 400000"). |

---

### MBA Interviews

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/interviews` | Stores qualitative stakeholder interview evidence. |
| `GET` | `/api/v1/interviews` | Lists all qualitative interview evidence records. |

---

### Proposals

Persistent pilot candidates: each proposal is a named idea with versioned inputs, its own evidence trail, and optional links from observations and decisions. Storing inputs with the proposal keeps later measurements comparable over time. Archiving hides a proposal from the default list without losing its data.

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/proposals` | Creates a proposal (`name` plus optional `inputs` object and `notes`). Names must be unique within the workspace. |
| `GET` | `/api/v1/proposals` | Lists proposals. Active first, newest first. Add `?include_archived=true` to see archived ones too. |
| `GET` | `/api/v1/proposals/:id` | Retrieves one proposal by UUID, including its stored inputs. |
| `PATCH` | `/api/v1/proposals/:id` | Update `name`, `inputs`, `notes`. Omitted fields keep their stored value. |
| `POST` | `/api/v1/proposals/:id/archive` | Archives a proposal. |
| `POST` | `/api/v1/proposals/:id/restore` | Restores an archived proposal to active. |
| `DELETE` | `/api/v1/proposals/:id` | Permanently deletes the proposal and its evidence. Observations and decisions keep, with their link set to null. |
| `POST` | `/api/v1/proposals/:id/evidence` | Attaches an evidence row (`kind`: interview/measurement/document/assumption, plus `source`, `quality`, `note`). |
| `GET` | `/api/v1/proposals/:id/evidence` | Lists the evidence attached to the proposal. |
| `GET` | `/api/v1/proposals/:id/evaluations` | Evaluations run for this proposal, each with the inputs version it used. |
| `POST` | `/api/v1/proposals/:id/approvals` | *(admin)* Records `approved` / `rejected` / `changes_requested` on the proposal's **current inputs version**, with an optional `comment` and `snapshot_id` (the engine outcome the approver saw is stored with it). |
| `GET` | `/api/v1/proposals/:id/approvals` | Lists approvals, newest first; `stale: true` when the inputs changed after the approval. |

#### Example Create proposal:
```bash
curl -s -X POST http://localhost:8080/api/v1/proposals \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Invoice field extraction",
    "inputs": {"task": "Extract fields from scanned invoices", "volume_per_month": 400},
    "notes": "Candidate from the November ops review. Measure minutes saved per invoice."
  }' | jq
```

---

### Security, workspaces and members

Every stored record belongs to a **workspace** (one organisation). With `AUTH_MODE=off` (local development) everything runs in the default workspace as admin. With `AUTH_MODE=required`, every `/api/v1` call sends `Authorization: Bearer <token>`; `X-Workspace-ID` picks the workspace (optional when the user belongs to exactly one). Another organisation's workspace id answers `403 not_a_member`, the same as a workspace that does not exist. Roles gate the routes: **viewer** reads and runs calculations, **analyst** also creates and edits records and uploads, **admin** also deletes, approves, changes settings and manages members. Requests are rate limited per client IP (`429 rate_limited` with `Retry-After`). Every successful edit, export and file download is written to the audit trail with the signed-in user as actor.

The first user and token come from the command line (only the token's SHA-256 is stored):

```bash
cd backend
go run ./cmd/api bootstrap -email you@example.com -workspace "Finance"   # prints the token once
```

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/me` | Who is calling, the current workspace and role, and every workspace the user belongs to. |
| `GET` / `POST` | `/api/v1/workspaces` | List your workspaces / create one (you become its admin). |
| `GET` / `POST` / `DELETE` | `/api/v1/members`, `/api/v1/members/:user_id` | List members; *(admin)* add someone by `email` with a `role` or change their role; remove a member (the last admin cannot be removed). |
| `GET` / `POST` / `DELETE` | `/api/v1/tokens`, `/api/v1/tokens/:id` | Your API tokens: list, create (the raw token is returned once), revoke. |

### Budget settings and workspace views

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` / `PUT` | `/api/v1/settings` | The workspace decision context: `budget`, `mode`, `budget_set` (total / first-year / recurring / implementation / contingency), engine `defaults` (horizon, discount rate, token assumptions) and ranking `weights`. `PUT` is admin; unknown keys are 422. |
| `POST` | `/api/v1/workspace/dashboard` | Executive dashboard built from the workspace's stored proposals (`proposal_ids`, or every active proposal) and its settings. The latest observation linked to each proposal fills its observed adoption/quality. The engine answer is stored as a **snapshot** and returned as `{snapshot, result}`. `use_fixture: true` runs the shipped case instead. |
| `POST` | `/api/v1/workspace/decision`, `/workspace/comparison`, `/workspace/yearly-savings` | The same for the decision ranking, the section-2 comparison and the yearly savings view. |
| `POST` | `/api/v1/workspace/export/decision-report`, `/workspace/export/yearly-csv` | Downloads for the stored proposals (`decision-report.md`, `yearly-savings.csv`). |
| `GET` | `/api/v1/snapshots`, `/api/v1/snapshots/:id` | Stored snapshots; one snapshot comes back with `stale` and the reasons (a proposal's inputs changed, a proposal was deleted, the engine's rules version moved on). |
| `POST` | `/api/v1/snapshots/:id/replay` | Re-runs the stored request uncached and reports `matches: true` when today's engine reproduces the stored answer exactly. |
| `POST` | `/api/v1/economics`, `/decision`, `/views/dashboard`, `/views/yearly-savings`, `/pricing/quote`, `/pricing/optimise`, `/forecast/usage`, `/export/decision-report`, `/export/yearly-csv` | Cached pass-through to the engine endpoints of rules version 7+ for explicit proposal lists (see `decision-engine/docs/engine.md`). |

### Stored uploads and review

Documents are kept, not just parsed: the gateway validates the extension against the file signature (UTF-8 `.csv`, PDF, XLSX, XLS or DOCX), enforces `UPLOAD_MAX_BYTES`, stores the original with its SHA-256 (the same file uploaded again is the same upload), and a background worker sends it to the engine parser, retrying outages with backoff (5 s, 20 s, 45 s; three attempts). A file the engine rejects fails at once with the engine's reason. Status moves `pending_upload → received → processing → parsed | failed → confirmed` for S3 uploads and `received → processing → parsed | failed → confirmed` for legacy PostgreSQL uploads. Upload validation never trusts the browser MIME type.

When `S3_DOCUMENTS_BUCKET` is configured, the frontend requests a presigned PUT session from `/documents/upload-session`, uploads directly to private S3, and calls `/documents/:id/complete`. Completion verifies size, content type, SHA-256 metadata and the file signature before queueing the existing worker. Existing PostgreSQL-backed uploads remain readable and are not deleted or migrated automatically.

Proposal-document extraction is available for an uploaded proposal document through the workspace-scoped
routes below. It is deterministic and evidence-preserving by default. When Bedrock is explicitly enabled,
the same route uses the Bedrock Converse API; the stored warnings include `extraction_mode: deterministic`
or `extraction_mode: bedrock` so the source of each result is visible. PDF text layers, XLS/XLSX tables,
DOCX text, and CSV are read when possible.
Scanned PDFs and fields that are not stated clearly remain `missing` or `ambiguous` and must be corrected by
an analyst before confirmation. Every extracted value includes a source reference and excerpt.

| `POST` | `/api/v1/uploads/:id/proposal-extraction` | Start an idempotent extraction. Returns a `review` result with the exact 11 fields and evidence metadata. |
| `GET` | `/api/v1/uploads/:id/proposal-extraction` | Read the workspace-scoped extraction result and status. |
| `PATCH` | `/api/v1/uploads/:id/proposal-extraction` | `{field, value, note?}` records an analyst correction and marks that field explicitly reviewed. |
| `POST` | `/api/v1/uploads/:id/proposal-extraction/confirm` | Requires all 11 fields to be explicit; returns validated proposal `inputs` using `ai_running_cost_inr` for the engine contract. |

The extraction result is stored separately from `proposals.inputs` in migration `00015_proposal_extractions.sql`.
The review screen submits the returned `name` and `inputs` through the existing `POST /api/v1/proposals`
contract after confirmation, so normal proposal name and input validation still applies. The extraction
endpoint itself does not write proposal rows or evidence metadata into `proposals.inputs`.

#### Bedrock status and live-test setup

The Bedrock adapter uses the AWS SDK for Go v2 `Converse` operation and therefore requires a configured model
that supports Converse in the selected region. It sends extracted text and source references in a strict
JSON-only prompt, then validates the exact 11-field response, enums, null handling, and evidence server-side.
Missing credentials or model access never produces fabricated or success-shaped AI output. If Bedrock is
disabled, the deterministic extractor is used instead.
Server validation rejects model-derived INR amounts; annual benefits and operating costs must be explicitly
stated with evidence or remain null, so time-saved and productivity claims cannot become cash values.

The configuration boundary is opt-in and fails clearly when enabled without the required settings:

| Variable | Current meaning |
|---|---|
| `BEDROCK_ENABLED` | Set `true` only to select the Bedrock adapter; default `false`, which selects deterministic extraction. |
| `BEDROCK_REGION` | AWS region for Bedrock. There is no repository default; use a region where the selected model is available. |
| `BEDROCK_MODEL_ID` | Exact model ID enabled for the AWS account in `BEDROCK_REGION`; do not copy a model ID from another account or region. |
| `AWS_REGION` | Existing S3 region. Used as the Bedrock region fallback only when `BEDROCK_REGION` is empty. |

For a live Bedrock test, first enable model access in the AWS account for the selected region, then
configure `BEDROCK_ENABLED=true`, `BEDROCK_REGION` (or `AWS_REGION`), and the provider's exact
`BEDROCK_MODEL_ID`. The runtime IAM role should receive only `bedrock:InvokeModel` for that model's
resource ARN, in addition to the existing scoped S3 permissions documented above. Use the standard AWS
credential/provider chain; never put access keys in source or frontend configuration. The selected model
must support the Converse API; model-specific native `InvokeModel` request bodies are intentionally not
guessed. The adapter applies a 45-second timeout and relies on the SDK's bounded retry behavior for transient
failures. No live test has been run in this environment.

Live setup checklist:

1. Choose an AWS region where both the private documents bucket and an approved Bedrock Converse model
   are available. Record the exact model ID from the account's Bedrock model-access page; this repository
   intentionally does not prescribe a provider or model ID.
2. Run the API with the standard AWS credential chain: an attached task/instance role in deployment, or
   a locally configured AWS profile. Do not put access keys in `.env`, source control, or frontend code.
   Verify the identity separately with the operator's approved AWS tooling before starting the API.
3. Create or select a private S3 bucket with Block Public Access, default encryption, and CORS restricted
   to the deployed frontend origin. Grant the API role the scoped S3 object actions described below.
4. Grant the API role `bedrock:InvokeModel` only for the selected model resource. Confirm model access is
   enabled in the selected region.
5. Set `AWS_REGION`, `S3_DOCUMENTS_BUCKET`, `BEDROCK_ENABLED=true`, `BEDROCK_REGION` (unless it equals
   `AWS_REGION`), and `BEDROCK_MODEL_ID`. Restart the API; invalid or incomplete Bedrock settings fail
   startup with an actionable error.
6. Upload a non-sensitive proposal, complete the S3 object, start extraction, and verify the review result.
   Confirm that the stored warning identifies `extraction_mode: bedrock`, evidence references are present,
   unsupported fields remain missing or ambiguous, and the analyst confirmation is required before the
   existing proposal endpoint is called.

S3 setup requires a private bucket with Block Public Access enabled, default encryption (SSE-S3 or SSE-KMS), and CORS allowing only the deployed frontend origin for `PUT` and `HEAD` with the `Content-Type`, `Content-Length` and `x-amz-meta-sha256` headers. The gateway IAM role needs `s3:PutObject`, `s3:AbortMultipartUpload`, `s3:HeadObject`, `s3:GetObject` and `s3:DeleteObject` only on `arn:aws:s3:::BUCKET/workspaces/*`; it does not need `s3:*` or bucket-wide access. Set `AWS_REGION`, `S3_DOCUMENTS_BUCKET` and optionally `PRESIGNED_URL_EXPIRY_SECONDS`; credentials come from the standard AWS provider chain or the instance/task IAM role.

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/uploads` | *(analyst)* Multipart `file` + `kind` (`balance_sheet` / `pilots`) + optional `unit`, `period`, `amount_column`. `202` with the queued upload, or `200` with `duplicate: true`. |
| `GET` | `/api/v1/uploads`, `/api/v1/uploads/:id` | The queue; one upload with its parse result, its corrections and the review rows (original value beside corrected value). |
| `GET` | `/api/v1/uploads/:id/file` | The original file as an attachment (workspace-scoped, `nosniff`, audited). |
| `POST` | `/api/v1/uploads/:id/corrections` | *(analyst)* `{row_ref, field, value, note?}`: a reviewed value. The original is kept; balance sheets correct `amount_inr`, pilot imports correct money, benefit type, overlap group, ramp/delay, adoption, readiness or task. |
| `POST` | `/api/v1/uploads/:id/confirm` | *(analyst)* `{rows: [...]}` (balance sheet) or `{keep: [...]}` (pilots). The engine re-reads the stored file with the corrections applied, so the burn and the imported rows are still computed by the engine. |
| `POST` | `/api/v1/uploads/:id/retry` | *(analyst)* Puts a failed upload back in the queue. |

With S3 configured, the direct-upload contract is:

| Method | Endpoint | Request / response |
| :--- | :--- | :--- |
| `POST` | `/api/v1/documents/upload-session` | *(analyst)* `{filename, kind, size_bytes, content_type, sha256, unit?, period?, amount_column?}`. Returns `{upload, duplicate, upload_url, headers}`. The URL is short-lived and the key is generated by the gateway. |
| `PUT` | `upload_url` | Browser sends the original bytes directly to S3 with the returned `headers`; no gateway or AWS credentials are exposed. |
| `POST` | `/api/v1/documents/:id/complete` | No body. The gateway checks private-object existence, exact size/content type, SHA-256 metadata, and the file signature before moving `pending_upload` to `received`. Repeating a successful completion returns `duplicate: true`. |

---

## Configuration Reference

### Backend Configuration (`backend/.env`)

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the Go Gin HTTP server. |
| `DATABASE_URL` | *Required* | PostgreSQL connection string (`postgres://user:pass@host:5432/db`). |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL. Leave empty to run without cache. |
| `RULES_ENGINE_URL` | `http://localhost:8000` | URL of the Python FastAPI Decision Engine. |
| `RULES_VERSION` | *unset* | Fallback cache namespace, used only until the engine reports its own `rules_version` from `/health`. The cache key follows the engine automatically, so nobody edits this by hand. |
| `RULES_VERSION_POLL_SECONDS` | `30` | How often the gateway re-reads the engine's `rules_version`. |
| `ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated list of allowed CORS origins. |
| `REQUEST_TIMEOUT_SECONDS` | `5` | Request context timeout in seconds. |
| `CACHE_TTL_SECONDS` | `1800` | TTL in seconds for cached rule responses (0 disables cache). |
| `MAX_BODY_BYTES` | `1048576` | Maximum allowed request body size in bytes (default 1 MB). |
| `DOCUMENT_MAX_BYTES` | `26214400` | Largest PDF / Excel / CSV document on the balance-sheet routes (25 MB; at most 64 MB). A CSV keeps the `MAX_BODY_BYTES` limit. |
| `DOCUMENT_MAX_PAGES` | `500` | Most PDF pages read in one request; a longer report needs the `pages` choice. |
| `DOCUMENT_TIMEOUT_SECONDS` | `60` | Time allowed to receive and read one document (the usual request timeout stays short). |
| `DOCUMENT_CONCURRENCY` | `2` | Documents read at the same time; more get a `429` and are asked to retry. |
| `AUTO_MIGRATE` | `false` | If `true`, applies pending Goose migrations on server startup. |
| `GIN_MODE` | `release` | Gin server mode (`release` or `debug`). |
| `AUTH_MODE` | `off` | `off`: no sign-in, default workspace. `required`: bearer tokens, workspaces and roles (the OpenTofu deployment defaults to `required`). |
| `RATE_LIMIT_PER_MINUTE` | `600` | Requests per minute per client IP; `0` disables rate limiting. |
| `RATE_LIMIT_BURST` | `120` | Requests allowed at once above the per-minute rate. |
| `UPLOAD_MAX_BYTES` | `2000000` | Largest stored upload in bytes. The current rules-engine upload contract parses at most 2 MB; increase only when that contract and database capacity are updated together. |
| `AWS_REGION` | *unset* | AWS region used by the standard SDK credential/provider chain. Required with `S3_DOCUMENTS_BUCKET`. |
| `S3_DOCUMENTS_BUCKET` | *unset* | Private bucket for S3-backed document originals. When unset, legacy PostgreSQL byte-backed uploads remain available. |
| `PRESIGNED_URL_EXPIRY_SECONDS` | `600` | Presigned PUT lifetime, bounded to 60–3600 seconds. |
| `WORKER_ENABLED` | `true` | Run the upload processing worker in this gateway process. |
| `WORKER_POLL_MS` | `1000` | How often the worker looks for queued uploads (new uploads also wake it immediately). |

### Decision Engine Configuration (`decision-engine/.env`)

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `8000` | Port for the Uvicorn server. |
| `HOST` | `0.0.0.0` | Bind host address. |
| `RELOAD` | `true` | Enable hot code reloading for local development. |

---

## Database Schema & Migrations

The backend uses [Goose](https://github.com/pressly/goose) migrations located in [`backend/internal/migrations/`](backend/internal/migrations).

### Tables

1. **`portfolios`**: Master records of saved pilot selections with frozen starting state, negotiated scope, final evidence JSON, and the multi-period conventions in force (`horizon_months`, `discount_rate_pct` — NULL when unknown).
2. **`portfolio_pilots`**: Child rows for each pilot under a portfolio (`ON DELETE CASCADE`), including funding status, setup cost, run cost, net benefit, payback speed, and the multi-period outcomes (`profit_inr`, `roi_pct` — NULL when the engine did not report them).
3. **`proposals`** *(migration 00005)*: Persistent pilot candidates with a unique name, an `active`/`archived` status, and versioned `inputs` JSON so later measurements stay comparable.
4. **`evidence`** *(migration 00005)*: Evidence rows attached to a proposal (`interview` / `measurement` / `document` / `assumption`, with source, quality and note).
5. **`observations`**: Measured real-world adoption percentage, quality score, and evidence source for a pilot. Optionally linked to a proposal (`ON DELETE SET NULL`).
6. **`decisions`**: The deterministic stop-rule verdict (`GO`, `REVISE`, `STOP`) generated for each observation. Optionally linked to a proposal (`ON DELETE SET NULL`).
7. **`evaluations`**: Audit log of dual-mode evaluations computed through `/api/v1/evaluate`; readable via `GET /api/v1/evaluations`.
8. **`interviews`**: Qualitative interview records with business stakeholders.
9. **`pilot_plans`** *(migration 00007)*: One current 30-day checkpoint plan per pilot — objective, owner, adoption/quality targets, cost limit, duration, stop rule and lifecycle status (`planned` / `active` / `revise` / `stopped` / `completed`). Expected values come from the engine plan generator; actuals live in `observations`.
10. **`audit_events`** *(migration 00008)*: Every successful edit, export and download — action, entity, actor (the signed-in user), request id and identifiers only (never document contents).
11. **`proposals.version`, `evaluations.proposal_id/proposal_version`** *(migration 00009)*: The inputs version goes up only when the stored inputs change; an evaluation records the proposal version it used.
12. **`workspaces`, `users`, `memberships`, `api_tokens`** *(migration 00010)*: Organisations, people, roles per workspace and hashed API tokens. Every table above gained `workspace_id`; names (portfolios, proposals) and pilot plans are unique **per workspace**. Existing rows moved into the default workspace.
13. **`uploads`, `upload_corrections`** *(migration 00011)*: Stored originals with SHA-256, the processing job (status, attempts, next attempt, last error), the engine's parse and confirm answers, and reviewer corrections with original and corrected values.
14. **`snapshots`, `approvals`, `evaluations.rules_version`** *(migration 00012)*: Reproducible engine answers (request, response, proposal versions, rules and contract versions), approvals per proposal version, and the engine version behind each evaluation.

### Managing Migrations CLI

```bash
cd backend

# View status of applied and pending migrations
go run ./cmd/api migrate status

# Apply all pending migrations
go run ./cmd/api migrate up

# Roll back the most recent migration
go run ./cmd/api migrate down

# Create a user, a workspace and a sign-in token (needed when AUTH_MODE=required)
go run ./cmd/api bootstrap -email you@example.com -workspace "Finance"
```

### Shared lint and test commands

From the repository root: `make test` (engine, gateway, frontend), `make lint` (gofmt, go vet, golangci-lint when installed, TypeScript typecheck), `make contracts` (regenerate the engine contract examples). Gateway store tests run against a disposable Postgres when `TEST_DATABASE_URL` is set. Path ownership for reviews is in `.github/CODEOWNERS`.

---

## Troubleshooting & FAQ

### Port 8080 or 8000 is already in use
Check what process is holding the port and terminate it:
```bash
# On Linux / macOS
lsof -i :8080
lsof -i :8000
kill -9 <PID>

# On Windows
Get-NetTCPConnection -LocalPort 8080
Stop-Process -Id <PID>
```

### Redis is down or unreachable
The backend features a **fail-open** cache architecture. If Redis is down, the server will log a warning and route all requests directly to the FastAPI rules engine without failing incoming API calls. To start Redis manually:
```bash
# Linux
redis-server --daemonize yes

# Docker
docker run -d -p 6379:6379 --name redis redis:alpine
```

### `DATABASE_URL` contains placeholder `CHANGE_ME`
If connecting fails with password authentication errors:
1. Ensure the PostgreSQL role `c23` is created using `sudo -u postgres psql -f backend/db/init.sql`.
2. Check your `.env` password and verify connection:
   ```bash
   PGPASSWORD=CHANGE_ME psql -U c23 -h localhost -d c23
   ```
