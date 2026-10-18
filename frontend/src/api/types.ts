// Shapes of the Go gateway (backend/internal/model, httpapi) and of the rules engine responses it forwards
// (decision-engine/app). If the source and these types ever disagree, the source wins.

export type Mode = 'independent' | 'conservative'
export type Readiness = 'low' | 'medium' | 'high'
export type BenefitType = 'cashable assumption' | 'time freed'
export type StopDecision = 'GO' | 'REVISE' | 'STOP'

/* ───────── Scenario overrides accepted by the rules routes ───────── */
export interface FixturePilot {
  id: string
  task: string
  setup_cost_inr: number
  annual_running_cost_inr: number | null
  annual_benefit_inr: number
  benefit_type: string
  overlap_group: string
  /* Phase 2 additive inputs */
  one_time_costs?: { label?: string; amount_inr: number }[]
  recurring_costs?: { label?: string; amount_inr_per_year: number }[]
  benefit_driver?: { volume_per_month?: number; minutes_saved_per_unit?: number; loaded_cost_per_hour?: number }
  ramp_months?: number
  delay_months?: number
}
export interface Fixture {
  case_id?: string
  synthetic?: boolean
  data_version?: string
  currency?: string
  missing_values?: string
  records: { pilots: FixturePilot[]; expected_first_pilot_payback_months?: number; notes?: string }
  public_checks?: string[]
  limit?: string
}
/* ───────── Phase 2 additive: multi-period, provenance, token forecast ───────── */
export interface MultiPeriod {
  horizon_months: number
  discount_rate: number
  profit_inr: number
  roi_pct: number | null
  npv_inr: number
  breakeven_month: number | null
  breakeven_beyond_horizon: boolean
  cumulative: number[]
  value_per_1m_tokens: number | null
  value_vs_price_ratio: number | null
}
export interface ProvenanceEntry { value?: unknown; source: string | null; note?: string | null }
export type ProvenanceMap = Record<string, ProvenanceEntry>
export interface ForecastBlock {
  inputs: { tokens_per_month?: number | null; price_per_1m_tokens?: number | null; ai_fixed_cost_inr_per_year?: number | null; annual_usage_growth_pct?: number | null; annual_price_drift_pct?: number | null }
  ai_run_cost_inr: number | null
  next_fy_cost_inr: number | null
  inflation_flag: boolean | null
  flags: { code: string; message: string }[]
  provenance: ProvenanceMap | { source: string } | null
}

/* ───────── Team extension: assumptions per pilot (additive Phase 2 fields on both rows) ───────── */
export interface ExtensionPilot {
  id: string
  description?: string
  adoption_pct: number | null
  readiness: Readiness | null
  observed_adoption_pct: number | null
  observed_quality_pct: number | null
  /* Phase 2 additive inputs: cost lines / benefit driver / ramp-up and delay */
  one_time_costs?: { label?: string; amount_inr: number }[]
  recurring_costs?: { label?: string; amount_inr_per_year: number }[]
  benefit_driver?: { volume_per_month?: number; minutes_saved_per_unit?: number; loaded_cost_per_hour?: number }
  ramp_months?: number
  delay_months?: number
}
export interface ExtensionDefaults {
  target_adoption_pct: number
  quality_threshold_pct: number
  assumed_run_cost_pct_of_setup: number
  assumed_adoption_pct: number
  /* Phase 2 additive conventions (send null to nullify an assumption) */
  horizon_months?: number | null
  discount_rate?: number | null
  annual_usage_growth_pct?: number | null
  annual_price_drift_pct?: number | null
  tokens_per_month?: number | null
  price_per_1m_tokens?: number | null
  ai_fixed_cost_inr_per_year?: number | null
}
export interface Extension {
  _label?: string
  pilot_budget_inr?: number
  defaults: Partial<ExtensionDefaults>
  pilots: ExtensionPilot[]
}
export interface ScenarioOverrides {
  fixture?: Fixture
  extension?: Extension
}
export interface ScenarioBody extends ScenarioOverrides {
  mode?: Mode
  budget?: number | null
}

/* ───────── Rules engine responses ───────── */
export interface Flag { code: string; message: string }
export interface PilotView { id: string; benefit: number; net: number; payback_months: number | null; multi_period?: MultiPeriod }
export interface ScenarioCase { scenario: 'bad' | 'base' | 'good'; adoption_pct: number; realized_benefit: number; net_annual: number; payback_months: number | null; assumptions?: ScenarioAssumptions }
export interface StopRule { decision: StopDecision; reasons: string[]; metric: string; threshold: number | null; actual: number | null }

