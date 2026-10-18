import { useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { Card, Kpi, OutcomePill, PageHeader, Pill } from '../../components/fynd/Page'
import { GroupedBars } from '../../components/fynd/Charts'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatDateTime, formatINR, formatINRCompact } from '../../domain/format'
import { SET_MAX, SET_MIN, humanize, outcomeLabel, severityTone } from '../../domain/fynd'
import { useFynd } from '../../state/useFynd'
import { engineNames, useEvaluation } from '../../state/useEvaluation'
import type { Evaluation } from '../../state/useEvaluation'

export default function Dashboard() {
  const navigate = useNavigate()
  const { proposalId, setProposalId, burn } = useFynd()
  const ev = useEvaluation()
  const names = engineNames(ev.data, ev.evaluable)
  const sel = names.find((n) => n.key === proposalId) ?? names[0] ?? null

  // keep the shared selection valid once the evaluation is known
  useEffect(() => {
    if (!sel) return
    if (proposalId !== sel.key) setProposalId(sel.key)
  }, [sel, proposalId, setProposalId])

  const noProposals = ev.docs !== null && ev.evaluable.length < SET_MIN

  return (
    <>
      <PageHeader title="Executive dashboard" subtitle="One service at a time: what it costs today, what it could save, and what the decision rules recommend."
        actions={<>
          {ev.data && <button className="btn" onClick={() => void ev.run()} disabled={ev.loading}>{ev.loading ? 'Evaluating…' : 'Re-run evaluation'}</button>}
          <Link to="/ai-proposals" className="btn btn-primary">Manage proposals</Link>
        </>} />


      {ev.docsError != null && <ErrorNotice title="Proposals could not be loaded." error={ev.docsError} onRetry={() => void ev.reloadDocs()} />}
      {noProposals && (
        <Empty title="Nothing to evaluate yet"
          actions={<Link to="/ai-proposals" className="btn btn-primary">Add or import proposals</Link>}>
          The engine evaluates proposals as a set of 2 to 8, so the dashboard needs at least two active proposals with an implementation cost and an expected annual benefit ({ev.evaluable.length} now).
          {ev.drafts.length > 0 && <> {ev.drafts.length} draft proposal{ev.drafts.length === 1 ? ' is' : 's are'} missing those inputs: {ev.drafts.map((d) => d.name).join(', ')}.</>}
        </Empty>
      )}
      {ev.error != null && <ErrorNotice title="The evaluation did not run." error={ev.error} onRetry={() => void ev.run()} />}
      {ev.loading && !ev.data && <Skeleton rows={3} height={110} />}

      {ev.data && sel && (
        <>
          <div className="toolbar">
            <div className="field" style={{ minWidth: 280 }}>
              <label className="label" htmlFor="svc">Service being evaluated</label>
              <select id="svc" className="select" value={sel.key} onChange={(e) => setProposalId(e.target.value)}>
                {names.map((n) => <option key={n.key} value={n.key}>{n.name}</option>)}
              </select>
            </div>
            {ev.drafts.length > 0 && <span className="hint">{ev.drafts.length} draft proposal{ev.drafts.length === 1 ? '' : 's'} left out (missing cost or benefit).</span>}
            {ev.left.length > 0 && <span className="hint">The engine evaluates at most {SET_MAX} proposals: the {SET_MAX} most recently updated are included, {ev.left.length} left out ({ev.left.map((p) => p.name).join(', ')}).</span>}
            <span className="grow" />
            <Trace e={ev.data} />
          </div>
          <ServiceView e={ev.data} name={sel.name} notes={sel.notes} burnSource={burn?.source ?? null} onAnalysis={() => navigate('/analysis')} onOptions={() => navigate('/comparison')} />
        </>
      )}
    </>
  )
}

function Trace({ e }: { e: Evaluation }) {
  const h = e.dashboard.result.header
  return (
    <p className="trace" style={{ textAlign: 'right' }}>
      Rules v{h.rules_version} · formula {h.formula_version} · {h.mode} mode · {h.horizon_months}-month horizon · r = {h.discount_rate}
      {' · '}budget {h.budget === null ? 'not set' : formatINR(h.budget)}<br />
      Snapshots <span className="mono">{e.decision.snapshot.id.slice(0, 8)}</span> (decision) and <span className="mono">{e.dashboard.snapshot.id.slice(0, 8)}</span> (dashboard) · {formatDateTime(e.dashboard.snapshot.created_at)}
    </p>
  )
}

