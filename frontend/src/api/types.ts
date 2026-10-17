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
export interface ExtensionPilot {
  id: string
  description?: string
  adoption_pct: number | null
  readiness: Readiness | null
  observed_adoption_pct: number | null
  observed_quality_pct: number | null
}
export interface ExtensionDefaults {
  target_adoption_pct: number
  quality_threshold_pct: number
  assumed_run_cost_pct_of_setup: number
  assumed_adoption_pct: number
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
export interface PilotView { id: string; benefit: number; net: number; payback_months: number | null }
export interface ScenarioCase { scenario: 'bad' | 'base' | 'good'; adoption_pct: number; realized_benefit: number; net_annual: number; payback_months: number | null }
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
}
export interface InvalidRecord { id: string; errors: string[] }
export interface PilotsResponse { pilots: Pilot[]; invalid: InvalidRecord[]; warnings: string[]; tie_break_order: string[] }

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
}
export interface ManualCheck { ok: boolean; problems: string[] }
export interface DefaultsDoc { starting_state: Fixture; extension: Extension }
export interface EvaluateResponse { pilots: PilotsResponse; independent: PortfolioResult; conservative: PortfolioResult }

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
}
export interface SavedPortfolio {
  id: string
  name: string
  mode: Mode
  budget_inr: number | null
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

/* ───────── Operations ───────── */
export interface ReadyResponse { ready: boolean; checks: Record<string, string> }
