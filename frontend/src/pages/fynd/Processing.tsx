import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { api, ApiError, saveDownload } from '../../api/client'
import type { BalanceSheetConfirm, BalanceSheetParse } from '../../api/types'
import type { Upload, UploadDetail } from '../../api/viewTypes'
import { Card, PageHeader, Pill, Timeline } from '../../components/fynd/Page'
import type { StepState } from '../../components/fynd/Page'
import { fmtBytes } from '../../components/fynd/Dropzone'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatDateTime, formatINR } from '../../domain/format'
import { useFynd } from '../../state/useFynd'
import { UploadStatusPill } from './uploadStatus'
import { ProposalExtractionReview } from './ProposalExtractionReview'

export default function Processing() {
  const { id } = useParams()
  return id ? <UploadView id={id} /> : <UploadList />
}

function UploadList() {
  const [list, setList] = useState<Upload[] | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const load = useCallback(() => { setErr(null); api.listUploads(50).then(setList).catch(setErr) }, [])
  useEffect(load, [load])
  return (
    <>
      <PageHeader title="Processing and review" subtitle="Stored documents, their processing state and the rows waiting for your review."
        crumbs={[{ to: '/', label: 'Home' }, { label: 'Processing' }]} actions={<Link to="/" className="btn btn-primary">Upload a document</Link>} />
      {err != null && <ErrorNotice title="Uploads could not be listed." error={err} onRetry={load} />}
      {err == null && list === null && <Skeleton rows={4} height={44} />}
      {list?.length === 0 && <Empty title="No stored documents yet" actions={<Link to="/" className="btn btn-primary">Upload a document</Link>}>Upload a supported document from Home and choose “Store and process”.</Empty>}
      {list && list.length > 0 && (
        <div className="tbl-wrap"><table className="tbl">
          <caption className="sr">Stored uploads</caption>
          <thead><tr><th scope="col">Document</th><th scope="col">Kind</th><th scope="col">Status</th><th scope="col" className="num">Attempts</th><th scope="col">Uploaded</th><th scope="col">Last updated</th></tr></thead>
          <tbody>{list.map((u) => (
            <tr key={u.id}>
              <td className="strong"><Link to={`/processing/${u.id}`}>{u.filename}</Link><span className="cell-sub">{u.format.toUpperCase()} · {fmtBytes(u.size_bytes)}</span></td>
              <td>{u.kind === 'balance_sheet' ? 'Balance sheet' : 'Pilot import'}</td>
              <td><UploadStatusPill status={u.status} /></td>
              <td className="num">{u.attempts} / {u.max_attempts}</td>
              <td className="caption">{formatDateTime(u.created_at)}</td>
              <td className="caption">{formatDateTime(u.updated_at)}</td>
            </tr>))}</tbody>
        </table></div>
      )}
    </>
  )
}

function stagesFor(u: Upload): { title: string; sub?: React.ReactNode; state: StepState; aside?: React.ReactNode }[] {
  const s = u.status
  const parse: StepState = s === 'received' ? 'waiting' : s === 'processing' ? 'active' : s === 'failed' ? 'failed' : 'done'
  return [
    { title: 'Document received', sub: <>Stored with SHA-256 <span className="mono">{u.sha256.slice(0, 12)}…</span> · {fmtBytes(u.size_bytes)}</>, state: 'done', aside: formatDateTime(u.created_at) },
    { title: 'File validation', sub: `The gateway checked the bytes really are ${u.format.toUpperCase()} and within the size limit.`, state: 'done' },
    {
      title: 'Parsing by the rules engine', state: parse,
      sub: s === 'failed'
        ? <>Attempt {u.attempts} of {u.max_attempts} failed{u.last_error ? <>: “{u.last_error}”</> : ''}.{u.attempts < u.max_attempts ? <> Next automatic attempt {formatDateTime(u.next_attempt_at)}.</> : ' No automatic attempts left.'}</>
        : s === 'received' ? 'Queued for the background worker. Progress is not reported in steps, only the state.' : s === 'processing' ? 'The worker is reading the document now.' : 'The engine returned candidate rows.',
    },
    { title: 'Financial information review', sub: 'Choose which rows are AI spend; correct an amount if the extraction was wrong. The original value is always kept.', state: s === 'parsed' ? 'review' : s === 'confirmed' ? 'done' : 'waiting' },
    { title: 'Evaluation readiness', sub: s === 'confirmed' ? 'Confirmed: the engine re-read the stored file with your corrections.' : 'Available after confirmation.', state: s === 'confirmed' ? 'done' : 'waiting' },
  ]
}

