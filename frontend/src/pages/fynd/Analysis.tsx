import { Fragment, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../../api/client'
import type { Approval } from '../../api/viewTypes'
import { Card, OutcomePill, PageHeader, Pill } from '../../components/fynd/Page'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatDateTime, formatINR, formatPayback, formatPct } from '../../domain/format'
import { SET_MIN, humanize, outcomeLabel, severityTone } from '../../domain/fynd'
import { useFynd } from '../../state/useFynd'
import { engineNames, useEvaluation } from '../../state/useEvaluation'
import type { Evaluation } from '../../state/useEvaluation'

export default function Analysis() {
  const { proposalId, setProposalId } = useFynd()
  const ev = useEvaluation()
  const names = engineNames(ev.data, ev.evaluable)
  const sel = names.find((n) => n.key === proposalId) ?? names[0] ?? null
  useEffect(() => { if (sel && sel.key !== proposalId) setProposalId(sel.key) }, [sel, proposalId, setProposalId])

  return (
    <>
      <PageHeader title="Decision analysis" subtitle="The full reasoning behind a recommendation: definitions, gates, evidence, risks and what would change the outcome."
        crumbs={[{ to: '/dashboard', label: 'Dashboard' }, { label: 'Decision analysis' }]}
        actions={<>{ev.data && <button className="btn" onClick={() => void ev.run()} disabled={ev.loading}>{ev.loading ? 'Evaluating…' : 'Re-run'}</button>}<Link to="/reports" className="btn btn-primary">Reports</Link></>} />
      {ev.docsError != null && <ErrorNotice title="Proposals could not be loaded." error={ev.docsError} onRetry={() => void ev.reloadDocs()} />}
      {ev.docs !== null && ev.evaluable.length < SET_MIN && <Empty title="Nothing to analyse yet" actions={<Link to="/ai-proposals" className="btn btn-primary">Manage proposals</Link>}>The engine evaluates proposals as a set of 2 to 8: add at least two with an implementation cost and an expected annual benefit ({ev.evaluable.length} now).</Empty>}
      {ev.error != null && <ErrorNotice title="The evaluation did not run." error={ev.error} onRetry={() => void ev.run()} />}
      {ev.loading && !ev.data && <Skeleton rows={4} height={90} />}
      {ev.data && sel && (
        <>
          <div className="toolbar">
            <div className="field" style={{ minWidth: 280 }}>
              <label className="label" htmlFor="an-p">Proposal</label>
              <select id="an-p" className="select" value={sel.key} onChange={(e) => setProposalId(e.target.value)}>{names.map((n) => <option key={n.key} value={n.key}>{n.name}</option>)}</select>
            </div>
            <span className="grow" />
            <p className="trace">Decision snapshot <span className="mono">{ev.data.decision.snapshot.id}</span> · rules v{ev.data.decision.snapshot.rules_version ?? '?'} · {formatDateTime(ev.data.decision.snapshot.created_at)}</p>
          </div>
          <Detail e={ev.data} name={sel.name} id={sel.id} />
        </>
      )}
      <Card title="Related tools" sub="Existing workbench screens that work on the pilot scenario">
        <div className="row">
          <Link className="btn btn-sm" to="/evidence">Pilot evaluation and stop rules</Link>
          <Link className="btn btn-sm" to="/observations">Observations and stop-rule preview</Link>
          <Link className="btn btn-sm" to="/optimize">Portfolio selection and budget check</Link>
          <Link className="btn btn-sm" to="/portfolios">Saved portfolios</Link>
        </div>
      </Card>
    </>
  )
}