export interface Pilot {
  id: string
  task: string
  description: string
  setup: number
  run: number
  benefit: number
  benefit_type: string
  group: string
  adoption_pct: number
  readiness: Readiness
  observed_adoption_pct: number | null
  observed_quality_pct: number | null
  flags: Flag[]
  independent: PilotView
  conservative: PilotView
  breakeven_adoption_pct: number | null
  scenarios: ScenarioCase[]
  stop_rule: StopRule
  provenance?: ProvenanceMap
  /* Phase 5/6 additive */
  advice?: AdviceBlock
  checkpoint?: CheckpointBlock
}
export interface InvalidRecord { id: string; errors: string[] }
export interface PipelineRules {
  cash_factor_by_type: Record<string, number>
  readiness_factor: Record<string, number>
  tie_break_order: string[]
  horizon_months: number
  discount_rate: number
  scenario_multipliers?: Record<string, number>
  formulas: Record<string, string>
}
export interface PilotsResponse { pilots: Pilot[]; invalid: InvalidRecord[]; warnings: string[]; tie_break_order: string[]; forecast?: ForecastBlock }

export interface Rejected { id: string; reason: string }
export interface PortfolioResult {
  mode: Mode
  budget: number
  selected: string[]
  total_setup: number
  total_net: number
  budget_left: number
  rejected: Rejected[]
  ok: boolean
  message: string
  invalid: InvalidRecord[]
  warnings: string[]
  /* Phase 5 additive (generalized solver path) */
  objective?: string
  objective_value?: number
  solver?: { nodes: number; capped: boolean }
}
export interface ManualCheck { ok: boolean; problems: string[] }
export interface DefaultsDoc { starting_state: Fixture; extension: Extension; pipeline_rules?: PipelineRules }
export interface EvaluateResponse {
  pilots: PilotsResponse
  independent: PortfolioResult
  conservative: PortfolioResult
  /** Row id in the evaluations log; null when the history write failed (the answer still stands). */
  evaluation_id?: number | null
  proposal_id?: string | null
  proposal_version?: number | null
}

/** One row of the evaluations log (no bodies). */
export interface EvaluationSummary {
  id: number
  portfolio_id: string | null
  proposal_id: string | null
  proposal_version: number | null
  created_at: string
}

/** One successful edit or export. actor stays null until sign-in exists. */
export interface AuditEvent {
  id: number
  action: string
  entity_type: string
  entity_id: string | null
  actor: string | null
  request_id: string | null
  details: Record<string, unknown>
  created_at: string
}

/* ───────── Persisted by the gateway (PostgreSQL) ───────── */
export interface PortfolioPilotRow {
  pilot_id: string
  selected: boolean
  reason: string | null
  benefit_group: string | null
  setup_inr: number | null
  run_inr: number | null
  run_assumed: boolean
  net_inr: number | null
  payback_months: number | null
  profit_inr?: number | null
  roi_pct?: number | null
}
export interface SavedPortfolio {
  id: string
  name: string
  mode: Mode
  budget_inr: number | null
  horizon_months?: number | null
  discount_rate_pct?: number | null
  starting_state: Fixture
  negotiated_scope: { fixture_as_used: Fixture; extension: Extension; mode: Mode }
  final_evidence: { portfolio: PortfolioResult; pilots: PilotsResponse }
  created_at: string
  updated_at: string
  pilots: PortfolioPilotRow[]
}
export interface PortfolioSummary {
  id: string
  name: string
  mode: Mode
  budget_inr: number | null
  selected_count: number
  horizon_months?: number | null
  created_at: string
  updated_at: string
}
export interface Observation {
  id: number
  pilot_id: string
  adoption_pct: number | null
  quality_pct: number | null
  observed_on: string
  source: string | null
  created_at: string
}
export interface Decision {
  id: number
  pilot_id: string
  decision: StopDecision
  metric: string
  threshold: number | null
  actual: number | null
  reason: string
  observation_id: number | null
  created_at: string
}
export interface PilotHistory { observations: Observation[] | null; decisions: Decision[] | null }
export interface ObservationResult { observation: Observation; decision: Decision; stop_rule: StopRule }
export interface InterviewIn {
  interviewee_role: string
  task: string
  minutes_per_task: number | null
  useful_result: string | null
  quote: string | null
  interviewed_on: string | null
}
export interface Interview extends InterviewIn { id: number; created_at: string }

