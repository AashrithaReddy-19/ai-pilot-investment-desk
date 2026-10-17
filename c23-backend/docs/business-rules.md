# C23 Business Rules (`c23-rules-v1`)

Every evaluation, decision row and portfolio assessment carries `rules_version: c23-rules-v1`.
Source labels: **OFFICIAL** = C23 fixture / case bank; **TEAM** = team assumption (documented, changeable).

## 1. Financial formulas (OFFICIAL check 1) — `internal/finance`

| Metric | Formula | Unknown / edge handling |
|---|---|---|
| Net annual benefit | benefit − running cost | unknown if either input is unknown |
| Monthly net benefit | net annual / 12 | unknown if net unknown |
| Simple payback (months) | setup / monthly net | `NOT_CALCULABLE` if an input is unknown; `NEVER_PAYS_BACK` (null) if monthly net ≤ 0; `IMMEDIATE` (0) if setup = 0 and net > 0 |
| ROI % | net annual / setup × 100 | null if setup = 0 |
| Break-even adoption % | running / cash benefit × 100 | adoption at which the (linear-in-adoption) benefit covers running cost |

Money is stored as integer **paise**; derived values use exact rationals and are rendered to 2 decimals.
Reference: 200000 / 60000 / 180000 → net **120000**, monthly **10000**, payback **20** (exact, computed, never hard-coded).

## 2. Null policy (OFFICIAL: "null means unknown, never zero")

| Field null | Behaviour |
|---|---|
| setup / benefit / running cost | stays unknown; payback `NOT_CALCULABLE`; decision **REVISE** (`MISSING_*`); excluded from optimizer; blocks approval (`FINANCE_INCOMPLETE`) |
| running cost in a scenario with `assume_missing_run_cost: true` | setup × 20% (TEAM, from `extension.json`), flagged `RUN_COST_ASSUMED`, listed in `assumptions` |
| actual adoption / quality | `NOT_MEASURED` (never 0) → `INSUFFICIENT_EVIDENCE` |
| readiness | `READINESS_UNKNOWN` warning; no level is invented |
| overlap_group | `OVERLAP_UNKNOWN`; never treated as "no overlap"; blocks approval |
| budget | portfolio create rejected (422); stateless optimizer rejects; no portfolio without a known budget |

Nothing financial is ever mean/median-imputed.

## 3. Pilot decision rules — `internal/decision`

All rules run; every triggered reason code is returned (priority order). Final decision = **most severe**:
STOP > REVISE > REVIEW > PROCEED.

| # | Condition | Reason code | Forces | Source |
|---|---|---|---|---|
| R1 | setup / benefit / running unknown | `MISSING_SETUP_COST` / `MISSING_BENEFIT` / `MISSING_RUNNING_COST` | REVISE | OFFICIAL (null ≠ 0) |
| R2 | net annual cash benefit ≤ 0 | `NEVER_PAYS_BACK` | STOP | OFFICIAL edge |
| R3 | actual quality < minimum quality | `QUALITY_THRESHOLD_FAILED` | **STOP** (overrides good financials) | OFFICIAL check 3; STOP severity = TEAM decision |
| R4 | actual adoption < break-even adoption | `ADOPTION_BELOW_BREAKEVEN` (+`LOW_ADOPTION`) | STOP | OFFICIAL check 3 |
| R5 | actual adoption < minimum adoption | `LOW_ADOPTION` | REVISE | OFFICIAL check 3 |
| R6 | readiness level low | `LOW_READINESS` | REVISE | OFFICIAL edge ("low data readiness") |
| R7 | benefit not cashable and no realised cash saving | `TIME_NOT_CASH` | REVIEW | OFFICIAL gap ("cash vs freed capacity") |
| R8 | overlap group unknown | `OVERLAP_UNKNOWN` | REVIEW | OFFICIAL check 2 |
| R9 | adoption or quality not measured | `INSUFFICIENT_EVIDENCE` | REVIEW | case bank: "measure adoption and quality before wider investment" |
| – | none of the above | `ALL_RULES_PASSED` | PROCEED | |

