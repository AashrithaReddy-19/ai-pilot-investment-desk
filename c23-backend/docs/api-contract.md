# C23 Backend API Contract (Go + Gin)

Base URL: `http://localhost:8080`. All bodies are JSON (`Content-Type: application/json`).
Every example below is **real output** captured by `scripts/smoke_test.ps1 -SaveExamples`; full request/response files are in
[`docs/api-examples/`](api-examples/) (file name in brackets).

## Conventions

| Topic | Rule |
|---|---|
| Money | `*_inr` fields are JSON numbers in INR, max 2 decimals, `0 ≤ x ≤ 10,000,000,000,000`. Strings such as `"2 lakh"` or `"200000"` are rejected. |
| Percent | `*_pct` fields are JSON numbers `0..100`, max 2 decimals. |
| Unknown | `null` (or an absent field) means **unknown, never zero**. Derived values that depend on an unknown are `null` with an explicit status (`payback_status: "NOT_CALCULABLE"`). |
| IDs | Pilot id: `^[A-Za-z0-9_-]{1,64}$` (unique, case-insensitive). Portfolio id: lowercase UUID. Overlap id = normalised overlap group (`^[a-z0-9][a-z0-9 _.&-]{0,63}$`). |
| Strictness | Unknown fields → 422 `UNKNOWN_FIELD`/`INVALID_FIELD`; duplicate JSON keys → 422 `DUPLICATE_KEY`; `NaN`/`Infinity` → 400 `MALFORMED_JSON`; body > 1 MiB → 413. |
| Versioning | Updates (`PUT`, approve, resolve, optimize-apply) require the current `version`; stale → 409 `VERSION_CONFLICT`. |
| Decisions | `PROCEED` · `REVIEW` · `REVISE` · `STOP` (severity STOP > REVISE > REVIEW > PROCEED), rules version `c23-rules-v1`. |
| AI | `ai_status`: `ok` · `degraded` (service down/slow/invalid; deterministic data unaffected) · `disabled` (not configured). Always the service's **current** state, also on cache hits. |
| Headers (optional) | `X-Request-ID` (echoed; in logs and audit), `X-Actor` (audit label, not authentication), `X-API-Key` (required for POST/PUT only if `API_KEY` is set). Response: `X-Cache: HIT|MISS` on cached endpoints. |

### Error format (every 4xx/5xx)

```json
{"error":{"code":"VALIDATION_FAILED","message":"input failed validation",
  "fields":[{"field":"setup_cost_inr","code":"NOT_NUMERIC","message":"setup_cost_inr must be a JSON number in INR (strings such as \"2 lakh\" are rejected)"}],
  "request_id":"72abd5968da54bb4"}}
```
[error-validation]

| HTTP | `error.code` |
|---|---|
| 400 | `MALFORMED_JSON` [error-malformed-json], `MALFORMED_INPUT`, `INVALID_ID` [error-invalid-id], `INVALID_FORMAT` |
| 401 | `UNAUTHORIZED` (only when `API_KEY` is set) |
| 404 | `NOT_FOUND` [error-not-found], `ROUTE_NOT_FOUND` |
| 405 | `METHOD_NOT_ALLOWED` |
| 409 | `CONFLICT` (duplicate id/name), `VERSION_CONFLICT`, `APPROVAL_BLOCKED` (`details.blockers`) [approve-blocked] |
| 413 | `PAYLOAD_TOO_LARGE` |
| 422 | `VALIDATION_FAILED` + `fields[].code`: `REQUIRED`, `NOT_NUMERIC`, `NEGATIVE_VALUE`, `OUT_OF_RANGE`, `TOO_PRECISE`, `TOO_LARGE`, `EMPTY`, `NOT_TEXT`, `TOO_LONG`, `INVALID_ID`, `INVALID_LABEL`, `INVALID_ENUM`, `UNKNOWN_FIELD`, `INVALID_FIELD`, `DUPLICATE_KEY`, `DUPLICATE_ID`, `DUPLICATE_PILOT`, `UNKNOWN_PILOT`, `INVALID_RESOLUTION`, `NOT_IN_GROUP`, `PRESET_EXCLUSIVE`, `ADJUST_UNKNOWN_BASE`, `TOO_MANY_CANDIDATES` |
| 503 | `DATABASE_UNAVAILABLE` (no internal details exposed) |

---

