import type { PlanProgress, ScenarioBody } from './types'
import { ApiError } from './http'
import { checkpoint, listPlans } from './lifecycle'
import { history } from './workspace'

/**
 * The tracker's progress view for a stored proposal. GET /plans/:id runs its checkpoint with no scenario, so it
 * can only see the engine's built-in pilots; this view is rebuilt from the same three stored/engine sources
 * with the proposal's own scenario: the recorded plan, the latest observation, and POST /checkpoint/:id.
 * The gap is the measured percentage minus the plan's target, both as recorded.
 */
export async function planProgressFor(id: string, scenario: ScenarioBody): Promise<PlanProgress> {
  const plan = (await listPlans()).find((p) => p.pilot_id === id)
  if (!plan) throw new ApiError(`No plan is recorded for ${id}.`, { status: 404, code: 'not_found' })
  const latest = (await history(id, 1)).data.observations?.[0] ?? null
  const gaps: PlanProgress['gaps'] = {}
  if (latest?.adoption_pct != null && plan.adoption_target_pct != null) gaps.adoption_gap_pct = latest.adoption_pct - plan.adoption_target_pct
  if (latest?.quality_pct != null && plan.quality_target_pct != null) gaps.quality_gap_pct = latest.quality_pct - plan.quality_target_pct
  const verdict = (await checkpoint(id, scenario)).data
  return {
    pilot_id: id, plan, gaps, verdict,
    actual: latest ? { adoption_pct: latest.adoption_pct, quality_pct: latest.quality_pct, observed_on: latest.observed_on, source: latest.source } : null,
  }
}
