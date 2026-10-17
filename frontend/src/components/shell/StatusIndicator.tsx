import { useEffect, useRef, useState } from 'react'
import { useService } from './ServiceContext'

const LABEL = { checking: 'Connecting', online: 'Connected', degraded: 'Degraded', offline: 'Offline' } as const
const TONE = { checking: 'wait', online: 'ok', degraded: 'warn', offline: 'bad' } as const
const DEP: Record<string, string> = { postgres: 'Database (PostgreSQL)', rules_engine: 'Rules engine', redis: 'Cache (Redis, optional)' }

/** Real backend health from /health and /ready. The text always states the state, colour is only a hint. */
export function StatusIndicator() {
  const { state, ready, recheck } = useService()
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => { if (box.current && !box.current.contains(e.target as Node)) setOpen(false) }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('mousedown', onDown); document.removeEventListener('keydown', onKey) }
  }, [open])

  return (
    <div className="pop-wrap" ref={box}>
      <button className="status-btn" aria-expanded={open} aria-haspopup="true" onClick={() => setOpen((o) => !o)}>
        <span className={`dot dot-${TONE[state]}`} aria-hidden />
        <span><span className="sr">Backend status: </span>{LABEL[state]}</span>
      </button>
      {open && (
        <div className="popover" role="region" aria-label="Backend status details">
          <strong>{state === 'online' ? 'All services are responding' : state === 'offline' ? 'The gateway is not reachable' : state === 'checking' ? 'Checking services…' : 'Some services need attention'}</strong>
          {state === 'offline' && <p className="caption">Start the backend, then check again. No figures can be calculated or saved until it answers.</p>}
          {ready && (
            <ul className="stack" style={{ gap: 6 }}>
              {Object.entries(ready.checks).map(([k, v]) => {
                const tone = v === 'ok' ? 'ok' : v === 'disabled' ? 'off' : k === 'redis' ? 'warn' : 'bad'
                return (
                  <li key={k} className="row" style={{ justifyContent: 'space-between', flexWrap: 'nowrap' }}>
                    <span className="caption">{DEP[k] ?? k}</span>
                    <span className="badge"><span className={`dot dot-${tone}`} aria-hidden />{v}</span>
                  </li>
                )
              })}
            </ul>
          )}
          {ready && ready.checks.redis?.startsWith('down') && <p className="caption">Redis is optional. Everything works without it, just without response caching.</p>}
          <div><button className="btn btn-sm" onClick={recheck}>Check again</button></div>
        </div>
      )}
    </div>
  )
}
