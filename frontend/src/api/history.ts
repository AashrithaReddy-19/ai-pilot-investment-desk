import type { AuditEvent, EvaluationSummary, HistoryEvent } from './types'
import { V1, json } from './http'

/** The merged evaluation + decision timeline (newest first). */
export const timeline = (limit = 25) =>
  json<{ events: HistoryEvent[] | null; count: number }>('GET', `${V1}/history?limit=${limit}`)

/** The evaluations log, newest first (the gateway clamps limit to 1–50). */
export const listEvaluations = async (limit = 20) =>
  (await json<{ evaluations: EvaluationSummary[] | null }>('GET', `${V1}/evaluations?limit=${limit}`)).data.evaluations ?? []

/** Successful edits and exports, newest first; optionally for one record. */
export const listAudit = async (opts: { entityType?: string; entityId?: string; limit?: number } = {}) => {
  const q = new URLSearchParams({ limit: String(opts.limit ?? 25) })
  if (opts.entityType) q.set('entity_type', opts.entityType)
  if (opts.entityId) q.set('entity_id', opts.entityId)
  return (await json<{ events: AuditEvent[] | null; count: number }>('GET', `${V1}/audit?${q}`)).data.events ?? []
}