function Detail({ e, name, id }: { e: Evaluation; name: string; id: string | null }) {
  const d = e.decision.result.decisions[name]
  const row = e.dashboard.result.proposals.find((p) => p.id === name)
  const h = e.dashboard.result.header
  const risks = e.dashboard.result.risks.filter((r) => r.ids.includes(name))
  if (!d) return <div className="notice notice-warn"><div>No decision for “{name}” in this answer.</div></div>
  const eco = d.economics
  return (
    <>
      <Card className="reco" title={<span className="row" style={{ gap: 10 }}>{name} <OutcomePill outcome={d.outcome} /></span>} sub={`Reason codes: ${d.reason_codes.join(', ') || 'none'}`}>
        <div className="reco-text">{d.reasons.map((r) => <p key={r}>{r}</p>)}</div>
      </Card>

      <div className="grid-2">
        <Card title="Financial results" sub={`Engine figures over a ${h.horizon_months}-month horizon, ${h.mode} mode, discount rate ${h.discount_rate}`}>
          <dl className="dl">
            <dt>Net annual benefit</dt><dd>{formatINR(row?.net_annual_inr ?? null)} /yr</dd>
            <dt>Payback</dt><dd>{formatPayback(row?.payback_months)}</dd>
            <dt>Break-even month</dt><dd>{row?.breakeven_month == null ? 'Not available' : `Month ${row.breakeven_month}`}</dd>
            <dt>NPV (r = {h.discount_rate})</dt><dd>{formatINR(row?.npv_inr ?? null)}</dd>
            <dt>{h.horizon_months}-month ROI</dt><dd>{row?.roi_pct == null ? 'Not available' : formatPct(row.roi_pct, 1)}</dd>
            <dt>Ranking score</dt><dd>{row?.score ?? 'Not available'}{row?.rank != null ? ` (rank ${row.rank})` : ''}</dd>
          </dl>
          <p className="hint">ROI = profit over the horizon ÷ total investment, as the engine defines it (portfolio KPI source: “{e.dashboard.result.kpis.find((k) => k.key === 'roi_pct')?.source ?? 'not stated'}”). It is not capped. ROI and break-even are supporting evidence, not the selector.</p>
        </Card>
        <Card title="Economics" sub="Today's cost, what the AI avoids, and how much of it is cash">
          <dl className="dl">
            <dt>Current annual cost (baseline)</dt><dd>{eco.baseline_cost_inr === null ? 'Not stated' : `${formatINR(eco.baseline_cost_inr)} /yr`}</dd>
            <dt>Gross cost avoidance</dt><dd>{formatINR(eco.gross_cost_avoidance_inr)} /yr</dd>
            <dt>Cashable savings</dt><dd>{formatINR(eco.cashable_savings_inr)} /yr</dd>
            <dt>Capacity value (not cash)</dt><dd>{formatINR(eco.capacity_value_inr)} /yr</dd>
            <dt>Retained human cost</dt><dd>{eco.retained_human_cost_inr === null ? 'Unknown (no baseline)' : `${formatINR(eco.retained_human_cost_inr)} /yr`}</dd>
            <dt>Net cashable after AI cost</dt><dd>{formatINR(eco.net_cashable_inr)} /yr</dd>
            <dt>Cashable share</dt><dd>{formatPct(eco.cashable_share_pct)}</dd>
          </dl>
          <p className="hint">{eco.gross_note ? `Gross: ${eco.gross_note}. ` : ''}{eco.cashable_share_source ? `Cash share: ${eco.cashable_share_source}. ` : ''}{eco.competitiveness ? `Competitiveness: ${eco.competitiveness.class} (${eco.competitiveness.basis}).` : ''}</p>
        </Card>
      </div>

      <div className="grid-2">
        <Card title="Eligibility gates">
          <div className="tbl-wrap"><table className="tbl"><caption className="sr">Gates</caption>
            <thead><tr><th scope="col">Gate</th><th scope="col">Status</th><th scope="col">Detail</th></tr></thead>
            <tbody>{d.eligibility.gates.map((g) => (
              <tr key={g.gate}><td className="strong">{humanize(g.gate.toLowerCase())}</td><td><Pill tone={g.status === 'pass' ? 'ok' : g.status === 'fail' ? 'bad' : 'off'}>{humanize(g.status)}</Pill></td><td className="caption">{g.detail}</td></tr>
            ))}</tbody></table></div>
        </Card>
        <Card title="Evidence and data readiness">
          <p><Pill tone={d.evidence.band === 'high' ? 'ok' : d.evidence.band === 'medium' ? 'info' : 'warn'}>Evidence {d.evidence.band}</Pill>{' '}
            <span className="caption">{d.evidence.observed_weight} of {d.evidence.total_weight} decision weight observed{d.evidence.band_capped_by_fixture ? ' · capped because inputs come from the fixture' : ''}</span></p>
          <ul className="caption">{d.data_readiness.checks.map((c) => <li key={c.check}>{c.ok ? '✓' : '✗'} {c.check}</li>)}</ul>
          <p className="caption">Readiness: {d.data_readiness.level} ({Math.round(d.data_readiness.share * 100)}% of checks).</p>
        </Card>
      </div>

      <Card title="Risk assessment" sub="Engine risks for this proposal and the inputs that most need evidence" id="risks">
        {risks.length > 0 && <ul>{risks.map((r) => (
          <li key={r.code} className="risk"><Pill tone={severityTone(r.severity)}>{humanize(r.severity)}</Pill><span className="risk-title">{humanize(r.code)}</span><span className="risk-body">{r.message}</span></li>
        ))}</ul>}
        <div className="tbl-wrap"><table className="tbl"><caption className="sr">Evidence gaps</caption>
          <thead><tr><th scope="col">Input</th><th scope="col">Impact</th><th scope="col">Why it matters</th></tr></thead>
          <tbody>{d.gaps.map((g) => <tr key={g.input}><td className="mono">{g.input}</td><td><Pill tone={severityTone(g.impact)}>{humanize(g.impact)}</Pill></td><td className="wrap-cell">{g.why}</td></tr>)}</tbody>
        </table></div>
      </Card>

      <div className="grid-2">
        <Card title="What would change the outcome">
          {d.what_would_reverse.length === 0 ? <p className="caption">The engine found no single input change that reverses the outcome.</p> : (
            <ul className="stack" style={{ gap: 8 }}>{d.what_would_reverse.map((w) => <li key={w.field} className="caption"><strong>{outcomeLabel(w.new_outcome)}</strong>: {w.statement}</li>)}</ul>
          )}
        </Card>
        <Card title="Budget check" sub={eco.budget ? `Status: ${eco.budget.status} · contingency ${eco.budget.contingency_pct ?? 'n/a'}%` : undefined}>
          {!eco.budget ? <p className="caption">Not available.</p> : (
            <dl className="dl">{eco.budget.lines.map((l) => <Fragment key={l.line}><dt>{l.note}</dt><dd>{formatINR(l.need_inr)} need · {l.budget_inr === null ? 'no budget set' : `${formatINR(l.budget_inr)} budget`} · {l.status}</dd></Fragment>)}</dl>
          )}
          <p className="hint">Set budgets in <Link to="/settings">Settings</Link>.</p>
        </Card>
      </div>

      <Card title="Ranking across proposals" sub={`Weights: ${Object.entries(e.decision.result.weights).map(([k, v]) => `${humanize(k)} ${v}`).join(', ')}`}>
        <div className="tbl-wrap"><table className="tbl"><caption className="sr">Ranking</caption>
          <thead><tr><th scope="col" className="num">Rank</th><th scope="col">Proposal</th><th scope="col">Outcome</th><th scope="col" className="num">Score</th><th scope="col">Eligible</th></tr></thead>
          <tbody>{e.decision.result.ranking.map((r) => (
            <tr key={r.id} className={r.id === name ? 'selected' : ''}><td className="num">{r.rank ?? '—'}</td><td className="strong">{r.id}</td><td><OutcomePill outcome={r.outcome} /></td><td className="num">{r.score ?? 'Not available'}</td><td>{r.eligible ? 'Yes' : 'No'}</td></tr>
          ))}</tbody></table></div>
        {e.decision.result.why_a_over_b.length > 0 && <ul className="caption">{e.decision.result.why_a_over_b.map((w) => <li key={w.over + w.under}><strong>{w.over}</strong> over <strong>{w.under}</strong>: {w.reasons.join('; ')}</li>)}</ul>}
      </Card>

      {id && <Approvals proposalId={id} snapshotId={e.decision.snapshot.id} outcome={d.outcome} />}
    </>
  )
}