## 1. Platform

### `GET /health` — always 200 [health]
```json
{"status":"ok","rules_version":"c23-rules-v1","time":"2026-10-17T14:07:52Z",
 "components":{"api":"ok","database":"ok","redis":"ok","ai_service":"ok"},
 "note":"redis and ai_service are optional; their failure never changes deterministic results"}
```
`status` is `degraded` only when the database is unavailable. `redis`: `ok|degraded|disabled`; `ai_service`: `ok|degraded|disabled`.

### `GET /ready` — 200 `{"ready":true,"database":"ok"}` or 503 `{"ready":false,"database":"unavailable"}` [ready]

### `GET /api/v1/rules`
Rules version, thresholds (`default_minimum_adoption_pct` 60, `default_minimum_quality_pct` 80, `low_readiness_below_pct` 50, ...), reason priority, `reason_forces_decision`, tie-break order, resolution types, disclaimer.

### `GET /api/v1/defaults`
`{"pilot_budget_inr":600000,"pilot_budget_source":"TEAM_ASSUMPTION (extension.json)","thresholds":{...},"rules_version":"c23-rules-v1"}`

---

## 2. Data import

### `POST /api/v1/import/official` — no body
Imports `data/raw/official_fixture.json` + `extension.json` after SHA-256 verification against `MANIFEST.json`; values labelled `OFFICIAL_FIXTURE`.
201 `{"imported":["AI1",...,"AI5"],"rejected":[],"warnings":[],"file_errors":[],"source":"OFFICIAL_FIXTURE","pilot_budget_inr":600000}` ·
409 `CONFLICT` if already imported [import-official] · 500 `OFFICIAL_DATA_UNAVAILABLE` if a raw file was tampered with.

### `POST /api/v1/import`
```json
{"fixture": {"records":{"pilots":[ {...pilot...} ]}} , "extension": {"pilot_budget_inr":600000,"pilots":[{"id":"AI1","adoption_pct":70,"readiness":"high"}]}}
```
`fixture` may also be a bare array of pilots. **All-or-nothing**: any invalid row → 422 with `details.rejected[]` (row, id, errors) and nothing stored. Values labelled `TEAM_ASSUMPTION` (actual_* → `OBSERVED`). 201 same shape as above.

---

## 3. Pilots

**Pilot fields** (input and output): `id`*, `task`*, `description`, `setup_cost_inr`, `annual_running_cost_inr`, `annual_benefit_inr`,
`benefit_type` (free text; canonicalised to `cashable|time_freed|other|unknown`, original kept in `benefit_type_raw`), `overlap_group`
(normalised; `null` = unknown), `expected_adoption_pct`, `actual_adoption_pct`, `minimum_adoption_pct`, `expected_quality_pct`,
`actual_quality_pct`, `minimum_quality_pct`, `data_readiness_pct`, `technical_readiness_pct`, `process_readiness_pct`, `readiness_label`
(`low|medium|high`), `capacity_value_inr`, `realized_cash_saving_inr`. Output adds `field_sources` (per-field provenance:
`OFFICIAL_FIXTURE|TEAM_ASSUMPTION|OBSERVED|UNKNOWN`), `assumptions`, `version`, `created_at`, `updated_at`. (* required)

### `GET /api/v1/pilots` [list-pilots]
`{"count":5,"pilots":[{"pilot":{...},"decision":"REVIEW","primary_reason":"INSUFFICIENT_EVIDENCE","reason_codes":[...],"warnings":[...],"calculated_metrics":{...},"failed_rules":[...],"rules_version":"c23-rules-v1"}]}`

### `POST /api/v1/pilots` → 201 pilot [create-pilot]
```json
{"id":"P9","task":"Invoice matching","setup_cost_inr":100000,"annual_running_cost_inr":10000,"annual_benefit_inr":80000,
 "benefit_type":"cashable","overlap_group":"finance","actual_adoption_pct":80,"actual_quality_pct":90}
```
Errors: 422 field errors, 409 duplicate id (case-insensitive).

### `GET /api/v1/pilots/{id}` → pilot [get-pilot-ai1] · 400 bad id · 404

### `PUT /api/v1/pilots/{id}` → updated pilot
Full replacement; body must include the current `"version"`. The object returned by GET can be sent back as-is (read-only fields are ignored).
Changed fields become `TEAM_ASSUMPTION` (actual_* → `OBSERVED`); unchanged fields keep their provenance. 409 `VERSION_CONFLICT` if stale. Audited (before/after).

