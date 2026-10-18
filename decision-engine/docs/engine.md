# Decision engine (Part A)

The Python FastAPI rules engine in `decision-engine/`. It is **stateless and deterministic**: the
same request always gives the same answer, nothing is stored, and no LLM touches a number. The Go
gateway persists results and forwards requests; it may treat engine JSON as opaque.

- Run: `./start.sh` (port 8000, Swagger at `/docs`). Tests: `./venv/bin/python -m pytest -q` (222).
- Versions (`app/version.py`, reported by `GET /health`): `rules_version` **8**, `contract_version`
  **1**, `formula_version` **cf-1**. The gateway should put `rules_version` in its cache key.
- Contracts: `contracts/engine/<endpoint>/{request.json, response.*, schema.json}` for every
  endpoint, indexed in `contracts/engine/INDEX.md`; changes listed in `contracts/CHANGELOG.md`.
- Every rule and constant below is also served live by `GET /defaults` → `pipeline_rules`, which is
  the source of truth if this page and the code ever disagree.

## Ground rules

1. **Missing is never zero.** An unknown input is `null` plus a flag, never `0`.
2. **Provenance on every decision-driving value:** `fixture (synthetic)`, `assumed (team)`,
   `observed`, `calculated`. Fixture money caps the evidence band at Low. Money in user proposals
   defaults to `assumed (team)`; send `money_source: "observed"` for invoice- or contract-backed figures.
3. **Additive contract changes only.** New fields are fine; a rename, removal or type change bumps
   `CONTRACT_VERSION` and needs both owners.
4. **Change workflow:** change code → `./venv/bin/python scripts/build_contracts.py` → bump
   `RULES_VERSION` → add a `CHANGELOG.md` line. `tests/test_contracts.py` fails until all three are done.

## Modules

| Module | Package | Role |
|---|---|---|
| `core.py` | A1/A2 | Loader and validation, net/payback, conservative haircut, provenance, portfolio, stop rule, scenarios, token forecast block |
| `cashflow.py` | A2 | Monthly flows, components, yearly totals, cumulative, break-even, profit, ROI, NPV |
| `economics.py` | A3 | Benefit split, competitiveness, budget status |
| `pricing.py` | A4 | Pricing rules, usage forecast + backtest, package optimiser |
| `forecast.py` | A4 | Token run cost and next-FY cost (labelled fallback) |
| `ingest.py`, `xlsx.py` | A5 | CSV/XLSX parsing, units, periods, findings |
| `advice.py` | A6 | Evidence band, gaps, legacy outcome, next-action table, head-to-head |
| `decision.py` | A6 | Data readiness, eligibility, weighted ranking, outcomes, reversal |
| `views.py`, `reports.py` | A7 | Dashboard, yearly savings, CSV / Markdown reports and memo |
| `whatif.py`, `compare.py`, `solver.py`, `lifecycle.py` | A7 / earlier phases | What-if and sweep, proposal comparison, branch-and-bound, checkpoint/plan/reallocation |

## Configuration (team assumptions, editable per request via `extension.defaults`)

| Key | Default | Meaning |
|---|---|---|
| `pilot_budget_inr` | 600,000 | Default budget |
| `target_adoption_pct` | 60 | Stop-rule and plan adoption target |
| `quality_threshold_pct` | 80 | Stop-rule and plan quality gate |
| `assumed_run_cost_pct_of_setup` | 20 | Running cost when none is given (flag `RUN_COST_ASSUMED`) |
| `assumed_adoption_pct` | 50 | Adoption when none is given (flag `ADOPTION_ASSUMED`) |
| `horizon_months` | 36 | Cash-flow horizon |
| `discount_rate` | 0.10 | Annual rate for NPV |
| `tokens_per_month` | 50,000,000 | Token usage |
| `price_per_1m_tokens` | 250 | INR per 1M tokens |
| `ai_fixed_cost_inr_per_year` | 20,000 | Fixed AI platform cost |
| `annual_usage_growth_pct` | 20 | Usage growth for the next FY |
| `annual_price_drift_pct` | −10 | Price drift for the next FY |
| `scenario_multipliers` | bad 0.5 / base 1.0 / good 1.25 | Multiply adoption only |

Fixed engine constants: readiness factor low 0.6 / medium 0.8 / high 1.0; cash factor
`cashable`/`cashable assumption` 1.0, `time freed` 0.3, `unknown` 0.3, `non-cashable` 0.0; tie-break
order net benefit → payback → readiness → adoption → setup → id.

## Formulas

### Steady state and conservative view (A1)
- Net annual = benefit − running cost. Payback months = setup ÷ net × 12 (none when net ≤ 0).
- Conservative benefit = benefit × adoption% × readiness factor × cash factor.
- Break-even adoption = running cost ÷ (benefit × readiness factor × cash factor) × 100.
- **Public check:** AI1 setup 2.0 L, benefit 1.8 L, run 0.6 L → net **1.2 L/yr**, payback **20 months**.

