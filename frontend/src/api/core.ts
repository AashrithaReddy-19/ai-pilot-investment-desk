import type { DefaultsDoc, ReadyResponse } from './types'
import { V1, json, send } from './http'

export const health = () => json<{ ok: boolean }>('GET', '/health')
/** /ready answers 503 with the same body when a dependency is down, so the body is read either way. */
export const ready = async (): Promise<ReadyResponse> => {
  const res = await send('GET', '/ready')
  try { return (await res.json()) as ReadyResponse } catch { return { ready: false, checks: {} } }
}
export const defaults = () => json<DefaultsDoc>('GET', `${V1}/defaults`)
