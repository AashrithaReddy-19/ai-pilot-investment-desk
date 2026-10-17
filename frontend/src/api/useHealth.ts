import { useCallback, useEffect, useState } from 'react'
import { api } from './client'
import type { ReadyResponse } from './types'

export type ServiceState = 'checking' | 'online' | 'degraded' | 'offline'
export interface ServiceStatus { state: ServiceState; ready: ReadyResponse | null; recheck: () => void }

/**
 * Real backend health. GET /health says the gateway is up; GET /ready says whether PostgreSQL, the rules engine
 * and Redis are. Redis is optional, so a Redis problem alone is degraded (no cache), not offline.
 */
export function useServiceStatus(): ServiceStatus {
  const [state, setState] = useState<ServiceState>('checking')
  const [ready, setReady] = useState<ReadyResponse | null>(null)

  const check = useCallback(() => {
    setState('checking')
    api.health()
      .then(async () => {
        try {
          const r = await api.ready()
          setReady(r)
          const redisDown = Object.entries(r.checks).some(([k, v]) => k === 'redis' && v.startsWith('down'))
          setState(!r.ready ? 'degraded' : redisDown ? 'degraded' : 'online')
        } catch {
          setReady(null)
          setState('degraded')
        }
      })
      .catch(() => { setReady(null); setState('offline') })
  }, [])

  useEffect(() => {
    check()
    const t = window.setInterval(check, 60000)
    return () => { window.clearInterval(t) }
  }, [check])

  return { state, ready, recheck: check }
}
