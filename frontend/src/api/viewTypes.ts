/**
 * Shapes of the workspace views, snapshots, settings, approvals and stored uploads, as the gateway returns
 * them (backend/internal/httpapi/views.go, uploads.go, model/platform.go) and as the rules engine answers
 * /views/dashboard, /decision, /compare, /views/yearly-savings and /forecast/usage (contract_version 1).
 * Only fields the screens read are typed; nullable fields stay nullable, because missing is never zero.
 */
import type { CompareResponse, Flag, Mode } from './types'

/* ---------------------------------------------------------------- engine view answers */

export interface NextAction { code: string; action: string; owner_hint: string; expected_test: string }

export interface DashboardKpi { key: string; label: string; value: number | null; unit: string; source: string }

export interface DashboardProposalRow {
  /** The engine names proposals by their stored name; snapshot.proposals maps the name back to the stored id. */
  id: string
  state: string
  outcome: string
  score: number | null
  rank: number | null
  funding_rank: number | null
  net_annual_inr: number | null
  payback_months: number | null
  roi_pct: number | null
  npv_inr: number | null
  breakeven_month: number | null
  evidence_band: string | null
  budget_status: string | null
  competitiveness: string | null
  reason: string | null
}

export interface DashboardRisk { code: string; message: string; ids: string[]; severity: string }

export interface SeriesChart { x: number[]; x_label: string; unit: string; series: { id: string; values: (number | null)[] }[]; portfolio: (number | null)[] }

export interface BudgetLine { line: string; need_inr: number | null; budget_inr: number | null; status: string; note: string }
export interface BudgetBlock { ids?: string[]; status: string; contingency_pct: number | null; lines: BudgetLine[]; total_with_contingency_inr: number | null }

export interface YearRow {
  year: number
  months?: number
  benefit_inr: number | null
  cashable_inr: number | null
  capacity_inr?: number | null
  running_cost_inr: number | null
  one_time_inr: number | null
  net_inr: number | null
  cumulative_inr: number | null
}

export interface DashboardView {
  header: { title: string; rules_version: string; contract_version: number; formula_version: string; mode: Mode; horizon_months: number; discount_rate: number; budget: number | null; proposal_count: number }
  summary: { headline: string; text: string; read_more: string }
  kpis: DashboardKpi[]
  recommendation: { outcome: string; best: string | null; contenders: string[]; funded: string[]; reasons: string[]; compare_text: string }
  best_proposal: string | null
  proposals: DashboardProposalRow[]
  risks: DashboardRisk[]
  next_actions: NextAction[]
  charts: {
    series_order: string[]
    cumulative_cash_flow: SeriesChart
    yearly_savings: SeriesChart
    roi_breakeven: { unit: Record<string, string>; rows: { id: string; roi_pct: number | null; breakeven_month: number | null }[] }
    cash_vs_capacity: { unit: string; rows: { id: string; cashable_inr: number | null; capacity_inr: number | null; running_cost_inr: number | null }[] }
  }
  yearly_portfolio: YearRow[]
  budget: BudgetBlock
  links: Record<string, string>
  consistency: { check: string; ok: boolean }[]
  flags: Flag[]
  warnings: string[]
}

export interface Economics {
  baseline_cost_inr: number | null
  baseline_note: string | null
  gross_cost_avoidance_inr: number | null
  gross_note: string | null
  cashable_savings_inr: number | null
  capacity_value_inr: number | null
  retained_human_cost_inr: number | null
  cashable_share_pct: number | null
  cashable_share_source: string | null
  net_cashable_inr: number | null
  flags: Flag[]
  competitiveness: { class: string; ratio: number | null; basis: string; annualised_ai_cost_inr: number | null } | null
  budget: BudgetBlock | null
}

export interface ProposalDecision {
  outcome: string
  reasons: string[]
  reason_codes: string[]
  eligibility: { eligible: boolean; gates: { gate: string; status: string; detail: string }[] }
  data_readiness: { level: string; share: number; checks: { check: string; ok: boolean }[]; missing: string[] }
  evidence: { band: string; share: number; observed_weight: number; total_weight: number; band_capped_by_fixture: boolean }
  gaps: { input: string; impact: string; why: string }[]
  economics: Economics
  what_would_reverse: { field: string; from_value: number | null; to_value: number | null; change_pct: number | null; new_outcome: string; statement: string }[]
  next_actions: NextAction[]
}

