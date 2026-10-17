import { Fragment } from 'react'
import type { Pilot, PilotsResponse } from '../../api/types'
import { formatINR, formatPayback, formatPct, groupLabel } from '../../domain/format'
import { currentEvalKey, useWorkspace } from '../../state/useWorkspace'
import { AssumedTag, Empty, hasFlag, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { StageHead } from './PilotsStage'

const ASSUMED_LINES = new Set(['RUN_COST_ASSUMED', 'ADOPTION_ASSUMED', 'READINESS_ASSUMED', 'BENEFIT_TYPE_UNKNOWN', 'OVERLAP_UNKNOWN', 'TIME_NOT_CASH'])

/** Stage 2. Both benefit views side by side, exactly as POST /api/v1/evaluate returned them. */
export function EvaluateStage() {
  const s = useWorkspace()
  const { evaluation: run, source, drafts, evaluate } = s
  const hasScenario = source === 'synthetic' || (source === 'drafts' && drafts.length > 0)
  const current = run.key !== null && run.key === currentEvalKey(s)
  const loading = run.status === 'loading'

  return (
    <section className="stage" id="stage-2" aria-labelledby="h-stage-2">
      <StageHead n={2} title="Evaluate pilots" actions={<button className="btn btn-primary" onClick={() => void evaluate()} disabled={!hasScenario || loading}>{loading ? 'Evaluating…' : 'Evaluate pilots'}</button>}>
        The rules engine calculates net benefit and payback two ways. Nothing is estimated until you press the button.
      </StageHead>
      <h2 id="h-stage-2" className="sr">Evaluate pilots</h2>

      {!hasScenario && <Empty title="Nothing to evaluate yet">Add a pilot in stage 1, or explore the synthetic C23 case, then come back here.</Empty>}

      <div aria-live="polite" className="stack">
        {hasScenario && run.status === 'idle' && <Empty title="Not evaluated yet">Press “Evaluate pilots” to see independent and conservative results side by side.</Empty>}
        {loading && <Skeleton rows={4} height={44} />}
        {run.status === 'error' && <ErrorNotice title="Not evaluated." error={run.error} onRetry={() => void evaluate()} />}
        {run.status === 'done' && !current && (
          <div className="notice notice-warn"><div><strong>Out of date.</strong> The pilots or budget changed after this evaluation, so its figures are hidden. Evaluate again for current results.</div>
            <button className="btn btn-sm" onClick={() => void evaluate()}>Evaluate again</button></div>
        )}
        {run.status === 'done' && current && run.data && <Results data={run.data.pilots} requestId={run.requestId} cache={run.cache} />}
      </div>
    </section>
  )
}

function Results({ data, requestId, cache }: { data: PilotsResponse; requestId?: string; cache?: string }) {
  const { pilots, invalid, warnings } = data
  // Group identical messages so the same assumption is not repeated once per pilot.
  const grouped = new Map<string, string[]>()
  for (const p of pilots) for (const f of p.flags) if (ASSUMED_LINES.has(f.code)) grouped.set(f.message, [...(grouped.get(f.message) ?? []), p.id])
  const assumed = [...grouped.entries()].map(([message, ids]) => ({ ids, message }))
  return (
    <div className="stack" style={{ gap: 24 }}>
      <p className="caption">Rule-based calculation from the figures entered. Projected, not measured.{requestId && <> Request <span className="mono">{requestId}</span>{cache && <> · cache {cache}</>}.</>}</p>

      {pilots.length === 0 ? <Empty title="No valid pilots">Every record was rejected. See the excluded records below.</Empty> : (
        <>
          <div className="tbl-wrap only-wide">
            <table className="tbl">
              <caption className="sr">Independent and conservative results for each pilot</caption>
              <thead>
                <tr>
                  <th rowSpan={2} className="sticky" scope="col">Pilot</th>
                  <th rowSpan={2} className="num" scope="col">Setup</th>
                  <th rowSpan={2} className="num" scope="col">Running / yr</th>
                  <th colSpan={2} scope="colgroup" style={{ textAlign: 'center', borderLeft: '1px solid var(--line-strong)' }}>Independent</th>
                  <th colSpan={2} scope="colgroup" style={{ textAlign: 'center', borderLeft: '1px solid var(--line-strong)' }}>Conservative</th>
                  <th rowSpan={2} className="num" scope="col" style={{ borderLeft: '1px solid var(--line-strong)' }}>Breakeven adoption</th>
                  <th rowSpan={2} scope="col">Stop rule</th>
                </tr>
                <tr>
                  <th className="num" scope="col" style={{ borderLeft: '1px solid var(--line-strong)' }}>Net / yr</th><th className="num" scope="col">Payback</th>
                  <th className="num" scope="col" style={{ borderLeft: '1px solid var(--line-strong)' }}>Net / yr</th><th className="num" scope="col">Payback</th>
                </tr>
              </thead>
              <tbody>
                {pilots.map((p) => (
                  <tr key={p.id}>
                    <td className="sticky"><div className="cell-pilot"><span className="id">{p.id}</span><span className="nm">{p.task}</span></div></td>
                    <td className="num">{formatINR(p.setup)}</td>
                    <td className="num">{formatINR(p.run)} {hasFlag(p.flags, 'RUN_COST_ASSUMED') && <AssumedTag />}</td>
                    <td className={`num strong${p.independent.net <= 0 ? ' neg' : ''}`} style={{ borderLeft: '1px solid var(--line)' }}>{formatINR(p.independent.net)}</td>
                    <td className="num">{formatPayback(p.independent.payback_months)}</td>
                    <td className={`num strong${p.conservative.net <= 0 ? ' neg' : ''}`} style={{ borderLeft: '1px solid var(--line)' }}>{formatINR(p.conservative.net)}</td>
                    <td className="num">{formatPayback(p.conservative.payback_months)}</td>
                    <td className="num" style={{ borderLeft: '1px solid var(--line)' }}>{p.breakeven_adoption_pct === null ? 'Never' : formatPct(p.breakeven_adoption_pct, 1)}</td>
                    <td><span className={`decision decision-${p.stop_rule.decision}`}>{p.stop_rule.decision}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <ul className="stack only-narrow" aria-label="Results by pilot">
            {pilots.map((p) => <PilotCard key={p.id} p={p} />)}
          </ul>

          <div className="stack">
            <h3 className="h3">What if adoption differs?</h3>
            <p className="caption">Conservative view at half, as assumed, and one and a quarter times the assumed adoption. A what-if, not a forecast.</p>
            <div className="tbl-wrap">
              <table className="tbl">
                <caption className="sr">Bad, base and good adoption cases per pilot, conservative view</caption>
                <thead><tr><th className="sticky" scope="col">Pilot</th>
                  {(['bad', 'base', 'good'] as const).map((k) => <Fragment key={k}><th className="num" scope="col" style={{ textTransform: 'capitalize' }}>{k} · adoption</th><th className="num" scope="col">{k} · net / yr</th><th className="num" scope="col">{k} · payback</th></Fragment>)}
                </tr></thead>
                <tbody>
                  {pilots.map((p) => (
                    <tr key={p.id}>
                      <td className="sticky"><div className="cell-pilot"><span className="id">{p.id}</span><span className="nm">{p.task}</span></div></td>
                      {(['bad', 'base', 'good'] as const).map((k) => {
                        const c = p.scenarios.find((x) => x.scenario === k)
                        return c ? <Fragment key={k}><td className="num">{formatPct(c.adoption_pct, 1)}</td><td className={`num${c.net_annual <= 0 ? ' neg' : ''}`}>{formatINR(c.net_annual)}</td><td className="num">{formatPayback(c.payback_months)}</td></Fragment>
                          : <td key={k} colSpan={3} className="num muted">Not provided</td>
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}

      <div className="split">
        <div className="stack">
          <h3 className="h3">Assumed by the service</h3>
          {assumed.length === 0 ? <p className="caption">Every input was entered, so nothing was assumed.</p> : (
            <ul className="stack" style={{ gap: 8 }}>{assumed.map((a, i) => <li key={i} className="caption"><span className="mono">{a.ids.join(', ')}</span> {a.message}</li>)}</ul>
          )}
          {warnings.map((w) => <div key={w} className="notice notice-warn"><div>{w}</div></div>)}
          {invalid.length > 0 && (
            <div className="notice notice-bad" role="alert"><div><strong>Excluded records.</strong>
              <ul>{invalid.map((i) => <li key={i.id}><span className="mono">{i.id}</span>: {i.errors.join('; ')}</li>)}</ul></div></div>
          )}
        </div>
        <details className="more" open>
          <summary>What these measures mean</summary>
          <div className="more-body caption">
            <p><strong>Net benefit per year</strong> is annual benefit minus annual running cost.</p>
            <p><strong>Payback</strong> is setup cost divided by net benefit, times 12 months, assuming the benefit is steady from day one. “Never” means net benefit is zero or negative.</p>
            <p><strong>Independent</strong> takes the benefit exactly as entered.</p>
            <p><strong>Conservative</strong> multiplies benefit by adoption, a readiness factor and a cash factor, because time saved is not cash unless a cost is removed.</p>
            <p><strong>Breakeven adoption</strong> is the adoption at which the conservative net benefit reaches zero.</p>
            <p><strong>Stop rule</strong> compares measured adoption and quality with thresholds. REVISE usually means nothing has been measured yet.</p>
            <p>Overlap groups: {Array.from(new Set(pilots.map((p) => groupLabel(p.group)))).join(', ') || 'none'}.</p>
          </div>
        </details>
      </div>
    </div>
  )
}

function PilotCard({ p }: { p: Pilot }) {
  return (
    <li className="panel" style={{ gap: 12 }}>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <div className="cell-pilot"><span className="id">{p.id}</span><span className="nm">{p.task}</span></div>
        <span className={`decision decision-${p.stop_rule.decision}`}>{p.stop_rule.decision}</span>
      </div>
      <div className="split" style={{ gap: 16, gridTemplateColumns: 'repeat(2, minmax(0, 1fr))' }}>
        <div><div className="eyebrow">Independent</div><div className="kv" style={{ marginTop: 6 }}><span>Net / yr</span><span className={p.independent.net <= 0 ? 'neg' : ''}>{formatINR(p.independent.net)}</span><span>Payback</span><span>{formatPayback(p.independent.payback_months)}</span></div></div>
        <div><div className="eyebrow">Conservative</div><div className="kv" style={{ marginTop: 6 }}><span>Net / yr</span><span className={p.conservative.net <= 0 ? 'neg' : ''}>{formatINR(p.conservative.net)}</span><span>Payback</span><span>{formatPayback(p.conservative.payback_months)}</span></div></div>
      </div>
      <div className="kv"><span>Setup</span><span>{formatINR(p.setup)}</span><span>Running / yr</span><span>{formatINR(p.run)} {hasFlag(p.flags, 'RUN_COST_ASSUMED') && <AssumedTag />}</span><span>Breakeven adoption</span><span>{p.breakeven_adoption_pct === null ? 'Never' : formatPct(p.breakeven_adoption_pct, 1)}</span></div>
    </li>
  )
}
