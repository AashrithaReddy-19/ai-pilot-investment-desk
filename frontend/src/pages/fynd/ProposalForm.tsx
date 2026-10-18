import { useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { Proposal } from '../../api/types'
import { Modal } from '../../components/ui/Modal'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField, TextArea, TextField } from '../../components/ui/Fields'
import { ProposalImport } from './ProposalImport'
import { BENEFIT_TYPES, EMPTY_FORM, READINESS, formFromInputs, inputsFromForm } from '../../domain/fynd'
import type { FormErrors, ProposalForm } from '../../domain/fynd'

const NAME_RE = /^[A-Za-z0-9_\- ]{1,80}$/

/**
 * Create or edit a stored proposal. Fields map one-to-one to the stored-input keys the gateway hands to the
 * engine; blank optional fields are left out, so the engine applies (and flags) its documented assumption.
 * Create is POST /proposals, edit is PATCH /proposals/:id (the methods the router registers).
 */
export function ProposalFormDialog({ open, editing, startWith = 'manual', onClose, onSaved, onImported }: {
  open: boolean; editing: Proposal | null; startWith?: 'manual' | 'import'; onClose: () => void; onSaved: (p: Proposal) => void; onImported: (created: number) => void
}) {
  const [mode, setMode] = useState<'manual' | 'import'>(startWith)
  // each opening starts on the tab the opener asked for
  const [wasOpen, setWasOpen] = useState(false)
  if (open !== wasOpen) { setWasOpen(open); if (open) setMode(editing ? 'manual' : startWith) }
  const switcher = editing ? null : (
    <div className="segmented" role="group" aria-label="How to add proposals" style={{ alignSelf: 'flex-start' }}>
      <button type="button" aria-pressed={mode === 'manual'} onClick={() => setMode('manual')}>Enter manually</button>
      <button type="button" aria-pressed={mode === 'import'} onClick={() => setMode('import')}>Import from file</button>
    </div>
  )
  return (
    <Modal open={open} onClose={onClose} labelledBy="pf-title">
      {open && (mode === 'import' && !editing
        ? <ProposalImport switcher={switcher} onClose={onClose} onImported={onImported} />
        : <Body key={editing?.id ?? 'new'} editing={editing} switcher={switcher} onClose={onClose} onSaved={onSaved} />)}
    </Modal>
  )
}