Thresholds (TEAM, `extension.json` defaults; a test fails if code and file drift):
minimum adoption **60%**, minimum quality **80%** — a pilot's own `minimum_*_pct` overrides them.
Readiness: mean of supplied `*_readiness_pct`; < **50** low, ≥ **75** high (TEAM; only used when numbers are supplied),
otherwise the `readiness_label` (TEAM) is used. Boundaries: a value **equal** to a minimum passes.

Warnings (no decision change): `DOUBLE_COUNT_RISK` (shares a group with another pilot), `READINESS_UNKNOWN`,
`ADOPTION_EXPECTATION_UNKNOWN`, `BENEFIT_TYPE_UNKNOWN`, `ZERO_SETUP_COST`, `RUN_COST_ASSUMED`.

Official fixture today (nothing measured yet): AI1–AI4 **REVIEW** (`INSUFFICIENT_EVIDENCE`); AI5 **REVISE**
(`LOW_READINESS`, readiness label "low" is a TEAM assumption).

## 4. Time freed is not cash saved (OFFICIAL gap)

`capacity_value_inr` and `realized_cash_saving_inr` are stored separately. Two bases are always reported:
**stated** (annual_benefit as supplied) and **cash** (cashable → annual benefit; anything else → only
`realized_cash_saving_inr`, otherwise unknown). Portfolios and the optimizer default to the **cash** basis.
The fixture's `"cashable assumption"` is treated as cashable but the ledger notes that cashability is itself an assumption.

## 5. Overlap and the benefit attribution ledger (OFFICIAL check 2)

Pilots sharing a known `overlap_group` draw on **one benefit pool** = the largest single claim in the group (TEAM rule:
the same hours/revenue cannot be saved twice). AI4 + AI5 (`support`): claimed 360000, pool 180000.

| State | Recognised benefit | Approval |
|---|---|---|
| Unresolved | only the largest claim (lowest ID on ties); others 0 | blocked (`OVERLAP_UNRESOLVED`, `DOUBLE_COUNT_RISK`) |
| `REMOVE_ONE` | non-primary pilots removed from the portfolio | allowed |
| `EXCLUDE_SECOND` | primary counted; others 0 (their running cost still counts) | allowed |
| `SPLIT_BENEFIT` | pool × share (default equal), floored to paise, capped at own claim | allowed |
| `CUSTOM_ALLOCATION` | explicit INR; each ≤ own claim; total ≤ pool | allowed |

Invalid resolutions are rejected (422) and can never raise the recognised total above the pool; the database also
enforces recognised ≤ basis benefit per ledger row. Every ledger row records claimed, basis, recognised, group,
source, resolution, review status and note.

## 6. Portfolio status and approval — `internal/portfolio`

| Status | When |
|---|---|
| `DRAFT` | no pilots |
| `REJECTED` | budget missing or exceeded, a STOP pilot selected, a critical finance input unknown |
| `NEEDS_REVIEW` | unresolved overlap, unknown overlap group, or a REVISE pilot (revise before committing money) |
| `VALID` | none of the above → approval allowed |
| `APPROVED` | `POST /approve` succeeded on a freshly recomputed VALID assessment; snapshot + fingerprint frozen |

Any later change clears approval; if pilot data changes underneath an approved portfolio, `approval_stale: true`.
Budget rule: total setup ≤ budget (equal is allowed; exact to the paisa).

## 7. Optimizer

Exhaustive search over all subsets of eligible pilots (≤ 20 candidates; C23 has 5–8 → ≤ 256 subsets). No ML.
Eligible: finance complete, not STOP, not REVISE, overlap group known, positive net on the basis.
Constraints: setup ≤ budget, at most one pilot per overlap group. Objective: maximise recognised annual net benefit.
Tie-break: higher net → faster payback → higher readiness → higher adoption → lower setup → stable ID order.
Candidates are sorted by ID first, so input order cannot change the result (tested with 50 random shuffles).
Every excluded pilot gets a reason code.

## 8. Scenarios

Overrides are validated exactly like real input, applied to a copy, labelled TEAM (actual_* stay OBSERVED), evaluated
side by side with the baseline and stored in `scenario_runs`. Official values are never modified.