### Cash flow (A2, `cf-1`)
- Months 1..horizon. One-time cost in month 1. Running cost from month 1 (waiting is not free).
- Benefit in month t = annual benefit ÷ 12 × ramp(t). ramp = 0 during `delay_months`, then
  min(1, active month ÷ `ramp_months`) when `ramp_months` ≥ 2, else 1.
- Break-even month = first month with cumulative ≥ 0 (rounded to the rupee); `null` beyond the horizon.
- Profit = Σ flows. ROI = profit ÷ one-time cost × 100 (`null` when the cost is 0).
  NPV = Σ flow(t) ÷ (1 + r)^(t/12); at r = 0 NPV equals profit.
- Cost lines: `one_time_costs` replace setup, `recurring_costs` replace the running cost.
  `benefit_driver` = volume/month × minutes saved ÷ 60 × hourly cost × 12.
- Each `multi_period` result carries `formula_version` and a `breakdown` (inputs + substituted
  formulas) that can be stored as-is and re-run.

### Economics and budgets (A3)
- Baseline cost = `annual_cost_inr`, or volume/month × minutes/unit ÷ 60 × loaded hourly cost × 12.
- Gross cost avoidance = baseline × `automation_share_pct` when both are given, else the typed benefit.
- Cashable = gross × cashable share (`cashable_share_pct`, default = the benefit type's cash factor).
- Capacity value = gross − cashable. Retained human cost = baseline − gross (`null` without a baseline).
  Net cashable = cashable − running cost.
- Competitiveness ratio = AI cost per unit ÷ human cost per unit (with a baseline volume), else
  annualised AI cost (setup × 12 ÷ horizon + running) ÷ gross. Classes: ≤ 0.40 highly competitive,
  ≤ 0.70 competitive, ≤ 1.00 marginal, otherwise uncompetitive.
- Budget set (all optional): `implementation_inr` ≥ setup; `first_year_inr` ≥ setup + 12 months of running cost;
  `recurring_inr_per_year` ≥ running cost; `total_inr` ≥ (setup + running over the horizon) ×
  (1 + `contingency_pct`, default 10%). Per line: `over`, `at_risk` (> 90% used, or the total fits only
  without contingency), `within`, `unknown`. Overall status = the worst line.

### Pricing and forecasting (A4)
- Rules (units per month): `per_unit`; `tiered` (`graduated`: each unit at its own tier, `volume`: all
  units at the tier the total falls in; last tier open-ended); `credit` (units × credits/unit − included,
  × price/credit); `commitment` (fixed price for the committed units + overage per unit); `package`
  (cheapest mix of whole packages + optional overage; ≤ 4 kinds, ≤ 50 of each). Modifiers:
  `fixed_monthly_inr` is added; `minimum_monthly_inr` sets a floor on the bill.
- Usage forecast: with ≥ 4 months of history, the last min(3, max(1, n ÷ 3)) months are held out.
  `naive`, `linear_trend` (OLS) and `compound_growth` are fitted on the rest, and the lowest hold-out
  MAPE wins (ties go to that order). Low/high = base × (1 ∓ max(MAPE, 10%)). With less history:
  `assumed_growth_fallback` = last usage × (1 + growth)^(m/12), always flagged `FORECAST_FALLBACK`.
- Optimiser: each plan priced on low/base/high; `recommended` = cheapest base, `robust_choice` =
  cheapest high, plus regret against the best plan in each scenario.
- Token block (in `/pilots`): run cost = tokens × 12 × price ÷ 1M + fixed; next FY = tokens × (1 + growth)
  and price × (1 + drift), fixed unchanged; inflation flag when growth ≥ 0. Labelled
  `method: assumed_growth_fallback`. Worked example: 50M tokens at ₹250 + ₹20,000 → ₹1,70,000/yr;
  next FY ₹1,82,000.

### Parsing (A5)
- CSV (UTF-8, BOM ok, `,` `;` or tab) or XLSX (first sheet, stdlib reader; ≤ 20 MB uncompressed,
  ≤ 20,000 rows). Uploads ≤ 2 MB. Nothing is stored.
- Money: thousands separators, `.` decimal, `(1,00,000)` negative, `₹`, inline `cr` / `L` / `k`
  suffixes beat the column unit; blanks (`—`, `n/a`, …) are missing. Units: rupees, thousands, lakh,
  crore — taken from the header or the user's `unit`.
- Periods: recurring amounts are annualised from the period their header names (monthly × 12,
  quarterly × 4, half-year × 2) or from the user's `period`. Setup is never annualised. Month names
  are deliberately not hints ("31 Mar 2024" is a year-end date).
- `findings`: `{severity: error|warning|info, code, line, field, message}`, errors first.
  `POSSIBLE_DUPLICATE` = a different id with the same task (ignoring case) and the same money figures.

### Decisions (A6)
- **Evidence band** (Phase 5, unchanged): input weights benefit 3, running cost 2, setup 2, adoption 2,
  quality 1, readiness 1. Share of weight that is observed or calculated: ≥ 0.75 high, ≥ 0.40 medium,
  else low; fixture money caps the band at low.
- **Data readiness**: 7 checks (run cost stated, adoption stated, readiness stated, benefit type known,
  overlap known, baseline supplied, team readiness ≥ medium). ≥ 0.85 ready, ≥ 0.50 partial, else not ready.
- **Eligibility gates**: `NET_POSITIVE`, `BREAKEVEN_IN_HORIZON`, `BUDGET_FIT` (not checked without
  a budget set), `BENEFIT_HAS_CASH`.
- **Outcomes, first matching rule wins:**
  1. net ≤ 0 → `renegotiate` if a running-cost cut of ≤ 30% makes the net positive, else `reject`
  2. no cashable benefit → `reject`
  3. competitiveness uncompetitive → `renegotiate`
  4. break-even beyond the horizon or over budget → `defer`
  5. nothing observed or calculated → `gather_evidence`
  6. band low, data not ready, or competitiveness marginal → `limited_pilot`
  7. otherwise → `invest`
- **Weighted score** (0–100; each criterion min-max normalised across the set, all-equal = 100):
  net 0.30, NPV 0.20, payback 0.15 (lower is better; never = 0), evidence 0.15, readiness 0.10,
  cash share 0.10. Override with `weights` (re-normalised). Eligible proposals rank first; ties use
  the 6-tier order.
- **Set level**: the investable leader wins, but `no_clear_winner` when the top `invest` (or
  `limited_pilot`) proposals are within **5 points**; with nothing investable, the most constructive
  outcome present (gather_evidence > renegotiate > defer > reject).
- **What would reverse it**: for setup, running cost and benefit, a binary search (0–3× the base value,
  0.5% resolution) for the nearest value that changes the outcome, nearest first.
- The legacy `advice.outcome` (`reject / insufficient_information / defer / stage / fund`) is kept
  unchanged next to `decision.outcome`.

### Views (A7)
- Yearly savings: the monthly engine components summed into 12-month years. Σ yearly net = `profit_inr`
  (checked per proposal). The portfolio row covers the funded set.
- Dashboard: funded set = the compare rule (drop net ≤ 0 or break-even beyond the horizon; highest total
  net within the budget, each overlap group counted once). Best proposal = the decision set leader.
  Proposal states: `recommended` (funded), `viable` (eligible, not funded), `not_viable`. Four
  charts share one `series_order`. `consistency` re-checks the KPIs against compare, the yearly view and
  the cumulative series; the decision report copies its figures from the dashboard.

## Endpoints

| Method | Path | Returns |
|---|---|---|
| GET | `/health` | ok + versions |
| GET | `/defaults` | Shipped data + every rule above |
| POST | `/pilots` | Per-pilot views, provenance, scenarios, advice, economics, decision, checkpoint, stop rule; token forecast |
| POST | `/portfolio`, `/portfolio/check` | Budget-constrained selection (net / npv / risk_adjusted); manual check |
| POST | `/stoprule/{pid}`, `/checkpoint/{pid}`, `/plan`, `/reallocation` | Lifecycle |
| POST | `/whatif`, `/sweep` | Base vs changed; one-variable ladder |
| POST | `/compare` | 2–8 proposals scored and selected |
| POST | `/economics`, `/decision` | A3 split and budget; A6 ranking and outcomes |
| POST | `/views/dashboard`, `/views/yearly-savings` | A7 views |
| POST | `/pricing/quote`, `/forecast/usage`, `/pricing/optimise` | A4 |
| POST (multipart) | `/balance-sheet/parse`, `/balance-sheet/confirm`, `/pilots/import/parse`, `/pilots/import/confirm` | A5 |
| POST (text) | `/export/csv`, `/export/evidence`, `/export/memo`, `/export/yearly-csv`, `/export/decision-report` | Downloads |

Proposal-set endpoints (`/economics`, `/decision`, `/views/*`, the two new exports) take
`{proposals?, budget?, mode?, extension?, budget_set?, weights?, ai_burn_to_date?}`. Proposals use the
`/compare` shape plus the optional `baseline`, `cashable_share_pct`, `money_source`, observed values
and cost lines. Without `proposals`, the shipped fixture is used. Input errors are 422 with a
`detail` message.

## Not covered (Later tier)
PDF text extraction and OCR, and multi-currency with timestamped exchange rates.
