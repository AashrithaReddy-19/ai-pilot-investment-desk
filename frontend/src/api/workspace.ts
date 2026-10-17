import type { EvaluateResponse, ManualCheck, PilotHistory, PilotsResponse, PortfolioResult, ScenarioBody, StopRule, SweepParameter, SweepResponse, WhatIfOverrides, WhatIfResponse } from './types'
import { V1, enc, json } from './http'

export const pilots = (b: ScenarioBody) => json<PilotsResponse>('POST', `${V1}/pilots`, b)
export const portfolio = (b: ScenarioBody & { objective?: string }) => json<PortfolioResult>('POST', `${V1}/portfolio`, b)
export const checkSelection = (b: ScenarioBody & { ids: string[] }) => json<ManualCheck>('POST', `${V1}/portfolio/check`, b)
export const evaluate = (b: ScenarioBody) => json<EvaluateResponse>('POST', `${V1}/evaluate`, b)
export const stopRule = (id: string, b: ScenarioBody & { observed_adoption_pct?: number; observed_quality_pct?: number }) =>
  json<StopRule>('POST', `${V1}/stoprule/${enc(id)}`, b)
export const history = (id: string, limit = 50) => json<PilotHistory>('GET', `${V1}/pilots/${enc(id)}/history?limit=${limit}`)
export const whatIf = (b: ScenarioBody & { pilot_id: string; overrides: WhatIfOverrides }) => json<WhatIfResponse>('POST', `${V1}/whatif`, b)
export const sweep = (b: ScenarioBody & { pilot_id?: string; parameter: SweepParameter }) => json<SweepResponse>('POST', `${V1}/sweep`, b)
