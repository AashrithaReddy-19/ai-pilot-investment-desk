import { useCallback, useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { ObservationResult, PilotHistory, StopRule } from '../../api/types'
import { PILOT_ID_RE, parseAmount } from '../../domain/draft'
import { formatDate, formatDateTime, formatPct } from '../../domain/format'
import { overridesOf, scenarioPilots, useWorkspace } from '../../state/useWorkspace'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextField } from '../../components/ui/Fields'

/** Reads a percentage. null = left blank (not measured), undefined = invalid. */
function pct(s: string): number | null | undefined {
  const t = s.trim()
  if (t === '') return null
  const n = parseAmount(t)
  return Number.isFinite(n) && n >= 0 && n <= 100 ? n : undefined
}

/**
 * Three separate things, kept visibly apart:
 *  1. Preview a stop-rule decision (POST /stoprule/:id). Stores nothing.
 *  2. Record a real measurement (POST /pilots/:id/observations). Stores the observation and the decision it produced.
 *  3. History of what was recorded (GET /pilots/:id/history).
 */
export function ObservationTools() {
  const s = useWorkspace()
  const { source, drafts } = s
  const pilots = scenarioPilots({ source, drafts })
  const overrides = overridesOf(source, drafts) ?? {}
  const [pid, setPid] = useState('')
  const [adoption, setAdoption] = useState('')
  const [quality, setQuality] = useState('')
  const [src, setSrc] = useState('')
  const [errs, setErrs] = useState<{ pid?: string; adoption?: string; quality?: string; values?: string; src?: string }>({})
  const [preview, setPreview] = useState<{ id: string; rule: StopRule } | null>(null)
  const [recorded, setRecorded] = useState<ObservationResult | null>(null)
  const [hist, setHist] = useState<{ id: string; data: PilotHistory | null; error: unknown; loading: boolean } | null>(null)
  const [busy, setBusy] = useState<'preview' | 'record' | null>(null)
  const [fail, setFail] = useState<unknown>(null)

  useEffect(() => { if (!pid && pilots[0]) setPid(pilots[0].id) }, [pilots, pid])

  const validate = (): { id: string; a: number | null; q: number | null } | null => {
    const e: typeof errs = {}
    const id = pid.trim()
    if (!PILOT_ID_RE.test(id)) e.pid = 'Use 1 to 40 letters, digits, dots, hyphens or underscores.'
    const a = pct(adoption), q = pct(quality)
    if (a === undefined) e.adoption = 'Adoption must be between 0 and 100.'
    if (q === undefined) e.quality = 'Quality must be between 0 and 100.'
    if (a === null && q === null) e.values = 'Enter adoption, quality, or both.'
    if (src.trim().length > 100) e.src = 'Keep the source to 100 characters.'
    setErrs(e)
    return Object.keys(e).length || a === undefined || q === undefined ? null : { id, a, q }
  }
  const body = (a: number | null, q: number | null) => ({ ...overrides, ...(a !== null && { observed_adoption_pct: a }), ...(q !== null && { observed_quality_pct: q }) })

  const loadHistory = useCallback(async (id: string) => {
    setHist({ id, data: null, error: null, loading: true })
    try { setHist({ id, data: (await api.history(id, 50)).data, error: null, loading: false }) }
    catch (e) { setHist({ id, data: null, error: e, loading: false }) }
  }, [])

  const doPreview = async () => {
    const v = validate(); if (!v) return
    setBusy('preview'); setFail(null); setRecorded(null)
    try { setPreview({ id: v.id, rule: (await api.stopRule(v.id, body(v.a, v.q))).data }) }
    catch (e) { setPreview(null); setFail(e) }
    finally { setBusy(null) }
  }
  const doRecord = async () => {
    const v = validate(); if (!v) return
    setBusy('record'); setFail(null); setPreview(null)
    try {
      const r = (await api.recordObservation(v.id, { ...body(v.a, v.q), ...(src.trim() && { source: src.trim() }) })).data
      setRecorded(r)
      await loadHistory(v.id)
    } catch (e) { setRecorded(null); setFail(e) }
    finally { setBusy(null) }
  }

  const h = hist?.data
  return (
    <div className="stack" style={{ gap: 28 }}>
      {pilots.length === 0 && <div className="notice notice-info" role="note"><div>No pilots are loaded in the decision workspace, so there are no pilot IDs to pick from. Add pilots there to use their IDs.</div></div>}

      <div className="split">
        <form className="stack" onSubmit={(e) => e.preventDefault()} noValidate>
          <div className="field">
            <label className="label" htmlFor="obs-pid">Pilot ID<span className="req req-yes">required</span></label>
            <input id="obs-pid" className="input" list="obs-pilots" value={pid} onChange={(e) => { setPid(e.target.value); setErrs((x) => ({ ...x, pid: undefined })); setPreview(null); setRecorded(null) }} autoComplete="off"
              aria-invalid={errs.pid ? true : undefined} aria-describedby={errs.pid ? 'obs-pid-msg' : undefined} />
            <datalist id="obs-pilots">{pilots.map((p) => <option key={p.id} value={p.id}>{p.task}</option>)}</datalist>
            {errs.pid && <span id="obs-pid-msg" className="field-error" role="alert">{errs.pid}</span>}
          </div>
          <div className="form-grid">
            <TextField id="obs-a" label="Measured adoption" unit="%" inputMode="decimal" value={adoption} onChange={(v) => { setAdoption(v); setErrs((x) => ({ ...x, adoption: undefined, values: undefined })) }} error={errs.adoption} />
            <TextField id="obs-q" label="Measured quality" unit="%" inputMode="decimal" value={quality} onChange={(v) => { setQuality(v); setErrs((x) => ({ ...x, quality: undefined, values: undefined })) }} error={errs.quality} />
          </div>
          {errs.values && <span className="field-error" role="alert">{errs.values}</span>}
          <TextField id="obs-src" label="Source of the measurement" value={src} onChange={setSrc} error={errs.src} maxLength={100} hint="For example, “Q3 usage report”. Recorded with the observation." />
          <div className="row">
            <button className="btn" onClick={() => void doPreview()} disabled={busy !== null}>{busy === 'preview' ? 'Previewing…' : 'Preview decision'}</button>
            <button className="btn btn-primary" onClick={() => void doRecord()} disabled={busy !== null}>{busy === 'record' ? 'Recording…' : 'Record observation'}</button>
          </div>
          <p className="caption"><strong>Preview</strong> shows what the stop rule would decide and stores nothing. <strong>Record</strong> saves the measurement and the decision it produced in the database.</p>
        </form>

        <div className="stack" aria-live="polite">
          {fail != null && <ErrorNotice title="That did not work." error={fail} />}
          {!preview && !recorded && fail == null && <Empty title="No decision yet">Enter what was measured, then preview the decision or record it.</Empty>}
          {preview && (
            <div className="panel">
              <p className="eyebrow">Preview only · nothing was recorded</p>
              <RuleView rule={preview.rule} />
            </div>
          )}
          {recorded && (
            <div className="panel">
              <div className="notice notice-ok"><div><strong>Recorded.</strong> The service confirmed observation {recorded.observation.id} for {recorded.observation.pilot_id} on {formatDate(recorded.observation.observed_on)}.</div></div>
              <RuleView rule={recorded.stop_rule} />
            </div>
          )}
        </div>
      </div>

      <div className="stack">
        <div className="row" style={{ justifyContent: 'space-between' }}>
          <h3 className="h3">History {hist ? `for ${hist.id}` : ''}</h3>
          <button className="btn btn-sm" onClick={() => { const id = pid.trim(); if (PILOT_ID_RE.test(id)) void loadHistory(id); else setErrs((x) => ({ ...x, pid: 'Enter a valid pilot ID first.' })) }}>Show history</button>
        </div>
        {!hist && <p className="caption">History lists what has been recorded for a pilot ID. It is stored by ID only, so the same ID in another scenario shares one history.</p>}
        {hist?.loading && <Skeleton rows={2} />}
        {hist?.error != null && <ErrorNotice title="History could not be loaded." error={hist.error} onRetry={() => void loadHistory(hist.id)} />}
        {h && (
          (h.observations ?? []).length === 0 && (h.decisions ?? []).length === 0
            ? <Empty title="No observations recorded">Nothing has been recorded for {hist!.id} yet.</Empty>
            : (
              <div className="split">
                <div className="tbl-wrap"><table className="tbl">
                  <caption className="sr">Recorded observations</caption>
                  <thead><tr><th scope="col">Observed</th><th className="num" scope="col">Adoption</th><th className="num" scope="col">Quality</th><th scope="col">Source</th></tr></thead>
                  <tbody>{(h.observations ?? []).map((o) => <tr key={o.id}><td>{formatDate(o.observed_on)}</td><td className="num">{formatPct(o.adoption_pct)}</td><td className="num">{formatPct(o.quality_pct)}</td><td>{o.source ?? <span className="muted">Not provided</span>}</td></tr>)}</tbody>
                </table></div>
                <div className="tbl-wrap"><table className="tbl">
                  <caption className="sr">Stop-rule decisions that followed</caption>
                  <thead><tr><th scope="col">Decision</th><th scope="col">Reason</th><th scope="col">Recorded</th></tr></thead>
                  <tbody>{(h.decisions ?? []).map((d) => <tr key={d.id}><td><span className={`decision decision-${d.decision}`}>{d.decision}</span></td><td className="wrap-cell">{d.reason}</td><td className="caption">{formatDateTime(d.created_at)}</td></tr>)}</tbody>
                </table></div>
              </div>
            )
        )}
      </div>
    </div>
  )
}

function RuleView({ rule }: { rule: StopRule }) {
  return (
    <div className="stack" style={{ gap: 10 }}>
      <p><span className={`decision decision-${rule.decision}`} style={{ fontSize: 'var(--t-md)' }}>{rule.decision}</span></p>
      <p>{rule.reasons.join(' ')}</p>
      <div className="kv" style={{ maxWidth: 380 }}>
        <span>Measure tested</span><span>{rule.metric}</span>
        <span>Threshold</span><span>{rule.threshold === null ? 'Not applicable' : rule.threshold}</span>
        <span>Observed value</span><span>{rule.actual === null ? 'Not applicable' : rule.actual}</span>
      </div>
      <p className="caption">A deterministic rule applied to the numbers entered.</p>
    </div>
  )
}