/* ───────── Proposals (PostgreSQL) ───────── */
export type ProposalStatus = 'active' | 'archived'
export interface ProposalSummary {
  id: string
  name: string
  status: ProposalStatus
  /** Inputs version: starts at 1 and goes up each time the stored inputs change. */
  version: number
  notes: string | null
  created_at: string
  updated_at: string
}
export interface Proposal extends ProposalSummary { inputs: unknown }
export type EvidenceKind = 'interview' | 'measurement' | 'document' | 'assumption'
export type EvidenceQuality = 'observed' | 'assumed' | 'calculated' | 'fixture'
export interface Evidence {
  id: number
  proposal_id: string
  kind: EvidenceKind
  source: string
  quality: EvidenceQuality | null
  note: string | null
  created_at: string
}

/* ───────── Operations ───────── */
export interface ReadyResponse { ready: boolean; checks: Record<string, string> }

/* ───────── Phase 3 additive: named scenarios, what-if, sweep, history ───────── */
export interface ScenarioAssumptions { adoption_multiplier: number; multiplies: string; held_at_base: string[] }
export type WhatIfOverrides = Partial<{
  setup_cost_inr: number
  annual_running_cost_inr: number | null
  annual_benefit_inr: number
  adoption_pct: number
  readiness: Readiness
  observed_adoption_pct: number | null
  observed_quality_pct: number | null
  ramp_months: number
  delay_months: number
  budget: number
  mode: Mode
  horizon_months: number
  discount_rate: number
  tokens_per_month: number
  price_per_1m_tokens: number
  annual_usage_growth_pct: number
  annual_price_drift_pct: number
}>
export interface WhatIfPilotBlock {
  benefit_inr: number
  net: number
  payback_months: number | null
  profit_inr: number
  roi_pct: number | null
  npv_inr: number
  breakeven_month: number | null
  verdict: StopDecision
  stop_rule: StopRule
  portfolio: { selected: string[]; total_net: number; budget_left: number; ok: boolean; message: string }
}
export interface WhatIfResponse {
  pilot_id: string
  mode: Mode
  overrides_applied: WhatIfOverrides
  base: WhatIfPilotBlock
  changed: WhatIfPilotBlock
  deltas: Partial<Record<'benefit_inr' | 'net' | 'payback_months' | 'profit_inr' | 'roi_pct' | 'npv_inr' | 'breakeven_month', number | null>>
  verdict_flip?: { from: StopDecision; to: StopDecision }
  flip_statement?: string | null
  flip_note?: string
}
export type SweepParameter = 'adoption_pct' | 'annual_benefit_inr' | 'annual_running_cost_inr' | 'price_per_1m_tokens' | 'annual_usage_growth_pct'
export interface SweepStep {
  value: number
  net?: number
  payback_months?: number | null
  profit_inr?: number
  roi_pct?: number | null
  npv_inr?: number
  breakeven_month?: number | null
  ai_run_cost_inr?: number | null
  next_fy_cost_inr?: number | null
  inflation_flag?: boolean | null
}
export interface SweepResponse {
  parameter: SweepParameter
  pilot_id: string | null
  mode: Mode
  steps: SweepStep[]
  direction: 'up_improves' | 'down_improves'
  monotonic: boolean | null
  flags: Flag[]
}
export interface HistoryEvent {
  id: number
  kind: 'evaluation' | 'decision'
  at: string
  pilot_id?: string | null
  proposal_id?: string | null
  verdict?: StopDecision | null
  previous_verdict?: StopDecision | null
  observed_adoption_pct?: number | null
  observed_quality_pct?: number | null
  reason?: string | null
  mode?: Mode | null
  budget?: number | null
  selected?: string[]
  change_summary?: string | null
}

// ---- Phase 4: balance sheet, proposal import, compare ----

export type BalanceUnit = 'auto' | 'rupees' | 'thousands' | 'lakh' | 'crore' | 'millions' | 'billions'

/** Where a row came from in the original document (added by the gateway for PDF, XLSX and XLS). */
export interface StatementSource { sheet?: string; row?: number; page?: number; text?: string }

export interface BalanceCandidate {
  row: number
  label: string
  matched: string[]
  raw: string | null
  amount_inr: number | null
  annualised_amount_inr?: number | null
  /** Why the row was flagged, e.g. "Matched keyword: software". */
  reason?: string
  source?: StatementSource
}

