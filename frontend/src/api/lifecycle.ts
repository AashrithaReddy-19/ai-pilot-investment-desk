import type { CheckpointResponse, CreatePlanResponse, PlanProgress, PilotPlan, ReallocationResponse, ScenarioBody } from './types'
import { V1, enc, json, text, file } from './http'
import type { Download } from './http'

/* ---- Phase 6 lifecycle: checkpoint preview, 30-day plans, reallocation, memo ---- */

export const checkpoint = (id: string, b: ScenarioBody & { observed_adoption_pct?: number; observed_quality_pct?: number; revised_before?: boolean; duration_days?: number }) =>
  json<CheckpointResponse>('POST', `${V1}/checkpoint/${enc(id)}`, b)

export const createPlan = (b: { pilot_id: string; owner?: string; duration_days?: number } & Partial<ScenarioBody>) =>
  json<CreatePlanResponse>('POST', `${V1}/plans`, b)
export const listPlans = async () => (await json<{ plans: PilotPlan[] | null }>('GET', `${V1}/plans`)).data.plans ?? []
export const planProgress = (id: string) => json<PlanProgress>('GET', `${V1}/plans/${enc(id)}`)
export const setPlanStatus = (id: string, status: PilotPlan['status']) => json<PilotPlan>('POST', `${V1}/plans/${enc(id)}/status`, { status })

export const realloc = (b: { pilot_id: string; portfolio_name?: string; evaluation_id?: string | number }) =>
  json<ReallocationResponse>('POST', `${V1}/reallocation`, b)

/** The one-page decision memo as markdown text (the gateway embeds the stored plan and actuals). */
export const memoText = (b: ScenarioBody & { pilot_id: string }) => text(`${V1}/export/memo`, b)

/** The same memo as a browser download. */
export const memoDownload = (b: ScenarioBody & { pilot_id: string }): Promise<Download> => file(`${V1}/export/memo`, b, 'memo.md')
