# C23 AI Pilot Investment Decision Desk

> An enterprise decision desk and quantitative analytics platform for evaluating, prioritizing, ranking, and making stop-rule decisions on AI pilot initiatives.

---

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [System Components](#system-components)
  - [Decision Engine (FastAPI)](#decision-engine-fastapi)
  - [Backend API Gateway (Go Gin)](#backend-api-gateway-go-gin)
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
- [API Reference](#api-reference)
  - [Health & Readiness](#health--readiness)
  - [Evaluation & Analytics](#evaluation--analytics)
  - [Portfolios Management](#portfolios-management)
  - [Observations & Stop-Rule History](#observations--stop-rule-history)
  - [MBA Interviews](#mba-interviews)
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
- **Directory**: [`decision-engine/`](file:///home/aarush/Projects/fynd/decision-engine)
- **Runtime**: Python 3.10+ (tested on Python 3.14)
- **Framework**: FastAPI, Uvicorn, Pydantic
- **Role**: Pure financial modeling, sensitivity analysis, knapsack portfolio optimization, CSV/Markdown dossier generation.
- **Port**: `8000`
- **Swagger Docs**: `http://localhost:8000/docs`

### Backend API Gateway (Go Gin)
- **Directory**: [`backend/`](file:///home/aarush/Projects/fynd/backend)
- **Runtime**: Go 1.22+ (tested on Go 1.24 / 1.26)
- **Framework**: Gin, pgx v5, go-redis v9, goose v3
- **Role**: Authentication/CORS, transaction management, persistence of portfolios, audit evaluations, qualitative interviews, and caching.
- **Port**: `8080`

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
│   │       └── main.go                 # Backend entrypoint (server & migrations CLI)
│   ├── db/
│   │   └── init.sql                    # Initial PostgreSQL user & database setup
│   ├── internal/
│   │   ├── cache/                      # Redis fail-open cache implementation
│   │   ├── config/                     # Environment configuration loader
│   │   ├── httpapi/                    # Gin router, handlers, middleware
│   │   ├── migrations/                 # Goose embedded SQL migrations (00001 to 00004)
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
│   │   ├── main.py                     # FastAPI routes & payload schemas
│   │   └── reports.py                  # CSV & Markdown report formatters
│   ├── data/
│   │   ├── starting_state.json         # Base pilot fixture dataset
│   │   └── extension.json              # Extension schema and default overrides
│   ├── tests/
│   │   └── test_core.py                # 19 comprehensive unit tests
│   ├── .env.example                    # Decision engine environment template
│   ├── pytest.ini                      # Pytest discovery and pythonpath configuration
│   ├── requirements.txt                # Python dependencies (fastapi, uvicorn, etc.)
│   ├── start.sh                        # Decision Engine Linux/macOS startup script
│   └── start.ps1                       # Decision Engine Windows PowerShell startup script
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
| **Go** | 1.22+ | Backend server runtime |
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

---

## Automated Scripts Reference

### Startup Scripts (`start.sh` & `start.ps1`)

Both `backend/` and `decision-engine/` feature self-healing startup scripts that execute pre-flight checks, install any missing prerequisites, and provide a clear status report:

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

Located at the repository root, [`test.sh`](file:///home/aarush/Projects/fynd/test.sh) and [`test.ps1`](file:///home/aarush/Projects/fynd/test.ps1) run automated end-to-end integration tests using `curl` and `jq`.

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

#### Guaranteed Data Cleanup:
Any data created during the test run is guaranteed to be deleted before the script exits (using `trap ... EXIT` in Bash and `try { ... } finally { ... }` in PowerShell):
- The portfolio `run_1` is deleted via `DELETE /api/v1/portfolios/run_1` (cascading to all portfolio pilot items).
- The deletion is independently verified (`404 Not Found`).
- Any evaluation audit records created during the test run in the `evaluations` table are pruned from PostgreSQL.

---

## Running Unit & Integration Tests

### 1. Decision Engine Unit Tests
Runs 19 automated tests covering financial arithmetic, sensitivity scenarios, knapsack portfolio selection, tie-breaking rules, and stop-rule evaluations:
```bash
cd decision-engine
./venv/bin/python -m pytest -q
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

### 4. Continuous Integration (GitHub Actions)
Automated testing is orchestrated via [`.github/workflows/ci.yml`](file:///home/aarush/Projects/fynd/.github/workflows/ci.yml) on push, pull requests, and manual dispatch:
- **`test-decision-engine`**: Installs dependencies and executes all 19 Pytest tests for the Python decision engine (`python -m pytest -v`).
- **`test-backend`**: Starts PostgreSQL 16 and Redis 7 service containers and runs all Go unit tests and store integration tests with race detection (`go test -v -race ./internal/...`).
- **`test-integration`**: Boots live PostgreSQL and Redis services, executes database migrations (`go run ./cmd/api migrate up`), starts both the Decision Engine and Gin API Gateway in the background, runs [`test.sh`](file:///home/aarush/Projects/fynd/test.sh), and verifies guaranteed cleanup.

---

## AWS Deployment with OpenTofu

The repository includes complete OpenTofu Infrastructure-as-Code to provision an AWS EC2 instance and automatically deploy the full C23 platform (Go Gin Backend, Python Decision Engine, PostgreSQL, and Redis).

### Architecture Overview

OpenTofu provisions:
- **Compute**: An EC2 instance (Ubuntu 24.04 LTS, `t3.small` with 20 GB gp3 storage).
- **Networking & Security**: A dedicated security group opening port `22` (SSH), port `8080` (Go Gin API Gateway), and port `8000` (FastAPI Decision Engine).
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
   - **Decision Engine API Docs**: `http://<PUBLIC_IP>:8000/docs`
   - **SSH Access**: `ssh ubuntu@<PUBLIC_IP>`

5. **Teardown**:
   To destroy all provisioned AWS resources and stop charges:
   ```bash
   tofu destroy
   ```

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
| `GET` | `/api/v1/defaults` | Shipped starting state and extension configurations. |
| `POST` | `/api/v1/pilots` | Evaluates all pilots with payback periods, stop rules, and scenarios. |
| `POST` | `/api/v1/portfolio` | Runs knapsack optimization under a given budget and mode. |
| `POST` | `/api/v1/portfolio/check` | Validates a manual pilot selection list against budget and conflicts. |
| `POST` | `/api/v1/evaluate` | Computes **both** `independent` and `conservative` portfolios simultaneously and records evaluation history. |
| `POST` | `/api/v1/stoprule/:id` | Previews stop-rule decision for a pilot given simulated adoption & quality. |
| `POST` | `/api/v1/export/csv` | Exports evaluated pilot metrics as downloadable CSV (`pilots.csv`). |
| `POST` | `/api/v1/export/evidence`| Exports markdown decision evidence dossier (`evidence.md`). |

#### Example `/api/v1/evaluate`:
```bash
curl -s -X POST http://localhost:8080/api/v1/evaluate \
  -H "Content-Type: application/json" \
  -d '{"budget": 600000}' | jq
```

---

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
| `POST` | `/api/v1/pilots/:id/observations` | Records observed real-world adoption % and quality %; evaluates and stores stop-rule decision. |
| `GET` | `/api/v1/pilots/:id/history` | Fetches historical timeline of observations and resulting decisions for a pilot. |

---

### MBA Interviews

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `POST` | `/api/v1/interviews` | Stores qualitative stakeholder interview evidence. |
| `GET` | `/api/v1/interviews` | Lists all qualitative interview evidence records. |

---

## Configuration Reference

### Backend Configuration (`backend/.env`)

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | Port for the Go Gin HTTP server. |
| `DATABASE_URL` | *Required* | PostgreSQL connection string (`postgres://user:pass@host:5432/db`). |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis connection URL. Leave empty to run without cache. |
| `RULES_ENGINE_URL` | `http://localhost:8000` | URL of the Python FastAPI Decision Engine. |
| `RULES_VERSION` | `1` | Cache namespace version (bump whenever rules logic changes). |
| `ALLOWED_ORIGINS` | `http://localhost:5173` | Comma-separated list of allowed CORS origins. |
| `REQUEST_TIMEOUT_SECONDS` | `5` | Request context timeout in seconds. |
| `CACHE_TTL_SECONDS` | `1800` | TTL in seconds for cached rule responses (0 disables cache). |
| `MAX_BODY_BYTES` | `1048576` | Maximum allowed request body size in bytes (default 1 MB). |
| `AUTO_MIGRATE` | `false` | If `true`, applies pending Goose migrations on server startup. |
| `GIN_MODE` | `release` | Gin server mode (`release` or `debug`). |

### Decision Engine Configuration (`decision-engine/.env`)

| Variable | Default Value | Description |
| :--- | :--- | :--- |
| `PORT` | `8000` | Port for the Uvicorn server. |
| `HOST` | `0.0.0.0` | Bind host address. |
| `RELOAD` | `true` | Enable hot code reloading for local development. |

---

## Database Schema & Migrations

The backend uses [Goose](https://github.com/pressly/goose) migrations located in [`backend/internal/migrations/`](file:///home/aarush/Projects/fynd/backend/internal/migrations).

### Tables

1. **`portfolios`**: Master records of saved pilot selections with frozen starting state, negotiated scope, and final evidence JSON.
2. **`portfolio_pilots`**: Child rows for each pilot under a portfolio (`ON DELETE CASCADE`), including funding status, setup cost, run cost, net benefit, and payback speed.
3. **`observations`**: Measured real-world adoption percentage, quality score, and evidence source for a pilot.
4. **`decisions`**: The deterministic stop-rule verdict (`CONTINUE`, `REVISE`, `STOP`) generated for each observation.
5. **`evaluations`**: Audit log of dual-mode evaluations computed through `/api/v1/evaluate`.
6. **`interviews`**: Qualitative interview records with business stakeholders.

### Managing Migrations CLI

```bash
cd backend

# View status of applied and pending migrations
go run ./cmd/api migrate status

# Apply all pending migrations
go run ./cmd/api migrate up

# Roll back the most recent migration
go run ./cmd/api migrate down
```

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