### `POST /api/v1/pilots/{id}/evaluate` — no body [evaluate-ai1]
Evaluates with `c23-rules-v1`, **stores the decision** (input + calculation snapshot), adds an advisory explanation.
```json
{"evaluation":{"pilot_id":"AI1","rules_version":"c23-rules-v1","decision":"REVIEW","primary_reason":"INSUFFICIENT_EVIDENCE",
   "reason_codes":["INSUFFICIENT_EVIDENCE"],"warnings":[],
   "failed_rules":[{"rule":"R9","code":"INSUFFICIENT_EVIDENCE","forces_decision":"REVIEW","metric":"actual_adoption_pct/actual_quality_pct","threshold":"measured","actual":"NOT_MEASURED","message":"..."}],
   "calculated_metrics":{
     "stated_basis":{"annual_benefit_inr":180000,"net_annual_benefit_inr":120000,"monthly_net_benefit_inr":10000,"payback_months":20,"payback_status":"CALCULABLE","roi_pct":60},
     "cash_basis":{...}, "cash_basis_rule":"cashable: ...", "breakeven_adoption_pct":33.33, "overall_readiness_pct":null,
     "readiness_level":"high", "readiness_basis":"readiness_label (TEAM_ASSUMPTION)", "adoption_status":"NOT_MEASURED", "quality_status":"NOT_MEASURED",
     "stage":"PRE_PILOT", "minimum_adoption":{"value":60,"source":"TEAM_ASSUMPTION (rules default)"}, "minimum_quality":{"value":80,...}, "low_readiness_below_pct":50},
   "input_snapshot":{...pilot...},"explanation":"AI1: REVIEW (INSUFFICIENT_EVIDENCE). Stated basis: net INR 120000/yr, INR 10000/month, payback 20 months. ..."},
 "decision_id":12,"ai_status":"ok","ai_explanation":"AI1 needs human review before wider investment: decision REVIEW. ...","ai_model":"template-explainer@1.0.0","cache":"MISS"}
```
`payback_status`: `CALCULABLE | NOT_CALCULABLE (input unknown) | NEVER_PAYS_BACK (net ≤ 0) | IMMEDIATE (setup 0)`.
Reason codes: `MISSING_SETUP_COST`, `MISSING_BENEFIT`, `MISSING_RUNNING_COST`, `NEVER_PAYS_BACK`, `QUALITY_THRESHOLD_FAILED`, `ADOPTION_BELOW_BREAKEVEN`,
`LOW_ADOPTION`, `LOW_READINESS`, `TIME_NOT_CASH`, `OVERLAP_UNKNOWN`, `INSUFFICIENT_EVIDENCE` (see docs/business-rules.md).
With AI down: same `evaluation`, `"ai_status":"degraded"`, `"ai_explanation":null`.

### `GET /api/v1/pilots/{id}/decisions` [list-decisions]
`{"pilot_id":"AI1","decisions":[{"id","pilot_id","pilot_version","decision","primary_reason","reason_codes","rules_version","input_snapshot","calculation_snapshot","created_at"}]}` (newest first, max 200)

### `POST /api/v1/pilots/{id}/scenarios` → 201 [scenario-running-120000]
What-if on a **copy** (official values never change). Body (choose one style):
```json
{"name":"judge","overrides":{"annual_running_cost_inr":120000}}
{"name":"shock","adjust_pct":{"annual_running_cost_inr":25}}
{"name":"shock","preset":"quality_down_10"}
{"overrides":{"annual_running_cost_inr":null},"assume_missing_run_cost":true}
```
Response: `{"scenario_id":7,"result":{"name","base_pilot_id","rules_version","changes":[{"field":"annual_running_cost_inr","before":60000,"after":120000}],"assumptions":[...],"baseline":{evaluation},"scenario":{evaluation},"label":"SCENARIO (team assumption, not official data)"}}`
→ scenario stated basis: net 60000, monthly 5000, payback **40**. Errors: 422 for invalid overrides, `id` override, unknown preset, adjusting an unknown value (`ADJUST_UNKNOWN_BASE`).

