# Graph Report - fynd  (2026-10-18)

## Corpus Check
- 176 files · ~172,373 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 3941 nodes · 7834 edges · 211 communities (190 shown, 21 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 309 edges (avg confidence: 0.85)
- Token cost: 1,200 input · 800 output

## Community Hubs (Navigation)
- Handlers
- types.ts
- Core Test Suite
- model.go
- useWorkspace.ts
- main.py
- core.py
- fakePlatform
- Portfolios DB Migration
- decision.py
- client.ts
- Economics Test Suite
- Checkpoint Contract Schema
- Views Yearly Savings Contract Schema
- Cache
- Pricing Optimise Contract Schema
- Views Dashboard Contract Schema
- Audit Test Suite
- Reallocation Contract Schema
- compare.py
- Sweep Contract Schema
- Store
- Advice Test Suite
- Cashflow Test Suite
- Portfolio Contract Schema
- package.json
- Compare Contract Schema
- aws_instance.app
- Cache Test Suite
- A1_Fixes Test Suite
- Defaults Contract Schema
- Job test-integration (end-to-end API tests on live services)
- Contracts Test Suite
- Auth Test Suite
- Defaults Contract Schema
- Views Dashboard Contract Schema
- draft.ts
- Proposal
- WorkspaceID
- Views Dashboard Contract Schema
- pricing.py
- Compare Test Suite
- Lifecycle Test Suite
- Economics Contract Schema
- WhatIf.tsx
- Plan Contract Schema
- Views Dashboard Contract Schema
- Store
- Defaults Contract Schema
- Pilots Contract Schema
- Defaults Contract Schema
- Pilots Contract Schema
- Decision Contract Schema
- Defaults Contract Schema
- Pilots Contract Schema
- Pilots Contract Schema
- Pricing Quote Contract Schema
- SimplePages.tsx
- Audit Test Suite
- Decision Contract Schema
- Defaults Contract Schema
- Proposals.tsx
- parse_balance_sheet
- Pilots Import Parse Contract Schema
- Views Dashboard Contract Schema
- Deps
- Balance Sheet Parse Contract Schema
- Whatif Contract Schema
- Defaults Contract Schema
- Pilots Contract Schema
- Pilots Contract Schema
- Stoprule Contract Schema
- Compare.tsx
- handlers.go
- Plans Test Suite
- Ingest_A5 Test Suite
- Decision Contract Schema
- Decision Contract Schema
- History.tsx
- Proposals Test Suite
- ingest.py
- Decision Contract Schema
- Economics Contract Schema
- Economics Contract Schema
- Views Dashboard Contract Schema
- Whatif Contract Schema
- Ingest Test Suite
- compilerOptions
- Balance Sheet Confirm Contract Schema
- Defaults Contract Schema
- Economics Contract Schema
- Pilots Contract Schema
- Views Dashboard Contract Schema
- Views Dashboard Contract Schema
- Whatif Contract Schema
- Whatif Contract Schema
- go_pkg_io
- time.Time
- xlsx.py
- Defaults Contract Schema
- Economics Contract Schema
- Forecast Usage Contract Schema
- Health Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Import Parse Contract Schema
- Pilots Import Parse Contract Schema
- Pilots Import Parse Contract Schema
- Config Test Suite
- Balance Sheet Confirm Contract Schema
- Balance Sheet Confirm Contract Schema
- Compare Contract Schema
- Decision Contract Schema
- Decision Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Compare Contract Schema
- Compare Contract Schema
- Compare Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Views Dashboard Contract Schema
- Balance Sheet Confirm Contract Schema
- Balance Sheet Parse Contract Schema
- Compare Contract Schema
- Defaults Contract Schema
- Economics Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Contract Schema
- Test Test Suite
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Economics Contract Schema
- Forecast Usage Contract Schema
- Forecast Usage Contract Schema
- Forecast Usage Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Import Parse Contract Schema
- Pilots Contract Schema
- Portfolio Check Contract Schema
- Views Dashboard Contract Schema
- Engine Contract Examples Index
- Compare Contract Schema
- Defaults Contract Schema
- Worker Test Suite
- Balance Sheet Confirm Contract Schema
- Balance Sheet Parse Contract Schema
- Balance Sheet Parse Contract Schema
- Economics Contract Schema
- Forecast Usage Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Import Confirm Contract Schema
- Pilots Import Parse Contract Schema
- rateLimiter
- Audit Test Suite
- Client
- Balance Sheet Confirm Contract Schema
- Balance Sheet Parse Contract Schema
- Compare Contract Schema
- Decision Contract Schema
- Defaults Contract Schema
- Pilots Import Parse Contract Schema
- Forecast Usage Contract Schema
- Whatif Contract Schema
- C23 AI Decision Desk Overview
- errors.go
- Pilot Plans DB Migration
- Balance Sheet Confirm Contract Schema
- Balance Sheet Parse Contract Schema
- Export Csv Contract Schema
- Export Decision Report Contract Schema
- Export Evidence Contract Schema
- Export Memo Contract Schema
- Export Yearly Csv Contract Schema
- Views Dashboard Contract Schema
- Balance Sheet Parse Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Forecast Usage Contract Schema
- check_tcp_port
- Balance Sheet Confirm Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Defaults Contract Schema
- Whatif Contract Schema
- check_tcp_port
- Interviews DB Migration
- Compare Contract Schema
- Compare Contract Schema
- start.sh script
- CI Pipeline (GitHub Actions)
- Go Linter Configuration
- Economics & Budget Fitting
- httpx
- python-multipart
- c23desk

## God Nodes (most connected - your core abstractions)
1. `Handlers` - 89 edges
2. `do()` - 71 edges
3. `Store` - 67 edges
4. `WorkspaceID()` - 58 edges
5. `newRouter()` - 53 edges
6. `ok200()` - 43 edges
7. `json()` - 38 edges
8. `view()` - 35 edges
9. `ErrorNotice()` - 34 edges
10. `build_portfolio()` - 33 edges

## Surprising Connections (you probably didn't know these)
- `Steady State and Conservative Valuation` --semantically_similar_to--> `Dual Mode Optimization (Independent vs Conservative)`  [INFERRED] [semantically similar]
  decision-engine/docs/engine.md → README.md
- `Job test-browser-e2e (Playwright, full stack behind vite preview)` --references--> `uvicorn`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt
- `Job test-integration (end-to-end API tests on live services)` --references--> `uvicorn`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt
- `Job test-browser-e2e (Playwright, full stack behind vite preview)` --references--> `fastapi`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt
- `Job test-decision-engine (Python FastAPI / Pytest)` --references--> `fastapi`  [INFERRED]
  .github/workflows/ci.yml → decision-engine/requirements.txt

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Staged CI pipeline: unit tests, integration, frontend, browser e2e** — _github_workflows_ci_ci_pipeline, _github_workflows_ci_job_test_decision_engine, _github_workflows_ci_job_test_backend, _github_workflows_ci_job_test_integration, _github_workflows_ci_job_test_frontend, _github_workflows_ci_job_test_browser_e2e [EXTRACTED 1.00]
- **Full-stack service container runtime for integration/browser e2e jobs** — _github_workflows_ci_job_test_integration, _github_workflows_ci_job_test_browser_e2e, _github_workflows_ci_service_postgres_16_alpine, _github_workflows_ci_service_redis_7_alpine [EXTRACTED 1.00]
- **Engine Export Artifact Contracts** — decision_engine_contracts_engine_export_csv_response_csv_export, decision_engine_contracts_engine_export_decision_report_response_decision_report, decision_engine_contracts_engine_export_evidence_response_evidence_report, decision_engine_contracts_engine_export_memo_response_decision_memo [EXTRACTED 1.00]

## Communities (211 total, 21 thin omitted)

### Community 0 - "Handlers"
Cohesion: 0.06
Nodes (31): need(), requireWorkspace(), fail(), queryLimit(), isNull(), mustJSON(), optPct(), accessLog() (+23 more)

### Community 1 - "types.ts"
Cohesion: 0.03
Nodes (108): balanceSheetConfirm(), balanceSheetParse(), compare(), importPilotsConfirm(), importPilotsParse(), recordObservation(), defaults(), health() (+100 more)

### Community 2 - "Core Test Suite"
Cohesion: 0.04
Nodes (50): build_portfolio(), check_manual_selection(), load_pilots(), _int0(), _lines(), _nat(), _num(), pilot_key() (+42 more)

### Community 3 - "model.go"
Cohesion: 0.08
Nodes (23): bearer(), newRawToken(), newRequestID(), requestID(), validRequestID(), isFKViolation(), sniffFormat(), UploadFields() (+15 more)

### Community 4 - "useWorkspace.ts"
Cohesion: 0.11
Nodes (56): describeError(), Pilot, PilotHistory, StopRule, AssumedTag(), Empty(), hasFlag(), Skeleton() (+48 more)

### Community 5 - "main.py"
Cohesion: 0.08
Nodes (52): to_pilots(), _apply_overrides(), balance_sheet_confirm(), balance_sheet_parse(), checkpoint_now(), CheckpointRequest, compare_v(), CompareRequest (+44 more)

### Community 6 - "core.py"
Cohesion: 0.07
Nodes (37): _actions_for(), advice_for(), _band(), decide(), out(), evidence(), _reason_codes(), breakdown() (+29 more)

### Community 7 - "fakePlatform"
Cohesion: 0.06
Nodes (14): fakePlatform, fakePlatform, APIToken, Approval, ApprovalIn, Member, Snapshot, SnapshotIn (+6 more)

### Community 8 - "Portfolios DB Migration"
Cohesion: 0.06
Nodes (45): idx_portfolios_created, portfolio_pilots, portfolios, touch_updated_at(), trg_portfolios_touch, decisions, idx_decisions_pilot, idx_observations_pilot (+37 more)

### Community 9 - "decision.py"
Cohesion: 0.07
Nodes (31): _inr(), _assess(), _criteria(), data_readiness(), decide_one(), decide_set(), decision_for(), DecisionError (+23 more)

### Community 10 - "client.ts"
Cohesion: 0.07
Nodes (40): api, saveDownload(), AdviceOutcome, CheckpointOutcome, EvidenceBand, Flag, ReadyResponse, StopDecision (+32 more)

### Community 11 - "Economics Test Suite"
Cohesion: 0.07
Nodes (22): ai_run_cost_inr(), inflation_flag(), next_fy_cost_inr(), _num(), value_per_1m_tokens(), P(), test_benefit_above_baseline_is_flagged(), test_competitiveness_ratio_and_per_unit() (+14 more)

### Community 12 - "Checkpoint Contract Schema"
Cohesion: 0.04
Nodes (45): type, type, type, type, type, type, properties, required (+37 more)

### Community 13 - "Views Yearly Savings Contract Schema"
Cohesion: 0.05
Nodes (45): type, type, type, items, type, properties, required, type (+37 more)

### Community 14 - "Cache"
Cohesion: 0.07
Nodes (14): bootstrap(), followRulesVersion(), main(), run(), Cache, New(), TestDisabledCacheCallsThrough(), Config (+6 more)

### Community 15 - "Pricing Optimise Contract Schema"
Cohesion: 0.05
Nodes (42): type, items, type, type, properties, required, type, items (+34 more)

### Community 16 - "Views Dashboard Contract Schema"
Cohesion: 0.05
Nodes (43): type, type, type, type, type, type, type, properties (+35 more)

### Community 17 - "Audit Test Suite"
Cohesion: 0.16
Nodes (38): TestProposalEvaluationsEndpoint(), do(), newRouter(), ok200(), TestBadBodies(), TestCORS(), TestCORSPreflightAllowsEveryRegisteredMethod(), TestEmptyBodyBecomesEmptyObject() (+30 more)

### Community 18 - "Reallocation Contract Schema"
Cohesion: 0.06
Nodes (41): type, type, type, type, items, type, type, type (+33 more)

### Community 19 - "compare.py"
Cohesion: 0.08
Nodes (19): head_to_head(), pair(), CompareError, _inrupee(), _lakh(), _num(), _recommendation_text(), rows_ok_flags() (+11 more)

### Community 20 - "Sweep Contract Schema"
Cohesion: 0.05
Nodes (39): type, type, type, type, properties, required, type, type (+31 more)

### Community 21 - "Store"
Cohesion: 0.08
Nodes (8): Evaluation, History, PilotPlan, Portfolio, PortfolioIn, PortfolioPilot, planStub, Store

### Community 22 - "Advice Test Suite"
Cohesion: 0.08
Nodes (18): _CapReached, Item, solve(), bound(), dfs(), _tie_key(), advise(), P() (+10 more)

### Community 23 - "Cashflow Test Suite"
Cohesion: 0.09
Nodes (20): breakeven_month_from_cumulative(), cumulative(), monthly_cash_flows(), monthly_components(), npv(), _num(), profit_loss(), ramp_factor() (+12 more)

### Community 24 - "Portfolio Contract Schema"
Cohesion: 0.05
Nodes (38): type, type, type, type, properties, required, type, type (+30 more)

### Community 25 - "package.json"
Cohesion: 0.05
Nodes (35): screens, dependencies, react, react-dom, react-router-dom, zustand, devDependencies, @playwright/test (+27 more)

### Community 26 - "Compare Contract Schema"
Cohesion: 0.05
Nodes (38): type, type, type, type, type, items, type, type (+30 more)

### Community 27 - "aws_instance.app"
Cohesion: 0.12
Nodes (35): aws_instance.app, aws_key_pair.deployer, aws_security_group.app, data.aws_ami.ubuntu, data.aws_subnets.default, data.aws_vpc.default, Terraform module: ., output.backend_api_url (+27 more)

### Community 28 - "Cache Test Suite"
Cohesion: 0.14
Nodes (29): TestKeyChangesWithContentEndpointAndVersion(), TestKeyIgnoresFieldOrderAndWhitespace(), TestKeyRejectsInvalidJSON(), TestSetVersionChangesTheKeyNamespace(), withVersion(), TestCrossWorkspaceIsolation(), TestSettingsSnapshotsAndApprovals(), TestTokensAndMemberships() (+21 more)

### Community 29 - "A1_Fixes Test Suite"
Cohesion: 0.07
Nodes (11): test_more_than_40_candidates_is_a_422_not_a_500(), test_observed_zero_adoption_is_a_measurement_not_missing(), test_pilots_honours_request_scenario_multipliers(), test_steady_state_public_checks_unchanged(), ext_row(), test_scenario_multipliers_are_editable(), test_whatif_base_vs_changed_with_deltas(), test_whatif_benefit_override_beats_the_driver() (+3 more)

### Community 30 - "Defaults Contract Schema"
Cohesion: 0.06
Nodes (35): type, type, type, type, type, type, properties, type (+27 more)

### Community 31 - "Job test-integration (end-to-end API tests on live services)"
Cohesion: 0.09
Nodes (22): Job test-backend (Go Gin unit & store tests), Job test-browser-e2e (Playwright, full stack behind vite preview), Job test-decision-engine (Python FastAPI / Pytest), Job test-frontend (typecheck / build / Vitest), Job test-integration (end-to-end API tests on live services), postgres:16-alpine service container, redis:7-alpine service container, fastapi (+14 more)

### Community 32 - "Contracts Test Suite"
Cohesion: 0.09
Nodes (10): dump(), main(), call(), infer(), merge(), _type(), _types(), validate() (+2 more)

### Community 33 - "Auth Test Suite"
Cohesion: 0.13
Nodes (25): newAuthRouter(), TestAuthOffUsesTheDefaultWorkspaceAsAdmin(), TestAuthRequiredRejectsMissingAndUnknownTokens(), TestCORSAllowsAuthHeaders(), TestMeReportsUserWorkspaceAndRole(), TestRateLimitReturns429WithRetryAfter(), TestRolesGateRoutes(), TestWorkspaceIsChosenAndIsolated() (+17 more)

### Community 34 - "Defaults Contract Schema"
Cohesion: 0.06
Nodes (32): items, type, items, type, items, type, type, properties (+24 more)

### Community 35 - "Views Dashboard Contract Schema"
Cohesion: 0.06
Nodes (32): properties, required, type, type, type, type, type, type (+24 more)

### Community 36 - "draft.ts"
Cohesion: 0.12
Nodes (27): BenefitType, Extension, ExtensionPilot, Fixture, FixturePilot, Readiness, ConfirmDialog(), Props (+19 more)

### Community 37 - "Proposal"
Cohesion: 0.12
Nodes (9): Decision, DecisionIn, Observation, ObservationIn, Proposal, ProposalIn, proposalInWorkspace(), fakeProposalStore (+1 more)

### Community 38 - "WorkspaceID"
Cohesion: 0.14
Nodes (8): ActorID(), Correction, CorrectionIn, Upload, UploadIn, WorkspaceID(), scanUpload(), fakeUploads

### Community 39 - "Views Dashboard Contract Schema"
Cohesion: 0.10
Nodes (29): items, type, items, type, items, type, required, type (+21 more)

### Community 40 - "pricing.py"
Cohesion: 0.15
Nodes (18): forecast_usage_v(), backtest(), cost_of(), _fit(), forecast_usage(), _mape(), monthly_bill(), _nonneg() (+10 more)

### Community 41 - "Compare Test Suite"
Cohesion: 0.15
Nodes (23): post(), test_budget_7_lakh_funds_a_and_b_with_joint_text(), test_cash_vs_capacity_split(), test_compare_horizon_and_discount_via_extension(), test_compare_validation_422s(), test_conservative_mode_and_null_run_cost(), test_delay_past_horizon_is_a_three_year_loss(), test_missing_tokens_and_price_never_zero() (+15 more)

### Community 42 - "Lifecycle Test Suite"
Cohesion: 0.13
Nodes (18): cp(), loaded(), mk(), P(), REAL(), test_checkpoint_boundary_is_inclusive_at_targets(), test_checkpoint_extend_on_low_band_even_when_gates_pass(), test_checkpoint_extend_younger_than_planned_duration() (+10 more)

### Community 43 - "Economics Contract Schema"
Cohesion: 0.07
Nodes (27): type, type, type, type, type, type, type, type (+19 more)

### Community 44 - "WhatIf.tsx"
Cohesion: 0.17
Nodes (24): AreaProps, Base, Below(), describedBy(), Label(), SelectField(), SelectProps, TextArea() (+16 more)

### Community 45 - "Plan Contract Schema"
Cohesion: 0.08
Nodes (25): type, type, type, type, type, type, type, properties (+17 more)

### Community 46 - "Views Dashboard Contract Schema"
Cohesion: 0.09
Nodes (26): properties, required, type, properties, required, type, items, type (+18 more)

### Community 47 - "Store"
Cohesion: 0.11
Nodes (6): Interview, InterviewIn, PortfolioSummary, ProposalSummary, New(), Store

### Community 48 - "Defaults Contract Schema"
Cohesion: 0.08
Nodes (25): type, type, items, type, properties, required, type, items (+17 more)

### Community 49 - "Pilots Contract Schema"
Cohesion: 0.08
Nodes (25): type, type, type, type, type, properties, type, type (+17 more)

### Community 50 - "Defaults Contract Schema"
Cohesion: 0.08
Nodes (24): properties, required, type, properties, required, type, type, properties (+16 more)

### Community 51 - "Pilots Contract Schema"
Cohesion: 0.09
Nodes (23): required, type, type, required, type, items, type, properties (+15 more)

### Community 52 - "Decision Contract Schema"
Cohesion: 0.09
Nodes (23): required, type, required, type, type, properties, type, action (+15 more)

### Community 53 - "Defaults Contract Schema"
Cohesion: 0.09
Nodes (23): type, type, items, type, items, type, flip_adoption_step_pct, flip_scan_step_of_base (+15 more)

### Community 54 - "Pilots Contract Schema"
Cohesion: 0.10
Nodes (23): properties, required, type, properties, required, type, properties, required (+15 more)

### Community 55 - "Pilots Contract Schema"
Cohesion: 0.09
Nodes (22): type, type, properties, required, type, type, type, adoption_pct (+14 more)

### Community 56 - "Pricing Quote Contract Schema"
Cohesion: 0.09
Nodes (21): type, type, properties, required, type, type, items, type (+13 more)

### Community 57 - "SimplePages.tsx"
Cohesion: 0.26
Nodes (17): Vite React Web Application Entry, App(), AppShell(), BalanceSheetPage(), ComparePage(), EvidencePage(), Head(), HistoryPage() (+9 more)

### Community 58 - "Audit Test Suite"
Cohesion: 0.18
Nodes (14): engineBodies(), newAuditRouter(), TestAuditCoversPlansAndMemo(), TestAuditListValidatesAndClamps(), TestAuditRecordsSuccessfulEditsAndExports(), TestAuditSkipsFailedRequests(), TestAuditWriteFailureDoesNotFailTheRequest(), TestEvaluateLinksTheProposalVersionWithoutSendingIt() (+6 more)

### Community 59 - "Decision Contract Schema"
Cohesion: 0.13
Nodes (21): properties, items, type, required, type, items, type, gaps (+13 more)

### Community 60 - "Defaults Contract Schema"
Cohesion: 0.10
Nodes (21): type, type, type, type, type, properties, type, type (+13 more)

### Community 61 - "Proposals.tsx"
Cohesion: 0.12
Nodes (8): Evidence, EvidenceKind, EvidenceQuality, Proposal, ProposalSummary, CreateErrs, ProposalDetail(), Proposals()

### Community 62 - "parse_balance_sheet"
Cohesion: 0.15
Nodes (12): _cell(), _classify_columns(), findings(), _map_columns(), parse_balance_sheet(), parse_money(), parse_pilots_csv(), money() (+4 more)

### Community 63 - "Pilots Import Parse Contract Schema"
Cohesion: 0.10
Nodes (19): type, type, type, type, properties, error_rows, findings, flags (+11 more)

### Community 64 - "Views Dashboard Contract Schema"
Cohesion: 0.10
Nodes (20): type, type, type, properties, required, type, type, type (+12 more)

### Community 65 - "Deps"
Cohesion: 0.15
Nodes (8): AuditEvent, AuditEventIn, AuditFilter, Auditor, Cache, Deps, noAudit, recordingAuditor

### Community 66 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.11
Nodes (19): type, type, type, type, type, type, type, properties (+11 more)

### Community 67 - "Whatif Contract Schema"
Cohesion: 0.18
Nodes (19): properties, type, type, properties, properties, type, type, type (+11 more)

### Community 68 - "Defaults Contract Schema"
Cohesion: 0.11
Nodes (18): required, type, required, type, required, type, type, required (+10 more)

### Community 69 - "Pilots Contract Schema"
Cohesion: 0.11
Nodes (18): properties, required, type, properties, type, type, type, properties (+10 more)

### Community 70 - "Pilots Contract Schema"
Cohesion: 0.11
Nodes (18): properties, required, type, type, baseline_cost_inr, baseline_note, budget, capacity_value_inr (+10 more)

### Community 71 - "Stoprule Contract Schema"
Cohesion: 0.11
Nodes (17): type, type, type, type, properties, actual, decision, metric (+9 more)

### Community 72 - "Compare.tsx"
Cohesion: 0.20
Nodes (17): CompareProposalRow, PilotsImportParse, BalancePanel(), BarList(), blankRow(), CfRow, Compare(), ComparePanel() (+9 more)

### Community 73 - "handlers.go"
Cohesion: 0.15
Nodes (15): budgetFrom(), buildRows(), extConventions(), floatPtr(), modeOr(), strPtr(), TestBuildRowsMapsSelectedRejectedAndAssumedRunCost(), TestExtConventions() (+7 more)

### Community 74 - "Plans Test Suite"
Cohesion: 0.27
Nodes (16): jsonOf(), mustRaw(), reallocEcho(), sampleEvaluation(), sampleHistory(), samplePlan(), strptr(), TestCheckpointMergesPlanAndObservationState() (+8 more)

### Community 75 - "Ingest_A5 Test Suite"
Cohesion: 0.12
Nodes (3): write_simple(), test_xlsx_balance_sheet_matches_the_csv_path(), test_xlsx_pilot_import_and_numbers_without_float_noise()

### Community 76 - "Decision Contract Schema"
Cohesion: 0.12
Nodes (16): required, type, additionalProperties, type, x-keyed-by, properties, decisions, warnings (+8 more)

### Community 77 - "Decision Contract Schema"
Cohesion: 0.12
Nodes (17): properties, required, type, baseline_cost_inr, baseline_note, budget, capacity_value_inr, cashable_savings_inr (+9 more)

### Community 78 - "History.tsx"
Cohesion: 0.19
Nodes (11): AuditEvent, EvaluationSummary, HistoryEvent, describeDetails(), describeEvent(), History(), loadAudit(), loadEvaluations() (+3 more)

### Community 79 - "Proposals Test Suite"
Cohesion: 0.15
Nodes (5): EvaluationSummary, fakeEvalStore, fakeStore, limitStore, workspaceStore

### Community 80 - "ingest.py"
Cohesion: 0.24
Nodes (7): _check_override(), confirm_balance_sheet(), confirm_pilots_csv(), _decode(), _num(), _overrides(), ParseError

### Community 81 - "Decision Contract Schema"
Cohesion: 0.21
Nodes (16): type, properties, properties, required, type, type, type, type (+8 more)

### Community 82 - "Economics Contract Schema"
Cohesion: 0.13
Nodes (16): properties, required, type, properties, required, type, type, type (+8 more)

### Community 83 - "Economics Contract Schema"
Cohesion: 0.15
Nodes (16): properties, required, type, type, type, items, type, properties (+8 more)

### Community 84 - "Views Dashboard Contract Schema"
Cohesion: 0.12
Nodes (16): properties, required, type, type, items, type, items, type (+8 more)

### Community 85 - "Whatif Contract Schema"
Cohesion: 0.12
Nodes (15): required, type, required, type, type, type, properties, changed (+7 more)

### Community 86 - "Ingest Test Suite"
Cohesion: 0.18
Nodes (8): bal(), test_amount_column_choice_and_unparsable(), test_bracketed_negative_in_balance_sheet(), test_clean_parse_candidates_and_confirm(), test_crore_unit_and_unknown_row_422(), test_no_ai_row_gives_gap_flag_and_none(), test_two_ambiguious_rows_are_candidates_confirm_picks(), test_unit_detection_and_choice()

### Community 87 - "compilerOptions"
Cohesion: 0.12
Nodes (15): compilerOptions, isolatedModules, jsx, lib, module, moduleResolution, noEmit, noFallthroughCasesInSwitch (+7 more)

### Community 88 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.13
Nodes (15): type, type, type, type, type, type, properties, ai_burn_annualised_inr (+7 more)

### Community 89 - "Defaults Contract Schema"
Cohesion: 0.13
Nodes (15): type, items, type, chart_sync, charts, proposal_states, view_rules, yearly_rule (+7 more)

### Community 90 - "Economics Contract Schema"
Cohesion: 0.14
Nodes (15): items, type, items, type, items, type, items, required (+7 more)

### Community 91 - "Pilots Contract Schema"
Cohesion: 0.17
Nodes (15): type, properties, required, type, type, properties, required, type (+7 more)

### Community 92 - "Views Dashboard Contract Schema"
Cohesion: 0.13
Nodes (15): type, type, type, type, best, compare_text, contenders, outcome (+7 more)

### Community 93 - "Views Dashboard Contract Schema"
Cohesion: 0.14
Nodes (15): properties, required, type, cash_vs_capacity, roi_breakeven, rows, unit, properties (+7 more)

### Community 94 - "Whatif Contract Schema"
Cohesion: 0.13
Nodes (15): type, type, type, actual, decision, metric, reasons, stop_rule (+7 more)

### Community 95 - "Whatif Contract Schema"
Cohesion: 0.13
Nodes (15): type, type, type, properties, required, type, budget_left, message (+7 more)

### Community 96 - "go_pkg_io"
Cohesion: 0.18
Nodes (5): New(), TestDoForwardsBodyAndRequestID(), TestDoReturnsNon2xxAsResponseNotError(), TestDoTimesOut(), TestPing()

### Community 97 - "time.Time"
Cohesion: 0.18
Nodes (5): Evidence, EvidenceIn, HistoryEvent, User, historyStub

### Community 98 - "xlsx.py"
Cohesion: 0.22
Nodes (7): _table(), _col_index(), is_xlsx(), _num_text(), read_first_sheet(), _text(), XlsxError

### Community 99 - "Defaults Contract Schema"
Cohesion: 0.14
Nodes (14): properties, required, type, type, type, type, cash_factor_by_type, cashable (+6 more)

### Community 100 - "Economics Contract Schema"
Cohesion: 0.14
Nodes (13): required, type, properties, portfolio_budget, rules, warnings, required, required (+5 more)

### Community 101 - "Forecast Usage Contract Schema"
Cohesion: 0.14
Nodes (13): type, type, type, type, properties, band_pct, flags, months (+5 more)

### Community 102 - "Health Contract Schema"
Cohesion: 0.14
Nodes (13): type, type, type, properties, contract_version, formula_version, ok, rules_version (+5 more)

### Community 103 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.14
Nodes (13): type, type, type, properties, corrections, flags, format, warnings (+5 more)

### Community 104 - "Pilots Import Parse Contract Schema"
Cohesion: 0.16
Nodes (14): type, properties, required, type, properties, required, type, type (+6 more)

### Community 105 - "Pilots Import Parse Contract Schema"
Cohesion: 0.18
Nodes (14): required, type, properties, type, properties, required, type, annual_benefit_inr (+6 more)

### Community 106 - "Pilots Import Parse Contract Schema"
Cohesion: 0.16
Nodes (14): items, type, items, type, required, type, items, type (+6 more)

### Community 107 - "Config Test Suite"
Cohesion: 0.24
Nodes (9): env(), intEnv(), Load(), splitCSV(), TestLoadDefaults(), TestLoadReportsAllProblems(), TestLoadRequiresDatabaseURL(), TestSecurityAndWorkerDefaults() (+1 more)

### Community 108 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.15
Nodes (13): type, type, required, type, type, properties, required, type (+5 more)

### Community 109 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.17
Nodes (13): type, type, type, properties, annualisation_factor, choice, multiplier, source (+5 more)

### Community 110 - "Compare Contract Schema"
Cohesion: 0.15
Nodes (12): properties, proposals, selection, warnings, type, required, $schema, required (+4 more)

### Community 111 - "Decision Contract Schema"
Cohesion: 0.15
Nodes (13): type, type, type, properties, required, type, best, contenders (+5 more)

### Community 112 - "Decision Contract Schema"
Cohesion: 0.15
Nodes (13): properties, required, type, properties, band, band_capped_by_fixture, checks, data_readiness (+5 more)

### Community 113 - "Defaults Contract Schema"
Cohesion: 0.15
Nodes (13): properties, required, type, type, properties, required, type, items (+5 more)

### Community 114 - "Defaults Contract Schema"
Cohesion: 0.15
Nodes (13): type, type, type, type, type, case_id, currency, data_version (+5 more)

### Community 115 - "Defaults Contract Schema"
Cohesion: 0.15
Nodes (13): items, type, properties, type, data_readiness_checks, no_clear_winner_margin_points, ranking_scale, renegotiate_max_run_cut (+5 more)

### Community 116 - "Defaults Contract Schema"
Cohesion: 0.15
Nodes (13): type, type, type, items, type, max_candidates, max_exact_candidates, node_budget (+5 more)

### Community 117 - "Compare Contract Schema"
Cohesion: 0.17
Nodes (12): properties, required, type, type, type, type, assumptions, discount_rate (+4 more)

### Community 118 - "Compare Contract Schema"
Cohesion: 0.18
Nodes (12): items, type, items, type, required, type, cumulative, head_to_head (+4 more)

### Community 119 - "Compare Contract Schema"
Cohesion: 0.17
Nodes (12): type, type, net_annual_savings_inr, price_per_1m_tokens, token_value_vs_price, value_per_1m_tokens, value_vs_price_ratio, properties (+4 more)

### Community 120 - "Defaults Contract Schema"
Cohesion: 0.17
Nodes (11): required, type, properties, pipeline_rules, starting_state, required, $schema, required (+3 more)

### Community 121 - "Defaults Contract Schema"
Cohesion: 0.17
Nodes (12): items, type, properties, required, type, items, type, type (+4 more)

### Community 122 - "Defaults Contract Schema"
Cohesion: 0.17
Nodes (12): properties, required, type, type, type, type, compare_rules, drop_rule (+4 more)

### Community 123 - "Views Dashboard Contract Schema"
Cohesion: 0.17
Nodes (12): type, type, type, properties, required, type, compare, decision (+4 more)

### Community 124 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, type, properties, type, ai_fixed_cost_inr_per_year, annual_price_drift_pct, annual_usage_growth_pct (+3 more)

### Community 125 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, properties, type, amount_inr, annualised_amount_inr, label, raw (+3 more)

### Community 126 - "Compare Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, type, type, budget, budget_left, message, ok (+3 more)

### Community 127 - "Defaults Contract Schema"
Cohesion: 0.18
Nodes (11): required, type, properties, required, type, type, type, defaults (+3 more)

### Community 128 - "Economics Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, type, type, type, annualised_ai_cost, at_risk_rule, competitiveness_ratio (+3 more)

### Community 129 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, properties, adoption_pct, id, readiness, setup_cost_inr, task (+3 more)

### Community 130 - "Pilots Contract Schema"
Cohesion: 0.18
Nodes (11): type, type, type, properties, type, ai_fixed_cost_inr_per_year, annual_price_drift_pct, annual_usage_growth_pct (+3 more)

### Community 132 - "Test Test Suite"
Cohesion: 0.35
Nodes (9): api(), api_code(), cleanup(), test.sh script, fail(), pass(), title(), show() (+1 more)

### Community 133 - "Defaults Contract Schema"
Cohesion: 0.20
Nodes (10): type, type, type, bad, base, good, scenario_multipliers, properties (+2 more)

### Community 134 - "Defaults Contract Schema"
Cohesion: 0.20
Nodes (10): type, type, contract_version, formula_version, rules_version, versions, type, properties (+2 more)

### Community 135 - "Defaults Contract Schema"
Cohesion: 0.20
Nodes (10): type, type, type, expected_first_pilot_payback_months, notes, pilots, records, properties (+2 more)

### Community 136 - "Defaults Contract Schema"
Cohesion: 0.20
Nodes (10): type, type, type, high, low, medium, readiness_factor, properties (+2 more)

### Community 137 - "Economics Contract Schema"
Cohesion: 0.20
Nodes (10): type, properties, required, type, type, type, competitive, competitiveness_edges (+2 more)

### Community 138 - "Forecast Usage Contract Schema"
Cohesion: 0.20
Nodes (10): properties, required, type, type, backtest, holdout_months, winner, winner_mape (+2 more)

### Community 139 - "Forecast Usage Contract Schema"
Cohesion: 0.20
Nodes (10): items, items, items, type, required, type, holdout_actual, scores (+2 more)

### Community 140 - "Forecast Usage Contract Schema"
Cohesion: 0.20
Nodes (10): required, type, properties, required, type, required, type, base (+2 more)

### Community 141 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.27
Nodes (10): properties, properties, type, type, type, annualisation_factor, column, period (+2 more)

### Community 142 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.20
Nodes (10): required, type, required, type, properties, required, type, annual_benefit_inr (+2 more)

### Community 143 - "Pilots Import Parse Contract Schema"
Cohesion: 0.27
Nodes (10): properties, properties, type, type, type, annualisation_factor, column, period (+2 more)

### Community 144 - "Pilots Contract Schema"
Cohesion: 0.20
Nodes (10): required, type, actual, decision, metric, stop_rule, threshold, properties (+2 more)

### Community 145 - "Portfolio Check Contract Schema"
Cohesion: 0.20
Nodes (9): type, type, properties, ok, problems, required, $schema, title (+1 more)

### Community 146 - "Views Dashboard Contract Schema"
Cohesion: 0.20
Nodes (10): type, headline, read_more, summary, text, type, properties, required (+2 more)

### Community 147 - "Engine Contract Examples Index"
Cohesion: 0.25
Nodes (9): Engine Contract Rules Version 7, Engine Contract Rules Version 8, CSV Export Response Contract, Decision Report Response Contract, Evidence Report Response Contract, Decision Memo Response Contract, Engine Contract Examples Index, Multi-period Cash Flow Model (cf-1) (+1 more)

### Community 148 - "Compare Contract Schema"
Cohesion: 0.22
Nodes (9): recommendation, selected, text, properties, required, type, items, type (+1 more)

### Community 149 - "Defaults Contract Schema"
Cohesion: 0.22
Nodes (9): properties, required, items, adoption_pct, description, id, observed_adoption_pct, observed_quality_pct (+1 more)

### Community 150 - "Worker Test Suite"
Cohesion: 0.43
Nodes (7): Backoff(), job(), TestBackoffGrows(), TestNotifyNeverBlocks(), TestWorkerFailsRejectedFilesAtOnceAndRetriesOutages(), TestWorkerStoresTheParseResult(), worker()

### Community 151 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.25
Nodes (8): items, type, required, type, confirmations, rows_confirmed, items, type

### Community 152 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.25
Nodes (8): type, type, properties, required, type, annualisation_factor, choice, period

### Community 153 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.25
Nodes (8): type, multiplier, source, unit, type, properties, required, type

### Community 154 - "Economics Contract Schema"
Cohesion: 0.25
Nodes (8): properties, required, type, annualised_ai_cost_inr, basis, class, competitiveness, ratio

### Community 155 - "Forecast Usage Contract Schema"
Cohesion: 0.32
Nodes (8): type, properties, properties, properties, items, type, annual_inr, monthly_inr

### Community 156 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.25
Nodes (8): type, type, choice, multiplier, unit, properties, required, type

### Community 157 - "Pilots Import Confirm Contract Schema"
Cohesion: 0.29
Nodes (8): items, type, required, type, items, type, extension_pilots, pilots

### Community 158 - "Pilots Import Parse Contract Schema"
Cohesion: 0.25
Nodes (8): type, type, choice, multiplier, unit, properties, required, type

### Community 159 - "rateLimiter"
Cohesion: 0.33
Nodes (5): newRateLimiter(), rateLimit(), TestRateLimiterRefills(), bucket, rateLimiter

### Community 162 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.29
Nodes (7): type, properties, type, amount_inr, label, row, type

### Community 163 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.29
Nodes (7): items, type, type, items, type, amount_columns, matched

### Community 164 - "Compare Contract Schema"
Cohesion: 0.29
Nodes (7): properties, required, type, capacity_benefit_inr, cash_benefit_inr, cash_share_pct, cash_vs_capacity

### Community 165 - "Decision Contract Schema"
Cohesion: 0.29
Nodes (7): properties, required, type, type, eligibility, eligible, gates

### Community 166 - "Defaults Contract Schema"
Cohesion: 0.29
Nodes (7): type, public_checks, tie_break_order, items, type, items, type

### Community 167 - "Pilots Import Parse Contract Schema"
Cohesion: 0.29
Nodes (7): required, type, properties, required, type, annual_running_cost_inr, periods

### Community 168 - "Forecast Usage Contract Schema"
Cohesion: 0.33
Nodes (6): properties, type, holdout_predictions, mape, method, usable

### Community 169 - "Whatif Contract Schema"
Cohesion: 0.33
Nodes (6): type, properties, required, type, observed_adoption_pct, overrides_applied

### Community 170 - "C23 AI Decision Desk Overview"
Cohesion: 0.33
Nodes (5): Steady State and Conservative Valuation, Dual Mode Optimization (Independent vs Conservative), Knapsack Portfolio Selection, Dynamic Stop-Rule Engine, C23 AI Decision Desk Overview

### Community 172 - "Pilot Plans DB Migration"
Cohesion: 0.70
Nodes (4): idx_pilot_plans_status, pilot_plans, touch_pilot_plans(), trg_pilot_plans_touch

### Community 173 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.40
Nodes (4): required, $schema, title, type

### Community 174 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.40
Nodes (4): required, $schema, title, type

### Community 175 - "Export Csv Contract Schema"
Cohesion: 0.40
Nodes (4): contentMediaType, $schema, title, type

### Community 176 - "Export Decision Report Contract Schema"
Cohesion: 0.40
Nodes (4): contentMediaType, $schema, title, type

### Community 177 - "Export Evidence Contract Schema"
Cohesion: 0.40
Nodes (4): contentMediaType, $schema, title, type

### Community 178 - "Export Memo Contract Schema"
Cohesion: 0.40
Nodes (4): contentMediaType, $schema, title, type

### Community 179 - "Export Yearly Csv Contract Schema"
Cohesion: 0.40
Nodes (4): contentMediaType, $schema, title, type

### Community 180 - "Views Dashboard Contract Schema"
Cohesion: 0.40
Nodes (4): required, $schema, title, type

### Community 181 - "Balance Sheet Parse Contract Schema"
Cohesion: 0.50
Nodes (4): items, type, required, candidates

### Community 182 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): properties, required, type, actions

### Community 183 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): properties, required, type, band_edges

### Community 184 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): properties, required, type, data_readiness_edges

### Community 185 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): properties, required, type, next_actions

### Community 186 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): ranking_weights, properties, required, type

### Community 187 - "Defaults Contract Schema"
Cohesion: 0.50
Nodes (4): reversal, properties, required, type

### Community 188 - "Forecast Usage Contract Schema"
Cohesion: 0.50
Nodes (4): items, required, type, low

### Community 190 - "Balance Sheet Confirm Contract Schema"
Cohesion: 0.67
Nodes (3): required, type, period

### Community 191 - "Defaults Contract Schema"
Cohesion: 0.67
Nodes (3): items, type, eligibility_gates

### Community 192 - "Defaults Contract Schema"
Cohesion: 0.67
Nodes (3): items, type, outcome_order

### Community 193 - "Defaults Contract Schema"
Cohesion: 0.67
Nodes (3): set_outcomes, items, type

### Community 194 - "Whatif Contract Schema"
Cohesion: 0.67
Nodes (3): required, type, base

## Ambiguous Edges - Review These
- `Persistent proposals with versioned inputs + evidence trail` → `Evidence bands, decision outcomes, next-best actions (Phase 5)`  [AMBIGUOUS]
  README.md · relation: conceptually_related_to

## Knowledge Gaps
- **24 isolated node(s):** `c23desk`, `@types/node`, `@types/react`, `@types/react-dom`, `typescript` (+19 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 1497 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **21 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `properties` connect `Defaults Contract Schema` to `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`, `Defaults Contract Schema`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._
- **Are the 45 inferred relationships involving `do()` (e.g. with `TestAuditCoversPlansAndMemo()` and `TestAuditListValidatesAndClamps()`) actually correct?**
  _`do()` has 45 INFERRED edges - model-reasoned connections that need verification._
- **What connects `c23desk`, `@types/node`, `@types/react` to the rest of the system?**
  _24 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Handlers` be split into smaller, more focused modules?**
  _Cohesion score 0.05625396825396825 - nodes in this community are weakly interconnected._
- **Why does `Handlers` connect `Handlers` to `Deps`, `Auth Test Suite`, `fakePlatform`, `handlers.go`, `Cache`?**
  _High betweenness centrality (0.004) - this node is a cross-community bridge._
- **Should `types.ts` be split into smaller, more focused modules?**
  _Cohesion score 0.0337465564738292 - nodes in this community are weakly interconnected._

### Low-confidence Hints
_AMBIGUOUS edges — the extractor was unsure. Verify before acting on these._

- **What is the exact relationship between `Persistent proposals with versioned inputs + evidence trail` and `Evidence bands, decision outcomes, next-best actions (Phase 5)`?**
  _Edge tagged AMBIGUOUS (relation: conceptually_related_to) - confidence is low._