// A "draft" is a pilot the user has typed in during this session. The gateway has no endpoint to store pilots,
// so drafts live only in memory and are sent to the rules engine as `fixture` and `extension` overrides.
import type { BenefitType, Extension, ExtensionPilot, Fixture, FixturePilot, Readiness, ScenarioOverrides } from '../api/types'

export interface DraftPilot {
  id: string
  task: string
  description: string
  setup_cost_inr: number
  annual_running_cost_inr: number | null
  annual_benefit_inr: number
  benefit_type: BenefitType
  overlap_group: string
  adoption_pct: number | null
  readiness: Readiness | null
  observed_adoption_pct: number | null
  observed_quality_pct: number | null
}

/** Text as typed, before validation. Blank is kept as blank so it can never silently become zero. */
export interface DraftForm {
  id: string
  task: string
  description: string
  setup: string
  running: string
  benefit: string
  benefitType: BenefitType
  group: string
  adoption: string
  readiness: Readiness | ''
  obsAdoption: string
  obsQuality: string
}
export type DraftErrors = Partial<Record<keyof DraftForm, string>>

/** Same rule the gateway enforces on pilot ids in /pilots/:id/... routes. */
export const PILOT_ID_RE = /^[A-Za-z0-9_.-]{1,40}$/
export const BENEFIT_TYPES: { value: BenefitType; label: string; hint: string }[] = [
  { value: 'cashable assumption', label: 'Cashable', hint: 'A cost removed or revenue gained.' },
  { value: 'time freed', label: 'Time freed', hint: 'Staff time saved. The engine counts this at a cash haircut unless a cost is removed.' },
]

export const emptyForm = (suggestedId: string): DraftForm => ({
  id: suggestedId, task: '', description: '', setup: '', running: '', benefit: '', benefitType: 'cashable assumption',
  group: '', adoption: '', readiness: '', obsAdoption: '', obsQuality: '',
})

const text = (v: number | null | undefined) => (v === null || v === undefined ? '' : String(v))
export const formFromDraft = (d: DraftPilot): DraftForm => ({
  id: d.id, task: d.task, description: d.description, setup: text(d.setup_cost_inr), running: text(d.annual_running_cost_inr),
  benefit: text(d.annual_benefit_inr), benefitType: d.benefit_type, group: d.overlap_group, adoption: text(d.adoption_pct),
  readiness: d.readiness ?? '', obsAdoption: text(d.observed_adoption_pct), obsQuality: text(d.observed_quality_pct),
})

/** Accepts 200000, 2,00,000 or 200 000. NaN for anything else, and for blank. */
export function parseAmount(s: string): number {
  const t = s.replace(/[,\s_]/g, '')
  return t === '' ? NaN : Number(t)
}

export function nextId(existing: string[]): string {
  const used = new Set(existing)
  for (let n = 1; ; n++) if (!used.has(`P${n}`)) return `P${n}`
}

export function validateDraft(f: DraftForm, otherIds: string[]): { errors: DraftErrors; draft: DraftPilot | null } {
  const errors: DraftErrors = {}
  const id = f.id.trim()
  if (!PILOT_ID_RE.test(id)) errors.id = 'Use 1 to 40 letters, digits, dots, hyphens or underscores.'
  else if (otherIds.includes(id)) errors.id = 'Another pilot in this scenario already uses this ID.'

  const task = f.task.trim()
  if (task === '') errors.task = 'Give the pilot a name.'
  else if (task.length > 120) errors.task = 'Keep the name to 120 characters.'

  const money = (key: 'setup' | 'running' | 'benefit', label: string, required: boolean): number | null => {
    const raw = f[key].trim()
    if (raw === '') { if (required) errors[key] = `${label} is required.`; return null }
    const n = parseAmount(raw)
    if (!Number.isFinite(n)) { errors[key] = `${label} must be a number.`; return null }
    if (n < 0) { errors[key] = `${label} cannot be negative.`; return null }
    return n
  }
  const pct = (key: 'adoption' | 'obsAdoption' | 'obsQuality', label: string): number | null => {
    const raw = f[key].trim()
    if (raw === '') return null
    const n = parseAmount(raw)
    if (!Number.isFinite(n)) { errors[key] = `${label} must be a number.`; return null }
    if (n < 0 || n > 100) { errors[key] = `${label} must be between 0 and 100.`; return null }
    return n
  }

  const setup = money('setup', 'Setup cost', true)
  const running = money('running', 'Running cost', false)
  const benefit = money('benefit', 'Expected annual benefit', true)
  const adoption = pct('adoption', 'Adoption')
  const oa = pct('obsAdoption', 'Measured adoption')
  const oq = pct('obsQuality', 'Measured quality')
  if (f.description.length > 400) errors.description = 'Keep notes to 400 characters.'
  if (f.group.trim().length > 60) errors.group = 'Keep the overlap group to 60 characters.'

  if (Object.keys(errors).length > 0 || setup === null || benefit === null) return { errors, draft: null }
  return {
    errors,
    draft: {
      id, task, description: f.description.trim(), setup_cost_inr: setup, annual_running_cost_inr: running, annual_benefit_inr: benefit,
      benefit_type: f.benefitType, overlap_group: f.group.trim(), adoption_pct: adoption, readiness: f.readiness === '' ? null : f.readiness,
      observed_adoption_pct: oa, observed_quality_pct: oq,
    },
  }
}

/** The fixture override in the exact shape the rules engine reads (records.pilots[]). */
export function toFixture(drafts: DraftPilot[]): Fixture {
  const pilots: FixturePilot[] = drafts.map((d) => ({
    id: d.id, task: d.task, setup_cost_inr: d.setup_cost_inr, annual_running_cost_inr: d.annual_running_cost_inr,
    annual_benefit_inr: d.annual_benefit_inr, benefit_type: d.benefit_type, overlap_group: d.overlap_group,
  }))
  return { records: { pilots } }
}

/** The extension override. Empty `defaults` makes the engine fall back to its shipped default assumptions. */
export function toExtension(drafts: DraftPilot[]): Extension {
  const pilots: ExtensionPilot[] = drafts.map((d) => ({
    id: d.id, description: d.description, adoption_pct: d.adoption_pct, readiness: d.readiness,
    observed_adoption_pct: d.observed_adoption_pct, observed_quality_pct: d.observed_quality_pct,
  }))
  return { defaults: {}, pilots }
}

export const overridesFor = (drafts: DraftPilot[]): ScenarioOverrides => ({ fixture: toFixture(drafts), extension: toExtension(drafts) })

/** Rebuilds editable drafts from a saved scope (fixture_as_used + extension). */
export function draftsFromScope(fixture: Fixture, extension: Extension): DraftPilot[] {
  const ext = new Map(extension.pilots.map((p) => [p.id, p]))
  return fixture.records.pilots.map((p) => {
    const e = ext.get(p.id)
    return {
      id: p.id, task: p.task, description: e?.description ?? '', setup_cost_inr: p.setup_cost_inr,
      annual_running_cost_inr: p.annual_running_cost_inr ?? null, annual_benefit_inr: p.annual_benefit_inr,
      benefit_type: (p.benefit_type === 'time freed' ? 'time freed' : 'cashable assumption') as BenefitType,
      overlap_group: p.overlap_group ?? '', adoption_pct: e?.adoption_pct ?? null, readiness: e?.readiness ?? null,
      observed_adoption_pct: e?.observed_adoption_pct ?? null, observed_quality_pct: e?.observed_quality_pct ?? null,
    }
  })
}
