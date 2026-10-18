import { Fragment, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { api, saveDownload } from '../../api/client'
import type { CompareResponse, Mode } from '../../api/types'
import type { ViewRequest, WorkspaceView, YearlyView } from '../../api/viewTypes'
import { Card, PageHeader, Pill } from '../../components/fynd/Page'
import { GroupedBars, SERIES_COLORS } from '../../components/fynd/Charts'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatDateTime, formatINR, formatINRCompact, formatPayback, formatPct } from '../../domain/format'
import { isEvaluable } from '../../domain/fynd'
import { useFynd, useProposalDocs } from '../../state/useFynd'

type Metric = 'net_inr' | 'benefit_inr' | 'running_cost_inr' | 'one_time_inr'
const METRICS: { key: Metric; label: string; note: string }[] = [
  { key: 'net_inr', label: 'Net cash flow', note: 'benefit − running cost − one-time cost' },
  { key: 'benefit_inr', label: 'Benefit (gross)', note: 'gross benefit in the year, after ramp-up and delay' },
  { key: 'running_cost_inr', label: 'AI running cost', note: 'recurring cost in the year' },
  { key: 'one_time_inr', label: 'One-time cost', note: 'implementation cost in the year it is spent' },
]

interface Result { cmp: WorkspaceView<CompareResponse>; yearly: WorkspaceView<YearlyView>; request: ViewRequest }

