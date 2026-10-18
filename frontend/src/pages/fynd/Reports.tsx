import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, saveDownload } from '../../api/client'
import type { Download } from '../../api/client'
import type { Snapshot, SnapshotDetail, ReplayResult } from '../../api/viewTypes'
import { Card, PageHeader, Pill } from '../../components/fynd/Page'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { History } from '../../features/history/History'
import { formatDateTime } from '../../domain/format'
import { SET_MIN, evaluationSet, humanize } from '../../domain/fynd'
import { useProposalDocs } from '../../state/useFynd'

/** Reports are the engine's own exports for the stored proposals; the browser only previews and saves them. */
export default function Reports() {
  const { docs } = useProposalDocs(false)
  const ids = useMemo(() => evaluationSet(docs ?? []).set.map((p) => p.id), [docs])
  const [preview, setPreview] = useState<{ name: string; text: string; requestId?: string } | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [err, setErr] = useState<unknown>(null)

  const get = async (kind: 'report' | 'csv', show: boolean) => {
    setBusy(kind); setErr(null)
    try {
      const d: Download = kind === 'report' ? await api.workspaceReport({ proposal_ids: ids }) : await api.workspaceYearlyCsv({ proposal_ids: ids })
      if (show) setPreview({ name: d.filename, text: await d.blob.text(), requestId: d.requestId })
      else saveDownload(d)
    } catch (e) { setErr(e) } finally { setBusy(null) }
  }

  return (
    <>
      <PageHeader title="Reports" subtitle="Engine-generated reports for the stored proposals, the snapshots behind every view, and the audit trail." />
      {docs !== null && ids.length < SET_MIN && <Empty title="Not enough evaluable proposals" actions={<Link to="/ai-proposals" className="btn btn-primary">Manage proposals</Link>}>Reports are built by the engine from a set of 2 to 8 active proposals that have an implementation cost and an expected annual benefit ({ids.length} now).</Empty>}
      {ids.length >= SET_MIN && (
        <div className="grid-2">
          <Card title="Decision report" sub={`Markdown, built by the engine for ${ids.length} active proposal${ids.length === 1 ? '' : 's'} with the workspace settings`}>
            <div className="row">
              <button className="btn btn-primary" disabled={busy !== null} onClick={() => void get('report', false)}>{busy === 'report' ? 'Preparing…' : 'Download decision-report.md'}</button>
              <button className="btn" disabled={busy !== null} onClick={() => void get('report', true)}>Preview</button>
            </div>
          </Card>
          <Card title="Yearly savings" sub="CSV of the yearly view: benefit, running cost, one-time cost, net and cumulative per proposal">
            <div className="row">
              <button className="btn btn-primary" disabled={busy !== null} onClick={() => void get('csv', false)}>{busy === 'csv' ? 'Preparing…' : 'Download yearly-savings.csv'}</button>
              <button className="btn" disabled={busy !== null} onClick={() => void get('csv', true)}>Preview</button>
            </div>
          </Card>
        </div>
      )}
      {err != null && <ErrorNotice title="The report could not be generated." error={err} />}
      {preview && (
        <Card title={`Preview: ${preview.name}`} sub={preview.requestId ? `Request ${preview.requestId}` : undefined} actions={<button className="btn btn-quiet btn-sm" onClick={() => setPreview(null)}>Close</button>}>
          <pre className="mono" style={{ whiteSpace: 'pre-wrap', maxHeight: 480, overflow: 'auto', background: 'var(--surface-sunken)', padding: 16, borderRadius: 8, margin: 0 }}>{preview.text}</pre>
        </Card>
      )}
      <Snapshots />
      <Card title="Decision history and audit trail" sub="Evaluations, decisions, edits and downloads recorded by the gateway">
        <History />
      </Card>
      <Card title="Other exports" sub="Existing workbench exports for the pilot scenario">
        <div className="row">
          <Link className="btn btn-sm" to="/evidence">Evidence dossier and pilot CSV</Link>
          <Link className="btn btn-sm" to="/memo">Decision memo</Link>
          <Link className="btn btn-sm" to="/portfolios">Saved portfolios</Link>
        </div>
      </Card>
    </>
  )
}

function Snapshots() {
  const [list, setList] = useState<Snapshot[] | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [detail, setDetail] = useState<Record<string, SnapshotDetail | ReplayResult | { error: unknown }>>({})
  const load = () => { setErr(null); api.listSnapshots(undefined, 20).then(setList).catch(setErr) }
  useEffect(load, [])
  const check = async (id: string) => {
    try { const d = (await api.getSnapshot(id)).data; setDetail((x) => ({ ...x, [id]: d })) } catch (e) { setDetail((x) => ({ ...x, [id]: { error: e } })) }
  }
  const replay = async (id: string) => {
    try { const r = (await api.replaySnapshot(id)).data; setDetail((x) => ({ ...x, [id]: r })) } catch (e) { setDetail((x) => ({ ...x, [id]: { error: e } })) }
  }
  return (
    <Card title="Snapshots" sub="Every dashboard, decision, comparison and yearly view is stored with its exact request and engine version">
      {err != null && <ErrorNotice title="Snapshots could not be listed." error={err} onRetry={load} />}
      {err == null && list === null && <Skeleton rows={3} height={36} />}
      {list?.length === 0 && <p className="caption">No snapshot yet. Running the dashboard or a comparison creates one.</p>}
      {list && list.length > 0 && (
        <div className="tbl-wrap"><table className="tbl"><caption className="sr">Snapshots</caption>
          <thead><tr><th scope="col">When</th><th scope="col">View</th><th scope="col">Proposals (inputs version)</th><th scope="col">Rules</th><th scope="col">Check</th></tr></thead>
          <tbody>{list.map((s) => {
            const d = detail[s.id]
            return (
              <tr key={s.id}>
                <td className="caption">{formatDateTime(s.created_at)}<span className="cell-sub mono">{s.id.slice(0, 8)}</span></td>
                <td>{humanize(s.kind)}</td>
                <td className="wrap-cell">{s.proposals.length === 0 ? <span className="muted">No stored proposals</span> : s.proposals.map((p) => `${p.name} (v${p.version})`).join(', ')}</td>
                <td>v{s.rules_version ?? '?'}</td>
                <td style={{ minWidth: 220 }}>
                  <div className="row" style={{ gap: 6 }}>
                    <button className="btn btn-sm" onClick={() => void check(s.id)}>Is it current?</button>
                    <button className="btn btn-sm" onClick={() => void replay(s.id)}>Replay</button>
                  </div>
                  {d && 'error' in d && <span className="cell-sub" style={{ color: 'var(--bad)' }}>The check failed.</span>}
                  {d && 'staleness' in d && (d.staleness.stale ? <span className="cell-sub"><Pill tone="warn">stale</Pill> {d.staleness.reasons.join('; ')}</span> : <span className="cell-sub"><Pill tone="ok">current</Pill></span>)}
                  {d && 'matches' in d && <span className="cell-sub"><Pill tone={d.matches ? 'ok' : 'warn'}>{d.matches ? 'reproduces' : 'differs'}</Pill> {d.note}</span>}
                </td>
              </tr>
            )
          })}</tbody></table></div>
      )}
    </Card>
  )
}
