import { create } from 'zustand'
import { api, ApiError } from '../api/client'
import type { DefaultsDoc, EvaluateResponse, Mode, PilotsResponse, PortfolioResult, ScenarioBody, ScenarioOverrides } from '../api/types'
import { DraftPilot, overridesFor } from '../domain/draft'

export type Source = 'none' | 'drafts' | 'synthetic'
export type RunStatus = 'idle' | 'loading' | 'done' | 'error'
export interface Run<T> { status: RunStatus; data: T | null; error: ApiError | null; key: string | null; requestId?: string; cache?: string }
const idle = <T,>(): Run<T> => ({ status: 'idle', data: null, error: null, key: null })

/** The portfolio the service chose, plus the per-pilot figures it was chosen from (both from the same scenario). */
export interface Optimization { portfolio: PortfolioResult; pilots: PilotsResponse }

/** '' = no budget given. undefined = not a non-negative number. */
export function parseBudget(input: string): number | null | undefined {
  const t = input.trim().replace(/[,\s]/g, '')
  if (t === '') return null
  const n = Number(t)
  return Number.isFinite(n) && n >= 0 ? n : undefined
}

/** The overrides every scenario call carries. null means there is nothing to evaluate yet. Synthetic sends none, so the engine uses its shipped case. */
export function overridesOf(source: Source, drafts: DraftPilot[]): ScenarioOverrides | null {
  if (source === 'drafts') return drafts.length > 0 ? overridesFor(drafts) : null
  if (source === 'synthetic') return {}
  return null
}

/** Identifies the exact inputs behind a result. A result whose key differs from the current one is stale and must not be shown as current. */
export const scenarioKey = (source: Source, drafts: DraftPilot[]): string =>
  source === 'drafts' ? `d:${JSON.stringify(drafts)}` : source === 'synthetic' ? 's' : ''

interface State {
  source: Source
  drafts: DraftPilot[]
  synthetic: DefaultsDoc | null
  syntheticStatus: 'idle' | 'loading' | 'ready' | 'error'
  syntheticError: ApiError | null
  budgetInput: string
  mode: Mode
  evaluation: Run<EvaluateResponse>
  optimization: Run<Optimization>

  addDraft: (d: DraftPilot) => void
  updateDraft: (oldId: string, d: DraftPilot) => void
  removeDraft: (id: string) => void
  startFresh: () => void
  loadSynthetic: () => Promise<void>
  copySyntheticToDrafts: () => void
  loadScope: (drafts: DraftPilot[], mode: Mode, budget: number | null) => void
  setBudgetInput: (v: string) => void
  setMode: (m: Mode) => void
  evaluate: () => Promise<void>
  optimize: () => Promise<void>
}

const asApiError = (e: unknown): ApiError => (e instanceof ApiError ? e : new ApiError(e instanceof Error ? e.message : 'Unknown error'))