### `GET /api/v1/pilots/{id}/scenarios` [list-scenarios] — stored runs, newest first
### `GET /api/v1/scenario-presets`
`running_cost_up_25`, `benefit_down_20`, `adoption_down_30`, `quality_down_10`, `missing_running_cost` (label + description).

---

## 4. Portfolios

**Assessment object** (in every portfolio response and preview): `status` (`DRAFT|NEEDS_REVIEW|VALID|APPROVED|REJECTED`), `approval_allowed`, `basis`
(`cash` default | `stated`), `budget_inr`, `pilot_ids`, `total_setup_cost_inr`, `total_annual_running_cost_inr`, `total_claimed_benefit_inr`,
`total_recognized_benefit_inr`, `net_annual_benefit_inr`, `monthly_net_benefit_inr`, `payback{payback_months,payback_status}`, `budget_remaining_inr`,
`benefit_ledger[]`, `overlaps[]`, `blockers[]`, `warnings[]`, `pilot_decisions{}`.

Ledger row [create-portfolio-overlap]:
`{"pilot_id":"AI5","benefit_group":"support","claimed_benefit_inr":180000,"basis_benefit_inr":180000,"recognized_benefit_inr":0,"annual_running_cost_inr":60000,"source":"OFFICIAL_FIXTURE","resolution":"NONE","review_status":"UNRESOLVED","review_note":"Shares benefit group \"support\" with AI4: not counted until resolved."}`
Overlap: `{"id":"support","overlap_group":"support","pilot_ids":["AI4","AI5"],"code":"DOUBLE_COUNT_RISK","status":"UNRESOLVED","resolution":null,"claimed_total_inr":360000,"benefit_pool_inr":180000,"recognized_total_inr":180000}`
Blocker codes: `BUDGET_MISSING`, `BUDGET_EXCEEDED`, `STOP_PILOT_SELECTED`, `FINANCE_INCOMPLETE` (→ REJECTED); `OVERLAP_UNRESOLVED`, `OVERLAP_UNKNOWN`, `PILOT_NEEDS_REVISION` (→ NEEDS_REVIEW).

### `POST /api/v1/portfolios` → 201 portfolio [create-portfolio-valid]
```json
{"name":"Q3 plan","budget_inr":600000,"basis":"cash","pilot_ids":["AI1","AI2","AI4"],"overlap_resolutions":[]}
```
Portfolio: `{"id","name","budget_inr","basis","status","rules_version","pilot_ids","overlap_resolutions","assessment","approved_snapshot?","approved_at?","approval_stale","version","created_at","updated_at"}`.
Any status can be saved (an over-budget draft is stored as `REJECTED`). 422: name pattern, null/negative/string budget, duplicate or unknown pilot, bad basis; 409 duplicate name.

### `GET /api/v1/portfolios` · `GET /api/v1/portfolios/{id}` [get-portfolio]
GET recomputes the assessment from current pilot data (`X-Cache`). For an approved portfolio, `approval_stale: true` if inputs changed since approval.

### `PUT /api/v1/portfolios/{id}` — same body as create + `"version"`; clears any approval; audited.

### `POST /api/v1/portfolios/{id}/approve` — `{"version":1}` [approve-ok]
Recomputes everything; succeeds only if status is `VALID` → `APPROVED` with a frozen snapshot. Otherwise 409 [approve-blocked]:
```json
{"error":{"code":"APPROVAL_BLOCKED","message":"portfolio status is NEEDS_REVIEW; approval requires VALID",
  "details":{"status":"NEEDS_REVIEW","blockers":[{"code":"PILOT_NEEDS_REVISION","pilot_id":"AI5","message":"AI5 must be revised before funding (LOW_READINESS)."},
  {"code":"OVERLAP_UNRESOLVED","message":"DOUBLE_COUNT_RISK: [AI4 AI5] share benefit group \"support\" (claimed INR 360000, pool INR 180000). Resolve before approval."}]},"request_id":"..."}}
```

### `GET /api/v1/portfolios/{id}/overlaps` [portfolio-overlaps]
`{"portfolio_id","overlaps":[...],"pilots_with_unknown_overlap_group":[],"resolution_types":[...],"ai_status":"ok","ai_suggestions":[{"pilot_a":"AI4","pilot_b":"AI5","semantic_similarity":0.5845,"possible_overlap":true,"band":"possible","reason":"...","method":"lexical-domain-v1"}],"ai_model":"lexical-domain-overlap@1.1.0","authoritative":"overlap_group rules decide accounting and approval; ai_suggestions are advisory only"}`

