# Engine contract changelog

Every change to a published engine response, newest first. Additive changes keep the contract
version and bump `RULES_VERSION`; a rename, removal or type change bumps `CONTRACT_VERSION`
(both in `app/version.py`) and needs both owners to approve.

## 1.0 — rules version 10 (2026-10-18)

Additive:

- `POST /balance-sheet/confirm` gains `observed_forecast`: the AI run cost from the rows the user confirmed
  (annualised from the period their column names) and the next-FY plan from it, × (1 + usage growth) ×
  (1 + price drift) from the team assumptions, with `inflation_flag`, `inputs`, `basis` (`confirmed_rows`) and
  a `note`. Both are `null` when no confirmed row has an amount. `next_fy` is unchanged: it is still the
  token-assumption estimate and does not read the statement.

## 1.0 — rules version 9 (2026-10-18)

Additive, for annual reports and the gateway's AI reader:

- Money units gain `millions` (×1,000,000) and `billions` (×1,000,000,000): as a `unit` choice, detected
  from amount-column headers ("₹ in million", "(INR bn)"), and as inline suffixes on a value (`9,663.3 mn`,
  `1.5 bn`). `/defaults` → `parse_rules.unit_multipliers` lists them. Applies to the balance sheet and the
  proposal import alike.
- `POST /balance-sheet/parse` and `/confirm` accept an optional `propose` form field: `keywords` (default,
  unchanged behaviour) or `all`, which makes every described row a candidate. The gateway sends `all` for
  a table its AI reader already proposed; `matched` is then empty for rows no keyword matched, and the
  user still confirms every row. `/defaults` → `parse_rules.balance_sheet_propose_modes` lists the modes.

## 1.0 — rules version 8 (2026-10-18)

Additive, for the gateway's stored-upload review (B4):

- `POST /balance-sheet/confirm` accepts an optional `overrides` form field, a JSON object
  `{"<row>": amount_inr}` of reviewer corrections. Corrected rows carry `original_amount_inr` and
  `corrected: true`, and the flag `CORRECTED_BY_REVIEWER` is raised. The burn is still summed by the engine.
- `POST /pilots/import/confirm` accepts `overrides` as `{"<id>": {field: value}}` for task, money,
  benefit type, overlap group, ramp/delay, adoption and readiness. The response always carries a
  `corrections` list (empty when nothing was corrected).

## 1.0 — rules version 7 (2026-10-18)

First published contract. Everything below is additive over rules version 6: no existing field
was renamed, removed or retyped.

- `GET /health` now reports `rules_version`, `contract_version`, `formula_version`.
- `GET /defaults` → `pipeline_rules` gains `versions`, `whatif_rules`, `compare_rules`,
  `economics_rules`, `pricing_rules`, `decision_rules`, `view_rules`, and five formulas.
- `POST /pilots`: every pilot gains `economics` (A3), `decision` (A6) and `money_source`;
  `multi_period` gains `formula_version` and `breakdown`; top-level `forecast` gains `method` and
  `method_note`. The request accepts an optional `budget_set`. Request `scenario_multipliers` are
  now honoured in `scenarios` (bug fix). The legacy `advice` block is unchanged, except that an
  observed adoption of 0% now counts as measured (bug fix) and user-stated proposal money adds
  the `MONEY_ASSUMED` reason code.
- `POST /compare`: proposals accept optional `observed_adoption_pct`, `observed_quality_pct`,
  `one_time_costs`, `recurring_costs`, `benefit_driver`, `baseline`, `cashable_share_pct`,
  `money_source`. Lakh figures in the recommendation text never use scientific notation.
- Upload endpoints accept XLSX (first sheet) as well as CSV, take an optional `period` form field,
  and return `format`, `period`/`periods` and a `findings` review list; balance-sheet candidates
  carry `annualised_amount_inr`, confirm returns `ai_burn_annualised_inr`. Pilot import flags
  `POSSIBLE_DUPLICATE` rows.
- New endpoints: `POST /economics`, `/decision`, `/views/dashboard`, `/views/yearly-savings`,
  `/export/yearly-csv`, `/export/decision-report`, `/pricing/quote`, `/forecast/usage`,
  `/pricing/optimise`.
- Money in report and message text is printed with thousands separators, never `1.5e+06`.
