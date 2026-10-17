import { useState } from 'react'
import { api, saveDownload } from '../../api/client'
import { overridesOf, useWorkspace, currentOptKey } from '../../state/useWorkspace'
import { formatINR } from '../../domain/format'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextField } from '../../components/ui/Fields'

const NAME_RE = /^[A-Za-z0-9_-]{1,40}$/

/** Saves the optimized portfolio shown in stage 3 through POST /api/v1/portfolios, and offers the service's own exports. */
export function SavePortfolio({ onSaved }: { onSaved?: () => void }) {
  const s = useWorkspace()
  const { source, drafts, optimization: run, mode } = s
  const current = run.status === 'done' && run.key === currentOptKey(s) && run.data
  const pf = current ? run.data!.portfolio : null
  const [name, setName] = useState('')
  const [nameError, setNameError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [confirmOverwrite, setConfirmOverwrite] = useState(false)
  const [saved, setSaved] = useState<string | null>(null)
  const [fail, setFail] = useState<unknown>(null)
  const [dl, setDl] = useState<'csv' | 'evidence' | null>(null)
  const [dlFail, setDlFail] = useState<unknown>(null)
  const [dlOk, setDlOk] = useState<string | null>(null)

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaved(null); setFail(null)
    const o = overridesOf(source, drafts)
    if (!pf || !o) return
    if (!NAME_RE.test(name)) { setNameError('Use 1 to 40 letters, digits, hyphens or underscores. No spaces.'); return }
    setNameError(null); setSaving(true)
    try {
      if (!confirmOverwrite) {
        const existing = await api.listPortfolios()
        if (existing.some((p) => p.name === name)) { setConfirmOverwrite(true); setSaving(false); return }
      }
      const r = await api.savePortfolio({ ...o, mode, budget: pf.budget, name })
      setSaved(r.data.name); setConfirmOverwrite(false)
      onSaved?.()
    } catch (err) { setFail(err) }
    finally { setSaving(false) }
  }

  const download = async (kind: 'csv' | 'evidence') => {
    const o = overridesOf(source, drafts)
    if (!o) return
    setDl(kind); setDlFail(null); setDlOk(null)
    try {
      const body = { ...o, mode, ...(pf ? { budget: pf.budget } : {}) }
      const d = kind === 'csv' ? await api.exportCsv(body) : await api.exportEvidence(body)
      saveDownload(d)
      setDlOk(`Downloaded ${d.filename}.`)
    } catch (err) { setDlFail(err) }
    finally { setDl(null) }
  }

  return (
    <div className="split">
      <form className="stack" onSubmit={save} noValidate>
        <h3 className="h3">Save this portfolio</h3>
        <p className="caption">Saving stores the pilots used, the budget and benefit view, and the evidence the engine returned, in the database under a name you choose.</p>
        {pf ? (
          <div className="kv" style={{ maxWidth: 400 }}>
            <span>Benefit view</span><span style={{ textTransform: 'capitalize' }}>{pf.mode}</span>
            <span>Budget</span><span>{formatINR(pf.budget)}</span>
            <span>Funded</span><span>{pf.selected.length ? pf.selected.join(', ') : 'None'}</span>
            <span>Projected net / yr</span><span>{pf.selected.length ? formatINR(pf.total_net) : 'Not provided'}</span>
          </div>
        ) : <div className="notice notice-info"><div>Optimize a portfolio in stage 3 first, so what is saved matches what you reviewed.</div></div>}
        <TextField id="pf-name" label="Portfolio name" required value={name} onChange={(v) => { setName(v); setNameError(null); setConfirmOverwrite(false); setSaved(null) }} error={nameError ?? undefined} placeholder="q3-pilot-review" hint="Letters, digits, hyphens and underscores." />
        <div aria-live="polite" className="stack" style={{ gap: 8 }}>
          {confirmOverwrite && <div className="notice notice-warn" role="alert"><div>A portfolio named “{name}” already exists. Saving again replaces it. Press the button once more to confirm.</div></div>}
          {fail != null && <ErrorNotice title="Not saved." error={fail} />}
          {saved && <div className="notice notice-ok"><div>Saved as “{saved}”. The service confirmed it. Open it under Saved portfolios.</div></div>}
        </div>
        <div><button className="btn btn-primary" type="submit" disabled={!pf || saving}>{saving ? 'Saving…' : confirmOverwrite ? 'Replace saved portfolio' : 'Save portfolio'}</button></div>
      </form>

      <div className="stack">
        <h3 className="h3">Take the evidence with you</h3>
        <p className="caption">Both files are produced by the rules engine for the scenario on screen.</p>
        <div className="row">
          <button className="btn" onClick={() => void download('evidence')} disabled={!overridesOf(source, drafts) || dl !== null}>{dl === 'evidence' ? 'Preparing…' : 'Download evidence report'}</button>
          <button className="btn" onClick={() => void download('csv')} disabled={!overridesOf(source, drafts) || dl !== null}>{dl === 'csv' ? 'Preparing…' : 'Download pilot table (CSV)'}</button>
        </div>
        <div aria-live="polite">
          {dlFail != null && <ErrorNotice title="Download failed." error={dlFail} />}
          {dlOk && <p className="caption">{dlOk}</p>}
        </div>
      </div>
    </div>
  )
}