### `POST /api/v1/portfolios/{id}/overlaps/{group}/resolve` → portfolio [resolve-overlap]
```json
{"resolution_type":"CUSTOM_ALLOCATION","allocations_inr":{"AI4":120000,"AI5":60000},"review_note":"agreed with support lead","version":1}
{"resolution_type":"EXCLUDE_SECOND","primary_pilot_id":"AI4","version":1}
{"resolution_type":"SPLIT_BENEFIT","shares_pct":{"AI4":70,"AI5":30},"version":1}
{"resolution_type":"REMOVE_ONE","primary_pilot_id":"AI4","version":1}
```
Validated against the shared pool: each allocation ≤ own claim, total ≤ pool; otherwise 422 `INVALID_RESOLUTION` [resolve-overlap-invalid]. 404 if the group is not an overlap in this portfolio.

### `POST /api/v1/portfolios/{id}/optimize` — `{}` or `{"apply":true,"version":1}`
`{"optimization":{OptimizeResult},"applied":false,"portfolio":null}`. **OptimizeResult** [preview-optimize]: `rules_version, method ("exhaustive subset search"), basis, budget_inr, selected[], excluded[{pilot_id,code,reason}], candidates_considered, combinations_evaluated, tie_break_order[], message, assessment`.

---

## 5. Previews and stateless endpoints (nothing stored)

| Route | Body | Response |
|---|---|---|
| `POST /api/v1/preview/portfolio` [preview-over-budget] | `{"budget_inr":600000,"basis":"cash","pilot_ids":["AI1","AI2","AI3","AI4"],"overlap_resolutions":[]}` | `{"assessment":{... "status":"REJECTED","budget_remaining_inr":-200000 ...},"pilot_evaluations":{...},"persisted":false}` |
| `POST /api/v1/preview/optimize` [preview-optimize] | `{"budget_inr":100000,"basis":"cash"}` | `{"optimization":{...},"persisted":false}` (stored pilots) |
| `POST /api/v1/evaluate` [evaluate-stateless-time-freed] | a pilot object | `{"evaluation":{...},"persisted":false}` — works with the database down |
| `POST /api/v1/optimize` [optimize-stateless] | `{"budget_inr":600000,"basis":"cash","pilots":[...] or fixture}` | `{"result":{OptimizeResult},"rejected_rows":[],"persisted":false}` — works with the database down |
| `GET /api/v1/dashboard?portfolio_id=` | — | decision counts, missing-evidence count, shared groups, pilots, newest approved (else newest) portfolio or optimizer recommendation |

## 6. Export and audit

| Route | Response |
|---|---|
| `GET /api/v1/export/{portfolioId}?format=json` [export-json] | `{"disclaimer","rules_version","portfolio","pilot_evaluations"}` |
| `?format=markdown` / `?format=csv` | evidence report / benefit ledger (CSV formula-injection safe) |
| `GET /api/v1/audit/{pilot\|portfolio}/{id}` [audit-portfolio] | `{"entity_type","entity_id","entries":[{"id","entity_type","entity_id","action","actor","request_id","before_state","after_state","created_at"}]}` |
| `GET /api/v1/activity?limit=50` | latest audit entries across all entities |

Audit actions: `import`, `create`, `update`, `approve`, `optimize_apply`, `resolve_overlap:{group}`.

---

## 7. Internal AI service (FastAPI, port 8001 — called only by the Go API)

| Route | Body | Response |
|---|---|---|
| `GET /health` | — | `{"status":"ok","advisory_only":true,"capabilities":{...}}` |
| `POST /internal/semantic-overlap` | `{"pilots":[{"id":"AI4","text":"...","overlap_group":"support"}, ...]}` (2–20) | `{"model_name":"lexical-domain-overlap","model_version":"1.1.0","advisory":true,"threshold":0.3,"pairs":[{"pilot_a","pilot_b","semantic_similarity","possible_overlap","band","reason","method"}]}` |
| `POST /internal/explain` | `{"evaluation":{...Go evaluation...}}` | `{"model_name":"template-explainer","model_version":"1.0.0","explanation","grounded","checks","failures"}` |

The Go client enforces a 1.5 s timeout and rejects responses with unknown pilot IDs, scores outside 0–1, `advisory:false` or `grounded:false`.