export interface SheetInfo { name: string; hidden: boolean; rows: number; usable: boolean; suggested: boolean }
export interface PageInfo {
  total: number
  selected: number[]
  with_tables: number[]
  no_text: number[]
  /** Unit stated on each table page ("" = none stated). Keys are page numbers. */
  units: Record<string, string>
}
export type StatementFormat = 'csv' | 'pdf' | 'xlsx' | 'xls'
/** How the gateway read the document before the engine saw it. */
export interface Extraction {
  source_format: StatementFormat
  sheet?: string
  sheets?: SheetInfo[]
  pages?: PageInfo
  dropped_columns?: { header: string; reason: string }[]
  currency?: string
  unit_detected?: string
  unit_source?: string
  converted_rows?: number
  notes?: string[]
  /** False when the engine's row numbers could not be tied to source rows, so no source is shown. */
  sources_matched: boolean
  /** Whether this server has the AI reader (Claude on Amazon Bedrock, Textract for scans). */
  ai_available?: boolean
  /** What the AI reader did, when it ran. */
  ai_assist?: AIAssistInfo
}

/** The gateway's AI reader: why it ran, what it read, and what it proposed or left out. */
export interface AIAssistInfo {
  used: boolean
  trigger?: 'scanned_document' | 'no_table_found' | 'no_candidates' | 'requested'
  method?: string
  model?: string
  ocr?: string
  pages_read?: number
  pages_sent?: number[]
  proposed: number
  kept: number
  dropped?: { page: number; label: string; amount: string; reason: string }[]
  notes?: string[]
  cached?: boolean
  /** Set when it ran and could not help. */
  error?: string
  kept_for_minutes?: number
}
export interface ParseFinding { severity: string; code: string; line: number | null; field: string | null; message: string }

/** Optional document choices sent beside the file: a worksheet name, PDF pages like "3-5,9", and whether
 *  to use the AI reader ("auto": only when the ordinary reader finds nothing; "on": use its reading). */
export interface StatementOptions { sheet?: string; pages?: string; aiAssist?: 'auto' | 'on' | 'off' }

export interface ParseUnit { choice: string; unit: string; multiplier: number; source: string }

export interface BalanceSheetParse {
  file_bytes: number
  rows: number
  label_column: string
  amount_columns: string[]
  amount_column_used: string
  unit: ParseUnit
  ai_burn_to_date: null
  candidates: BalanceCandidate[]
  warnings: string[]
  flags: Flag[]
  findings?: ParseFinding[]
  extraction?: Extraction
}

export interface BalanceSheetConfirm {
  ai_burn_to_date: number | null
  source: string
  rows_confirmed: number[]
  confirmations: { row: number; label: string; amount_inr: number | null; source?: StatementSource }[]
  amount_column_used: string
  unit: ParseUnit
  next_fy: {
    inputs: Record<string, unknown>
    ai_run_cost_inr: number | null
    next_fy_cost_inr: number | null
    inflation_flag: boolean | null
  }
  /** The plan from the statement: the confirmed rows, annualised, moved by usage growth and price drift.
   *  next_fy above is the token-assumption estimate, which does not read the statement. */
  observed_forecast?: {
    basis: 'confirmed_rows'
    ai_run_cost_inr: number | null
    next_fy_cost_inr: number | null
    inflation_flag: boolean | null
    inputs: { annual_usage_growth_pct?: number | null; annual_price_drift_pct?: number | null }
    note: string
  }
  warnings: string[]
  flags: Flag[]
  extraction?: Extraction
}

export interface ImportPilotRow {
  id: string
  task: string
  setup_cost_inr: number
  annual_benefit_inr: number
  annual_running_cost_inr?: number
  benefit_type?: string
  overlap_group?: string
  ramp_months?: number
  delay_months?: number
}

export interface ImportRecord {
  line: number
  errors?: { field: string; reason: string }[]
  pilot?: ImportPilotRow
  extension?: Record<string, unknown>
}

export interface PilotsImportParse {
  rows: number
  valid_rows: number
  error_rows: number
  headers: string[]
  column_map: Record<string, string>
  unit: ParseUnit
  records: ImportRecord[]
  pilots: ImportPilotRow[]
  warnings: string[]
  flags: Flag[]
}

export interface PilotsImportConfirm {
  pilots: ImportPilotRow[]
  extension_pilots: { id: string; adoption_pct?: number; readiness?: string }[]
  warnings: string[]
  flags: Flag[]
  unit: ParseUnit
}

export interface CompareProposalInput {
  name: string
  setup_cost_inr: number
  annual_benefit_inr: number
  ai_running_cost_inr?: number | null
  benefit_type?: string
  readiness?: string
  adoption_pct?: number
  ramp_months?: number
  delay_months?: number
  overlap_group?: string
}

