import type { HistoryEvent } from './types'
import { V1, json } from './http'

/** The merged evaluation + decision timeline (newest first). */
export const timeline = (limit = 25) =>
  json<{ events: HistoryEvent[] | null; count: number }>('GET', `${V1}/history?limit=${limit}`)
