/**
 * Presentation helpers for the FYND screens. Nothing here calculates a financial figure: every number shown
 * comes from a gateway or engine answer. These helpers only label, map and validate what the user types.
 */
import type { SnapshotProposal } from '../api/viewTypes'

export type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'off'

/** Known engine outcomes get a plain label; any other outcome is shown as the engine wrote it, never guessed. */
const OUTCOME: Record<string, { label: string; tone: Tone }> = {
  invest: { label: 'Invest', tone: 'ok' },
  fund: { label: 'Fund', tone: 'ok' },
  pilot: { label: 'Run a limited pilot', tone: 'info' },
  stage: { label: 'Stage the funding', tone: 'info' },
  renegotiate: { label: 'Renegotiate', tone: 'warn' },
  gather_evidence: { label: 'Gather evidence', tone: 'warn' },
  insufficient_information: { label: 'Insufficient information', tone: 'warn' },
  defer: { label: 'Defer', tone: 'warn' },
  reject: { label: 'Reject', tone: 'bad' },
  no_clear_winner: { label: 'No clear winner', tone: 'off' },
}

export function humanize(code: string): string {
  const s = code.replace(/[_-]+/g, ' ').trim()
  return s ? s[0].toUpperCase() + s.slice(1) : code
}

export function outcomeLabel(outcome: string | null | undefined): string {
  if (!outcome) return 'Not available'
  return OUTCOME[outcome]?.label ?? humanize(outcome)
}
export function outcomeTone(outcome: string | null | undefined): Tone {
  return (outcome && OUTCOME[outcome]?.tone) || 'off'
}

export function severityTone(sev: string | null | undefined): Tone {
  if (sev === 'high' || sev === 'critical') return 'bad'
  if (sev === 'medium') return 'warn'
  if (sev === 'low') return 'info'
  return 'off'
}

/** The engine names proposals by their stored name; the snapshot says which stored proposal each name is. */
export function proposalIdByName(list: SnapshotProposal[], name: string): string | null {
  return list.find((p) => p.name === name)?.id ?? null
}
export function proposalNameById(list: SnapshotProposal[], id: string): string | null {
  return list.find((p) => p.id === id)?.name ?? null
}

/* ---------------------------------------------------------------- proposal inputs form */

/** The stored-input keys the gateway hands to the engine (views.go proposalEngineKeys), as a typed form. */
export interface ProposalForm {
  setup_cost_inr: string
  annual_benefit_inr: string
  ai_running_cost_inr: string
  baseline_annual_cost_inr: string
  benefit_type: string
  readiness: string
  adoption_pct: string
  ramp_months: string
  delay_months: string
  overlap_group: string
  cashable_share_pct: string
}

export const EMPTY_FORM: ProposalForm = {
  setup_cost_inr: '', annual_benefit_inr: '', ai_running_cost_inr: '', baseline_annual_cost_inr: '', benefit_type: '',
  readiness: '', adoption_pct: '', ramp_months: '', delay_months: '', overlap_group: '', cashable_share_pct: '',
}

export const BENEFIT_TYPES = ['cashable', 'cashable assumption', 'time freed', 'non-cashable', 'unknown'] as const
export const READINESS = ['low', 'medium', 'high'] as const

const numStr = (v: unknown): string => (typeof v === 'number' && Number.isFinite(v) ? String(v) : '')
const strOf = (v: unknown): string => (typeof v === 'string' ? v : '')

/** Reads stored inputs into the form. Unknown keys are kept aside by the caller (see mergeInputs). */
export function formFromInputs(inputs: unknown): ProposalForm {
  const i = (inputs && typeof inputs === 'object' ? inputs : {}) as Record<string, unknown>
  const baseline = (i.baseline && typeof i.baseline === 'object' ? i.baseline : {}) as Record<string, unknown>
  return {
    setup_cost_inr: numStr(i.setup_cost_inr),
    annual_benefit_inr: numStr(i.annual_benefit_inr),
    ai_running_cost_inr: numStr(i.ai_running_cost_inr ?? i.annual_running_cost_inr),
    baseline_annual_cost_inr: numStr(baseline.annual_cost_inr),
    benefit_type: strOf(i.benefit_type),
    readiness: strOf(i.readiness),
    adoption_pct: numStr(i.adoption_pct),
    ramp_months: numStr(i.ramp_months),
    delay_months: numStr(i.delay_months),
    overlap_group: strOf(i.overlap_group),
    cashable_share_pct: numStr(i.cashable_share_pct),
  }
}

export type FormErrors = Partial<Record<keyof ProposalForm, string>>

function parseNum(s: string): number | null | undefined {
  const t = s.trim().replace(/[,\s]/g, '')
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) ? n : undefined
}

/**
 * Validates the form and returns the inputs object. Blank fields are left out (never sent as zero), so the
 * engine applies its own documented assumption and flags it. Setup and benefit are required by the gateway
 * before a proposal can be evaluated, so they are required here.
 */