export interface CompareBody {
  proposals: CompareProposalInput[]
  budget?: number | null
  mode?: Mode
  extension?: { defaults?: Record<string, unknown> }
}

export interface CompareProposalRow {
  id: string
  name: string
  setup_cost_inr: number
  ai_running_cost_inr: number
  annual_benefit_inr: number
  benefit_type: string
  cash_vs_capacity: { cash_benefit_inr: number; capacity_benefit_inr: number; cash_share_pct: number }
  net_annual_inr: number
  payback_months: number | null
  profit_inr: number | null
  roi_pct: number | null
  npv_inr: number | null
  breakeven_month: number | null
  breakeven_beyond_horizon: boolean | null
  cumulative: number[] | null
  flags: Flag[]
  rank: number | null
  status: 'recommended' | 'kept' | 'dropped' | 'not funded'
  reason: string | null
}

export interface CompareSelection {
  mode: Mode
  budget: number | null
  selected: string[]
  total_net_inr: number
  budget_left: number | null
  ok: boolean
  message: string
}

export interface CompareResponse {
  proposals: CompareProposalRow[]
  selection: CompareSelection
  recommendation: { text: string; selected: string[]; assumptions: Record<string, unknown> }
  head_to_head?: HeadToHead[]
  token_value_vs_price: {
    net_annual_savings_inr: number
    value_per_1m_tokens: number | null
    price_per_1m_tokens: number | null
    value_vs_price_ratio: number | null
    flags: Flag[]
  }
  warnings: string[]
}

/* ───────── Phase 5/6 additive: advice, checkpoint, plans, reallocation ───────── */
export type EvidenceBand = 'low' | 'medium' | 'high'
export type AdviceOutcome = 'fund' | 'stage' | 'defer' | 'insufficient_information' | 'reject'
export type CheckpointOutcome = 'scale' | 'extend' | 'revise' | 'stop' | 'pending'

export interface AdviceInput { input: string; source: string; observed: boolean; weight: number }
export interface AdviceGap { input: string; impact: 'high' | 'medium' | 'low'; why: string }
export interface AdviceEvidence {
  inputs: AdviceInput[]
  observed_weight: number
  total_weight: number
  share: number
  band: EvidenceBand
  band_capped_by_fixture: boolean
  gaps: AdviceGap[]
}
export interface AdviceAction { code: string; action: string; owner_hint: string; expected_test: string }
export interface AdviceBlock {
  evidence: AdviceEvidence
  outcome: AdviceOutcome
  reasons: string[]
  what_would_change_it: string[]
  actions: AdviceAction[]
  reason_codes: string[]
}
export interface CheckpointBlock {
  checkpoint: CheckpointOutcome
  reasons: string[]
  inputs?: {
    observed_adoption_pct: number | null
    observed_quality_pct: number | null
    target_adoption_pct: number | null
    quality_threshold_pct: number | null
    breakeven_adoption_pct: number | null
    duration_days: number | null
    revised_before: boolean
    evidence_band: EvidenceBand
  }
}
export interface HeadToHead { over: string; under: string; reasons: string[] }

export interface CheckpointResponse extends CheckpointBlock { pilot_id: string; stop_rule: StopRule }

export interface PilotPlan {
  pilot_id: string
  objective: string
  owner: string | null
  adoption_target_pct: number | null
  quality_target_pct: number | null
  cost_limit_inr: number | null
  duration_days: number
  stop_rule: string
  status: 'planned' | 'active' | 'revise' | 'stopped' | 'completed'
  created_at: string
  updated_at: string
}

export interface CreatePlanResponse { plan: PilotPlan; cost_limit_note: string; source: string }

export interface PlanProgress {
  pilot_id: string
  plan: PilotPlan
  actual: { adoption_pct: number | null; quality_pct: number | null; observed_on: string; source: string | null } | null
  gaps: Partial<Record<'adoption_gap_pct' | 'quality_gap_pct', number>>
  verdict: CheckpointResponse | null
}

export interface ReallocationResponse {
  mode: Mode
  objective: string
  budget: number | null
  freed_budget_inr: number
  locked_funded: string[]
  stopped: string[]
  remaining_budget_inr: number | null
  reallocation: PortfolioResult | null
  ok: boolean
  message: string
  source: { type: string; id?: string | number; name?: string }
  accepted: false
  note: { text: string; accept_via: string }
}
