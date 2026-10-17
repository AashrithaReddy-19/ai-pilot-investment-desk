import type { ObservationResult, ScenarioBody } from './types'
import { V1, enc, json } from './http'

export const recordObservation = (id: string, b: ScenarioBody & { observed_adoption_pct?: number; observed_quality_pct?: number; source?: string }) =>
  json<ObservationResult>('POST', `${V1}/pilots/${enc(id)}/observations`, b)