export default function Comparison() {
  const navigate = useNavigate()
  const location = useLocation() as { state?: { ids?: string[] } }
  const setProposalId = useFynd((s) => s.setProposalId)
  const { docs, error: docsError, reload } = useProposalDocs(false)
  const evaluable = useMemo(() => (docs ?? []).filter((p) => p.status === 'active' && isEvaluable(p.inputs)), [docs])
  const [picked, setPicked] = useState<Set<string> | null>(null)
  const [mode, setMode] = useState<Mode>('independent')
  const [metric, setMetric] = useState<Metric>('net_inr')
  const [res, setRes] = useState<Result | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [dlErr, setDlErr] = useState<unknown>(null)

  // start from what another screen asked for, topped up with the other evaluable proposals (max 8)
  useEffect(() => {
    if (picked !== null || docs === null) return
    const asked = (location.state?.ids ?? []).filter((id) => evaluable.some((p) => p.id === id))
    const rest = evaluable.map((p) => p.id).filter((id) => !asked.includes(id))
    setPicked(new Set([...asked, ...rest].slice(0, Math.max(3, asked.length)).slice(0, 8)))
  }, [docs, evaluable, picked, location.state])

  const ids = useMemo(() => evaluable.filter((p) => picked?.has(p.id)).map((p) => p.id), [evaluable, picked])
  const request: ViewRequest = useMemo(() => ({ proposal_ids: ids, mode }), [ids, mode])
  const key = JSON.stringify(request)
  const stale = res !== null && JSON.stringify(res.request) !== key

  const run = async () => {
    if (ids.length < 2) return
    setBusy(true); setErr(null)
    try {
      const [cmp, yearly] = await Promise.all([api.workspaceComparison(request), api.workspaceYearly(request)])
      setRes({ cmp: cmp.data, yearly: yearly.data, request })
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }
  // run once the first selection is known; later changes wait for the button so a half-made selection is not run
  useEffect(() => { if (res === null && ids.length >= 2 && !busy && err === null) void run() }, [ids.length]) // only the first time a valid selection exists

  const exportCsv = async () => {
    if (!res) return
    setDlErr(null)
    try { saveDownload(await api.workspaceYearlyCsv(res.request)) } catch (e) { setDlErr(e) }
  }

  const y = res?.yearly.result
  const c = res?.cmp.result
  const order = c?.proposals.map((p) => p.id) ?? []
  const years = y?.proposals[0]?.years.map((r) => r.year) ?? []
  const series = (y?.proposals ?? []).slice().sort((a, b) => order.indexOf(a.id) - order.indexOf(b.id))
    .map((p, i) => ({ name: p.id, values: p.years.map((r) => r[metric] ?? null), color: SERIES_COLORS[i % SERIES_COLORS.length] }))
  const m = METRICS.find((x) => x.key === metric)!
  const idByName = (name: string) => res?.cmp.snapshot.proposals.find((p) => p.name === name)?.id ?? null

  return (
    <>
      <PageHeader title="Proposal comparison" subtitle="Compare proposals side by side on the same horizon, mode and rules."
        actions={<>
          <button className="btn" onClick={() => void exportCsv()} disabled={!res || stale}>Export yearly CSV</button>
          <Link to="/scenarios" className="btn">Scenario analysis</Link>
        </>} />
      {docsError != null && <ErrorNotice title="Proposals could not be loaded." error={docsError} onRetry={() => void reload()} />}
      {docs === null && docsError == null && <Skeleton rows={2} height={60} />}
      {docs && evaluable.length < 2 && (
        <Empty title="At least two evaluable proposals are needed" actions={<Link to="/ai-proposals" className="btn btn-primary">Manage proposals</Link>}>
          The comparison runs on stored proposals that have an implementation cost and an expected annual benefit ({evaluable.length} now).
        </Empty>
      )}

      {docs && evaluable.length >= 2 && (
        <Card title="Comparison context" sub="Choose 2 to 8 proposals. The engine compares them on one horizon with the workspace settings.">
          <fieldset>
            <legend className="sr">Proposals to compare</legend>
            <div className="row" style={{ gap: '8px 20px' }}>
              {evaluable.map((p) => (
                <label key={p.id} className="row" style={{ gap: 8, flexWrap: 'nowrap' }}>
                  <input type="checkbox" checked={picked?.has(p.id) ?? false} disabled={!picked?.has(p.id) && (picked?.size ?? 0) >= 8}
                    onChange={(e) => setPicked((s) => { const n = new Set(s ?? []); if (e.target.checked) n.add(p.id); else n.delete(p.id); return n })} />
                  {p.name}
                </label>
              ))}
            </div>
          </fieldset>
          <div className="toolbar">
            <div className="field"><span className="label" id="cm-mode">Selection mode</span>
              <div className="segmented" role="group" aria-labelledby="cm-mode">
                {(['independent', 'conservative'] as Mode[]).map((x) => <button key={x} type="button" aria-pressed={mode === x} onClick={() => setMode(x)}>{x === 'independent' ? 'Independent' : 'Conservative'}</button>)}
              </div>
            </div>
            <button className="btn btn-primary" onClick={() => void run()} disabled={busy || ids.length < 2}>{busy ? 'Comparing…' : res ? 'Update comparison' : 'Compare'}</button>
            {ids.length < 2 && <span className="hint">Select at least two.</span>}
            {stale && <Pill tone="warn">Selection changed: results below are for the previous selection</Pill>}
          </div>
        </Card>
      )}

      {err != null && <ErrorNotice title="The comparison did not run." error={err} onRetry={() => void run()} />}
      {dlErr != null && <ErrorNotice title="The export did not download." error={dlErr} />}
      {busy && !res && <Skeleton rows={3} height={90} />}

      {res && c && y && (
        <>
          <Card title={`${m.label} by year`} sub={`${m.note}. ${y.horizon_months}-month horizon, ${y.mode} mode, INR. Bars start at zero.`}
            actions={<select className="select" aria-label="Chart metric" value={metric} onChange={(e) => setMetric(e.target.value as Metric)} style={{ minHeight: 32 }}>
              {METRICS.map((x) => <option key={x.key} value={x.key}>{x.label}</option>)}
            </select>}>
            <GroupedBars caption={`${m.label} by year for the compared proposals`} x={years} xLabel="year" series={series} fmt={formatINR} axisFmt={formatINRCompact} />
            <p className="hint">The current process cost is not drawn here: the yearly view does not return a per-year baseline, so it would have to be invented. Each proposal's stated annual baseline is on Decision Analysis.</p>
          </Card>

          <Card title="Detailed comparison" sub={c.recommendation.text}>
            <div className="tbl-wrap"><table className="tbl metric-tbl">
              <caption className="sr">Comparison metrics per proposal</caption>
              <thead><tr><th scope="col" className="sticky">Metric</th>{c.proposals.map((p) => <th key={p.id} scope="col" className="num">{p.name}</th>)}</tr></thead>
              <tbody>
                <Group label="Costs" span={c.proposals.length} />
                <MetricRow label="Implementation (one-time)" values={c.proposals.map((p) => formatINR(p.setup_cost_inr))} />
                <MetricRow label="AI running cost (recurring, /yr)" values={c.proposals.map((p) => formatINR(p.ai_running_cost_inr))} />
                <MetricRow label="Year-1 net cash flow" values={c.proposals.map((p) => formatINR(y.proposals.find((q) => q.id === p.id)?.years[0]?.net_inr ?? null))} />
                <Group label="Benefit" span={c.proposals.length} />
                <MetricRow label="Annual benefit (gross cost avoidance)" values={c.proposals.map((p) => formatINR(p.annual_benefit_inr))} />
                <MetricRow label="of which cash" values={c.proposals.map((p) => `${formatINR(p.cash_vs_capacity.cash_benefit_inr)} (${formatPct(p.cash_vs_capacity.cash_share_pct)})`)} />
                <MetricRow label="of which capacity (not cash)" values={c.proposals.map((p) => formatINR(p.cash_vs_capacity.capacity_benefit_inr))} />
                <MetricRow label="Net annual benefit" values={c.proposals.map((p) => formatINR(p.net_annual_inr))} strong />
                <Group label={`Over the ${y.horizon_months}-month horizon`} span={c.proposals.length} />
                <MetricRow label="Profit" values={c.proposals.map((p) => formatINR(p.profit_inr))} />
                <MetricRow label="NPV" values={c.proposals.map((p) => formatINR(p.npv_inr))} />
                <MetricRow label="ROI (profit ÷ investment)" values={c.proposals.map((p) => formatPct(p.roi_pct, 1))} />
                <MetricRow label="Payback" values={c.proposals.map((p) => formatPayback(p.payback_months))} />
                <MetricRow label="Break-even month" values={c.proposals.map((p) => (p.breakeven_month === null ? (p.breakeven_beyond_horizon ? 'Beyond horizon' : 'Not available') : `Month ${p.breakeven_month}`))} />
                <Group label="Decision" span={c.proposals.length} />
                <MetricRow label="Rank" values={c.proposals.map((p) => (p.rank === null ? 'Not ranked' : `#${p.rank}`))} />
                <tr><th scope="row" className="sticky">Status</th>{c.proposals.map((p) => <td key={p.id} className="num"><Pill tone={p.status === 'recommended' ? 'ok' : p.status === 'dropped' ? 'bad' : 'off'}>{p.status}</Pill>{p.reason && <span className="cell-sub" style={{ whiteSpace: 'normal' }}>{p.reason}</span>}</td>)}</tr>
                <tr><th scope="row" className="sticky">Flags</th>{c.proposals.map((p) => <td key={p.id} className="caption" style={{ whiteSpace: 'normal', minWidth: 160, maxWidth: 260, textAlign: 'right' }}>{p.flags.length === 0 ? 'None' : p.flags.map((f) => f.message).join(' ')}</td>)}</tr>
                <tr><th scope="row" className="sticky"><span className="sr">Actions</span></th>{c.proposals.map((p) => {
                  const id = idByName(p.name)
                  return <td key={p.id} className="num"><button className="btn btn-sm" disabled={!id} onClick={() => { if (id) { setProposalId(id); navigate('/analysis') } }}>Detailed analysis</button></td>
                })}</tr>
              </tbody>
            </table></div>
            <p className="caption">{c.selection.message} {c.selection.budget !== null && <>Budget {formatINR(c.selection.budget)}, left {formatINR(c.selection.budget_left)}.</>} Selected: {c.selection.selected.join(', ') || 'none'}.</p>
            {(c.head_to_head ?? []).length > 0 && (
              <details className="more"><summary>Why one proposal ranks above another</summary>
                <ul className="more-body">{(c.head_to_head ?? []).map((h) => <li key={h.over + h.under}><strong>{h.over}</strong> over <strong>{h.under}</strong>: {h.reasons.join('; ')}</li>)}</ul>
              </details>
            )}
            <p className="trace">Assumptions: {String(c.recommendation.assumptions.horizon_months ?? 'n/a')} months, r = {String(c.recommendation.assumptions.discount_rate ?? 'n/a')}, {String(c.recommendation.assumptions.mode ?? mode)} mode, selector “{String(c.recommendation.assumptions.selector ?? 'not stated')}”. Yearly totals reconcile with profit: {y.proposals.every((p) => p.matches_profit) ? 'yes' : 'no'}.
              {' '}Snapshots <span className="mono">{res.cmp.snapshot.id.slice(0, 8)}</span> and <span className="mono">{res.yearly.snapshot.id.slice(0, 8)}</span> · {formatDateTime(res.cmp.snapshot.created_at)}</p>
          </Card>
        </>
      )}
    </>
  )
}

function Group({ label, span }: { label: string; span: number }) {
  return <tr><th scope="rowgroup" colSpan={span + 1} className="caption" style={{ background: 'var(--surface-sunken)', fontWeight: 650 }}>{label}</th></tr>
}

function MetricRow({ label, values, strong }: { label: string; values: string[]; strong?: boolean }) {
  return (
    <tr>
      <th scope="row" className="sticky" style={{ fontWeight: strong ? 650 : 500 }}>{label}</th>
      {values.map((v, i) => <Fragment key={i}><td className={`num${strong ? ' strong' : ''}`}>{v}</td></Fragment>)}
    </tr>
  )
}
