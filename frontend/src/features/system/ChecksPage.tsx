import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { useService } from '../../components/shell/ServiceContext'

const DEP: Record<string, string> = { postgres: 'Database (PostgreSQL)', rules_engine: 'Rules engine', redis: 'Cache (Redis, optional)' }

export function ChecksPage() {
  const svc = useService()

  return (
    <>
      <header className="page-head">
        <span className="eyebrow">API and system checks</span>
        <h1 className="h1">Verified, not assumed.</h1>
        <p className="lede">Live results from the backend: the health of the gateway, the database, the rules engine and the cache.</p>
      </header>

      <section className="stage" aria-labelledby="h-health">
        <div className="stage-head" style={{ gridTemplateColumns: 'minmax(0,1fr) auto' }}>
          <div><h2 className="h2" id="h-health">Service health</h2><p className="caption">From <span className="mono">/health</span> and <span className="mono">/ready</span>.</p></div>
          <button className="btn btn-sm" onClick={svc.recheck}>Check again</button>
        </div>
        {svc.state === 'offline' && <ErrorNotice title="The gateway is not reachable." error={new Error('Start the backend and check again.')} onRetry={svc.recheck} />}
        {svc.ready && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Dependency health reported by the gateway</caption>
            <thead><tr><th scope="col">Dependency</th><th scope="col">Status</th></tr></thead>
            <tbody>{Object.entries(svc.ready.checks).map(([k, v]) => (
              <tr key={k}><td>{DEP[k] ?? k}</td><td><span className="badge"><span className={`dot ${v === 'ok' ? 'dot-ok' : v === 'disabled' ? 'dot-off' : k === 'redis' ? 'dot-warn' : 'dot-bad'}`} aria-hidden />{v}</span></td></tr>
            ))}</tbody>
          </table></div>
        )}
        {svc.state === 'checking' && !svc.ready && <p className="caption">Checking…</p>}
      </section>
    </>
  )
}