function UploadView({ id }: { id: string }) {
  const location = useLocation() as { state?: { duplicate?: boolean } }
  const setBurn = useFynd((s) => s.setBurn)
  const [d, setD] = useState<UploadDetail | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [busy, setBusy] = useState(false)
  const [actionErr, setActionErr] = useState<unknown>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [checked, setChecked] = useState<Set<number>>(new Set())
  const [fix, setFix] = useState<Record<number, { value: string; note: string }>>({})

  const load = useCallback(async () => {
    try { setD((await api.getUpload(id)).data); setErr(null) } catch (e) { setErr(e) }
  }, [id])
  useEffect(() => { void load() }, [load])
  // poll only while the worker has something to do; the state is the only progress the backend reports
  useEffect(() => {
    if (!d || (d.upload.status !== 'received' && d.upload.status !== 'processing')) return
    const t = window.setTimeout(() => { void load() }, 1500)
    return () => window.clearTimeout(t)
  }, [d, load])

  const u = d?.upload
  const parsed = (u?.result ?? null) as BalanceSheetParse | null
  const confirmed = (u?.confirmed ?? null) as BalanceSheetConfirm | null
  const rawByRow = useMemo(() => new Map((parsed?.candidates ?? []).map((c) => [c.row, c])), [parsed])
  const rows = d?.review && d.review.kind === 'balance_sheet' ? d.review.rows : []

  useEffect(() => {
    if (confirmed && typeof confirmed.ai_burn_to_date === 'number' && u) setBurn({ value: confirmed.ai_burn_to_date, source: `confirmed upload “${u.filename}”` })
  }, [confirmed, u, setBurn])

  const retry = async () => {
    setBusy(true); setActionErr(null); setMsg(null)
    try { await api.retryUpload(id); setMsg('Queued again. The service confirmed it.'); await load() } catch (e) { setActionErr(e) } finally { setBusy(false) }
  }
  const correct = async (row: number) => {
    const f = fix[row]
    const n = Number((f?.value ?? '').replace(/[,\s]/g, ''))
    if (!f || f.value.trim() === '' || !Number.isFinite(n) || n < 0) { setActionErr(new ApiError('Enter a non-negative amount in rupees.', { code: 'invalid_value' })); return }
    setBusy(true); setActionErr(null); setMsg(null)
    try {
      await api.correctUpload(id, { row_ref: row, field: 'amount_inr', value: n, note: f.note.trim() || null })
      setMsg(`Row ${row} corrected. The original value is kept beside it.`)
      setFix((x) => { const y = { ...x }; delete y[row]; return y })
      await load()
    } catch (e) { setActionErr(e) } finally { setBusy(false) }
  }
  const confirm = async () => {
    if (checked.size === 0) return
    setBusy(true); setActionErr(null); setMsg(null)
    try { await api.confirmUpload(id, { rows: [...checked].sort((a, b) => a - b) }); setMsg('Confirmed. The engine re-read the stored file with your corrections.'); await load() } catch (e) { setActionErr(e) } finally { setBusy(false) }
  }
  const download = async () => {
    if (!u) return
    try { saveDownload(await api.downloadUpload(id, u.filename)) } catch (e) { setActionErr(e) }
  }

  return (
    <>
      <PageHeader title="Processing and review" subtitle={u ? u.filename : 'Stored document'}
        crumbs={[{ to: '/', label: 'Home' }, { to: '/processing', label: 'Processing' }, { label: u?.filename ?? id }]}
        actions={u && <button className="btn" onClick={() => void download()}>Download original</button>} />
      {location.state?.duplicate && <div className="notice notice-info"><div>This exact file was already stored in this workspace, so the existing upload is shown instead of a second copy.</div></div>}
      {err != null && <ErrorNotice title="The upload could not be loaded." error={err} onRetry={() => void load()} />}
      {err == null && !d && <Skeleton rows={4} height={44} />}
      {u && (
        <div className="grid-main">
          <div className="stack">
            <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
            {actionErr != null && <ErrorNotice title="That step did not complete." error={actionErr} />}

            {u.kind !== 'balance_sheet' && <div className="notice notice-info"><div>This is a proposal document. Review its extracted fields and evidence below before adding it to assessments.</div></div>}

            {u.kind === 'pilots' && <ProposalExtractionReview uploadId={u.id} filename={u.filename} />}

            {u.kind === 'balance_sheet' && (u.status === 'parsed' || u.status === 'confirmed') && (
              <Card title="Review extracted rows" sub={parsed ? `Unit: ${parsed.unit.unit} (${parsed.unit.source}) · amount column “${parsed.amount_column_used}” · ${parsed.rows} rows read` : undefined}>
                {(parsed?.warnings ?? []).length > 0 && <div className="notice notice-warn"><div><strong>Engine warnings</strong><ul>{parsed!.warnings.map((w) => <li key={w}>{w}</li>)}</ul></div></div>}
                {rows.length === 0 && <Empty title="No AI-spend candidates">The engine found no row matching its AI-spend rules. Nothing is assumed: the AI burn stays unknown.</Empty>}
                {rows.length > 0 && (
                  <div className="tbl-wrap"><table className="tbl">
                    <caption className="sr">Candidate rows</caption>
                    <thead><tr>
                      <th scope="col"><span className="sr">Count as AI spend</span></th><th scope="col" className="num">Row</th><th scope="col">Field</th>
                      <th scope="col" className="num">Extracted value</th><th scope="col">Source text</th><th scope="col">Correction</th><th scope="col">Status</th>
                    </tr></thead>
                    <tbody>{rows.map((r) => {
                      const c = rawByRow.get(r.row)
                      const done = confirmed?.rows_confirmed?.includes(r.row)
                      return (
                        <tr key={r.row} className={checked.has(r.row) ? 'selected' : ''}>
                          <td><input type="checkbox" aria-label={`Count row ${r.row} as AI spend`} checked={checked.has(r.row)} disabled={busy}
                            onChange={(e) => setChecked((s) => { const n = new Set(s); if (e.target.checked) n.add(r.row); else n.delete(r.row); return n })} /></td>
                          <td className="num mono">{r.row}</td>
                          <td className="strong" style={{ minWidth: 160 }}>{r.label}{c?.matched?.length ? <span className="cell-sub">Matched: {c.matched.join(', ')}</span> : null}</td>
                          <td className="num">{r.amount_inr === null ? <span className="muted">Not extracted</span> : formatINR(r.amount_inr)}
                            {r.corrected && <span className="cell-sub">Corrected to {formatINR(r.corrected_amount_inr ?? null)}</span>}</td>
                          <td className="caption">{c?.raw ?? <span className="muted">Not provided</span>}</td>
                          <td style={{ minWidth: 220 }}>
                            <div className="row" style={{ gap: 6, flexWrap: 'nowrap' }}>
                              <input className="input" style={{ minHeight: 32, width: 120 }} inputMode="decimal" aria-label={`Corrected amount for row ${r.row}`} placeholder="₹ amount"
                                value={fix[r.row]?.value ?? ''} onChange={(e) => setFix((x) => ({ ...x, [r.row]: { value: e.target.value, note: x[r.row]?.note ?? '' } }))} />
                              <button className="btn btn-sm" disabled={busy || !(fix[r.row]?.value ?? '').trim()} onClick={() => void correct(r.row)}>Save</button>
                            </div>
                            {fix[r.row] && <input className="input" style={{ minHeight: 30, marginTop: 6 }} aria-label={`Note for row ${r.row}`} placeholder="Why (optional)" maxLength={500}
                              value={fix[r.row].note} onChange={(e) => setFix((x) => ({ ...x, [r.row]: { ...x[r.row], note: e.target.value } }))} />}
                            {r.note && <span className="cell-sub">Note: {r.note}</span>}
                          </td>
                          <td>{done ? <Pill tone="ok">Confirmed</Pill> : r.corrected ? <Pill tone="info">Corrected</Pill> : <Pill tone="warn">Unreviewed</Pill>}</td>
                        </tr>
                      )
                    })}</tbody>
                  </table></div>
                )}
                <p className="hint">Confidence scores are not shown because the extraction does not produce them.</p>
                <div className="row">
                  <button className="btn btn-primary" disabled={busy || checked.size === 0} onClick={() => void confirm()}>{busy ? 'Working…' : `Confirm ${checked.size || ''} row${checked.size === 1 ? '' : 's'} as AI spend`}</button>
                  <span className="hint">The engine sums the confirmed rows, not the browser.</span>
                </div>
              </Card>
            )}

            {confirmed && (
              <Card title="Confirmed result" sub={confirmed.source}>
                <dl className="dl">
                  <dt>AI burn to date</dt><dd>{formatINR(confirmed.ai_burn_to_date)}</dd>
                  <dt>Next-FY AI cost (engine)</dt><dd>{formatINR(confirmed.next_fy?.next_fy_cost_inr ?? null)}</dd>
                  <dt>Rows confirmed</dt><dd>{confirmed.rows_confirmed.join(', ')}</dd>
                </dl>
                {confirmed.flags.length > 0 && <ul className="caption">{confirmed.flags.map((f) => <li key={f.code + f.message}><span className="mono">{f.code}</span> {f.message}</li>)}</ul>}
                <div className="row"><Link to="/baseline" className="btn">Open Financial Baseline</Link><Link to="/dashboard" className="btn btn-primary">Continue to dashboard</Link></div>
              </Card>
            )}
          </div>

          <Card title="Processing status" actions={<UploadStatusPill status={u.status} />}>
            <Timeline items={stagesFor(u)} />
            {u.status === 'failed' && <button className="btn" onClick={() => void retry()} disabled={busy}>Retry now</button>}
            <p className="trace">Upload <span className="mono">{u.id}</span> · updated {formatDateTime(u.updated_at)}</p>
          </Card>
        </div>
      )}
    </>
  )
}
