/**
 * Reading proposals from files. CSV and XLSX are read by the rules engine's importer (it understands units and
 * reports row errors); JSON is read here, because it is already structured. Every path ends in the same
 * validator as the Add Proposal form, so an imported proposal is held to the same rules as a typed one.
 */
import { EMPTY_FORM, inputsFromForm } from './fynd'
import type { ProposalForm } from './fynd'

/** The proposal name rule (gateway validProposalName). */
export const PROPOSAL_NAME_RE = /^[A-Za-z0-9_\- ]{1,80}$/

/** One proposal read from a file, ready to be stored once the person has checked it. */
export interface ImportCandidate {
  key: string
  name: string
  notes: string | null
  inputs: Record<string, unknown>
  /** Why it cannot be stored as it stands (invalid or missing values); empty when it can. */
  problems: string[]
  /** Where it came from, shown in the preview: "line 3" or "item 2". */
  origin: string
}

const FORM_KEYS = Object.keys(EMPTY_FORM) as (keyof ProposalForm)[]

/** Reads loose values (numbers or text) into the form, so one validator covers every import path. */
export function formFromLoose(src: Record<string, unknown>): ProposalForm {
  const f = { ...EMPTY_FORM }
  const put = (k: keyof ProposalForm, v: unknown) => {
    if ((typeof v === 'number' && Number.isFinite(v)) || (typeof v === 'string' && v.trim() !== '')) f[k] = String(v)
  }
  for (const k of FORM_KEYS) put(k, src[k])
  if (f.ai_running_cost_inr === '') put('ai_running_cost_inr', src.annual_running_cost_inr)
  const baseline = src.baseline && typeof src.baseline === 'object' ? (src.baseline as Record<string, unknown>) : null
  if (baseline) put('baseline_annual_cost_inr', baseline.annual_cost_inr)
  return f
}

export function validateCandidate(c: Pick<ImportCandidate, 'name'> & { src: Record<string, unknown>; previous: unknown }): { problems: string[]; inputs: Record<string, unknown> } {
  const { inputs, errors } = inputsFromForm(formFromLoose(c.src), c.previous)
  const problems = Object.values(errors).filter((x): x is string => !!x)
  if (!PROPOSAL_NAME_RE.test(c.name.trim())) problems.unshift('The name must be 1–80 characters: letters, digits, spaces, hyphens or underscores. Edit it in the table.')
  return { problems, inputs }
}

function build(key: string, origin: string, name: string, notes: string | null, src: Record<string, unknown>, previous: unknown): ImportCandidate {
  const { problems, inputs } = validateCandidate({ name, src, previous })
  return { key, name: name.trim(), notes, inputs, problems, origin }
}

/** A JSON file: an array of proposals, or { proposals: [...] }. Each is { name, notes?, inputs? } or the inputs written flat beside the name. */
export function parseJsonProposals(text: string): { candidates: ImportCandidate[]; problem: string | null } {
  let doc: unknown
  try { doc = JSON.parse(text) } catch { return { candidates: [], problem: 'The file is not valid JSON.' } }
  const wrapped = doc && typeof doc === 'object' && !Array.isArray(doc) ? (doc as { proposals?: unknown }).proposals : undefined
  const list = Array.isArray(doc) ? doc : Array.isArray(wrapped) ? wrapped : null
  if (!list) return { candidates: [], problem: 'Expected a JSON array of proposals, or an object with a "proposals" array.' }
  if (list.length === 0) return { candidates: [], problem: 'The file holds no proposals.' }
  if (list.length > 200) return { candidates: [], problem: 'At most 200 proposals can be imported from one file.' }
  const candidates = list.map((raw, i): ImportCandidate => {
    const item = raw && typeof raw === 'object' && !Array.isArray(raw) ? (raw as Record<string, unknown>) : null
    if (!item) return { key: `j${i}`, name: '', notes: null, inputs: {}, problems: ['This item is not an object.'], origin: `item ${i + 1}` }
    const nested = item.inputs && typeof item.inputs === 'object' && !Array.isArray(item.inputs) ? (item.inputs as Record<string, unknown>) : null
    const src = nested ?? item
    const name = typeof item.name === 'string' ? item.name : typeof item.task === 'string' ? item.task : ''
    const notesRaw = typeof item.notes === 'string' ? item.notes : typeof item.description === 'string' ? item.description : null
    return build(`j${i}`, `item ${i + 1}`, name, notesRaw && notesRaw.trim() ? notesRaw.trim().slice(0, 2000) : null, src, nested ?? {})
  })
  return { candidates, problem: null }
}

/** A row the engine's importer accepted (CSV or XLSX), with its extension (adoption, readiness). */
export function candidateFromEngineRow(rec: { line: number; pilot: Record<string, unknown>; extension?: Record<string, unknown> }, file: string): ImportCandidate {
  const src: Record<string, unknown> = { ...rec.pilot, ...(rec.extension ?? {}) }
  if (typeof src.overlap_group === 'string' && src.overlap_group.startsWith('__own_')) delete src.overlap_group
  const id = typeof rec.pilot.id === 'string' ? rec.pilot.id : ''
  const name = typeof rec.pilot.task === 'string' && rec.pilot.task.trim() ? rec.pilot.task : id
  return build(`l${rec.line}`, `line ${rec.line}`, name, `Imported from ${file}${id ? ` (id ${id})` : ''}.`, src, {})
}

/** A header-only CSV the engine's importer reads. No data rows: nothing is pre-filled. */
export const IMPORT_TEMPLATE_CSV = 'id,task,setup_cost_inr,annual_benefit_inr,annual_running_cost_inr,benefit_type,overlap_group,adoption_pct,readiness,ramp_months,delay_months\n'

export type ImportKind = 'engine' | 'json' | 'unsupported'
export function importKind(name: string): ImportKind {
  const ext = /\.([A-Za-z0-9]+)$/.exec(name.trim())?.[1].toLowerCase()
  if (ext === 'csv' || ext === 'xlsx') return 'engine'
  if (ext === 'json') return 'json'
  return 'unsupported'
}

export const IMPORT_ACCEPT = '.csv,.xlsx,.json,text/csv,application/json,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
