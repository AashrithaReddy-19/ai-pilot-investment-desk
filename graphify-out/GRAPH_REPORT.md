# Graph Report - fynd  (2026-10-18)

## Corpus Check
- 123 files · ~89,730 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 9 file(s) not represented in the graph (top: .example 4, (none) 2, .ini 1)

## Summary
- 1359 nodes · 3722 edges · 69 communities (52 shown, 17 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 143 edges (avg confidence: 0.85)
- Token cost: 9,500 input · 5,400 output

## Community Hubs (Navigation)
- Go Gateway Runtime
- Engine Route Handlers
- Core Portfolio Math
- CSV Ingest Parsers
- Advice Engine
- Gateway HTTP Tests
- Backend Infrastructure
- Frontend Build & E2E
- Cash-Flow Model
- Branch And Bound Solver
- Frontend Workspace Screens
- Store Layer
- TypeScript API Client
- Lifecycle Rules
- Decision Memo
- types
- benefittype
- outputs
- evaluationsummary
- test lifecycle
- vite
- paired
- forecast block
- readyresponse
- store
- whatif
- evaluation
- store test
- proposal
- json
- migrations
- core
- proposals
- tsconfig
- cache test
- plans test
- compare
- compare
- compareproposalrow
- observations decision
- savedownload
- scenariobody
- file
- lifecycle
- decision
- portfolios
- store
- evidence
- portfolio
- history
- http
- pilot plans
- interviews
- buildrows
- check tcp port
- check tcp port
- cleanup
- interviews
- start
- linter
- httpx
- python
- c23desk

## God Nodes (most connected - your core abstractions)
1. `Handlers` - 49 edges
2. `json()` - 46 edges
3. `newRouter()` - 44 edges
4. `do()` - 44 edges
5. `build_portfolio()` - 35 edges
6. `ErrorNotice()` - 35 edges
7. `view()` - 32 edges
8. `Store` - 31 edges
9. `fail()` - 30 edges
10. `Store` - 28 edges

## Surprising Connections (you probably didn't know these)
- `test.sh (root API integration test script)` --semantically_similar_to--> `test.ps1 (PowerShell twin of test.sh)`  [INFERRED] [semantically similar]
  .github/workflows/ci.yml → README.md
- `Vite React Web Application Entry` --references--> `App()`  [EXTRACTED]
  frontend/index.html → frontend/src/App.tsx
- `C23 AI Pilot Investment Decision Desk` --cites--> `CI Pipeline (GitHub Actions)`  [EXTRACTED]
  README.md → .github/workflows/ci.yml
- `Job test-decision-engine (Python FastAPI / Pytest)` --references--> `fastapi`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt
- `Job test-decision-engine (Python FastAPI / Pytest)` --references--> `pytest`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Staged CI pipeline: unit tests, integration, frontend, browser e2e** — _github_workflows_ci_ci_pipeline, _github_workflows_ci_job_test_decision_engine, _github_workflows_ci_job_test_backend, _github_workflows_ci_job_test_integration, _github_workflows_ci_job_test_frontend, _github_workflows_ci_job_test_browser_e2e [EXTRACTED 1.00]
- **Full-stack service container runtime for integration/browser e2e jobs** — _github_workflows_ci_job_test_integration, _github_workflows_ci_job_test_browser_e2e, _github_workflows_ci_service_postgres_16_alpine, _github_workflows_ci_service_redis_7_alpine [EXTRACTED 1.00]
- **C23 dual-service platform stack (gateway + engine + frontend + Postgres + Redis)** — readme_backend_api_gateway_go_gin, readme_decision_engine_fastapi, readme_frontend_web_dashboard_react_vite, readme_postgresql, readme_redis [EXTRACTED 0.90]

## Communities (69 total, 17 thin omitted)

### Community 0 - "Go Gateway Runtime"
Cohesion: 0.05
Nodes (36): main(), run(), New(), fail(), isTimeout(), truncate(), budgetFrom(), extConventions() (+28 more)

### Community 1 - "Engine Route Handlers"
Cohesion: 0.08
Nodes (49): check_manual_selection(), _apply_overrides(), balance_sheet_confirm(), balance_sheet_parse(), checkpoint_now(), CheckpointRequest, compare_v(), CompareRequest (+41 more)

### Community 2 - "Core Portfolio Math"
Cohesion: 0.07
Nodes (31): build_portfolio(), cash_factor(), realized_benefit(), get(), load(), obs(), test_20_month_payback(), test_benefit_driver_computes_benefit() (+23 more)

### Community 3 - "CSV Ingest Parsers"
Cohesion: 0.07
Nodes (25): _cell(), _classify_columns(), confirm_balance_sheet(), confirm_pilots_csv(), _decode(), _delimiter(), _map_columns(), _num() (+17 more)

### Community 4 - "Advice Engine"
Cohesion: 0.10
Nodes (29): _actions_for(), advice_for(), _band(), decide(), out(), evidence(), head_to_head(), pair() (+21 more)

### Community 5 - "Gateway HTTP Tests"
Cohesion: 0.15
Nodes (38): do(), newRouter(), ok200(), TestBadBodies(), TestCORS(), TestEmptyBodyBecomesEmptyObject(), TestExportsSetDownloadHeaders(), TestExtConventions() (+30 more)

### Community 6 - "Backend Infrastructure"
Cohesion: 0.13
Nodes (8): grpINR(), summariseEvalChange(), enginePlan, multiPeriodView, netView, pilotsView, portfolioView, PlanStatusIn

### Community 7 - "Frontend Build & E2E"
Cohesion: 0.05
Nodes (35): screens, dependencies, react, react-dom, react-router-dom, zustand, devDependencies, @playwright/test (+27 more)

### Community 8 - "Cash-Flow Model"
Cohesion: 0.10
Nodes (19): breakeven_month_from_cumulative(), cumulative(), monthly_cash_flows(), npv(), _num(), profit_loss(), ramp_factor(), roi_pct() (+11 more)

### Community 9 - "Branch And Bound Solver"
Cohesion: 0.08
Nodes (17): _CapReached, Item, solve(), bound(), dfs(), _tie_key(), advise(), P() (+9 more)

### Community 10 - "Frontend Workspace Screens"
Cohesion: 0.17
Nodes (28): api, Skeleton(), ErrorNotice(), Props, AreaProps, Base, Below(), describedBy() (+20 more)

### Community 11 - "Store Layer"
Cohesion: 0.09
Nodes (23): CI Pipeline (GitHub Actions), Job test-backend (Go Gin unit & store tests), Job test-browser-e2e (Playwright, full stack behind vite preview), Job test-decision-engine (Python FastAPI / Pytest), Job test-frontend (typecheck / build / Vitest), Job test-integration (end-to-end API tests on live services), postgres:16-alpine service container, redis:7-alpine service container (+15 more)

### Community 12 - "TypeScript API Client"
Cohesion: 0.08
Nodes (17): load_pilots(), _int0(), _lines(), _nat(), _num(), pilot_key(), _set_key(), reallocation() (+9 more)

### Community 13 - "Lifecycle Rules"
Cohesion: 0.16
Nodes (29): ApiError, DefaultsDoc, EvaluateResponse, Mode, PilotsResponse, PortfolioResult, ObservationTools(), SavePortfolio() (+21 more)

### Community 14 - "Decision Memo"
Cohesion: 0.17
Nodes (27): describeError(), AssumedTag(), Empty(), hasFlag(), SyntheticTag(), ConfirmDialog(), Icon(), PATHS (+19 more)

### Community 15 - "types"
Cohesion: 0.06
Nodes (33): AdviceAction, AdviceBlock, AdviceEvidence, AdviceGap, AdviceInput, BalanceCandidate, CheckpointBlock, CheckpointResponse (+25 more)

### Community 16 - "benefittype"
Cohesion: 0.12
Nodes (29): BenefitType, Extension, ExtensionPilot, Fixture, FixturePilot, Readiness, Props, Modal() (+21 more)

### Community 17 - "outputs"
Cohesion: 0.13
Nodes (30): aws_instance.app, aws_key_pair.deployer, aws_security_group.app, data.aws_ami.ubuntu, data.aws_subnets.default, data.aws_vpc.default, Terraform module: ., output.backend_api_url (+22 more)

### Community 18 - "evaluationsummary"
Cohesion: 0.10
Nodes (7): EvaluationSummary, Interview, InterviewIn, PortfolioSummary, ProposalSummary, New(), Store

### Community 19 - "test lifecycle"
Cohesion: 0.13
Nodes (18): cp(), loaded(), mk(), P(), REAL(), test_checkpoint_boundary_is_inclusive_at_targets(), test_checkpoint_extend_on_low_band_even_when_gates_pass(), test_checkpoint_extend_younger_than_planned_duration() (+10 more)

### Community 20 - "vite"
Cohesion: 0.21
Nodes (21): Vite React Web Application Entry, App(), AppShell(), BalancePanel(), Compare(), Memo(), AboutPage(), BalanceSheetPage() (+13 more)

### Community 21 - "paired"
Cohesion: 0.10
Nodes (7): test_node_cap_is_deterministic_and_flagged_on_conflict_pathology(), ext_row(), test_scenarios_carry_explicit_assumptions(), test_whatif_benefit_override_beats_the_driver(), test_whatif_cost_benefit_flip_statement(), test_whatif_mutates_no_stored_data(), test_whatif_verdict_flip_and_statement()

### Community 22 - "forecast block"
Cohesion: 0.13
Nodes (10): forecast_block(), ai_run_cost_inr(), inflation_flag(), next_fy_cost_inr(), _num(), value_per_1m_tokens(), test_inflation_flag(), test_next_fy() (+2 more)

### Community 23 - "readyresponse"
Cohesion: 0.13
Nodes (18): ReadyResponse, ServiceState, ServiceStatus, useServiceStatus(), ALL, GROUPS, Item, Ctx (+10 more)

### Community 24 - "store"
Cohesion: 0.15
Nodes (4): History, PilotPlan, planStub, Store

### Community 25 - "whatif"
Cohesion: 0.15
Nodes (9): apply(), flip_scan(), monotonic_ok(), _nonneg(), _numok(), OverrideError, pilot_numbers(), sweep_steps() (+1 more)

### Community 26 - "evaluation"
Cohesion: 0.18
Nodes (4): Evaluation, EvaluationIn, fakeCache, fakeEvalStore

### Community 27 - "store test"
Cohesion: 0.22
Nodes (14): contains(), deref(), f(), i(), newTestStore(), TestEvaluationsLog(), TestEvidenceAndObservationLink(), TestHistoryTimeline() (+6 more)

### Community 28 - "proposal"
Cohesion: 0.18
Nodes (4): Proposal, ProposalIn, nameOrNull(), fakeProposalStore

### Community 29 - "json"
Cohesion: 0.18
Nodes (16): json(), ManualCheck, PilotHistory, StopRule, SweepParameter, SweepResponse, WhatIfOverrides, WhatIfResponse (+8 more)

### Community 30 - "migrations"
Cohesion: 0.15
Nodes (6): New(), TestDoForwardsBodyAndRequestID(), TestDoReturnsNon2xxAsResponseNotError(), TestDoTimesOut(), TestPing(), Migrate()

### Community 31 - "core"
Cohesion: 0.17
Nodes (14): defaults(), health(), ready(), BASE, Download, ErrorEnvelope, fail(), Meta (+6 more)

### Community 32 - "proposals"
Cohesion: 0.19
Nodes (14): enc, addEvidence(), archiveProposal(), createProposal(), deleteProposal(), getProposal(), listEvidence(), restoreProposal() (+6 more)

### Community 33 - "tsconfig"
Cohesion: 0.12
Nodes (15): compilerOptions, isolatedModules, jsx, lib, module, moduleResolution, noEmit, noFallthroughCasesInSwitch (+7 more)

### Community 34 - "cache test"
Cohesion: 0.15
Nodes (12): TestDisabledCacheCallsThrough(), TestKeyChangesWithContentEndpointAndVersion(), TestKeyIgnoresFieldOrderAndWhitespace(), TestKeyRejectsInvalidJSON(), env(), Config, intEnv(), Load() (+4 more)

### Community 35 - "plans test"
Cohesion: 0.33
Nodes (14): contains(), jsonOf(), mustRaw(), reallocEcho(), sampleEvaluation(), sampleHistory(), samplePlan(), strptr() (+6 more)

### Community 36 - "compare"
Cohesion: 0.22
Nodes (7): CompareError, _inrupee(), _num(), _recommendation_text(), rows_ok_flags(), run(), validate()

### Community 37 - "compare"
Cohesion: 0.17
Nodes (14): balanceSheetConfirm(), balanceSheetParse(), compare(), importPilotsConfirm(), importPilotsParse(), recordObservation(), jsonUpload(), BalanceSheetConfirm (+6 more)

### Community 38 - "compareproposalrow"
Cohesion: 0.23
Nodes (14): CompareProposalRow, BarList(), blankRow(), CfRow, ComparePanel(), CompareResult(), CumulativeChart(), fmtSigned() (+6 more)

### Community 39 - "observations decision"
Cohesion: 0.24
Nodes (12): decisions, idx_decisions_pilot, idx_observations_pilot, observations, evidence, idx_decisions_proposal, idx_evidence_proposal, idx_observations_proposal (+4 more)

### Community 40 - "savedownload"
Cohesion: 0.22
Nodes (13): saveDownload(), AdviceOutcome, CheckpointOutcome, EvidenceBand, StopDecision, Status(), bandTone(), checkpointTone() (+5 more)

### Community 41 - "scenariobody"
Cohesion: 0.23
Nodes (11): ScenarioBody, ScenarioOverrides, num(), NumForm(), paramValue(), PILOT_LEVEL, READINESS, Strs (+3 more)

### Community 42 - "file"
Cohesion: 0.22
Nodes (9): file(), memoDownload(), deletePortfolio(), exportCsv(), exportEvidence(), getPortfolio(), savePortfolio(), PortfolioSummary (+1 more)

### Community 43 - "lifecycle"
Cohesion: 0.18
Nodes (9): checkpoint(), createPlan(), planProgress(), realloc(), setPlanStatus(), CreatePlanResponse, PilotPlan, PlanProgress (+1 more)

### Community 44 - "decision"
Cohesion: 0.47
Nodes (5): Decision, DecisionIn, Observation, ObservationIn, fkWithFK

### Community 45 - "portfolios"
Cohesion: 0.36
Nodes (7): idx_portfolios_created, portfolio_pilots, portfolios, touch_updated_at(), trg_portfolios_touch, evaluations, idx_evaluations_created

### Community 46 - "store"
Cohesion: 0.29
Nodes (3): HistoryEvent, fakeStore, historyStub

### Community 48 - "portfolio"
Cohesion: 0.36
Nodes (3): Portfolio, PortfolioIn, PortfolioPilot

### Community 49 - "history"
Cohesion: 0.29
Nodes (5): timeline(), V1, recordObservation(), HistoryEvent, ObservationResult

### Community 51 - "pilot plans"
Cohesion: 0.70
Nodes (4): idx_pilot_plans_status, pilot_plans, touch_pilot_plans(), trg_pilot_plans_touch

### Community 52 - "interviews"
Cohesion: 0.50
Nodes (3): addInterview(), Interview, InterviewIn

### Community 53 - "buildrows"
Cohesion: 0.50
Nodes (4): buildRows(), floatPtr(), strPtr(), TestBuildRowsMapsSelectedRejectedAndAssumedRunCost()

## Ambiguous Edges - Review These
- `Evidence bands, decision outcomes, next-best actions (Phase 5)` → `Persistent proposals with versioned inputs + evidence trail`  [AMBIGUOUS]
  README.md · relation: conceptually_related_to

## Knowledge Gaps
- **14 isolated node(s):** `c23desk`, `@types/node`, `@types/react`, `@types/react-dom`, `typescript` (+9 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 336 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **17 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `react` connect `Frontend Workspace Screens` to `compareproposalrow`, `Frontend Build & E2E`, `savedownload`, `scenariobody`, `Lifecycle Rules`, `Decision Memo`, `benefittype`, `vite`, `readyresponse`?**
  _High betweenness centrality (0.011) - this node is a cross-community bridge._
- **Are the 20 inferred relationships involving `newRouter()` (e.g. with `NewRouter()` and `TestCheckpointMergesPlanAndObservationState()`) actually correct?**
  _`newRouter()` has 20 INFERRED edges - model-reasoned connections that need verification._
- **What connects `c23desk`, `@types/node`, `@types/react` to the rest of the system?**
  _14 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Go Gateway Runtime` be split into smaller, more focused modules?**
  _Cohesion score 0.05317703024125042 - nodes in this community are weakly interconnected._
- **Why does `Handlers` connect `Go Gateway Runtime` to `cache test`, `Backend Infrastructure`?**
  _High betweenness centrality (0.007) - this node is a cross-community bridge._
- **Should `Engine Route Handlers` be split into smaller, more focused modules?**
  _Cohesion score 0.08299240210403273 - nodes in this community are weakly interconnected._

### Low-confidence Hints
_AMBIGUOUS edges — the extractor was unsure. Verify before acting on these._

- **What is the exact relationship between `Evidence bands, decision outcomes, next-best actions (Phase 5)` and `Persistent proposals with versioned inputs + evidence trail`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._