export interface DecisionView {
  overall: { outcome: string; best: string | null; contenders: string[]; reasons: string[]; next_actions: NextAction[] }
  ranking: { id: string; score: number | null; contributions: Record<string, number>; criteria: Record<string, number | null>; eligible: boolean; outcome: string; rank: number | null }[]
  why_a_over_b: { over: string; under: string; score_gap: number | null; reasons: string[] }[]
  weights: Record<string, number>
  decisions: Record<string, ProposalDecision>
  warnings: string[]
}

export interface YearlyView {
  mode: Mode
  horizon_months: number
  proposals: { id: string; years: YearRow[]; totals: Omit<YearRow, 'year' | 'cumulative_inr' | 'months'>; profit_inr: number | null; matches_profit: boolean }[]
  portfolio: { ids: string[]; years: YearRow[] }
  formula_version: string
}

export interface UsageForecast {
  method: string | null
  months: number
  base: number[] | null
  low: number[] | null
  high: number[] | null
  band_pct: number | null
  note: string | null
  backtest: { holdout_months: number; holdout_actual: number[]; scores: { method: string; mape: number | null; usable: boolean }[]; winner: string; winner_mape: number | null } | null
  flags: Flag[]
}

/* ---------------------------------------------------------------- gateway records */

export interface SnapshotProposal { id: string; name: string; version: number }
export interface Snapshot {
  id: string
  kind: 'dashboard' | 'decision' | 'comparison' | 'yearly_savings'
  proposals: SnapshotProposal[]
  rules_version: string | null
  contract_version: string | null
  created_by: string | null
  created_at: string
}
/** POST /workspace/{view}: the engine answer, kept as a snapshot. */
export interface WorkspaceView<T> { snapshot: Snapshot; result: T; stale: boolean }
export interface SnapshotDetail<T = unknown> extends WorkspaceView<T> { staleness: { stale: boolean; reasons: string[]; rules_version_now: string | null } }
export interface ReplayResult { snapshot_id: string; kind: string; matches: boolean; rules_version_then: string | null; rules_version_now: string | null; note: string }

/** The body every workspace view takes. Leaving proposal_ids out means every active proposal. */
export interface ViewRequest {
  proposal_ids?: string[]
  budget?: number | null
  mode?: Mode
  ai_burn_to_date?: number
  weights?: Record<string, number>
}

export interface WorkspaceSettings {
  budget: number | null
  mode?: Mode
  budget_set?: Record<string, number>
  defaults?: Record<string, number>
  weights?: Record<string, number>
}

export interface Approval {
  id: number
  proposal_id: string
  proposal_version: number
  decision: 'approved' | 'rejected' | 'changes_requested'
  snapshot_id: string | null
  engine_outcome: string | null
  comment: string | null
  decided_by: string | null
  created_at: string
  stale: boolean
}

export interface MeResponse {
  auth_mode: 'off' | 'required' | string
  user: { id: string; email: string; display_name: string | null } | null
  role: string
  workspace: { id: string; name: string } | null
  workspaces: { id: string; name: string; role?: string }[]
}

export type UploadStatus = 'pending_upload' | 'uploaded' | 'received' | 'processing' | 'parsed' | 'failed' | 'confirmed'
export interface Upload {
  id: string
  kind: 'balance_sheet' | 'pilots'
  filename: string
  content_type: string
  format: 'csv' | 'xlsx'
  size_bytes: number
  sha256: string
  options: Record<string, string>
  status: UploadStatus
  attempts: number
  max_attempts: number
  next_attempt_at: string
  last_error: string | null
  result: unknown
  confirmed: unknown
  created_by: string | null
  created_at: string
  updated_at: string
}
export interface Correction { id: number; upload_id: string; row_ref: string; field: string; original_value: unknown; corrected_value: unknown; note: string | null; created_at: string }
export interface BalanceReviewRow { row: number; label: string; amount_inr: number | null; corrected: boolean; corrected_amount_inr?: number; note?: string | null }
export interface UploadDetail {
  upload: Upload
  corrections: Correction[] | null
  review: { kind: 'balance_sheet'; rows: BalanceReviewRow[] } | { kind: 'pilots'; rows: { id: string; values: Record<string, unknown>; corrections: Record<string, unknown> }[] } | null
}

export type { CompareResponse }
