# Engine contract changelog

Every change to a published engine response, newest first. Additive changes keep the contract
version and bump `RULES_VERSION`; a rename, removal or type change bumps `CONTRACT_VERSION`
(both in `app/version.py`) and needs both owners to approve.

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
