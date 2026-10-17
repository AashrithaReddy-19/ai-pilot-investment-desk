# Contribution matrix

> **MANUAL ACTION REQUIRED:** fill in real team member names and confirm ownership. Nothing below attributes work to a
> person; module evidence is listed so each member can claim what they actually built and explain it.

| Module | Code evidence | Test evidence | Product / business contribution | Demo talking point | Owner (real name) |
|---|---|---|---|---|---|
| Financial engine | `backend/internal/money`, `internal/finance` | `finance_test.go` (20/40-month release gate), `money_test.go` | Exact INR, null ≠ 0 | "20 months is computed, never hard-coded" | MANUAL |
| Decision rules | `internal/decision`, `internal/rules` | `decision_test.go` (cases B, E, F, G) | Product change: recurring cost, adoption, stop rules | "Quality failure stops a 20-month payback" | MANUAL |
| Portfolio, overlap ledger, optimizer | `internal/portfolio` | `portfolio_test.go` (case C, D, 300 randomised invariants) | Double-count prevention | "AI4 + AI5 count once" | MANUAL |
| Scenarios & presets | `internal/scenario` | `scenario_test.go`, `preset_test.go` | Business shock testing | "Running cost +25% → 22.86 months" | MANUAL |
| Persistence & audit | `internal/store`, migrations | `store_test.go` (real PostgreSQL, rollback, concurrency) | "Why did this change?" | "Exactly one of 6 concurrent approvals wins" | MANUAL |
| API, cache, AI client | `internal/httpapi`, `internal/cache`, `internal/aiclient`, `internal/service` | `api_test.go`, `redteam_test.go`, `views_test.go` | Resilience | "Kill AI or Redis: same numbers" | MANUAL |
| AI service & evals | `decision-engine/app/ai/*`, `app/ai_service.py`, `evals/` | `test_ai_service.py`, `reports/ai_evals.md` | Advisory AI that cannot change money | "130 hallucination attacks rejected" | MANUAL |
| Data pipeline | `decision-engine/app/schemas.py`, `pipeline.py`, `scripts/` | `test_validation.py`, `test_pipeline.py`, `test_scripts.py` | Data audit, provenance | "Official data is SHA-256 locked" | MANUAL |
| Business evidence | `docs/business/*`, `docs/interviews/` | — | Venture brief, unit economics, growth plan | "No fake validation" | MANUAL |
| Interviews | `docs/interviews/interview-{1,2,3}.md` | — | Thresholds from real evidence | — | MANUAL (not yet done) |
