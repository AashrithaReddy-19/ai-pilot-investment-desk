import { Fragment, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, ApiError } from '../../api/client'
import type { Proposal } from '../../api/types'
import type { Snapshot } from '../../api/viewTypes'
import { Card, PageHeader, Pill, RowMenu } from '../../components/fynd/Page'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatDateTime, formatINR } from '../../domain/format'
import { formFromInputs, isEvaluable } from '../../domain/fynd'
import type { Tone } from '../../domain/fynd'
import { useFynd, useProposalDocs } from '../../state/useFynd'
import { ProposalFormDialog } from './ProposalForm'
import { Dropzone, fmtBytes } from '../../components/fynd/Dropzone'
import { STATEMENT_ACCEPT, statementFormat, validateStatementFile } from '../../domain/statement'

type EvalState = { label: string; tone: Tone; title: string }

/** Real states only: inputs complete or not, and whether a stored decision/dashboard snapshot used the current inputs version. */
function evalState(p: Proposal, snaps: Snapshot[]): EvalState {
  if (p.status === 'archived') return { label: 'Archived', tone: 'off', title: 'Hidden from the workspace views' }
  if (!isEvaluable(p.inputs)) return { label: 'Draft', tone: 'off', title: 'Implementation cost and annual benefit are needed before it can be evaluated' }
  const hit = snaps.find((s) => s.proposals.some((x) => x.id === p.id))
  if (!hit) return { label: 'Not evaluated', tone: 'info', title: 'Ready: no stored evaluation includes it yet' }
  const used = hit.proposals.find((x) => x.id === p.id)!.version
  if (used < p.version) return { label: 'Needs review', tone: 'warn', title: `Last evaluated on inputs v${used}; the inputs are now v${p.version}` }
  return { label: 'Evaluated', tone: 'ok', title: `Evaluated ${formatDateTime(hit.created_at)} on inputs v${used}` }
}

const PAGE = 15

