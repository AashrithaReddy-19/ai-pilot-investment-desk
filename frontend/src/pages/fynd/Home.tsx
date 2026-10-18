import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, ApiError } from '../../api/client'
import type { Upload } from '../../api/viewTypes'
import { Card, FlowSteps, PageHeader, Pill } from '../../components/fynd/Page'
import { Dropzone, fmtBytes } from '../../components/fynd/Dropzone'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { Icon } from '../../components/ui/Icon'
import { formatDateTime } from '../../domain/format'
import { FORMAT_LABEL, MAX_STATEMENT_MB, STATEMENT_ACCEPT, statementFormat, validateStatementFile } from '../../domain/statement'
import { useFynd } from '../../state/useFynd'
import { UploadStatusPill } from './uploadStatus'

/** The gateway validates signatures again; this is only the client-side capability list. */
const STORABLE = new Set(['csv', 'pdf', 'xlsx', 'xls', 'docx'])

export default function Home() {
  const navigate = useNavigate()
  const burn = useFynd((s) => s.burn)
  const [file, setFile] = useState<File | null>(null)
  const [fileError, setFileError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [uploads, setUploads] = useState<Upload[] | null>(null)
  const [uploadsOff, setUploadsOff] = useState<string | null>(null)
  const [evaluated, setEvaluated] = useState<boolean | null>(null)

  useEffect(() => {
    api.listUploads(10).then(setUploads).catch((e) => {
      setUploads([])
      setUploadsOff(e instanceof ApiError && e.code === 'not_configured' ? 'Stored uploads are not configured on this gateway.' : 'Stored uploads could not be listed.')
    })
    api.listSnapshots('dashboard', 1).then((l) => setEvaluated(l.length > 0)).catch(() => setEvaluated(null))
  }, [])

  const pick = (f: File) => {
    setErr(null)
    const problem = validateStatementFile(f)
    setFileError(problem)
    setFile(problem ? null : f)
  }
  const format = file ? statementFormat(file.name) : null
  const storable = format !== null && STORABLE.has(format)

  const store = async () => {
    if (!file) return
    setBusy(true); setErr(null)
    try {
      const r = (await api.createUpload(file, 'balance_sheet', 'auto', (value) => setProgress(value))).data
      navigate(`/processing/${r.upload.id}`, { state: { duplicate: r.duplicate } })
    } catch (e) { setErr(e) } finally { setBusy(false); setProgress(null) }
  }

  const uploaded = (uploads?.length ?? 0) > 0 || burn !== null
  const reviewed = (uploads ?? []).some((u) => u.status === 'confirmed') || burn !== null
  const steps = [
    { title: 'Upload documents', sub: uploaded ? 'A document has been uploaded' : 'Balance sheet or annual report', state: uploaded ? 'done' : 'current', to: '/' },
    { title: 'Review extracted information', sub: reviewed ? 'AI spend rows confirmed' : 'Confirm which rows are AI spend', state: reviewed ? 'done' : uploaded ? 'current' : 'todo', to: '/baseline' },
    { title: 'Evaluate opportunities', sub: evaluated ? 'A dashboard has been run' : 'Add proposals, then run the dashboard', state: evaluated ? 'done' : reviewed ? 'current' : 'todo', to: '/dashboard' },
  ] as const

  return (
    <>
      <PageHeader title="Home" subtitle="Start the investment evaluation: establish today's spend, then evaluate AI proposals against it." />
      <section className="hero">
        <h2 className="hero-title">Turn your financial data into AI investment decisions.</h2>
        <p className="hero-text">FYND reads what you already spend from your balance sheet, evaluates each AI proposal with a rule-based engine, and shows the evidence behind every recommendation, so the decision can be defended.</p>
      </section>

      <FlowSteps steps={steps.map((s) => ({ ...s }))} />

      <div className="grid-main">
        <Card title="Upload a financial document" sub="Balance sheet, profit and loss statement or annual report">
          <Dropzone accept={STATEMENT_ACCEPT} onFile={pick} disabled={busy}
            hint={<>Supported: <strong>PDF</strong>, <strong>Excel</strong> (.xlsx, .xls), <strong>Word</strong> (.docx) or <strong>CSV</strong>, up to {MAX_STATEMENT_MB} MB. The gateway validates the file signature and size before storing it.</>} />
          {fileError && <div className="notice notice-bad" role="alert"><div>{fileError}</div></div>}
          {file && format && (
            <div className="file-row">
              <Icon name="file" />
              <div className="grow">
                <div className="strong">{file.name}</div>
                <div className="caption">{FORMAT_LABEL[format]} · {fmtBytes(file.size)}</div>
              </div>
              <button className="btn btn-quiet btn-sm" onClick={() => { setFile(null); setErr(null) }} disabled={busy}>Remove</button>
            </div>
          )}
          {file && format && (
            <div className="stack" style={{ gap: 10 }}>
              <div className="row">
                <button className="btn btn-primary" onClick={() => void store()} disabled={!storable || busy || uploadsOff !== null}
                  title={!storable ? 'This file type cannot be stored by the current gateway' : undefined}>
                  {busy ? 'Uploading…' : 'Store and process'}
                </button>
                <button className="btn" onClick={() => navigate('/baseline', { state: { file } })} disabled={busy}>Read without storing</button>
              </div>
              <p className="hint">
                <strong>Store and process</strong> keeps the original file with its fingerprint, parses it in the background and lets you correct and confirm rows (audited).{' '}
                <strong>Read without storing</strong> reads the document once on the Financial Baseline screen; nothing is kept.
                {!storable && <> This {FORMAT_LABEL[format]} file cannot be stored by the current gateway.</>}
              </p>
              {busy && <p className="caption" role="status">{progress === null ? 'Preparing secure document upload…' : `Uploading to secure document storage… ${Math.round(progress * 100)}%`}</p>}
            </div>
          )}
          {err != null && <ErrorNotice title="The upload was not stored." error={err} onRetry={() => void store()} />}
        </Card>

        <Card title="Recent uploads" sub="Stored documents in this workspace" actions={<Link to="/processing" className="btn btn-sm">All uploads</Link>}>
          {uploadsOff && <p className="caption">{uploadsOff}</p>}
          {uploads === null && <p className="caption" role="status">Loading…</p>}
          {uploads && uploads.length === 0 && !uploadsOff && <p className="caption">No document has been stored yet.</p>}
          {uploads && uploads.length > 0 && (
            <ul className="stack" style={{ gap: 10 }}>
              {uploads.slice(0, 6).map((u) => (
                <li key={u.id} className="row" style={{ justifyContent: 'space-between', flexWrap: 'nowrap' }}>
                  <Link to={`/processing/${u.id}`} className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{u.filename}</Link>
                  <span className="caption" style={{ whiteSpace: 'nowrap' }}>{formatDateTime(u.created_at)}</span>
                  <UploadStatusPill status={u.status} />
                </li>
              ))}
            </ul>
          )}
          {burn && (
            <div className="notice notice-ok"><div>AI burn confirmed this session: <strong>{burn.value.toLocaleString('en-IN')}</strong> INR <span className="caption">({burn.source})</span></div></div>
          )}
          <Pill tone="info">Next: <Link to="/ai-proposals">add AI proposals</Link></Pill>
        </Card>
      </div>
    </>
  )
}
