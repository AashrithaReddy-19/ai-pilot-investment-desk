import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { ManualCheck } from '../../api/types'
import { formatINR, formatPayback } from '../../domain/format'
import { currentOptKey, overridesOf, parseBudget, scenarioPilots, useWorkspace } from '../../state/useWorkspace'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextField } from '../../components/ui/Fields'
import { StageHead } from './PilotsStage'

/** Stage 3. The service chooses the portfolio; this screen only collects the budget and shows its answer and reasons. */
export function OptimizeStage({ standalone = false }: { standalone?: boolean }) {
  const s = useWorkspace()
  const { source, drafts, budgetInput, mode, optimization: run, setBudgetInput, setMode, optimize } = s
  const [budgetError, setBudgetError] = useState<string | null>(null)
  const pilots = scenarioPilots({ source, drafts })
  const hasScenario = pilots.length > 0
  const current = run.key !== null && run.key === currentOptKey(s)
  const loading = run.status === 'loading'

  const run1 = () => {
    const b = parseBudget(budgetInput)
    if (typeof b !== 'number') { setBudgetError(b === null ? 'Enter a budget in INR.' : 'Enter the budget as a non-negative number.'); return }
    setBudgetError(null)
    void optimize()
  }

  return (
    <section className="stage" id="stage-3" aria-labelledby="h-stage-3">
      <StageHead n={3} title="Optimize the portfolio">
        Set a budget for setup costs. The service tests every combination that fits, never funds two pilots that claim the same benefit, and keeps the best.
      </StageHead>
      <h2 id="h-stage-3" className="sr">Optimize the portfolio</h2>

      {!hasScenario ? (
        <Empty title="Nothing to optimize yet">{standalone ? 'Add pilots in the decision workspace first.' : 'Add a pilot in stage 1.'}</Empty>
      ) : (
        <>
          <form className="row" style={{ alignItems: 'flex-end', gap: 20 }} onSubmit={(e) => { e.preventDefault(); run1() }} noValidate>
            <div style={{ width: 260, maxWidth: '100%' }}>
              <TextField id="budget" label="Budget for setup costs" required unit="INR" inputMode="numeric" value={budgetInput} onChange={(v) => { setBudgetInput(v); setBudgetError(null) }}
                error={budgetError ?? undefined} />
            </div>
            <div className="field">
              <span className="label" id="mode-l">Rank pilots by</span>
              <div className="segmented" role="group" aria-labelledby="mode-l">
                <button type="button" aria-pressed={mode === 'independent'} onClick={() => setMode('independent')}>Independent benefit</button>
                <button type="button" aria-pressed={mode === 'conservative'} onClick={() => setMode('conservative')}>Conservative benefit</button>
              </div>
            </div>
            <button className="btn btn-primary" type="submit" disabled={loading}>{loading ? 'Optimizing…' : 'Optimize portfolio'}</button>
          </form>

          <div aria-live="polite" className="stack" style={{ gap: 24 }}>
            {run.status === 'idle' && <Empty title="No portfolio yet">Enter a budget and press “Optimize portfolio”. No recommendation appears until the service has calculated one.</Empty>}
            {loading && <Skeleton rows={3} height={48} />}
            {run.status === 'error' && <ErrorNotice title="Not optimized." error={run.error} onRetry={run1} />}
            {run.status === 'done' && !current && (
              <div className="notice notice-warn"><div><strong>Out of date.</strong> The pilots, budget or ranking changed after this result, so it is hidden. Optimize again for a current answer.</div><button className="btn btn-sm" onClick={run1}>Optimize again</button></div>
            )}
            {run.status === 'done' && current && run.data && <Allocation />}
          </div>
        </>
      )}
    </section>
  )
}