function Body({ editing, switcher, onClose, onSaved }: { editing: Proposal | null; switcher: React.ReactNode; onClose: () => void; onSaved: (p: Proposal) => void }) {
  const [name, setName] = useState(editing?.name ?? '')
  const [notes, setNotes] = useState(editing?.notes ?? '')
  const [f, setF] = useState<ProposalForm>(editing ? formFromInputs(editing.inputs) : EMPTY_FORM)
  const [errs, setErrs] = useState<FormErrors & { name?: string; notes?: string }>({})
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)
  const set = (k: keyof ProposalForm) => (v: string) => setF((x) => ({ ...x, [k]: v }))

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    const { inputs, errors } = inputsFromForm(f, editing?.inputs)
    const all: typeof errs = { ...errors }
    const n = name.trim()
    if (!NAME_RE.test(n)) all.name = 'Use 1–80 characters: letters, digits, spaces, hyphens or underscores.'
    if (notes.length > 2000) all.notes = 'Notes must be at most 2000 characters.'
    setErrs(all)
    if (Object.keys(all).length) return
    setBusy(true); setErr(null)
    try {
      const body = { name: n, inputs, notes: notes.trim() || null }
      const saved = editing ? (await api.updateProposal(editing.id, body)).data : (await api.createProposal(body)).data
      onSaved(saved)
    } catch (x) {
      if (x instanceof ApiError && x.field === 'name') setErrs({ name: x.message })
      setErr(x)
    } finally { setBusy(false) }
  }

  return (
    <form className="dlg" onSubmit={submit} noValidate>
      <div className="dlg-head">
        <div>
          <h2 className="h2" id="pf-title">{editing ? `Edit “${editing.name}”` : 'Add proposal'}</h2>
          <p className="caption">{editing ? `Saving creates inputs version ${editing.version + 1} if any input changes; earlier evaluations keep the version they used.` : 'One AI solution for one business process. Amounts are in rupees.'}</p>
        </div>
        <button type="button" className="btn btn-quiet icon-btn" onClick={onClose} aria-label="Close">×</button>
      </div>
      <div className="dlg-body">
        {switcher}
        {err != null && <ErrorNotice title="The proposal was not saved." error={err} />}
        <TextField id="pf-name" label="Vendor or solution name" required value={name} onChange={setName} maxLength={80} error={errs.name} />
        <TextArea id="pf-notes" label="Use case and description" value={notes} onChange={setNotes} maxLength={2000} error={errs.notes} hint="Which service or process this addresses, and how success will be measured." />
        <fieldset>
          <legend>Costs</legend>
          <div className="form-grid">
            <TextField id="pf-setup" label="Implementation cost (one-time)" required unit="₹" inputMode="decimal" value={f.setup_cost_inr} onChange={set('setup_cost_inr')} error={errs.setup_cost_inr} />
            <TextField id="pf-run" label="Recurring AI running cost" unit="₹/yr" inputMode="decimal" value={f.ai_running_cost_inr} onChange={set('ai_running_cost_inr')} error={errs.ai_running_cost_inr}
              hint="Leave blank if unknown: the engine assumes a share of setup and flags it. It is never treated as free." />
          </div>
        </fieldset>
        <fieldset>
          <legend>Benefit and today's cost</legend>
          <div className="form-grid">
            <TextField id="pf-benefit" label="Expected annual benefit" required unit="₹/yr" inputMode="decimal" value={f.annual_benefit_inr} onChange={set('annual_benefit_inr')} error={errs.annual_benefit_inr} />
            <TextField id="pf-baseline" label="Current annual cost of the process" unit="₹/yr" inputMode="decimal" value={f.baseline_annual_cost_inr} onChange={set('baseline_annual_cost_inr')} error={errs.baseline_annual_cost_inr}
              hint="Today's cost (the baseline). Without it the retained human cost is unknown." />
            <SelectField id="pf-btype" label="Benefit type" value={f.benefit_type} onChange={set('benefit_type')} error={errs.benefit_type}
              options={[{ value: '', label: 'Not stated' }, ...BENEFIT_TYPES.map((b) => ({ value: b, label: b[0].toUpperCase() + b.slice(1) }))]} />
            <TextField id="pf-cash" label="Cashable share" unit="%" inputMode="decimal" value={f.cashable_share_pct} onChange={set('cashable_share_pct')} error={errs.cashable_share_pct}
              hint="Share of the benefit that is real cash, not freed capacity." />
          </div>
        </fieldset>
        <fieldset>
          <legend>Delivery assumptions</legend>
          <div className="form-grid">
            <TextField id="pf-adopt" label="Expected adoption" unit="%" inputMode="decimal" value={f.adoption_pct} onChange={set('adoption_pct')} error={errs.adoption_pct} />
            <SelectField id="pf-ready" label="Team data readiness" value={f.readiness} onChange={set('readiness')} error={errs.readiness}
              options={[{ value: '', label: 'Not stated' }, ...READINESS.map((r) => ({ value: r, label: r[0].toUpperCase() + r.slice(1) }))]} />
            <TextField id="pf-ramp" label="Ramp-up" unit="months" inputMode="numeric" value={f.ramp_months} onChange={set('ramp_months')} error={errs.ramp_months} />
            <TextField id="pf-delay" label="Delay before go-live" unit="months" inputMode="numeric" value={f.delay_months} onChange={set('delay_months')} error={errs.delay_months} />
            <TextField id="pf-group" label="Overlap group" value={f.overlap_group} onChange={set('overlap_group')} error={errs.overlap_group} maxLength={40}
              hint="Proposals that address the same benefit share a group, so it is not counted twice." />
          </div>
        </fieldset>
        <p className="hint">Vendor pricing documents cannot be attached here: the gateway has no upload for proposal documents. Record them as evidence on the proposal record instead.</p>
      </div>
      <div className="dlg-foot">
        <span className="grow hint">Only the service's confirmation counts as saved.</span>
        <button type="button" className="btn" onClick={onClose} disabled={busy}>Cancel</button>
        <button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : editing ? 'Save changes' : 'Add proposal'}</button>
      </div>
    </form>
  )
}
