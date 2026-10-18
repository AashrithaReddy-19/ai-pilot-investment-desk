import { useCallback, useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { AuditEvent, EvaluationSummary, HistoryEvent } from '../../api/types'
import { formatDateTime, formatINR, formatPct } from '../../domain/format'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'

/** One timeline row in plain words. Pure, so it is unit-tested without a browser. */
export function describeEvent(e: HistoryEvent): { what: string; detail: string } {
  if (e.kind === 'decision') {
    const was = e.previous_verdict ? ` (was ${e.previous_verdict})` : ''
    const measured = [
      e.observed_adoption_pct != null ? `adoption ${formatPct(e.observed_adoption_pct, 1)}` : null,
      e.observed_quality_pct != null ? `quality ${formatPct(e.observed_quality_pct, 1)}` : null,
    ].filter(Boolean).join(', ')
    return {
      what: `Stop rule ${e.verdict ?? 'not recorded'} for ${e.pilot_id ?? 'an unnamed pilot'}${was}`,
      detail: [measured, e.reason ?? ''].filter((s) => s !== '').join(' · ') || 'No measurement recorded',
    }
  }
  const funded = e.selected && e.selected.length > 0 ? `funded ${e.selected.join(', ')}` : 'nothing funded'
  const budget = e.budget != null ? `budget ${formatINR(e.budget)}` : 'budget not stated'
  return {
    what: `Evaluation${e.mode ? ` (${e.mode})` : ''}`,
    detail: e.change_summary && e.change_summary.trim() !== '' ? e.change_summary : `${funded}; ${budget}`,
  }
}

/** Audit details as short "key: value" pairs; identifiers and statuses only. */
export function describeDetails(d: Record<string, unknown> | null | undefined): string {
  if (!d) return ''
  return Object.entries(d)
    .filter(([, v]) => v !== null && v !== undefined && !(Array.isArray(v) && v.length === 0))
    .map(([k, v]) => `${k.replace(/_/g, ' ')}: ${Array.isArray(v) ? v.join(', ') : String(v)}`)
    .join(' · ')
}

function useLoaded<T>(load: () => Promise<T>) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<unknown>(null)
  const refresh = useCallback(async () => {
    try { setData(await load()); setError(null) } catch (e) { setError(e) }
  }, [load])
  useEffect(() => { void refresh() }, [refresh])
  return { data, error, refresh }
}

const loadTimeline = async () => (await api.timeline(50)).data.events ?? []
const loadEvaluations = () => api.listEvaluations(50)
const loadAudit = () => api.listAudit({ limit: 50 })

export function History() {
  const tl = useLoaded<HistoryEvent[]>(loadTimeline)
  const ev = useLoaded<EvaluationSummary[]>(loadEvaluations)
  const au = useLoaded<AuditEvent[]>(loadAudit)

  return (
    <div className="stack" style={{ gap: 36 }}>
      <section className="stack" aria-labelledby="h-timeline">
        <h2 className="h3" id="h-timeline">Timeline</h2>
        <p className="hint">Evaluations and stop-rule decisions together, newest first. Each evaluation says what changed against the one before it.</p>
        {tl.error != null && <ErrorNotice title="The timeline could not be loaded." error={tl.error} onRetry={() => void tl.refresh()} />}
        {tl.error == null && tl.data === null && <Skeleton rows={3} height={44} />}
        {tl.data?.length === 0 && <Empty title="Nothing has happened yet">Evaluations and recorded measurements appear here.</Empty>}
        {tl.data && tl.data.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Timeline of evaluations and stop-rule decisions</caption>
            <thead><tr><th scope="col">When</th><th scope="col">Event</th><th scope="col">Detail</th></tr></thead>
            <tbody>{tl.data.map((e) => {
              const d = describeEvent(e)
              return (
                <tr key={`${e.kind}-${e.id}`}>
                  <td>{formatDateTime(e.at)}</td>
                  <td className="strong">{d.what}</td>
                  <td className="wrap-cell">{d.detail}</td>
                </tr>)
            })}</tbody>
          </table></div>
        )}
      </section>

      <section className="stack" aria-labelledby="h-evals">
        <h2 className="h3" id="h-evals">Evaluations log</h2>
        <p className="hint">Every evaluation is stored with its request and both answers. When it was run for a proposal, the proposal’s inputs version is recorded with it.</p>
        {ev.error != null && <ErrorNotice title="The evaluations log could not be loaded." error={ev.error} onRetry={() => void ev.refresh()} />}
        {ev.error == null && ev.data === null && <Skeleton rows={3} height={44} />}
        {ev.data?.length === 0 && <Empty title="No evaluations stored">Evaluating a scenario in the decision workspace stores one here.</Empty>}
        {ev.data && ev.data.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Stored evaluations</caption>
            <thead><tr><th className="num" scope="col">Id</th><th scope="col">When</th><th scope="col">Proposal</th><th scope="col">Saved portfolio</th></tr></thead>
            <tbody>{ev.data.map((e) => (
              <tr key={e.id}>
                <td className="num mono">{e.id}</td>
                <td>{formatDateTime(e.created_at)}</td>
                <td>{e.proposal_id
                  ? <><span className="mono">{e.proposal_id.slice(0, 8)}</span>{e.proposal_version != null && <> · inputs v{e.proposal_version}</>}</>
                  : e.proposal_version != null
                    ? <span className="muted">Proposal deleted · inputs v{e.proposal_version}</span>
                    : <span className="muted">Not tied to a proposal</span>}</td>
                <td>{e.portfolio_id ? <span className="mono">{e.portfolio_id.slice(0, 8)}</span> : <span className="muted">Not saved</span>}</td>
              </tr>))}</tbody>
          </table></div>
        )}
      </section>

      <section className="stack" aria-labelledby="h-audit">
        <h2 className="h3" id="h-audit">Audit trail</h2>
        <p className="hint">Every successful change and download, newest first. It records identifiers and statuses, never document contents or interview quotes. The person column fills in once sign-in exists.</p>
        {au.error != null && <ErrorNotice title="The audit trail could not be loaded." error={au.error} onRetry={() => void au.refresh()} />}
        {au.error == null && au.data === null && <Skeleton rows={3} height={44} />}
        {au.data?.length === 0 && <Empty title="No changes recorded yet">Saving, editing, deleting or downloading something records it here.</Empty>}
        {au.data && au.data.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Audit trail of changes and downloads</caption>
            <thead><tr><th scope="col">When</th><th scope="col">Action</th><th scope="col">Record</th><th scope="col">Details</th><th scope="col">Person</th><th scope="col">Request id</th></tr></thead>
            <tbody>{au.data.map((a) => (
              <tr key={a.id}>
                <td>{formatDateTime(a.created_at)}</td>
                <td className="mono">{a.action}</td>
                <td>{a.entity_type}{a.entity_id && <> <span className="mono">{a.entity_id.length > 12 ? a.entity_id.slice(0, 8) : a.entity_id}</span></>}</td>
                <td className="wrap-cell">{describeDetails(a.details) || <span className="muted">None</span>}</td>
                <td>{a.actor ?? <span className="muted">Not signed in</span>}</td>
                <td className="mono">{a.request_id ?? <span className="muted">None</span>}</td>
              </tr>))}</tbody>
          </table></div>
        )}
      </section>
    </div>
  )
}
