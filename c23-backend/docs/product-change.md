# Product change evidence: "Add recurring costs, adoption and stop rules"

| | |
|---|---|
| **Request** | C23 case bank, `product_change`: *"Add recurring costs, adoption and stop rules"* (OFFICIAL). Also official check 3: *"Low adoption or failed quality threshold causes an explicit revise/stop decision."* MBA-team sign-off: **MANUAL** (see `business/negotiation_log.csv`). |
| **Decision** | Implement as versioned deterministic rules (`c23-rules-v1`), never as AI. Thresholds are TEAM_ASSUMPTION defaults (60% adoption, 80% quality) overridable per pilot, pending interview evidence. |
| **Scope** | Recurring cost (nullable, never 0), expected/actual/minimum adoption, break-even adoption, actual/minimum quality, readiness, STOP/REVISE/REVIEW/PROCEED with reason codes, audit snapshot of every decision, scenario presets for adoption/quality shocks. Out of scope: automated measurement of adoption (needs real usage data). |
| **Implementation** | `backend/internal/finance` (running cost in net/payback; unknown → NOT_CALCULABLE), `backend/internal/decision` (rules R1–R9), `backend/internal/rules` (thresholds + version), `backend/internal/store` (`decisions` table with input + calculation snapshot), API: `POST /pilots/{id}/evaluate` (failed rules with threshold vs actual), scenario presets (`adoption_down_30`, `quality_down_10`, `missing_running_cost`). |
| **Tests** | `TestCaseB_NullRunningCost`, `TestCaseE_AdoptionBelowBreakevenStops`, `TestLowAdoptionAboveBreakevenRevises`, `TestCaseF_QualityFailureStops`, `TestCaseG_NeverPaysBack`, `TestPresetsUseTheNormalEngine`, API demos 4–6, live smoke test (`scripts/smoke_test.ps1`). |
| **Result** | Running cost 60000 → 120000: payback 20 → 40 months. Adoption 20% (< 33.33% break-even): STOP. Adoption 45% (< 60%): REVISE. Quality 60% (< 80%): STOP even with a 20-month payback. Running cost unknown: payback unknown, REVISE. All automated and passing. |

## Before / after (legacy v0 → c23-rules-v1)

| Behaviour | v0 prototype | v1 |
|---|---|---|
| Running cost missing | silently assumed 20% of setup | unknown → REVISE (assumption only in an explicit, labelled scenario) |
| Quality below threshold | REVISE | STOP (overrides good financials) |
| Unknown overlap group | treated as standalone | OVERLAP_UNKNOWN, blocks approval |
| Decision explanation | one sentence | all reason codes + failed rules with threshold/actual + stored snapshot |
