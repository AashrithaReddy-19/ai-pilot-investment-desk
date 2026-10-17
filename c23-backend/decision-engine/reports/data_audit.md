# C23 Data Audit

> Read-only audit. Synthetic teaching fixture; not a benchmark dataset.

Audit version 1.0.0.

## Inputs

| Role | File | Format | SHA-256 |
|---|---|---|---|
| fixture | official_fixture.json | json | `83a42f1c0f4a607f…` |
| extension | extension.json | json | `f4df7f76430855bd…` |

## Dataset metadata

- **case_id**: C23
- **synthetic**: True
- **data_version**: v3.2
- **currency**: INR unless explicitly stated
- **missing_values**: null means unknown, never zero
- **limit**: Small teaching fixture for a bounded prototype. Market demand, willingness to pay and production performance remain unproven.

## Key observations

- Dataset is declared synthetic: a teaching fixture, not a benchmark. No statistical claims are made from it.
- 5 pilot rows; ML training/CV on these rows would be meaningless.
- setup_cost_inr is identical for all 5 pilots (200000): official economics do not differentiate pilots; ranking depends on team assumptions and the documented tie-break.
- annual_running_cost_inr is identical for all 5 pilots (60000): official economics do not differentiate pilots; ranking depends on team assumptions and the documented tie-break.
- annual_benefit_inr is identical for all 5 pilots (180000): official economics do not differentiate pilots; ranking depends on team assumptions and the documented tie-break.
- Overlap group 'support' is shared by AI4, AI5: DOUBLE_COUNT_RISK; benefit may be counted only once.
- benefit_type 'cashable assumption' (5 rows): cashability is itself an assumption, not proven cash removal.
- No training labels (adoption_success / quality_success) present: no target class distribution.
- observed_adoption_pct is null for every pilot: NOT_MEASURED (unknown, not zero).
- observed_quality_pct is null for every pilot: NOT_MEASURED (unknown, not zero).

## Table `fixture_pilots`

- Rows: 5 (non-object rows: 0)
- Duplicate rows: 0; duplicate ids: none
- Unknown columns: none

| Column | Kind | Types | Absent | Null | Missing % | Numeric (min / median / max) | Distribution |
|---|---|---|---|---|---|---|---|
| id | id | str:5 | 0 | 0 | 0.0 | - | - |
| task | text | str:5 | 0 | 0 | 0.0 | - | - |
| setup_cost_inr | money | int:5 | 0 | 0 | 0.0 | 200000 / 200000 / 200000 | - |
| annual_running_cost_inr | money | int:5 | 0 | 0 | 0.0 | 60000 / 60000 / 60000 | - |
| annual_benefit_inr | money | int:5 | 0 | 0 | 0.0 | 180000 / 180000 / 180000 | - |
| benefit_type | label | str:5 | 0 | 0 | 0.0 | - | cashable assumption: 5 |
| overlap_group | label | str:5 | 0 | 0 | 0.0 | - | catalog: 1, finance: 1, ops: 1, support: 2 |

## Table `extension_pilots`

- Rows: 5 (non-object rows: 0)
- Duplicate rows: 0; duplicate ids: none
- Unknown columns: none

| Column | Kind | Types | Absent | Null | Missing % | Numeric (min / median / max) | Distribution |
|---|---|---|---|---|---|---|---|
| id | id | str:5 | 0 | 0 | 0.0 | - | - |
| description | text | str:5 | 0 | 0 | 0.0 | - | - |
| adoption_pct | pct | int:5 | 0 | 0 | 0.0 | 40 / 60 / 70 | - |
| readiness | readiness | str:5 | 0 | 0 | 0.0 | - | high: 2, low: 1, medium: 2 |
| observed_adoption_pct | pct | null:5 | 0 | 5 | 100.0 | - | - |
| observed_quality_pct | pct | null:5 | 0 | 5 | 100.0 | - | - |

## Rule checks

- negative values: 0
- pct out of range: 0
- non numeric in numeric field: 0
- non finite: 0

## Overlap groups

- catalog: AI3
- finance: AI1
- ops: AI2
- support: AI4, AI5

## Validation preview (read-only)

- **strict**: 5 valid (5 complete, 0 incomplete), 0 rejected
- **scenario**: 5 valid (5 complete, 0 incomplete), 0 rejected

## Target class distribution

- none (no training labels)