export function inputsFromForm(f: ProposalForm, previous: unknown = {}): { inputs: Record<string, unknown>; errors: FormErrors } {
  const errors: FormErrors = {}
  const out: Record<string, unknown> = {}
  const money = (k: keyof ProposalForm, required: boolean): number | null => {
    const n = parseNum(f[k])
    if (n === undefined || (n !== null && n < 0)) { errors[k] = 'Enter a non-negative amount in rupees.'; return null }
    if (n === null && required) { errors[k] = 'Required before the proposal can be evaluated.'; return null }
    return n
  }
  const ranged = (k: keyof ProposalForm, lo: number, hi: number, whole: boolean): number | null => {
    const n = parseNum(f[k])
    if (n === null) return null
    if (n === undefined || n < lo || n > hi || (whole && !Number.isInteger(n))) {
      errors[k] = whole ? `Enter a whole number from ${lo} to ${hi}.` : `Enter a number from ${lo} to ${hi}.`
      return null
    }
    return n
  }
  const setup = money('setup_cost_inr', true)
  const benefit = money('annual_benefit_inr', true)
  const run = money('ai_running_cost_inr', false)
  const baseline = money('baseline_annual_cost_inr', false)
  const adoption = ranged('adoption_pct', 0, 100, false)
  const ramp = ranged('ramp_months', 0, 120, true)
  const delay = ranged('delay_months', 0, 120, true)
  const cash = ranged('cashable_share_pct', 0, 100, false)
  if (setup !== null) out.setup_cost_inr = setup
  if (benefit !== null) out.annual_benefit_inr = benefit
  if (run !== null) out.ai_running_cost_inr = run
  if (adoption !== null) out.adoption_pct = adoption
  if (ramp !== null) out.ramp_months = ramp
  if (delay !== null) out.delay_months = delay
  if (cash !== null) out.cashable_share_pct = cash
  if (f.benefit_type) out.benefit_type = f.benefit_type
  if (f.readiness) out.readiness = f.readiness
  const group = f.overlap_group.trim()
  if (group.length > 40) errors.overlap_group = 'Use at most 40 characters.'
  else if (group) out.overlap_group = group

  // Keep stored keys this form does not edit (cost lines, benefit driver, observed values, money source…),
  // and keep the other baseline fields when only the annual cost is edited.
  const prev = (previous && typeof previous === 'object' ? previous : {}) as Record<string, unknown>
  const merged: Record<string, unknown> = {}
  for (const [k, v] of Object.entries(prev)) if (!(k in EMPTY_FORM) && k !== 'baseline' && k !== 'annual_running_cost_inr') merged[k] = v
  const prevBaseline = (prev.baseline && typeof prev.baseline === 'object' ? { ...(prev.baseline as Record<string, unknown>) } : {}) as Record<string, unknown>
  delete prevBaseline.annual_cost_inr
  if (baseline !== null) prevBaseline.annual_cost_inr = baseline
  if (Object.keys(prevBaseline).length > 0) merged.baseline = prevBaseline
  return { inputs: { ...merged, ...out }, errors }
}

/** True when the stored inputs hold what the gateway needs to evaluate the proposal. */
export function isEvaluable(inputs: unknown): boolean {
  const i = (inputs && typeof inputs === 'object' ? inputs : {}) as Record<string, unknown>
  return typeof i.setup_cost_inr === 'number' && typeof i.annual_benefit_inr === 'number'
}

/** The engine evaluates proposal sets of 2 to 8 (it answers "Compare needs 2 to 8 proposals" otherwise). */
export const SET_MIN = 2
export const SET_MAX = 8

/**
 * The set every workspace view runs on: active, evaluable proposals, the most recently updated first, capped
 * at the engine's maximum. Sorted the same way everywhere so the dashboard, analysis, scenarios and reports
 * evaluate the identical set.
 */
export function evaluationSet<T extends { status: string; inputs: unknown; updated_at: string; name: string }>(docs: T[]): { set: T[]; left: T[] } {
  const ok = docs.filter((p) => p.status === 'active' && isEvaluable(p.inputs))
    .sort((a, b) => b.updated_at.localeCompare(a.updated_at) || a.name.localeCompare(b.name))
  return { set: ok.slice(0, SET_MAX), left: ok.slice(SET_MAX) }
}

/** The engine-key subset of stored inputs, named — the same mapping the gateway uses (views.go engineProposal). */
const ENGINE_KEYS = ['setup_cost_inr', 'annual_benefit_inr', 'ai_running_cost_inr', 'benefit_type', 'readiness', 'adoption_pct', 'ramp_months',
  'delay_months', 'overlap_group', 'observed_adoption_pct', 'observed_quality_pct', 'one_time_costs', 'recurring_costs', 'benefit_driver',
  'baseline', 'cashable_share_pct', 'money_source']

export function engineProposal(name: string, inputs: unknown, overrides: Record<string, number> = {}): Record<string, unknown> {
  const i = { ...((inputs && typeof inputs === 'object' ? inputs : {}) as Record<string, unknown>) }
  if (!('ai_running_cost_inr' in i) && 'annual_running_cost_inr' in i) i.ai_running_cost_inr = i.annual_running_cost_inr
  const out: Record<string, unknown> = { name }
  for (const k of ENGINE_KEYS) if (k in i) out[k] = i[k]
  return { ...out, ...overrides }
}