export default function AiProposals() {
  const navigate = useNavigate()
  const setProposalId = useFynd((s) => s.setProposalId)
  const { docs, error, reload } = useProposalDocs(true)
  const [snaps, setSnaps] = useState<Snapshot[]>([])
  const [q, setQ] = useState('')
  const [status, setStatus] = useState<'active' | 'archived' | 'all'>('active')
  const [sort, setSort] = useState<'updated' | 'name' | 'setup'>('updated')
  const [page, setPage] = useState(0)
  const [formOpen, setFormOpen] = useState(false)
  const [formStart, setFormStart] = useState<'manual' | 'import'>('manual')
  const [editing, setEditing] = useState<Proposal | null>(null)
  const [openId, setOpenId] = useState<string | null>(null)
  const [msg, setMsg] = useState<string | null>(null)
  const [actionErr, setActionErr] = useState<unknown>(null)
  const [confirm, setConfirm] = useState<{ kind: 'delete' | 'archive' | 'restore'; p: Proposal } | null>(null)
  const [busy, setBusy] = useState(false)
  const [proposalFile, setProposalFile] = useState<File | null>(null)
  const [proposalFileError, setProposalFileError] = useState<string | null>(null)
  const [uploadingProposal, setUploadingProposal] = useState(false)
  const [uploadError, setUploadError] = useState<unknown>(null)

  useEffect(() => {
    Promise.all([api.listSnapshots('decision', 20), api.listSnapshots('dashboard', 20)])
      .then(([a, b]) => setSnaps([...a, ...b].sort((x, y) => y.created_at.localeCompare(x.created_at))))
      .catch(() => setSnaps([]))
  }, [docs])

  const rows = useMemo(() => {
    const t = q.trim().toLowerCase()
    const list = (docs ?? []).filter((p) => (status === 'all' || p.status === status) && (!t || p.name.toLowerCase().includes(t) || (p.notes ?? '').toLowerCase().includes(t)))
    const setup = (p: Proposal) => formFromInputs(p.inputs).setup_cost_inr
    return [...list].sort((a, b) => sort === 'name' ? a.name.localeCompare(b.name)
      : sort === 'setup' ? (Number(setup(b) || -1) - Number(setup(a) || -1))
      : b.updated_at.localeCompare(a.updated_at))
  }, [docs, q, status, sort])
  const pages = Math.max(1, Math.ceil(rows.length / PAGE))
  const shown = rows.slice(page * PAGE, page * PAGE + PAGE)
  useEffect(() => { setPage(0) }, [q, status, sort])

  const act = async () => {
    if (!confirm) return
    setBusy(true); setActionErr(null); setMsg(null)
    const { kind, p } = confirm
    try {
      if (kind === 'delete') await api.deleteProposal(p.id)
      else if (kind === 'archive') await api.archiveProposal(p.id)
      else await api.restoreProposal(p.id)
      setMsg(`“${p.name}” ${kind === 'delete' ? 'deleted' : kind === 'archive' ? 'archived' : 'restored'}. The service confirmed it.`)
      await reload()
    } catch (e) { setActionErr(e) } finally { setBusy(false); setConfirm(null) }
  }

  const open = docs?.find((p) => p.id === openId) ?? null
  const uploadProposal = async () => {
    if (!proposalFile) return
    setUploadingProposal(true); setUploadError(null)
    try {
      const result = (await api.createUpload(proposalFile, 'pilots', 'auto')).data
      navigate(`/processing/${result.upload.id}`, { state: { duplicate: result.duplicate } })
    } catch (e) { setUploadError(e) } finally { setUploadingProposal(false) }
  }
  return (
    <>
      <PageHeader title="AI Proposals" subtitle="Manage vendor proposals for identified business opportunities."
        actions={<><button className="btn" onClick={() => { setEditing(null); setFormStart('import'); setFormOpen(true) }}>Import proposals</button>
          <button className="btn btn-primary" onClick={() => { setEditing(null); setFormStart('manual'); setFormOpen(true) }}>+ Add Proposal</button></>} />
      <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
      {actionErr != null && <ErrorNotice title="That change was not made." error={actionErr} />}

      <Card title="Upload a proposal document" sub="Extract proposal fields from a PDF, Excel, Word or CSV document, then review every value before submission.">
        <Dropzone accept={STATEMENT_ACCEPT} disabled={uploadingProposal} onFile={(file) => {
          const problem = validateStatementFile(file)
          setProposalFileError(problem)
          setProposalFile(problem ? null : file)
        }} hint={<>Supported: <strong>PDF</strong>, <strong>Excel</strong>, <strong>Word</strong> or <strong>CSV</strong>. Nothing is submitted until you review and confirm the extracted fields.</>} />
        {proposalFileError && <div className="notice notice-bad" role="alert"><div>{proposalFileError}</div></div>}
        {proposalFile && <div className="file-row"><div className="grow"><strong>{proposalFile.name}</strong><div className="caption">{statementFormat(proposalFile.name)?.toUpperCase()} · {fmtBytes(proposalFile.size)}</div></div>
          <button className="btn btn-primary" onClick={() => void uploadProposal()} disabled={uploadingProposal}>{uploadingProposal ? 'Uploading…' : 'Upload and extract proposal'}</button></div>}
        {uploadError != null && <ErrorNotice title="The proposal document was not uploaded." error={uploadError} onRetry={() => void uploadProposal()} />}
        {uploadError instanceof ApiError && uploadError.code === 'not_configured' && <p className="hint">Stored uploads are not configured on this gateway. Ask an administrator to enable the upload store.</p>}
      </Card>

      <Card>
        <div className="toolbar">
          <div className="field grow" style={{ maxWidth: 360 }}>
            <label className="label" htmlFor="pq">Search</label>
            <input id="pq" className="input" type="search" placeholder="Name or use case" value={q} onChange={(e) => setQ(e.target.value)} />
          </div>
          <div className="field"><label className="label" htmlFor="ps">Status</label>
            <select id="ps" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="active">Active</option><option value="archived">Archived</option><option value="all">All</option>
            </select></div>
          <div className="field"><label className="label" htmlFor="po">Sort by</label>
            <select id="po" className="select" value={sort} onChange={(e) => setSort(e.target.value as typeof sort)}>
              <option value="updated">Last updated</option><option value="name">Name</option><option value="setup">Implementation cost</option>
            </select></div>
        </div>

        {error != null && <ErrorNotice title="Proposals could not be loaded." error={error} onRetry={() => void reload()} />}
        {error == null && docs === null && <Skeleton rows={4} height={44} />}
        {docs && docs.length === 0 && (
          <Empty title="No proposals yet" actions={<><button className="btn btn-primary" onClick={() => { setEditing(null); setFormStart('manual'); setFormOpen(true) }}>+ Add Proposal</button>
            <button className="btn" onClick={() => { setEditing(null); setFormStart('import'); setFormOpen(true) }}>Import from file</button></>}>
            Add each vendor's offer with its implementation cost, running cost and expected benefit, or import many at once from a CSV, Excel or JSON file.
          </Empty>
        )}
        {docs && docs.length > 0 && rows.length === 0 && <p className="caption">No proposal matches the search and filter.</p>}
        {shown.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">AI proposals</caption>
            <thead><tr>
              <th scope="col" className="num">#</th><th scope="col">Vendor or solution</th><th scope="col">Use case</th><th scope="col">Evaluation status</th>
              <th scope="col" className="num">Implementation (one-time)</th><th scope="col" className="num">Running cost / yr</th><th scope="col">Last updated</th><th scope="col"><span className="sr">Actions</span></th>
            </tr></thead>
            <tbody>{shown.map((p, i) => {
              const f = formFromInputs(p.inputs)
              const st = evalState(p, snaps)
              return (
                <tr key={p.id} className={openId === p.id ? 'selected' : ''}>
                  <td className="num mono">{page * PAGE + i + 1}</td>
                  <td className="strong"><button className="link" onClick={() => setOpenId(openId === p.id ? null : p.id)} aria-expanded={openId === p.id}>{p.name}</button><span className="cell-sub">Inputs v{p.version}</span></td>
                  <td className="wrap-cell" style={{ minWidth: 200 }}>{p.notes ? (p.notes.length > 90 ? `${p.notes.slice(0, 90)}…` : p.notes) : <span className="muted">Not provided</span>}</td>
                  <td><Pill tone={st.tone} title={st.title}>{st.label}</Pill></td>
                  <td className="num">{f.setup_cost_inr ? formatINR(Number(f.setup_cost_inr)) : <span className="muted">Not provided</span>}</td>
                  <td className="num">{f.ai_running_cost_inr ? formatINR(Number(f.ai_running_cost_inr)) : <span className="muted" title="The engine assumes a share of setup and flags it">Not stated</span>}</td>
                  <td className="caption">{formatDateTime(p.updated_at)}</td>
                  <td>
                    <RowMenu label={`Actions for ${p.name}`} items={[
                      { label: 'View details', onClick: () => setOpenId(p.id) },
                      { label: 'Edit proposal', onClick: () => { setEditing(p); setFormOpen(true) } },
                      { label: 'Evaluate', disabled: !isEvaluable(p.inputs) || p.status !== 'active', title: !isEvaluable(p.inputs) ? 'Add implementation cost and annual benefit first' : undefined,
                        onClick: () => { setProposalId(p.id); navigate('/analysis') } },
                      { label: 'Open in dashboard', disabled: !isEvaluable(p.inputs) || p.status !== 'active', onClick: () => { setProposalId(p.id); navigate('/dashboard') } },
                      { label: 'Compare', disabled: p.status !== 'active', onClick: () => navigate('/comparison', { state: { ids: [p.id] } }) },
                      p.status === 'active' ? { label: 'Archive', onClick: () => setConfirm({ kind: 'archive', p }) } : { label: 'Restore', onClick: () => setConfirm({ kind: 'restore', p }) },
                      { label: 'Delete', danger: true, onClick: () => setConfirm({ kind: 'delete', p }) },
                    ]} />
                  </td>
                </tr>
              )
            })}</tbody>
          </table></div>
        )}
        {pages > 1 && (
          <div className="row" style={{ justifyContent: 'flex-end' }}>
            <span className="caption">Page {page + 1} of {pages} · {rows.length} proposals</span>
            <button className="btn btn-sm" disabled={page === 0} onClick={() => setPage(page - 1)}>Previous</button>
            <button className="btn btn-sm" disabled={page >= pages - 1} onClick={() => setPage(page + 1)}>Next</button>
          </div>
        )}
      </Card>

      {open && <ProposalDetails p={open} onEdit={() => { setEditing(open); setFormOpen(true) }} onClose={() => setOpenId(null)} />}

      <ProposalFormDialog open={formOpen} editing={editing} startWith={formStart} onClose={() => setFormOpen(false)}
        onImported={(n) => { setMsg(`${n} proposal${n === 1 ? '' : 's'} imported. The service confirmed each one.`); void reload() }}
        onSaved={(p) => { setFormOpen(false); setMsg(`“${p.name}” saved (inputs v${p.version}). The service confirmed it.`); setOpenId(p.id); void reload() }} />
      <ConfirmDialog open={confirm !== null} danger={confirm?.kind === 'delete'} busy={busy}
        title={confirm ? `${confirm.kind === 'delete' ? 'Delete' : confirm.kind === 'archive' ? 'Archive' : 'Restore'} “${confirm.p.name}”?` : ''}
        confirmLabel={confirm?.kind === 'delete' ? 'Delete proposal' : confirm?.kind === 'archive' ? 'Archive proposal' : 'Restore proposal'}
        body={confirm?.kind === 'delete'
          ? <p>This permanently removes the proposal and its evidence. Observations and decisions keep, with the link cleared. It cannot be undone. Deleting needs the admin role.</p>
          : <p>Archived proposals are left out of the dashboard, comparison and scenarios, but keep their evidence and history.</p>}
        onConfirm={() => void act()} onCancel={() => setConfirm(null)} />
    </>
  )
}