function Allocation() {
  const s = useWorkspace()
  const { optimization: run, source, drafts } = s
  const data = run.data!
  const pf = data.portfolio
  const names = new Map(scenarioPilots({ source, drafts }).map((p) => [p.id, p.task]))
  const byId = new Map(data.pilots.pilots.map((p) => [p.id, p]))
  const view = (id: string) => byId.get(id)?.[pf.mode]
  const selected = pf.selected
  const rejected = pf.rejected
  const spent = pf.budget > 0 ? Math.min(100, (pf.total_setup / pf.budget) * 100) : 0

  return (
    <div className="stack" style={{ gap: 28 }}>
      <div className="split split-wide">
        <div className="stack">
          <p className="caption eyebrow">Projected annual net benefit · {pf.mode} view</p>
          {selected.length > 0 ? <p className="figure">{formatINR(pf.total_net)}<span className="caption" style={{ marginLeft: 8 }}>a year</span></p> : <p className="figure muted">—</p>}
          <div className={`notice ${pf.ok && selected.length > 0 ? 'notice-ok' : 'notice-warn'}`}><div>{pf.message}</div></div>
          <p className="caption">Calculated by the rules engine.{run.requestId && <> Request <span className="mono">{run.requestId}</span>{run.cache && <> · cache {run.cache}</>}.</>}</p>
        </div>
        <div>
          <dl className="ledger">
            <dt>Available budget</dt><dd>{formatINR(pf.budget)}</dd>
            <dt>Total setup of funded pilots</dt><dd>{selected.length ? formatINR(pf.total_setup) : formatINR(0)}</dd>
            <dt>Remaining budget</dt><dd>{formatINR(pf.budget_left)}</dd>
            <dt>Budget feasibility</dt><dd>{pf.ok ? (selected.length ? 'A portfolio fits' : 'Nothing fits or pays back') : 'Not calculated'}</dd>
          </dl>
          <div style={{ marginTop: 12 }} role="img" aria-label={`${spent.toFixed(0)} percent of the budget is allocated`}>
            <div style={{ height: 4, background: 'var(--line)', borderRadius: 2, overflow: 'hidden' }}><div style={{ width: `${spent}%`, height: '100%', background: 'var(--accent)', transition: 'width 0.5s var(--ease)' }} /></div>
            <div className="hint" style={{ marginTop: 6 }}>{spent.toFixed(0)}% of budget allocated</div>
          </div>
        </div>
      </div>

      <div className="stack">
        <h3 className="h3">Funded</h3>
        {selected.length === 0 ? <p className="caption">No pilot is funded under this budget and view.</p> : (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Funded pilots</caption>
            <thead><tr><th className="sticky" scope="col">Pilot</th><th className="num" scope="col">Setup</th><th className="num" scope="col">Net / yr</th><th className="num" scope="col">Payback</th></tr></thead>
            <tbody>{selected.map((id) => { const v = view(id); const p = byId.get(id); return (
              <tr key={id}><td className="sticky"><div className="cell-pilot"><span className="id">{id}</span><span className="nm">{names.get(id) ?? p?.task ?? id}</span></div></td>
                <td className="num">{formatINR(p?.setup)}</td><td className="num strong">{formatINR(v?.net)}</td><td className="num">{formatPayback(v?.payback_months)}</td></tr>) })}</tbody>
          </table></div>
        )}
      </div>

      <div className="stack">
        <h3 className="h3">Not funded, and why</h3>
        {rejected.length === 0 ? <p className="caption">Every candidate was funded.</p> : (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Pilots not funded with the service's reasons</caption>
            <thead><tr><th className="sticky" scope="col">Pilot</th><th className="num" scope="col">Setup</th><th className="num" scope="col">Net / yr</th><th scope="col">Reason from the service</th></tr></thead>
            <tbody>{rejected.map((r) => { const v = view(r.id); const p = byId.get(r.id); return (
              <tr key={r.id}><td className="sticky"><div className="cell-pilot"><span className="id">{r.id}</span><span className="nm">{names.get(r.id) ?? p?.task ?? r.id}</span></div></td>
                <td className="num">{formatINR(p?.setup)}</td><td className={`num${v && v.net <= 0 ? ' neg' : ''}`}>{formatINR(v?.net)}</td><td className="wrap-cell">{r.reason}</td></tr>) })}</tbody>
          </table></div>
        )}
        {rejected.some((r) => r.reason.startsWith('Overlap')) && <p className="caption">Pilots that share a benefit group are not funded together, so the same benefit is counted once. This is a rule of the engine, not a judgement that the pilots are duplicates.</p>}
      </div>

      <p className="caption">Same inputs always give the same portfolio. When two combinations return the same total, the service decides by its documented order: {data.pilots.tie_break_order.map((t, i) => `${i + 1}. ${t}`).join(', ')}. For a whole portfolio with equal net benefit, faster payback means a lower total setup cost.</p>
      {pf.warnings.map((w) => <div key={w} className="notice notice-warn"><div>{w}</div></div>)}
      {pf.invalid.length > 0 && <div className="notice notice-bad"><div><strong>Excluded records.</strong><ul>{pf.invalid.map((i) => <li key={i.id}>{i.id}: {i.errors.join('; ')}</li>)}</ul></div></div>}

      <ManualCheckPanel />
    </div>
  )
}

/** Test your own pick against the budget and the shared-benefit rule, using POST /api/v1/portfolio/check. */
function ManualCheckPanel() {
  const s = useWorkspace()
  const { source, drafts, mode } = s
  const budget = s.optimization.data?.portfolio.budget
  const pilots = scenarioPilots({ source, drafts })
  const [picked, setPicked] = useState<string[]>([])
  const [result, setResult] = useState<ManualCheck | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setPicked([]); setResult(null) }, [s.optimization.key])

  const run = async () => {
    const o = overridesOf(source, drafts)
    if (!o || budget === undefined) return
    setBusy(true); setError(null)
    try { setResult((await api.checkSelection({ ...o, mode, budget, ids: picked })).data) }
    catch (e) { setResult(null); setError(e) }
    finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h3 className="h3">Check your own selection</h3>
      <p className="caption">Tick the pilots you would fund and the service checks them against the budget above and the shared-benefit rule.</p>
      <fieldset>
        <legend>Pilots to fund</legend>
        <div className="row" style={{ gap: '10px 24px' }}>
          {pilots.map((p) => (
            <label key={p.id} className="row" style={{ gap: 8, cursor: 'pointer' }}>
              <input type="checkbox" checked={picked.includes(p.id)} onChange={() => { setResult(null); setPicked((x) => (x.includes(p.id) ? x.filter((i) => i !== p.id) : [...x, p.id])) }} />
              <span className="mono">{p.id}</span><span className="caption">{p.task}</span>
            </label>
          ))}
        </div>
      </fieldset>
      <div className="row"><button className="btn" onClick={() => void run()} disabled={busy || picked.length === 0 || budget === undefined}>{busy ? 'Checking…' : 'Check selection'}</button>
        <span className="caption">{budget === undefined ? 'Optimize first so there is a budget to check against.' : `Against a budget of ${formatINR(budget)}.`}</span></div>
      <div aria-live="polite">
        {error != null && <ErrorNotice title="Could not check the selection." error={error} />}
        {result && (result.ok
          ? <div className="notice notice-ok"><div>This selection fits the budget and counts no benefit twice.</div></div>
          : <div className="notice notice-warn"><div><strong>This selection does not work.</strong><ul>{result.problems.map((m) => <li key={m}>{m}</li>)}</ul></div></div>)}
      </div>
    </div>
  )
}