function ServiceView({ e, name, notes, burnSource, onAnalysis, onOptions }: { e: Evaluation; name: string; notes: string | null; burnSource: string | null; onAnalysis: () => void; onOptions: () => void }) {
  const d = e.decision.result.decisions[name]
  const row = e.dashboard.result.proposals.find((p) => p.id === name)
  const risks = e.dashboard.result.risks.filter((r) => r.ids.includes(name))
  const yearly = e.dashboard.result.charts.yearly_savings
  const series = yearly.series.find((s) => s.id === name)
  if (!d) return <div className="notice notice-warn"><div>The engine answer has no decision for “{name}”. Re-run the evaluation.</div></div>
  const eco = d.economics
  return (
    <>
      <div className="grid-3">
        <Kpi label="Selected service" value={name} foot={notes ?? 'No use-case description stored for this proposal.'} />
        <Kpi label="Current annual cost" value={eco.baseline_cost_inr === null ? null : `${formatINR(eco.baseline_cost_inr)} /yr`}
          reason="No baseline (today's cost of the process) is stated for this proposal. Add it on the proposal to see the retained cost."
          foot={eco.baseline_cost_inr !== null ? `User-entered: ${eco.baseline_note ?? 'stated annual cost'}. Retained human cost ${formatINR(eco.retained_human_cost_inr)} /yr (engine).` : undefined} />
        <Kpi label="Estimated annual savings" value={eco.net_cashable_inr === null ? null : `${formatINR(eco.net_cashable_inr)} /yr`}
          reason="The engine could not derive cashable savings from the stated inputs."
          foot={<>Engine estimate: net cashable savings after AI running cost. Gross cost avoidance {formatINR(eco.gross_cost_avoidance_inr)}, cashable {formatINR(eco.cashable_savings_inr)}, capacity value {formatINR(eco.capacity_value_inr)} /yr.</>} />
      </div>

      <Card className="reco" title={null}>
        <div className="card-head">
          <div className="stack" style={{ gap: 8 }}>
            <span className="kpi-label">Recommendation for {name}</span>
            <h2 className="reco-title">{outcomeLabel(d.outcome)}</h2>
          </div>
          <div className="row" style={{ gap: 8 }}>
            <OutcomePill outcome={d.outcome} />
            <Pill tone={d.evidence.band === 'high' ? 'ok' : d.evidence.band === 'medium' ? 'info' : 'warn'} title={`${d.evidence.observed_weight} of ${d.evidence.total_weight} decision weight observed`}>Evidence: {d.evidence.band}</Pill>
            <Pill tone={d.data_readiness.level === 'ready' ? 'ok' : 'warn'} title={d.data_readiness.missing.length ? `Missing: ${d.data_readiness.missing.join(', ')}` : 'All readiness checks pass'}>Data: {d.data_readiness.level}</Pill>
          </div>
        </div>
        <div className="reco-text">{d.reasons.map((r) => <p key={r}>{r}</p>)}</div>
        {row?.reason && !d.reasons.includes(row.reason) && <p className="caption">{row.reason}</p>}
        {d.data_readiness.missing.length > 0 && <p className="caption">Missing before the recommendation can firm up: {d.data_readiness.missing.join(', ')}.</p>}
        <p className="caption">Across all proposals: {outcomeLabel(e.decision.result.overall.outcome)}{e.decision.result.overall.best ? `, best ${e.decision.result.overall.best}` : ''}. {e.dashboard.result.summary.headline}.</p>
        <div className="row">
          <button className="btn btn-primary" onClick={onAnalysis}>View Detailed Analysis</button>
          <button className="btn" onClick={onOptions}>Explore Proposal Options</button>
        </div>
        <p className="hint">ROI, NPV and payback are on the Decision Analysis screen with their definitions. {burnSource ? `AI burn to date sent to the engine: ${burnSource}.` : 'No AI burn to date was sent (confirm balance-sheet rows on Financial Baseline).'}</p>
      </Card>

      <div className="grid-main">
        <Card title="Risks and considerations" sub="From the engine's assessment of this proposal" actions={<Link to="/analysis" className="btn btn-sm">Detailed risk assessment</Link>}>
          {risks.length === 0 && d.economics.flags.length === 0 && <p className="caption">The engine raised no risk for this proposal.</p>}
          <ul>
            {risks.map((r) => (
              <li key={r.code} className="risk">
                <Pill tone={severityTone(r.severity)}>{humanize(r.severity)}</Pill>
                <span className="risk-title">{humanize(r.code)}</span>
                <span className="risk-body">{r.message} <span className="mono">({r.code})</span></span>
              </li>
            ))}
          </ul>
          {series && (
            <>
              <h3 className="card-title" style={{ marginTop: 8 }}>Yearly net cash flow</h3>
              <GroupedBars caption={`Yearly net cash flow for ${name}`} x={yearly.x} xLabel={yearly.x_label} series={[{ name: `${name} (net, ${yearly.unit})`, values: series.values }]}
                fmt={formatINR} axisFmt={formatINRCompact} height={200} />
              <p className="hint">Net = benefit − running cost − one-time cost, per year, as returned by the engine over its {e.dashboard.result.header.horizon_months}-month horizon.</p>
            </>
          )}
        </Card>
        <Card title="Next steps" sub="Derived by the engine from what is missing or unproven">
          {d.next_actions.length === 0 ? <p className="caption">No next action was returned.</p> : (
            <ol className="steps-list">
              {d.next_actions.map((a) => (
                <li key={a.code}><span><strong>{a.action[0].toUpperCase() + a.action.slice(1)}</strong><span className="cell-sub">Owner: {a.owner_hint} · Done when: {a.expected_test}</span></span></li>
              ))}
            </ol>
          )}
          <p className="hint">No due dates are shown because the workflow does not record them.</p>
        </Card>
      </div>
    </>
  )
}
