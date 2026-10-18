import { Fragment, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api/client'
import type { Mode, Proposal } from '../../api/types'
import type { DecisionView, UsageForecast, WorkspaceSettings, WorkspaceView, YearlyView } from '../../api/viewTypes'
import type { ExplicitSet } from '../../api/views'
import { Card, OutcomePill, PageHeader, Pill } from '../../components/fynd/Page'
import { GroupedBars, Lines } from '../../components/fynd/Charts'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextField } from '../../components/ui/Fields'
import { formatDateTime, formatINR, formatINRCompact, formatPayback } from '../../domain/format'
import { SET_MIN, engineProposal, evaluationSet, formFromInputs } from '../../domain/fynd'
import { useFynd, useProposalDocs } from '../../state/useFynd'

const MODES: { key: Mode; label: string; note: string }[] = [
  { key: 'independent', label: 'Independent', note: 'each proposal on its own stated inputs' },
  { key: 'conservative', label: 'Conservative', note: "the engine's cautious reading of the same inputs" },
]

type ModeRun = { yearly: YearlyView; decision: DecisionView }
type Saved = Record<Mode, { yearly: WorkspaceView<YearlyView>; decision: WorkspaceView<DecisionView> }>

export default function Scenarios() {
  const { proposalId, setProposalId } = useFynd()
  const { docs, error: docsError, reload } = useProposalDocs(false)
  // the engine runs sets of 2 to 8, so every scenario evaluates the whole set and reads the chosen proposal out of it
  const evaluable = useMemo(() => evaluationSet(docs ?? []).set, [docs])
  const enough = evaluable.length >= SET_MIN
  const p = enough ? (evaluable.find((x) => x.id === proposalId) ?? evaluable[0] ?? null) : null
  const setKey = evaluable.map((x) => `${x.id}:${x.version}`).join(',')
  const [shown, setShown] = useState<Record<Mode, boolean>>({ independent: true, conservative: true })
  const [saved, setSaved] = useState<Saved | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [settings, setSettings] = useState<WorkspaceSettings | null>(null)

  useEffect(() => { api.getSettings().then((r) => setSettings(r.data)).catch(() => setSettings({ budget: null })) }, [])
  useEffect(() => { if (p && p.id !== proposalId) setProposalId(p.id) }, [p, proposalId, setProposalId])

  const runSaved = async () => {
    setBusy(true); setErr(null)
    try {
      const out = {} as Saved
      await Promise.all(MODES.map(async (m) => {
        const body = { proposal_ids: evaluable.map((x) => x.id), mode: m.key }
        const [yearly, decision] = await Promise.all([api.workspaceYearly(body), api.workspaceDecision(body)])
        out[m.key] = { yearly: yearly.data, decision: decision.data }
      }))
      setSaved(out)
    } catch (e) { setErr(e); setSaved(null) } finally { setBusy(false) }
  }
  useEffect(() => { if (enough) void runSaved() }, [setKey]) // re-run when the set or any saved inputs change; switching proposal reuses the run

  const modes = MODES.filter((m) => shown[m.key])
  const rowOf = (m: Mode) => (saved && p ? saved[m].yearly.result.proposals.find((x) => x.id === p.name) ?? null : null)
  const first = rowOf('independent')
  const years = first?.years.map((y) => y.year) ?? []

  return (
    <>
      <PageHeader title="Forecast and scenarios" subtitle="How the engine's two modes and your assumptions change future costs and savings."
        actions={<Link to="/analysis" className="btn">Detailed analysis and evidence</Link>} />
      {docsError != null && <ErrorNotice title="Proposals could not be loaded." error={docsError} onRetry={() => void reload()} />}
      {docs === null && docsError == null && <Skeleton rows={2} height={60} />}
      {docs && !p && <Empty title="Not enough evaluable proposals" actions={<Link to="/ai-proposals" className="btn btn-primary">Manage proposals</Link>}>The engine evaluates proposals as a set of 2 to 8: scenarios need at least two stored proposals with an implementation cost and an expected annual benefit ({evaluable.length} now).</Empty>}

      {p && (
        <Card>
          <div className="toolbar">
            <div className="field" style={{ minWidth: 260 }}>
              <label className="label" htmlFor="sc-p">Proposal</label>
              <select id="sc-p" className="select" value={p.id} onChange={(e) => setProposalId(e.target.value)}>
                {evaluable.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
              </select>
            </div>
            <div className="field"><span className="label">Scenarios (engine modes)</span>
              <div className="row" style={{ gap: 16, minHeight: 38 }}>
                {MODES.map((m) => (
                  <label key={m.key} className="row" style={{ gap: 6 }} title={m.note}>
                    <input type="checkbox" checked={shown[m.key]} onChange={(e) => setShown((s) => ({ ...s, [m.key]: e.target.checked }))} />{m.label}
                  </label>
                ))}
              </div>
            </div>
            <span className="grow" />
            <span className="hint" style={{ maxWidth: 360 }}>These are the two modes the engine defines. It has no “expected” or “optimistic” scenario, so none is shown.</span>
          </div>
        </Card>
      )}

      {err != null && <ErrorNotice title="The scenarios did not run." error={err} onRetry={() => void runSaved()} />}
      {busy && !saved && <Skeleton rows={3} height={90} />}

      {p && saved && (
        <>
          <Card title={`Net cash flow by year · ${p.name}`} sub={`Engine forecast over ${first?.years.length ?? 0} years (${saved.independent.yearly.result.horizon_months}-month horizon). INR. Saved inputs v${p.version}.`}>
            {modes.length === 0 ? <p className="caption">Select at least one scenario.</p> : (
              <GroupedBars caption={`Net cash flow by year for ${p.name} per scenario`} x={years} xLabel="year" fmt={formatINR} axisFmt={formatINRCompact}
                series={modes.map((m, i) => ({ name: `${m.label}: net`, values: rowOf(m.key)?.years.map((y) => y.net_inr) ?? [], color: i === 0 ? 'var(--accent)' : 'var(--info)' }))} />
            )}
            <Lines caption={`Cumulative cash position by year for ${p.name}`} x={years} xLabel="Year" fmt={formatINR} axisFmt={formatINRCompact} height={200}
              series={modes.map((m, i) => ({ name: `${m.label}: cumulative`, values: rowOf(m.key)?.years.map((y) => y.cumulative_inr) ?? [], color: i === 0 ? 'var(--accent)' : 'var(--info)', dashed: i === 1 }))} />
            <p className="hint">The current process cost is not projected: the engine returns no per-year baseline, and the browser does not extrapolate one.</p>
          </Card>

          <Card title="Scenario results" sub="All values from the engine; “Not available” means the engine returned nothing for it.">
            <ScenarioTable rows={MODES.map((m) => ({ label: m.label, run: { yearly: saved[m.key].yearly.result, decision: saved[m.key].decision.result }, name: p.name }))} />
            <p className="trace">Saved evaluation · snapshots {MODES.map((m) => <span key={m.key} className="mono">{saved[m.key].yearly.snapshot.id.slice(0, 8)} </span>)}· {formatDateTime(saved.independent.yearly.snapshot.created_at)}</p>
          </Card>

          <WhatIf p={p} set={evaluable} settings={settings} />
          <Traceability p={p} d={saved.independent.decision.result} />
        </>
      )}

      <UsageForecastCard />
    </>
  )
}

function ScenarioTable({ rows }: { rows: { label: string; run: ModeRun; name: string }[] }) {
  return (
    <div className="tbl-wrap"><table className="tbl">
      <caption className="sr">Results per scenario</caption>
      <thead><tr>
        <th scope="col" className="sticky">Scenario</th><th scope="col">Outcome</th><th scope="col" className="num">Total benefit</th><th scope="col" className="num">Total running cost</th>
        <th scope="col" className="num">One-time cost</th><th scope="col" className="num">Net over horizon</th><th scope="col" className="num">Net annual (criteria)</th><th scope="col" className="num">Payback</th><th scope="col">Budget</th>
      </tr></thead>
      <tbody>{rows.map(({ label, run, name }) => {
        const y = run.yearly.proposals.find((x) => x.id === name)
        const rank = run.decision.ranking.find((r) => r.id === name)
        const dec = run.decision.decisions[name]
        const payback = rank?.criteria.payback
        return (
          <tr key={label}>
            <th scope="row" className="sticky strong">{label}</th>
            <td><OutcomePill outcome={dec?.outcome} /></td>
            <td className="num">{formatINR(y?.totals.benefit_inr ?? null)}</td>
            <td className="num">{formatINR(y?.totals.running_cost_inr ?? null)}</td>
            <td className="num">{formatINR(y?.totals.one_time_inr ?? null)}</td>
            <td className="num strong">{formatINR(y?.totals.net_inr ?? null)}</td>
            <td className="num">{formatINR(rank?.criteria.net_annual ?? null)}</td>
            <td className="num">{payback === undefined ? 'Not available' : formatPayback(payback)}</td>
            <td>{dec?.economics.budget ? <Pill tone="off" title="Budget status as the engine reports it">{dec.economics.budget.status}</Pill> : 'Not available'}</td>
          </tr>
        )
      })}</tbody>
    </table></div>
  )
}

/* -------------------------------------------------- what-if (unsaved) */

type Knob = 'adoption_pct' | 'setup_cost_inr' | 'ai_running_cost_inr' | 'annual_benefit_inr'
const KNOBS: { key: Knob; label: string; unit: string }[] = [
  { key: 'adoption_pct', label: 'Adoption', unit: '%' },
  { key: 'setup_cost_inr', label: 'Implementation cost', unit: '₹' },
  { key: 'ai_running_cost_inr', label: 'Recurring running cost', unit: '₹/yr' },
  { key: 'annual_benefit_inr', label: 'Annual benefit', unit: '₹/yr' },
]

function WhatIf({ p, set: members, settings }: { p: Proposal; set: Proposal[]; settings: WorkspaceSettings | null }) {
  const stored = formFromInputs(p.inputs)
  const [vals, setVals] = useState<Record<Knob, string>>({ adoption_pct: '', setup_cost_inr: '', ai_running_cost_inr: '', annual_benefit_inr: '' })
  const [budget, setBudget] = useState('')
  const [res, setRes] = useState<{ base: Record<Mode, ModeRun>; changed: Record<Mode, ModeRun>; key: string } | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [bad, setBad] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setRes(null); setVals({ adoption_pct: '', setup_cost_inr: '', ai_running_cost_inr: '', annual_benefit_inr: '' }); setBudget('') }, [p.id, p.version])

  const key = JSON.stringify({ vals, budget, id: p.id, v: p.version })
  const dirty = res !== null && res.key !== key

  const run = async () => {
    const overrides: Record<string, number> = {}
    for (const k of KNOBS) {
      const t = vals[k.key].trim().replace(/[,\s]/g, '')
      if (!t) continue
      const n = Number(t)
      if (!Number.isFinite(n) || n < 0 || (k.key === 'adoption_pct' && n > 100)) { setBad(`${k.label}: enter ${k.key === 'adoption_pct' ? 'a percentage from 0 to 100' : 'a non-negative amount'}.`); return }
      overrides[k.key] = n
    }
    const bt = budget.trim().replace(/[,\s]/g, '')
    const b = bt === '' ? (settings?.budget ?? null) : Number(bt)
    if (b !== null && (!Number.isFinite(b) || b < 0)) { setBad('Budget: enter a non-negative amount.'); return }
    setBad(null); setBusy(true); setErr(null)
    const set = (props: Record<string, unknown>[], mode: Mode): ExplicitSet => ({
      proposals: props, mode, budget: b,
      ...(settings?.defaults && Object.keys(settings.defaults).length ? { extension: { defaults: settings.defaults } } : {}),
      ...(settings?.budget_set && Object.keys(settings.budget_set).length ? { budget_set: settings.budget_set } : {}),
      ...(settings?.weights && Object.keys(settings.weights).length ? { weights: settings.weights } : {}),
    })
    const one = async (props: Record<string, unknown>[], mode: Mode): Promise<ModeRun> => {
      const s = set(props, mode)
      const [yearly, decision] = await Promise.all([api.yearlySavings({ proposals: s.proposals, mode, budget: s.budget, extension: s.extension }), api.decisionView(s)])
      return { yearly: yearly.data, decision: decision.data }
    }
    try {
      // the whole set, with only the chosen proposal changed, so rank and selection effects show too
      const baseP = members.map((x) => engineProposal(x.name, x.inputs))
      const chP = members.map((x) => engineProposal(x.name, x.inputs, x.id === p.id ? overrides : {}))
      const [bi, bc, ci, cc] = await Promise.all([one(baseP, 'independent'), one(baseP, 'conservative'), one(chP, 'independent'), one(chP, 'conservative')])
      setRes({ base: { independent: bi, conservative: bc }, changed: { independent: ci, conservative: cc }, key })
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  return (
    <Card title="What-if (unsaved)" sub="Change an assumption and the engine recalculates. Nothing is stored: the proposal keeps its saved inputs until you edit it."
      actions={dirty ? <Pill tone="warn">Changed since the last run</Pill> : res ? <Pill tone="info">Unsaved scenario</Pill> : undefined}>
      <div className="form-grid" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        {KNOBS.map((k) => (
          <TextField key={k.key} id={`wi-${k.key}`} label={k.label} unit={k.unit} inputMode="decimal" value={vals[k.key]} onChange={(v) => setVals((x) => ({ ...x, [k.key]: v }))}
            placeholder={stored[k.key] ? `saved: ${stored[k.key]}` : 'saved: not stated'} />
        ))}
        <TextField id="wi-budget" label="Budget" unit="₹" inputMode="decimal" value={budget} onChange={setBudget}
          placeholder={settings?.budget != null ? `workspace: ${settings.budget}` : 'workspace: none'} />
      </div>
      {bad && <div className="field-error" role="alert">{bad}</div>}
      <div className="row">
        <button className="btn btn-primary" onClick={() => void run()} disabled={busy}>{busy ? 'Calculating…' : 'Run what-if'}</button>
        <span className="hint">Blank fields keep the saved value. Linked observations are not included in what-if runs, so both columns below come from the same path.</span>
      </div>
      {err != null && <ErrorNotice title="The what-if did not run." error={err} onRetry={() => void run()} />}
      {res && (
        <div className="tbl-wrap"><table className="tbl">
          <caption className="sr">Saved inputs against the what-if</caption>
          <thead><tr><th scope="col" className="sticky">Scenario</th><th scope="col">Outcome (saved → what-if)</th><th scope="col" className="num">Net over horizon, saved</th><th scope="col" className="num">Net over horizon, what-if</th><th scope="col" className="num">Payback, saved</th><th scope="col" className="num">Payback, what-if</th></tr></thead>
          <tbody>{MODES.map((m) => {
            const b = res.base[m.key]; const c = res.changed[m.key]
            const pb = (r: ModeRun) => { const v = r.decision.ranking.find((x) => x.id === p.name)?.criteria.payback; return v === undefined ? 'Not available' : formatPayback(v) }
            return (
              <tr key={m.key}>
                <th scope="row" className="sticky strong">{m.label}</th>
                <td><span className="row" style={{ gap: 6 }}><OutcomePill outcome={b.decision.decisions[p.name]?.outcome} />→<OutcomePill outcome={c.decision.decisions[p.name]?.outcome} /></span></td>
                <td className="num">{formatINR(b.yearly.proposals.find((x) => x.id === p.name)?.totals.net_inr ?? null)}</td>
                <td className="num strong">{formatINR(c.yearly.proposals.find((x) => x.id === p.name)?.totals.net_inr ?? null)}</td>
                <td className="num">{pb(b)}</td>
                <td className="num strong">{pb(c)}</td>
              </tr>
            )
          })}</tbody>
        </table></div>
      )}
    </Card>
  )
}

/* -------------------------------------------------- traceability */

function Traceability({ p, d }: { p: Proposal; d: DecisionView }) {
  const dec = d.decisions[p.name]
  if (!dec) return null
  const f = formFromInputs(p.inputs)
  const entered: [string, string][] = [
    ['Implementation cost', f.setup_cost_inr], ['Recurring running cost', f.ai_running_cost_inr], ['Annual benefit', f.annual_benefit_inr],
    ['Current annual cost (baseline)', f.baseline_annual_cost_inr], ['Adoption', f.adoption_pct], ['Benefit type', f.benefit_type], ['Readiness', f.readiness], ['Overlap group', f.overlap_group],
  ]
  return (
    <Card title="Assumptions behind these results" sub="What you entered, what the engine assumed, and what is still unverified">
      <div className="grid-2">
        <div className="stack" style={{ gap: 8 }}>
          <h3 className="card-title">Inputs</h3>
          <dl className="dl">{entered.map(([k, v]) => <Fragment key={k}><dt>{k}</dt><dd>{v ? <><span>{v}</span> <Pill tone="info">user-entered</Pill></> : <Pill tone="off">not stated</Pill>}</dd></Fragment>)}</dl>
        </div>
        <div className="stack" style={{ gap: 8 }}>
          <h3 className="card-title">Engine assumptions and checks</h3>
          {dec.economics.flags.length === 0 ? <p className="caption">No assumption flags on the economics.</p> : (
            <ul className="caption">{dec.economics.flags.map((x) => <li key={x.code}><Pill tone="warn">assumed</Pill> {x.message}</li>)}</ul>
          )}
          <ul className="caption">{dec.data_readiness.checks.map((c) => <li key={c.check}>{c.ok ? '✓' : '✗'} {c.check}</li>)}</ul>
          <p className="caption">Evidence band {dec.evidence.band}: {dec.evidence.observed_weight} of {dec.evidence.total_weight} decision weight observed (verified).</p>
        </div>
      </div>
    </Card>
  )
}

/* -------------------------------------------------- usage forecast */

function UsageForecastCard() {
  const [history, setHistory] = useState('')
  const [months, setMonths] = useState('12')
  const [res, setRes] = useState<UsageForecast | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [bad, setBad] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const run = async () => {
    const vals = history.split(/[\s,;]+/).filter(Boolean).map(Number)
    const m = Number(months)
    if (vals.length === 0 || vals.some((v) => !Number.isFinite(v) || v < 0)) { setBad('Enter monthly usage as non-negative numbers, oldest first.'); return }
    if (!Number.isInteger(m) || m < 1 || m > 60) { setBad('Months ahead must be a whole number from 1 to 60.'); return }
    setBad(null); setBusy(true); setErr(null)
    try { setRes((await api.forecastUsage({ history: vals, months: m })).data) } catch (e) { setErr(e) } finally { setBusy(false) }
  }
  const n = res?.base?.length ?? 0
  return (
    <Card title="AI usage forecast" sub="A backtested forecast of monthly usage (tokens, requests or any unit you track) from your own history.">
      <div className="form-grid">
        <div className="field"><label className="label" htmlFor="uf-h">Monthly usage history <span className="req req-yes">required</span></label>
          <textarea id="uf-h" className="textarea" rows={2} value={history} onChange={(e) => setHistory(e.target.value)} />
          <span className="hint">Oldest first. More months give the backtest more to check.</span></div>
        <TextField id="uf-m" label="Months ahead" inputMode="numeric" value={months} onChange={setMonths} />
      </div>
      {bad && <div className="field-error" role="alert">{bad}</div>}
      <div><button className="btn" onClick={() => void run()} disabled={busy}>{busy ? 'Forecasting…' : 'Forecast usage'}</button></div>
      {err != null && <ErrorNotice title="The forecast did not run." error={err} />}
      {res && !res.base && <div className="notice notice-warn"><div>No forecast: {res.flags.map((f) => f.message).join(' ') || 'the engine returned none.'}</div></div>}
      {res && res.base && (
        <>
          <Lines caption="Forecast monthly usage with its band" x={Array.from({ length: n }, (_, i) => `+${i + 1}`)} xLabel="Month"
            series={[{ name: `Forecast (${res.method})`, values: res.base, color: 'var(--accent)' }]} fmt={(v) => v.toLocaleString('en-IN')} axisFmt={(v) => v.toLocaleString('en-IN', { notation: 'compact' })}
            band={res.low && res.high ? { low: res.low, high: res.high, label: `±${res.band_pct ?? '?'}% band` } : undefined} />
          <p className="caption">{res.note}{res.backtest && <> Methods tried: {res.backtest.scores.map((s) => `${s.method} (MAPE ${s.mape === null ? 'n/a' : `${(s.mape * 100).toFixed(1)}%`}${s.usable ? '' : ', unusable'})`).join(', ')}.</>}</p>
        </>
      )}
    </Card>
  )
}
