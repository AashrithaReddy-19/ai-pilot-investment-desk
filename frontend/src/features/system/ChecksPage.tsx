import { useCallback, useEffect, useState } from 'react'
import { api } from '../../api/client'
import { formatINR } from '../../domain/format'
import { Empty } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SyntheticTag } from '../../components/ui/Bits'
import { useService } from '../../components/shell/ServiceContext'

interface Outcome { state: 'pass' | 'fail'; detail: string; requestId?: string }

// Test input chosen by this page. It is not in the fixture; it exists only to trigger the third documented check.
const LOW_ADOPTION = 5
const LOW_QUALITY = 50
// Stated by the first documented check: INR 1.8 lakh benefit minus INR 0.6 lakh running cost.
const EXPECTED_NET = 120000

const DEP: Record<string, string> = { postgres: 'Database (PostgreSQL)', rules_engine: 'Rules engine', redis: 'Cache (Redis, optional)' }

export function ChecksPage() {
  const svc = useService()
  const [texts, setTexts] = useState<string[]>([])
  const [out, setOut] = useState<(Outcome | null)[]>([null, null, null])
  const [phase, setPhase] = useState<'running' | 'done' | 'error'>('running')
  const [error, setError] = useState<unknown>(null)

  const run = useCallback(async () => {
    setPhase('running'); setError(null); setOut([null, null, null])
    try {
      const defaults = (await api.defaults()).data
      setTexts(defaults.starting_state.public_checks ?? [])
      const expMonths = defaults.starting_state.records.expected_first_pilot_payback_months

      // 1. Finance: the first pilot's net benefit and simple payback.
      const pil = await api.pilots({ mode: 'independent' })
      const first = pil.data.pilots[0]
      const c1: Outcome = first && first.independent.net === EXPECTED_NET && Math.round(first.independent.payback_months ?? -1) === expMonths
        ? { state: 'pass', detail: `${first.id}: net ${formatINR(first.independent.net)} a year and payback ${first.independent.payback_months?.toFixed(1)} months. Expected ${formatINR(EXPECTED_NET)} and ${expMonths} months.`, requestId: pil.meta.requestId }
        : { state: 'fail', detail: first ? `${first.id}: net ${formatINR(first.independent.net)}, payback ${first.independent.payback_months ?? 'never'}. Expected ${formatINR(EXPECTED_NET)} and ${expMonths} months.` : 'The service returned no valid pilot.', requestId: pil.meta.requestId }

      // 2. Budget and overlap: funded pilots fit the budget and share no benefit group.
      const pf = await api.portfolio({ mode: 'independent' })
      const groups = pf.data.selected.map((id) => pil.data.pilots.find((p) => p.id === id)?.group)
      const dup = groups.length !== new Set(groups).size
      const c2: Outcome = pf.data.ok && pf.data.total_setup <= pf.data.budget && !dup
        ? { state: 'pass', detail: `${pf.data.selected.join(', ')} funded: ${formatINR(pf.data.total_setup)} of ${formatINR(pf.data.budget)}, with no shared benefit group.`, requestId: pf.meta.requestId }
        : { state: 'fail', detail: dup ? 'Two funded pilots share a benefit group.' : pf.data.message, requestId: pf.meta.requestId }

      // 3. Stop rule: low adoption or quality must give an explicit REVISE or STOP. Uses the preview route, so nothing is stored.
      const sr = await api.stopRule(first?.id ?? 'AI1', { observed_adoption_pct: LOW_ADOPTION, observed_quality_pct: LOW_QUALITY })
      const c3: Outcome = sr.data.decision === 'REVISE' || sr.data.decision === 'STOP'
        ? { state: 'pass', detail: `Test input ${LOW_ADOPTION}% adoption and ${LOW_QUALITY}% quality gave ${sr.data.decision}: ${sr.data.reasons[0]}`, requestId: sr.meta.requestId }
        : { state: 'fail', detail: `Expected REVISE or STOP, received ${sr.data.decision}.`, requestId: sr.meta.requestId }

      setOut([c1, c2, c3]); setPhase('done')
    } catch (e) { setError(e); setPhase('error') }
  }, [])
  useEffect(() => { void run() }, [run])

  const passed = out.filter((o) => o?.state === 'pass').length
  return (
    <>
      <header className="page-head">
        <span className="eyebrow">API and system checks</span>
        <h1 className="h1">Verified, not assumed.</h1>
        <p className="lede">Live results from the backend: service health, and the three public checks documented with the case.</p>
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

      <section className="stage" aria-labelledby="h-public">
        <div className="stage-head" style={{ gridTemplateColumns: 'minmax(0,1fr) auto' }}>
          <div><h2 className="h2" id="h-public">Public checks <SyntheticTag /></h2>
            <p className="caption">They run against the synthetic C23 test case, not against your pilots. They are transparent reference tests, not a hidden accuracy benchmark.</p></div>
          <button className="btn btn-sm" onClick={() => void run()} disabled={phase === 'running'}>{phase === 'running' ? 'Running…' : 'Run again'}</button>
        </div>
        {phase === 'error' && <ErrorNotice title="The checks could not run." error={error} onRetry={() => void run()} />}
        <div aria-live="polite">
          {phase === 'done' && <p className="caption" style={{ marginBottom: 8 }}>{passed} of 3 passed.</p>}
          {phase === 'error' && texts.length === 0 && <Empty title="Not run">Checks need the backend. Start it and run them again.</Empty>}
          <ol className="stack" style={{ gap: 0 }}>
            {[0, 1, 2].map((i) => {
              const o = out[i]
              return (
                <li key={i} style={{ display: 'grid', gridTemplateColumns: '84px minmax(0, 1fr)', gap: 16, padding: '18px 0', borderTop: '1px solid var(--line)' }}>
                  <span className={`decision ${o ? (o.state === 'pass' ? 'decision-GO' : 'decision-STOP') : ''}`}>{o ? (o.state === 'pass' ? 'PASS' : 'FAIL') : phase === 'error' ? 'NOT RUN' : 'RUNNING'}</span>
                  <div className="stack" style={{ gap: 6 }}>
                    {texts[i] ? <p>{texts[i]}</p> : <div className="skeleton" style={{ height: 20 }} aria-hidden />}
                    {o && <p className="caption">{o.detail}{o.requestId && <> Request <span className="mono">{o.requestId}</span>.</>}</p>}
                  </div>
                </li>
              )
            })}
          </ol>
        </div>
      </section>
    </>
  )
}
