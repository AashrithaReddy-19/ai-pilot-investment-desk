# Graph Report - fynd  (2026-10-17)

## Corpus Check
- Corpus is ~39,341 words - fits in a single context window. You may not need a graph.

## Summary
- 642 nodes · 1721 edges · 32 communities (18 shown, 14 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 49 edges (avg confidence: 0.85)
- Token cost: 3,500 input · 850 output

## Community Hubs (Navigation)
- Frontend API Client & HTTP Layer
- Decision Engine Core Analytics
- Backend Server Initialization & Wiring
- Backend Data Models & Structs
- Frontend Health & Readiness State
- Frontend UI Components & Forms
- Backend Router & Handler Endpoints
- OpenTofu AWS Infrastructure
- Frontend Node Package Configuration
- Backend HTTP API Test Suite
- Database Migration & Store Testing
- Frontend TypeScript Configuration
- Backend Environment Configuration
- Portfolios Database Schema
- HTTP Request Tracing Middleware
- Rules Engine Client & Tests
- Redis Caching Unit Tests
- Observations & Decisions Schema
- HTTP API Mock Store
- MBA Stakeholder Interviews Evidence
- Backend Startup Shell Scripts
- Bash API Integration Tests
- Decision Engine Startup Shell Script
- Go Linter Configuration
- C23 Desk Package Root
- Dual-Mode Optimization Concept
- OpenTofu AWS Deployment Concept

## God Nodes (most connected - your core abstractions)
1. `Handlers` - 27 edges
2. `useWorkspace` - 22 edges
3. `aws_instance.app` - 22 edges
4. `newRouter()` - 21 edges
5. `do()` - 20 edges
6. `formatINR()` - 20 edges
7. `react` - 19 edges
8. `ErrorNotice()` - 19 edges
9. `build_portfolio()` - 18 edges
10. `fail()` - 17 edges

## Surprising Connections (you probably didn't know these)
- `Knapsack Portfolio Selection with Overlap Rejection` --implements--> `build_portfolio()`  [EXTRACTED]
  README.md → decision-engine/app/core.py
- `Dynamic Stop-Rule Engine` --implements--> `evaluate_stop_rule()`  [EXTRACTED]
  README.md → decision-engine/app/core.py
- `Vite React Web Application Entry` --references--> `App()`  [EXTRACTED]
  frontend/index.html → frontend/src/App.tsx
- `TestDisabledCacheCallsThrough()` --calls--> `New()`  [INFERRED]
  backend/internal/cache/cache_test.go → backend/internal/cache/cache.go
- `TestBuildRowsMapsSelectedRejectedAndAssumedRunCost()` --calls--> `buildRows()`  [INFERRED]
  backend/internal/httpapi/handlers_test.go → backend/internal/httpapi/handlers.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **C23 Decision Desk Core Trinity** — readme_deterministic_quantitative_finance, readme_knapsack_portfolio_selection, readme_dynamic_stop_rule_engine [EXTRACTED 0.95]

## Communities (32 total, 14 thin omitted)

### Community 0 - "Frontend API Client & HTTP Layer"
Cohesion: 0.06
Nodes (103): api, ApiError, BASE, describeError(), Download, ErrorEnvelope, fail(), file() (+95 more)

### Community 1 - "Decision Engine Core Analytics"
Cohesion: 0.05
Nodes (58): breakeven_adoption_pct(), build_portfolio(), cash_factor(), check_manual_selection(), evaluate_stop_rule(), load_pilots(), _nat(), net_annual() (+50 more)

### Community 2 - "Backend Server Initialization & Wiring"
Cohesion: 0.08
Nodes (22): main(), run(), New(), Config, fail(), isTimeout(), truncate(), isNull() (+14 more)

### Community 3 - "Backend Data Models & Structs"
Cohesion: 0.08
Nodes (18): Decision, DecisionIn, EvaluationIn, History, Interview, InterviewIn, Observation, ObservationIn (+10 more)

### Community 4 - "Frontend Health & Readiness State"
Cohesion: 0.09
Nodes (30): Vite React Web Application Entry, ReadyResponse, ServiceState, ServiceStatus, useServiceStatus(), App(), ALL, AppShell() (+22 more)

### Community 5 - "Frontend UI Components & Forms"
Cohesion: 0.10
Nodes (34): BenefitType, Readiness, Props, AreaProps, Base, Below(), describedBy(), Label() (+26 more)

### Community 6 - "Backend Router & Handler Endpoints"
Cohesion: 0.12
Nodes (8): buildRows(), floatPtr(), strPtr(), apiError, netView, pilotsView, portfolioView, Fail-Open Deterministic Caching with Redis

### Community 7 - "OpenTofu AWS Infrastructure"
Cohesion: 0.13
Nodes (30): aws_instance.app, aws_key_pair.deployer, aws_security_group.app, data.aws_ami.ubuntu, data.aws_subnets.default, data.aws_vpc.default, Terraform module: ., output.backend_api_url (+22 more)

### Community 8 - "Frontend Node Package Configuration"
Cohesion: 0.06
Nodes (30): dependencies, react, react-dom, react-router-dom, zustand, devDependencies, @types/react, @types/react-dom (+22 more)

### Community 9 - "Backend HTTP API Test Suite"
Cohesion: 0.24
Nodes (21): do(), newRouter(), ok200(), TestBadBodies(), TestBuildRowsMapsSelectedRejectedAndAssumedRunCost(), TestCORS(), TestEmptyBodyBecomesEmptyObject(), TestExportsSetDownloadHeaders() (+13 more)

### Community 10 - "Database Migration & Store Testing"
Cohesion: 0.17
Nodes (6): Migrate(), f(), newTestStore(), TestInterviews(), TestObservationAndDecisionHistory(), TestPortfolioRoundTripUpsertAndDelete()

### Community 11 - "Frontend TypeScript Configuration"
Cohesion: 0.12
Nodes (15): compilerOptions, isolatedModules, jsx, lib, module, moduleResolution, noEmit, noFallthroughCasesInSwitch (+7 more)

### Community 12 - "Backend Environment Configuration"
Cohesion: 0.29
Nodes (7): env(), intEnv(), Load(), splitCSV(), TestLoadDefaults(), TestLoadReportsAllProblems(), TestLoadRequiresDatabaseURL()

### Community 13 - "Portfolios Database Schema"
Cohesion: 0.36
Nodes (7): idx_portfolios_created, portfolio_pilots, portfolios, touch_updated_at(), trg_portfolios_touch, evaluations, idx_evaluations_created

### Community 14 - "HTTP Request Tracing Middleware"
Cohesion: 0.38
Nodes (3): newRequestID(), requestID(), validRequestID()

### Community 15 - "Rules Engine Client & Tests"
Cohesion: 0.43
Nodes (5): New(), TestDoForwardsBodyAndRequestID(), TestDoReturnsNon2xxAsResponseNotError(), TestDoTimesOut(), TestPing()

### Community 16 - "Redis Caching Unit Tests"
Cohesion: 0.33
Nodes (4): TestDisabledCacheCallsThrough(), TestKeyChangesWithContentEndpointAndVersion(), TestKeyIgnoresFieldOrderAndWhitespace(), TestKeyRejectsInvalidJSON()

### Community 17 - "Observations & Decisions Schema"
Cohesion: 0.70
Nodes (4): decisions, idx_decisions_pilot, idx_observations_pilot, observations

## Knowledge Gaps
- **15 isolated node(s):** `c23desk`, `@types/react`, `@types/react-dom`, `typescript`, `Deterministic Quantitative Finance` (+10 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 155 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **14 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `C23 AI Pilot Investment Decision Desk` connect `Decision Engine Core Analytics` to `Backend Router & Handler Endpoints`?**
  _High betweenness centrality (0.101) - this node is a cross-community bridge._
- **What connects `c23desk`, `@types/react`, `@types/react-dom` to the rest of the system?**
  _15 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Frontend API Client & HTTP Layer` be split into smaller, more focused modules?**
  _Cohesion score 0.05874639391555206 - nodes in this community are weakly interconnected._
- **Why does `react` connect `Frontend API Client & HTTP Layer` to `Frontend Node Package Configuration`, `Frontend Health & Readiness State`, `Frontend UI Components & Forms`?**
  _High betweenness centrality (0.036) - this node is a cross-community bridge._
- **Should `Decision Engine Core Analytics` be split into smaller, more focused modules?**
  _Cohesion score 0.05493133583021224 - nodes in this community are weakly interconnected._
- **Why does `Handlers` connect `Backend Server Initialization & Wiring` to `Backend Router & Handler Endpoints`?**
  _High betweenness centrality (0.020) - this node is a cross-community bridge._
- **Should `Backend Server Initialization & Wiring` be split into smaller, more focused modules?**
  _Cohesion score 0.08243727598566308 - nodes in this community are weakly interconnected._