export const useWorkspace = create<State>((set, get) => ({
  source: 'none',
  drafts: [],
  synthetic: null,
  syntheticStatus: 'idle',
  syntheticError: null,
  budgetInput: '',
  mode: 'independent',
  evaluation: idle(),
  optimization: idle(),

  addDraft: (d) => set((s) => ({ source: 'drafts', drafts: [...s.drafts, d] })),
  updateDraft: (oldId, d) => set((s) => ({ drafts: s.drafts.map((x) => (x.id === oldId ? d : x)) })),
  removeDraft: (id) => set((s) => {
    const drafts = s.drafts.filter((x) => x.id !== id)
    return { drafts, source: drafts.length === 0 && s.source === 'drafts' ? 'none' : s.source }
  }),
  startFresh: () => set({ source: 'none', drafts: [], evaluation: idle(), optimization: idle() }),

  loadSynthetic: async () => {
    set({ syntheticStatus: 'loading', syntheticError: null })
    try {
      const { data } = await api.defaults()
      set({ synthetic: data, syntheticStatus: 'ready', source: 'synthetic' })
    } catch (e) {
      set({ syntheticStatus: 'error', syntheticError: asApiError(e) })
    }
  },
  copySyntheticToDrafts: () => {
    const doc = get().synthetic
    if (!doc) return
    const ext = new Map(doc.extension.pilots.map((p) => [p.id, p]))
    const drafts: DraftPilot[] = doc.starting_state.records.pilots.map((p) => {
      const e = ext.get(p.id)
      return {
        id: p.id, task: p.task, description: e?.description ?? '', setup_cost_inr: p.setup_cost_inr,
        annual_running_cost_inr: p.annual_running_cost_inr, annual_benefit_inr: p.annual_benefit_inr,
        benefit_type: p.benefit_type === 'time freed' ? 'time freed' : 'cashable assumption', overlap_group: p.overlap_group,
        adoption_pct: e?.adoption_pct ?? null, readiness: e?.readiness ?? null,
        observed_adoption_pct: e?.observed_adoption_pct ?? null, observed_quality_pct: e?.observed_quality_pct ?? null,
      }
    })
    set({ source: 'drafts', drafts })
  },
  loadScope: (drafts, mode, budget) => set({ source: drafts.length ? 'drafts' : 'none', drafts, mode, budgetInput: budget === null ? '' : String(budget), evaluation: idle(), optimization: idle() }),
  setBudgetInput: (budgetInput) => set({ budgetInput }),
  setMode: (mode) => set({ mode }),

  evaluate: async () => {
    const { source, drafts, budgetInput } = get()
    const o = overridesOf(source, drafts)
    if (!o) return
    const budget = parseBudget(budgetInput)
    const body: ScenarioBody = { ...o, ...(typeof budget === 'number' ? { budget } : {}) }
    const key = `${scenarioKey(source, drafts)}|b:${typeof budget === 'number' ? budget : ''}`
    set({ evaluation: { status: 'loading', data: null, error: null, key } })
    try {
      const r = await api.evaluate(body)
      set({ evaluation: { status: 'done', data: r.data, error: null, key, requestId: r.meta.requestId, cache: r.meta.cache } })
    } catch (e) {
      set({ evaluation: { status: 'error', data: null, error: asApiError(e), key } })
    }
  },

  optimize: async () => {
    const { source, drafts, budgetInput, mode } = get()
    const o = overridesOf(source, drafts)
    const budget = parseBudget(budgetInput)
    if (!o || typeof budget !== 'number') return
    const key = `${scenarioKey(source, drafts)}|m:${mode}|b:${budget}`
    set({ optimization: { status: 'loading', data: null, error: null, key } })
    try {
      const body: ScenarioBody = { ...o, mode, budget }
      const [portfolio, pilots] = await Promise.all([api.portfolio(body), api.pilots(body)])
      set({ optimization: { status: 'done', data: { portfolio: portfolio.data, pilots: pilots.data }, error: null, key, requestId: portfolio.meta.requestId, cache: portfolio.meta.cache } })
    } catch (e) {
      set({ optimization: { status: 'error', data: null, error: asApiError(e), key } })
    }
  },
}))

/** Keys describing what the screen currently asks for, to compare against a stored result's key. */
export const currentEvalKey = (s: Pick<State, 'source' | 'drafts' | 'budgetInput'>): string => {
  const b = parseBudget(s.budgetInput)
  return `${scenarioKey(s.source, s.drafts)}|b:${typeof b === 'number' ? b : ''}`
}
export const currentOptKey = (s: Pick<State, 'source' | 'drafts' | 'budgetInput' | 'mode'>): string => {
  const b = parseBudget(s.budgetInput)
  return `${scenarioKey(s.source, s.drafts)}|m:${s.mode}|b:${typeof b === 'number' ? b : ''}`
}

/** The pilots in the current scenario, as id and name, for pickers. Comes from user input or the service's own defaults. */
export function scenarioPilots(s: Pick<State, 'source' | 'drafts' | 'synthetic'>): { id: string; task: string }[] {
  if (s.source === 'drafts') return s.drafts.map((d) => ({ id: d.id, task: d.task }))
  if (s.source === 'synthetic' && s.synthetic) return s.synthetic.starting_state.records.pilots.map((p) => ({ id: p.id, task: p.task }))
  return []
}
