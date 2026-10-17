import { useEffect, useState } from 'react'
import { BENEFIT_TYPES, DraftErrors, DraftForm, DraftPilot, emptyForm, formFromDraft, nextId, validateDraft } from '../../domain/draft'
import { formatINR, formatPct } from '../../domain/format'
import { Modal } from '../../components/ui/Modal'
import { SelectField, TextArea, TextField } from '../../components/ui/Fields'

interface Props {
  open: boolean
  /** The draft being edited, or null to add a new one. */
  editing: DraftPilot | null
  /** Ids already used by the other drafts in this scenario. */
  usedIds: string[]
  onClose: () => void
  onSubmit: (draft: DraftPilot, previousId: string | null) => void
}

const DETAIL_KEYS: (keyof DraftForm)[] = ['group', 'description']
const ADVANCED_KEYS: (keyof DraftForm)[] = ['adoption', 'readiness', 'obsAdoption', 'obsQuality']
const filled = (f: DraftForm, keys: (keyof DraftForm)[]) => keys.some((k) => String(f[k]).trim() !== '')
const bad = (e: DraftErrors, keys: (keyof DraftForm)[]) => keys.some((k) => e[k])

/** Two steps: fill in the form, then review exactly what will be added to the scenario. */
export function PilotDialog({ open, editing, usedIds, onClose, onSubmit }: Props) {
  const [f, setF] = useState<DraftForm>(emptyForm('P1'))
  const [errors, setErrors] = useState<DraftErrors>({})
  const [step, setStep] = useState<'form' | 'review'>('form')
  const [draft, setDraft] = useState<DraftPilot | null>(null)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [advancedOpen, setAdvancedOpen] = useState(false)

  useEffect(() => {
    if (!open) return
    const start = editing ? formFromDraft(editing) : emptyForm(nextId(usedIds))
    setF(start); setErrors({}); setStep('form'); setDraft(null)
    setDetailsOpen(filled(start, DETAIL_KEYS)); setAdvancedOpen(filled(start, ADVANCED_KEYS))
  }, [open, editing, usedIds])

  const set = (k: keyof DraftForm) => (v: string) => { setF((x) => ({ ...x, [k]: v } as DraftForm)); setErrors((x) => ({ ...x, [k]: undefined })) }

  const review = (e: React.FormEvent) => {
    e.preventDefault()
    const r = validateDraft(f, usedIds)
    setErrors(r.errors)
    if (bad(r.errors, DETAIL_KEYS)) setDetailsOpen(true)
    if (bad(r.errors, ADVANCED_KEYS)) setAdvancedOpen(true)
    if (r.draft) { setDraft(r.draft); setStep('review') }
  }

  const missing = <span className="muted">Not provided</span>

  return (
    <Modal open={open} onClose={onClose} labelledBy="pilot-dialog-title">
      {step === 'form' ? (
        <form onSubmit={review} noValidate>
          <div className="dlg-head">
            <div><h2 className="h2" id="pilot-dialog-title">{editing ? `Edit ${editing.id}` : 'Add a pilot'}</h2>
              <p className="caption" style={{ marginTop: 4 }}>Only the fields marked required are needed to evaluate a pilot. Leave anything you do not know blank; the service will say what it assumed.</p></div>
            <button type="button" className="btn btn-quiet btn-sm" onClick={onClose}>Close</button>
          </div>
          <div className="dlg-body">
            <fieldset>
              <legend>The essentials</legend>
              <div className="form-grid">
                <TextField id="pd-task" label="Pilot name" required value={f.task} onChange={set('task')} error={errors.task} autoFocus hint="For example, “Invoice field extraction”." />
                <TextField id="pd-id" label="Pilot ID" required value={f.id} onChange={set('id')} error={errors.id} hint="A short code used to refer to this pilot in results." />
                <TextField id="pd-setup" label="One-time setup cost" required unit="INR" inputMode="numeric" value={f.setup} onChange={set('setup')} error={errors.setup} hint="Paid once. Commas are fine, for example 2,00,000." />
                <TextField id="pd-benefit" label="Expected benefit per year" required unit="INR" inputMode="numeric" value={f.benefit} onChange={set('benefit')} error={errors.benefit} />
                <TextField id="pd-running" label="Running cost per year" unit="INR" inputMode="numeric" value={f.running} onChange={set('running')} error={errors.running} hint="Leave blank if unknown. Blank is not zero: the service applies a default and labels it assumed." />
                <SelectField id="pd-type" label="Benefit type" required value={f.benefitType} onChange={set('benefitType')}
                  options={BENEFIT_TYPES.map((b) => ({ value: b.value, label: b.label }))}
                  hint={BENEFIT_TYPES.find((b) => b.value === f.benefitType)?.hint} />
              </div>
            </fieldset>

            <details className="more" open={detailsOpen} onToggle={(e) => setDetailsOpen(e.currentTarget.open)}>
              <summary>Details <span className="muted">overlap group, notes</span></summary>
              <div className="more-body">
                <TextField id="pd-group" label="Overlap group" value={f.group} onChange={set('group')} error={errors.group}
                  hint="Give pilots that would save the same hours or revenue the same name. The engine funds at most one pilot per group." />
                <TextArea id="pd-desc" label="Notes" value={f.description} onChange={set('description')} error={errors.description} maxLength={400} hint="Stored with the scenario sent to the rules engine." />
              </div>
            </details>

            <details className="more" open={advancedOpen} onToggle={(e) => setAdvancedOpen(e.currentTarget.open)}>
              <summary>Advanced assumptions <span className="muted">adoption, readiness, measured results</span></summary>
              <div className="more-body">
                <h3 className="h3">Estimates</h3>
                <div className="form-grid">
                  <TextField id="pd-adoption" label="Expected adoption" unit="%" inputMode="decimal" value={f.adoption} onChange={set('adoption')} error={errors.adoption} hint="0 to 100. Leave blank if unknown." />
                  <SelectField id="pd-ready" label="Data readiness" value={f.readiness} onChange={set('readiness')}
                    options={[{ value: '', label: 'Unknown' }, { value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' }]} />
                </div>
                <h3 className="h3">Measured results</h3>
                <p className="caption">Enter only what was actually measured in use. Measured values feed the stop rule and are shown apart from estimates.</p>
                <div className="form-grid">
                  <TextField id="pd-oa" label="Measured adoption" unit="%" inputMode="decimal" value={f.obsAdoption} onChange={set('obsAdoption')} error={errors.obsAdoption} />
                  <TextField id="pd-oq" label="Measured quality" unit="%" inputMode="decimal" value={f.obsQuality} onChange={set('obsQuality')} error={errors.obsQuality} />
                </div>
              </div>
            </details>
          </div>
          <div className="dlg-foot">
            <span className="grow caption">{Object.values(errors).some(Boolean) ? 'Fix the highlighted fields to continue.' : ''}</span>
            <button type="button" className="btn" onClick={onClose}>Cancel</button>
            <button type="submit" className="btn btn-primary">Review</button>
          </div>
        </form>
      ) : draft && (
        <div className="dlg">
          <div className="dlg-head">
            <div><h2 className="h2" id="pilot-dialog-title">Review before adding</h2>
              <p className="caption" style={{ marginTop: 4 }}>This is exactly what will be sent to the rules engine. Nothing is calculated until you evaluate.</p></div>
          </div>
          <div className="dlg-body">
            <div className="kv">
              <span>Pilot</span><span>{draft.id} · {draft.task}</span>
              <span>Setup cost</span><span>{formatINR(draft.setup_cost_inr)}</span>
              <span>Running cost per year</span><span>{draft.annual_running_cost_inr === null ? missing : formatINR(draft.annual_running_cost_inr)}</span>
              <span>Expected benefit per year</span><span>{formatINR(draft.annual_benefit_inr)}</span>
              <span>Benefit type</span><span>{draft.benefit_type}</span>
              <span>Overlap group</span><span>{draft.overlap_group || missing}</span>
              <span>Expected adoption</span><span>{draft.adoption_pct === null ? missing : formatPct(draft.adoption_pct)}</span>
              <span>Data readiness</span><span style={{ textTransform: 'capitalize' }}>{draft.readiness ?? missing}</span>
              <span>Measured adoption</span><span>{draft.observed_adoption_pct === null ? missing : formatPct(draft.observed_adoption_pct)}</span>
              <span>Measured quality</span><span>{draft.observed_quality_pct === null ? missing : formatPct(draft.observed_quality_pct)}</span>
            </div>
            <p className="caption">Anything marked “Not provided” stays unknown. If the engine needs it, it applies its own default and labels the result “assumed”.</p>
          </div>
          <div className="dlg-foot">
            <span className="grow" />
            <button className="btn" onClick={() => setStep('form')}>Back</button>
            <button className="btn btn-primary" onClick={() => onSubmit(draft, editing?.id ?? null)}>{editing ? 'Save changes' : 'Add to scenario'}</button>
          </div>
        </div>
      )}
    </Modal>
  )
}