function Approvals({ proposalId, snapshotId, outcome }: { proposalId: string; snapshotId: string; outcome: string }) {
  const [list, setList] = useState<Approval[] | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [decision, setDecision] = useState<Approval['decision']>('approved')
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [saveErr, setSaveErr] = useState<unknown>(null)
  const load = () => { setErr(null); api.listApprovals(proposalId).then(setList).catch(setErr) }
  useEffect(load, [proposalId])
  const unavailable = err instanceof ApiError && err.code === 'not_configured'
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true); setSaveErr(null); setMsg(null)
    try { await api.addApproval(proposalId, { decision, comment: comment.trim() || null, snapshot_id: snapshotId }); setComment(''); setMsg('Decision recorded. The service confirmed it.'); load() } catch (x) { setSaveErr(x) } finally { setBusy(false) }
  }
  return (
    <Card title="Approval record" sub="A human decision on this proposal's current inputs version, linked to the decision snapshot shown above">
      {unavailable && <p className="caption">Approvals are not available: workspace features are not configured on this gateway.</p>}
      {err != null && !unavailable && <ErrorNotice title="Approvals could not be loaded." error={err} onRetry={load} />}
      {list && list.length === 0 && <p className="caption">No approval decision has been recorded yet.</p>}
      {list && list.length > 0 && (
        <div className="tbl-wrap"><table className="tbl"><caption className="sr">Approvals</caption>
          <thead><tr><th scope="col">When</th><th scope="col">Decision</th><th scope="col">Engine outcome then</th><th scope="col">Inputs version</th><th scope="col">Comment</th></tr></thead>
          <tbody>{list.map((a) => (
            <tr key={a.id}><td className="caption">{formatDateTime(a.created_at)}</td><td><Pill tone={a.decision === 'approved' ? 'ok' : a.decision === 'rejected' ? 'bad' : 'warn'}>{humanize(a.decision)}</Pill></td>
              <td>{a.engine_outcome ? outcomeLabel(a.engine_outcome) : <span className="muted">Not recorded</span>}</td>
              <td>v{a.proposal_version}{a.stale && <> <Pill tone="warn">stale</Pill></>}</td><td className="wrap-cell">{a.comment ?? <span className="muted">None</span>}</td></tr>
          ))}</tbody></table></div>
      )}
      {!unavailable && (
        <form className="toolbar" onSubmit={save}>
          <div className="field"><label className="label" htmlFor="ap-d">Decision</label>
            <select id="ap-d" className="select" value={decision} onChange={(e) => setDecision(e.target.value as Approval['decision'])}>
              <option value="approved">Approve</option><option value="changes_requested">Request changes</option><option value="rejected">Reject</option>
            </select></div>
          <div className="field grow"><label className="label" htmlFor="ap-c">Comment</label><input id="ap-c" className="input" maxLength={2000} value={comment} onChange={(e) => setComment(e.target.value)} /></div>
          <button className="btn btn-primary" disabled={busy}>{busy ? 'Recording…' : 'Record decision'}</button>
        </form>
      )}
      <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
      {saveErr != null && <ErrorNotice title="The decision was not recorded." error={saveErr} />}
      <p className="hint">The engine currently says: {outcomeLabel(outcome)}. Recording needs the admin role; a later edit to the proposal marks this approval stale.</p>
    </Card>
  )
}