function ProposalDetails({ p, onEdit, onClose }: { p: Proposal; onEdit: () => void; onClose: () => void }) {
  const f = formFromInputs(p.inputs)
  const money = (s: string, suffix = '') => (s ? `${formatINR(Number(s))}${suffix}` : null)
  const rows: [string, string | null][] = [
    ['Implementation cost (one-time)', money(f.setup_cost_inr)],
    ['Recurring AI running cost', money(f.ai_running_cost_inr, ' /yr')],
    ['Expected annual benefit', money(f.annual_benefit_inr, ' /yr')],
    ['Current annual cost of the process', money(f.baseline_annual_cost_inr, ' /yr')],
    ['Benefit type', f.benefit_type || null],
    ['Cashable share', f.cashable_share_pct ? `${f.cashable_share_pct}%` : null],
    ['Expected adoption', f.adoption_pct ? `${f.adoption_pct}%` : null],
    ['Team data readiness', f.readiness || null],
    ['Ramp-up', f.ramp_months ? `${f.ramp_months} months` : null],
    ['Delay before go-live', f.delay_months ? `${f.delay_months} months` : null],
    ['Overlap group', f.overlap_group || null],
  ]
  return (
    <Card title={p.name} sub={`Inputs v${p.version} · ${p.status} · created ${formatDateTime(p.created_at)} · id ${p.id}`}
      actions={<><button className="btn btn-sm" onClick={onEdit}>Edit</button><Link className="btn btn-sm" to="/proposals">Evidence and records</Link><button className="btn btn-quiet btn-sm" onClick={onClose}>Close</button></>}>
      {p.notes && <p className="reco-text">{p.notes}</p>}
      <dl className="dl" style={{ maxWidth: 640 }}>
        {rows.map(([k, v]) => <Fragment key={k}><dt>{k}</dt><dd>{v ?? <span className="muted">Not stated</span>}</dd></Fragment>)}
      </dl>
      <p className="hint">These are the stored, user-entered inputs. Calculated figures come from the engine on the Dashboard, Comparison and Decision Analysis screens.</p>
    </Card>
  )
}
