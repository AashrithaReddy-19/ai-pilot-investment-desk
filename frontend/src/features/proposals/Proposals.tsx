import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { EvaluationSummary, Evidence, EvidenceKind, EvidenceQuality, Proposal, ProposalSummary } from '../../api/types'
import { formatDate, formatDateTime } from '../../domain/format'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField, TextArea, TextField } from '../../components/ui/Fields'

const NAME_RE = /^[A-Za-z0-9_\- ]{1,80}$/
const DRAFT_KEY = 'fynd.proposal-draft'

type CreateErrs = Partial<Record<'name' | 'notes', string>>

/** Proposals stored in the database: list, create, edit, archive and attach evidence. Create drafts survive a reload. */
export function Proposals({ refreshKey = 0 }: { refreshKey?: number }) {
  const [list, setList] = useState<ProposalSummary[] | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [showArchived, setShowArchived] = useState(false)
  const [openId, setOpenId] = useState<string | null>(null)
  const [doc, setDoc] = useState<Proposal | null>(null)
  const [docError, setDocError] = useState<unknown>(null)
  const [docLoading, setDocLoading] = useState(false)
  const [toDelete, setToDelete] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [toArchive, setToArchive] = useState<string | null>(null)
  const archivingItem = list?.find((x) => x.id === toArchive) ?? null
  const [busy, setBusy] = useState(false)
  const [name, setName] = useState('')
  const [notes, setNotes] = useState('')
  const [errs, setErrs] = useState<CreateErrs>({})
  const [okMsg, setOkMsg] = useState<string | null>(null)
  const [actionError, setActionError] = useState<unknown>(null)
  const [archiving, setArchiving] = useState(false)

  const refresh = useCallback(async () => {
    try { setList(await api.listProposals(showArchived)); setError(null) } catch (e) { setError(e) }
  }, [showArchived])
  useEffect(() => { void refresh() }, [refresh, refreshKey])

  // Reload-safe draft: keep what the user typed so a page reload or crash loses nothing.
  useEffect(() => {
    try { const d = JSON.parse(localStorage.getItem(DRAFT_KEY) ?? '{}'); setName(d.name ?? ''); setNotes(d.notes ?? '') } catch { /* ignore corrupt drafts */ }
    return () => { /* keep the draft; it is cleared on successful create */ }
  }, [])
  useEffect(() => { try { localStorage.setItem(DRAFT_KEY, JSON.stringify({ name, notes })) } catch { /* storage unavailable */ } }, [name, notes])

  const open = async (id: string) => {
    if (openId === id) { setOpenId(null); setDoc(null); return }
    setOpenId(id); setDoc(null); setDocError(null); setDocLoading(true)
    try { setDoc((await api.getProposal(id)).data) } catch (e) { setDocError(e) } finally { setDocLoading(false) }
  }

  const create = async (e: React.FormEvent) => {
    e.preventDefault()
    setOkMsg(null); setActionError(null)
    const n = name.trim()
    const x: CreateErrs = {}
    if (!NAME_RE.test(n)) x.name = 'Use 1–80 characters: letters, digits, spaces, hyphens or underscores.'
    if (notes.length > 2000) x.notes = 'Notes must be at most 2000 characters.'
    setErrs(x)
    if (Object.keys(x).length) return
    setBusy(true)
    try {
      const saved = (await api.createProposal({ name: n, notes: notes.trim() || null })).data
      setOkMsg(`Proposal “${saved.name}” created. The service confirmed it.`)
      setName(''); setNotes(''); try { localStorage.removeItem(DRAFT_KEY) } catch { /* ignore */ }
      await refresh()
    } catch (err) {
      if (err instanceof ApiError && err.field === 'name') setErrs({ name: err.message })
      setActionError(err)
    } finally { setBusy(false) }
  }

  const remove = async () => {
    if (!toDelete) return
    setDeleting(true); setActionError(null)
    try {
      const gone = toDelete
      await api.deleteProposal(gone)
      setOkMsg(`Deleted “${gone}”. The service confirmed it.`)
      if (openId === gone) { setOpenId(null); setDoc(null) }
      setToDelete(null)
      await refresh()
    } catch (e) { setActionError(e); setToDelete(null) } finally { setDeleting(false) }
  }

  const changeStatus = async (id: string, name0: string, archive: boolean) => {
    setArchiving(true); setActionError(null)
    try {
      const updated = (archive ? await api.archiveProposal(id) : await api.restoreProposal(id)).data
      setOkMsg(`“${name0}” is now ${updated.status}. The service confirmed it.`)
      await refresh()
    } catch (e) { setActionError(e) } finally { setArchiving(false); setToArchive(null) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <div aria-live="polite">{okMsg && <div className="notice notice-ok"><div>{okMsg}</div></div>}</div>
      {actionError != null && <ErrorNotice title="That change was not made." error={actionError} />}

      <form className="panel" onSubmit={create}>
        <h3 className="h3">New proposal</h3>
        <p className="hint">A proposal is a named AI pilot candidate. Its inputs are versioned with it, so later observations and decisions attach to the same idea over time.</p>
        <TextField id="prop-name" label="Name" required value={name} onChange={setName} maxLength={80} error={errs.name} />
        <TextArea id="prop-notes" label="Notes" value={notes} onChange={setNotes} maxLength={2000} error={errs.notes}
          hint="What the pilot is for and how you intend to measure it. Optional." />
        <button className="btn btn-primary" disabled={busy}>{busy ? 'Creating…' : 'Create proposal'}</button>
        <p className="hint">Your draft is kept on this device until you create the proposal, so a reload does not lose it.</p>
      </form>

      <div className="row" style={{ alignItems: 'center' }}>
        <label className="hint" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <input type="checkbox" checked={showArchived} onChange={(e) => { setShowArchived(e.target.checked); setOpenId(null); setDoc(null) }} />
          Include archived
        </label>
      </div>
      {error != null && <ErrorNotice title="Proposals could not be loaded." error={error} onRetry={() => void refresh()} />}
      {error == null && list === null && <Skeleton rows={3} height={48} />}
      {error == null && list?.length === 0 && <Empty title={showArchived ? 'No proposals yet' : 'No active proposals'}>Create one above. Observations and decisions can then be linked to it.</Empty>}
      {list && list.length > 0 && (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr">Proposals</caption>
            <thead><tr><th className="sticky" scope="col">Name</th><th scope="col">Status</th><th scope="col">Created</th><th scope="col">Last updated</th><th scope="col"><span className="sr">Actions</span></th></tr></thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id} aria-selected={openId === p.id}>
                  <td className="sticky strong">{p.name}</td>
                  <td><span className={`badge`}>{p.status === 'active' ? 'Active' : 'Archived'}</span></td>
                  <td className="caption">{formatDateTime(p.created_at)}</td>
                  <td className="caption">{formatDateTime(p.updated_at)}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="btn btn-quiet btn-sm" aria-expanded={openId === p.id} onClick={() => void open(p.id)}>{openId === p.id ? 'Close' : 'Open'}</button>
                    <button className="btn btn-quiet btn-sm" onClick={() => setToArchive(p.id)} disabled={archiving}>{p.status === 'active' ? 'Archive' : 'Restore'}</button>
                    <button className="btn btn-quiet btn-sm" onClick={() => setToDelete(p.id)} aria-label={`Delete ${p.name}`}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {docLoading && <Skeleton rows={2} height={60} />}
      {docError != null && <ErrorNotice title="That proposal could not be opened." error={docError} />}
      {doc && <ProposalDetail doc={doc} />}
      <ConfirmDialog open={toDelete !== null} danger confirmLabel="Delete proposal" title={`Delete “${toDelete}”?`} busy={deleting}
        body={<p>This permanently removes the proposal and its evidence from the database. Observations and decisions keep, with the link cleared, not deleted. It cannot be undone.</p>}
        onConfirm={() => void remove()} onCancel={() => setToDelete(null)} />
      <ConfirmDialog open={toArchive !== null} confirmLabel={archivingItem?.status === 'active' ? 'Archive proposal' : 'Restore proposal'} busy={archiving}
        title={archivingItem ? (archivingItem.status === 'active' ? `Archive “${archivingItem.name}”?` : `Restore “${archivingItem.name}”?`) : 'Change status?'}
        body={<p>Archived proposals are hidden from the default list but keep their evidence, observations and decisions.</p>}
        onConfirm={() => { if (archivingItem) void changeStatus(archivingItem.id, archivingItem.name, archivingItem.status === 'active') }} onCancel={() => setToArchive(null)} />
    </div>
  )
}

/** The open proposal: stored inputs, editable notes, and the evidence attached to it. */
function ProposalDetail({ doc: received }: { doc: Proposal }) {
  const [doc, setDoc] = useState(received)
  const [notes, setNotes] = useState(received.notes ?? '')
  const [savingNotes, setSavingNotes] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [evidence, setEvidence] = useState<Evidence[] | null>(null)
  const [evError, setEvError] = useState<unknown>(null)
  const [evBusy, setEvBusy] = useState(false)
  const [kind, setKind] = useState<EvidenceKind>('interview')
  const [quality, setQuality] = useState<EvidenceQuality>('observed')
  const [source, setSource] = useState('')
  const [note, setNote] = useState('')
  const [evErrs, setEvErrs] = useState<Partial<Record<'kind' | 'quality' | 'source' | 'note', string>>>({})
  const [evals, setEvals] = useState<EvaluationSummary[] | null>(null)
  const [evalsError, setEvalsError] = useState<unknown>(null)

  useEffect(() => {
    let alive = true
    api.listEvidence(received.id).then((l) => { if (alive) setEvidence(l) }).catch((e) => { if (alive) setEvError(e) })
    api.listProposalEvaluations(received.id).then((l) => { if (alive) setEvals(l) }).catch((e) => { if (alive) setEvalsError(e) })
    return () => { alive = false }
  }, [received.id])

  const saveNotes = async () => {
    setSavingNotes(true); setMsg(null); setErr(null)
    try {
      const updated = (await api.updateProposal(doc.id, { notes: notes.trim() || null })).data
      setDoc(updated)
      setMsg('Notes saved. The service confirmed it.')
    } catch (e) { setErr(e) } finally { setSavingNotes(false) }
  }

  const addEvidence = async (e: React.FormEvent) => {
    e.preventDefault()
    setMsg(null); setErr(null)
    const src = source.trim()
    if (src === '' || src.length > 200) { setEvErrs({ source: 'Enter who or what provided this, in up to 200 characters.' }); return }
    setEvErrs({})
    setEvBusy(true)
    try {
      await api.addEvidence(doc.id, { kind, source: src, quality, note: note.trim() || null })
      setSource(''); setNote('')
      setEvidence(await api.listEvidence(doc.id))
      setMsg('Evidence attached. The service confirmed it.')
    } catch (x) {
      if (x instanceof ApiError && x.field) setEvErrs({ [x.field as 'kind' | 'quality' | 'source' | 'note']: x.message })
      setErr(x)
    } finally { setEvBusy(false) }
  }

  return (
    <div className="panel" style={{ gap: 24 }}>
      <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
      {err != null && <ErrorNotice title="That change was not made." error={err} />}
      <div>
        <h3 className="h2">{doc.name}</h3>
        <p className="caption">Inputs version {doc.version} · created {formatDateTime(doc.created_at)} · last updated {formatDateTime(doc.updated_at)} · id <span className="mono">{doc.id}</span></p>
      </div>

      <div className="stack">
        <h4 className="h3">Stored inputs</h4>
        {Object.keys(doc.inputs ?? {}).length === 0
          ? <p className="muted">No inputs stored yet. Inputs are versioned with the proposal so later evaluations compare like with like.</p>
          : <pre className="mono" style={{ background: 'var(--bg-muted)', padding: 12, borderRadius: 8, overflow: 'auto', maxHeight: 300 }}>{JSON.stringify(doc.inputs, null, 2)}</pre>}
      </div>

      <div className="stack">
        <h4 className="h3">Notes</h4>
        <TextArea id={`notes-${doc.id}`} label="Notes" value={notes} onChange={setNotes} maxLength={2000} hint="Up to 2000 characters." />
        <div className="row">
          <button className="btn" onClick={() => void saveNotes()} disabled={savingNotes || notes === (doc.notes ?? '')}>{savingNotes ? 'Saving…' : 'Save notes'}</button>
        </div>
      </div>

      <div className="stack">
        <h4 className="h3">Evidence</h4>
        <form onSubmit={addEvidence} className="stack" aria-label={`Attach evidence to ${doc.name}`}>
          <div className="row" style={{ gap: 16, alignItems: 'flex-start' }}>
            <div style={{ minWidth: 180 }}>
              <SelectField id={`ev-kind-${doc.id}`} label="Kind" required value={kind} onChange={(v) => setKind(v as EvidenceKind)} error={evErrs.kind}
                options={[{ value: 'interview', label: 'Interview' }, { value: 'measurement', label: 'Measurement' }, { value: 'document', label: 'Document' }, { value: 'assumption', label: 'Assumption' }]} />
            </div>
            <div style={{ minWidth: 180 }}>
              <SelectField id={`ev-quality-${doc.id}`} label="Quality" value={quality} onChange={(v) => setQuality(v as EvidenceQuality)} error={evErrs.quality}
                options={[{ value: 'observed', label: 'Observed' }, { value: 'assumed', label: 'Assumed' }, { value: 'calculated', label: 'Calculated' }, { value: 'fixture', label: 'Fixture data' }]} />
            </div>
            <div style={{ flex: 1, minWidth: 220 }}>
              <TextField id={`ev-source-${doc.id}`} label="Source" required value={source} onChange={setSource} maxLength={200} error={evErrs.source}
                hint="Who or what provided this. Record a role, not a person’s name." />
            </div>
          </div>
          <TextArea id={`ev-note-${doc.id}`} label="Note" value={note} onChange={setNote} maxLength={2000} error={evErrs.note}
            hint="What was said or measured, in the source's own words where possible. Optional." />
          <button className="btn" disabled={evBusy}>{evBusy ? 'Attaching…' : 'Attach evidence'}</button>
        </form>
        {evError != null && <ErrorNotice title="Evidence could not be loaded." error={evError} />}
        {evError == null && evidence === null && <Skeleton rows={2} height={40} />}
        {evidence?.length === 0 && <p className="muted">No evidence attached yet.</p>}
        {evidence && evidence.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Evidence attached to this proposal</caption>
            <thead><tr><th scope="col">Date</th><th scope="col">Kind</th><th scope="col">Source</th><th scope="col">Quality</th><th scope="col">Note</th></tr></thead>
            <tbody>{evidence.map((ev) => (
              <tr key={ev.id}>
                <td>{formatDate(ev.created_at.slice(0, 10))}</td>
                <td style={{ textTransform: 'capitalize' }}>{ev.kind}</td>
                <td className="strong">{ev.source}</td>
                <td style={{ textTransform: 'capitalize' }}>{ev.quality && <span className="tag">{ev.quality}</span>}</td>
                <td className="wrap-cell">{ev.note ?? <span className="muted">Not provided</span>}</td>
              </tr>))}</tbody>
          </table></div>
        )}
      </div>

      <div className="stack">
        <h4 className="h3">Evaluations of this proposal</h4>
        {evalsError != null && <ErrorNotice title="Evaluations could not be loaded." error={evalsError} />}
        {evalsError == null && evals === null && <Skeleton rows={2} height={40} />}
        {evals?.length === 0 && <p className="muted">No evaluation has been run for this proposal yet.</p>}
        {evals && evals.length > 0 && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Evaluations run for this proposal</caption>
            <thead><tr><th className="num" scope="col">Id</th><th scope="col">When</th><th scope="col">Inputs version</th></tr></thead>
            <tbody>{evals.map((ev) => (
              <tr key={ev.id}>
                <td className="num mono">{ev.id}</td>
                <td>{formatDateTime(ev.created_at)}</td>
                <td>{ev.proposal_version == null
                  ? <span className="muted">Not recorded</span>
                  : <>v{ev.proposal_version}{ev.proposal_version < doc.version && <> <span className="tag tag-assumed" title="The stored inputs changed after this evaluation">stale inputs</span></>}</>}</td>
              </tr>))}</tbody>
          </table></div>
        )}
      </div>
    </div>
  )
}
