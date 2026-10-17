import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { CreatePlanResponse, PilotPlan, PlanProgress, ReallocationResponse } from '../../api/types'
import { formatINR, formatPct } from '../../domain/format'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField, TextField } from '../../components/ui/Fields'
import { Status } from '../../components/ui/Bits'

/** Phase 6 tracker: 30-day plans, expected-vs-actual progress, status moves and the
 *  reallocation recommendation when a pilot is stopped. Recommendations never auto-apply. */
export function Tracker() {
  const [plans, setPlans] = useState<PilotPlan[] | null>(null)
  const [pilotIds, setPilotIds] = useState<string[]>([])
  const [err, setErr] = useState<unknown>(null)

  const [selected, setSelected] = useState<string>('')
  const [progress, setProgress] = useState<PlanProgress | null>(null)
  const [progressErr, setProgressErr] = useState<unknown>(null)

  const [reallocation, setReallocation] = useState<ReallocationResponse | null>(null)
  const [reallocBusy, setReallocBusy] = useState(false)
  const [reallocErr, setReallocErr] = useState<unknown>(null)

  const load = async () => {
    setErr(null)
    try {
      setPlans(await api.listPlans())
    } catch (e) { setErr(e) }
  }
  useEffect(() => {
    void load()
    void (async () => {
      try {
        const doc = (await api.defaults()).data
        setPilotIds(doc.starting_state.records.pilots.map((p) => p.id))
      } catch { /* the create form simply stays empty; list still renders */ }
    })()
  }, [])

  const open = async (pid: string) => {
    setSelected(pid); setProgress(null); setProgressErr(null); setReallocation(null)
    try { setProgress((await api.planProgress(pid)).data) } catch (e) { setProgressErr(e) }
  }
  const move = async (status: PilotPlan['status']) => {
    if (!selected) return
    try {
      const saved = (await api.setPlanStatus(selected, status)).data
      setProgress((pr) => pr ? { ...pr, plan: { ...pr.plan, status: saved.status } } : pr)
      await load()
    } catch (e) { setProgressErr(e) }
  }
  const doRealloc = async () => {
    if (!selected) return
    setReallocBusy(true); setReallocErr(null)
    try { setReallocation((await api.realloc({ pilot_id: selected })).data) } catch (e) { setReallocErr(e) } finally { setReallocBusy(false) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <NewPlan created={(pid) => { void load(); void open(pid) }} pilotIds={pilotIds} />
      {err != null && <ErrorNotice title="The plans could not be loaded." error={err} onRetry={() => void load()} />}
      {plans?.length === 0 && <p className="muted">No plans yet — generate one above for a funded pilot.</p>}
      {plans && plans.length > 0 && (
        <div className="panel">
          <h3 className="h3">Recorded plans</h3>
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">30-day plans</caption>
            <thead><tr>
              <th scope="col">Pilot</th><th scope="col">Objective</th><th scope="col">Owner</th><th scope="col">Status</th>
              <th scope="col">Targets (adoption / quality)</th><th scope="col">Cost limit</th><th scope="col">Duration</th><th scope="col">Open</th>
            </tr></thead>
            <tbody>{plans.map((pl) => (
              <tr key={pl.pilot_id}>
                <td className="strong mono">{pl.pilot_id}</td>
                <td>{pl.objective}</td>
                <td>{pl.owner ?? <span className="tag">not assigned</span>}</td>
                <td><Status tone={statusTone(pl.status)}>{pl.status}</Status></td>
                <td>{pl.adoption_target_pct === null ? 'unknown' : formatPct(pl.adoption_target_pct)} / {pl.quality_target_pct === null ? 'unknown' : formatPct(pl.quality_target_pct)}</td>
                <td>{pl.cost_limit_inr === null ? 'unknown' : formatINR(pl.cost_limit_inr)}</td>
                <td>{pl.duration_days} days</td>
                <td><button className="btn btn-sm" onClick={() => void open(pl.pilot_id)} aria-pressed={selected === pl.pilot_id}>{selected === pl.pilot_id ? 'Open' : 'Open'}</button></td>
              </tr>
            ))}</tbody>
          </table></div>
        </div>
      )}

      {selected && progressErr != null && <ErrorNotice title="The plan progress could not be loaded." error={progressErr} />}
      {selected && progress && (
        <div className="panel" data-testid="plan-progress">
          <h3 className="h3">Tracker — {selected} ({progress.plan.status})</h3>
          <p className="hint">{progress.plan.stop_rule}</p>
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Expected versus actual for {selected}</caption>
            <thead><tr><th scope="col">Measure</th><th scope="col">Expected (plan)</th><th scope="col">Actual (observation)</th><th scope="col">Gap</th></tr></thead>
            <tbody>
              <tr><th scope="row">Adoption</th>
                <td>{progress.plan.adoption_target_pct === null ? 'unknown' : formatPct(progress.plan.adoption_target_pct)}</td>
                <td>{progress.actual?.adoption_pct == null ? <span className="tag">not recorded</span> : formatPct(progress.actual.adoption_pct)}</td>
                <td>{progress.gaps.adoption_gap_pct === undefined ? '—' : progress.gaps.adoption_gap_pct >= 0 ? `+${progress.gaps.adoption_gap_pct.toFixed(1)} pp` : `${progress.gaps.adoption_gap_pct.toFixed(1)} pp`}</td></tr>
              <tr><th scope="row">Quality</th>
                <td>{progress.plan.quality_target_pct === null ? 'unknown' : formatPct(progress.plan.quality_target_pct)}</td>
                <td>{progress.actual?.quality_pct == null ? <span className="tag">not recorded</span> : formatPct(progress.actual.quality_pct)}</td>
                <td>{progress.gaps.quality_gap_pct === undefined ? '—' : progress.gaps.quality_gap_pct >= 0 ? `+${progress.gaps.quality_gap_pct.toFixed(1)} pp` : `${progress.gaps.quality_gap_pct.toFixed(1)} pp`}</td></tr>
              <tr><th scope="row">Cost limit</th>
                <td colSpan={2}>{progress.plan.cost_limit_inr === null ? 'unknown' : formatINR(progress.plan.cost_limit_inr)} <span className="hint"> {progress.plan.duration_days} days</span></td>
                <td>actual spend comes from the recorded cost lines</td></tr>
            </tbody>
          </table></div>
          {progress.verdict && (
            <>
              <p className="hint">Verdict with actuals: <span className="strong">checkpoint {progress.verdict.checkpoint}</span> · stop rule <span className="strong">{progress.verdict.stop_rule.decision}</span></p>
              <p className="hint">{progress.verdict.reasons[0]}</p>
            </>
          )}
          <div className="row" style={{ gap: 8, flexWrap: 'wrap' }}>
            <span className="hint">Move status:</span>
            {(['active', 'revise', 'stopped', 'completed'] as const).map((st) => (
              <button key={st} className="btn btn-sm" disabled={progress.plan.status === st} onClick={() => void move(st)}>{st}</button>
            ))}
          </div>
          <div className="stack" style={{ gap: 8, marginTop: 12 }}>
            <button className="btn" onClick={() => void doRealloc()} disabled={reallocBusy}>{reallocBusy ? 'Working…' : `Reallocation preview for ${selected}`}</button>
            {reallocErr != null && <ErrorNotice title="The reallocation could not be computed." error={reallocErr} />}
            {reallocation && (
              <div data-testid="reallocation">
                <p className="hint"><span className="strong">{reallocation.message}</span></p>
                <p className="hint">
                  Freed {formatINR(reallocation.freed_budget_inr)} · locked {reallocation.locked_funded.join(', ') || 'none'} ·
                  remaining budget {reallocation.remaining_budget_inr === null ? 'unknown' : formatINR(reallocation.remaining_budget_inr)} ·
                  suggested: {reallocation.reallocation?.selected.join(', ') || 'none'}.
                </p>
                <p className="hint">{reallocation.note.text} Accept via <span className="mono">{reallocation.note.accept_via}</span>.</p>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function statusTone(s: PilotPlan['status']) {
  return s === 'active' ? 'accent' : s === 'revise' ? 'warn' : s === 'stopped' ? 'bad' : s === 'completed' ? 'ok' : 'off'
}

function NewPlan({ created, pilotIds }: { created: (pilotId: string) => void; pilotIds: string[] }) {
  const [pilot, setPilot] = useState(pilotIds[0] ?? '')
  const [owner, setOwner] = useState('')
  const [days, setDays] = useState('30')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState<CreatePlanResponse | null>(null)
  const [err, setErr] = useState<unknown>(null)

  useEffect(() => { if (!pilot && pilotIds.length > 0) setPilot(pilotIds[0]) }, [pilotIds])
  useEffect(() => { setDone(null) }, [pilot, owner, days])

  const create = async () => {
    setBusy(true); setErr(null)
    try {
      const r = (await api.createPlan({ pilot_id: pilot, owner: owner.trim() === '' ? undefined : owner.trim(), duration_days: days.trim() === '' ? undefined : Number(days) })).data
      setDone(r); created(r.plan.pilot_id)
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h3 className="h3">Generate a 30-day plan</h3>
      <p className="hint">Created when a pilot is funded; actuals come from the observations recorded for that pilot. Regenerating replaces the current plan.</p>
      <div className="row" style={{ flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
        {pilotIds.length > 0 ? (
          <div style={{ minWidth: 280 }}>
            <SelectField id="tp-pilot" label="Pilot" value={pilot} onChange={setPilot} required options={pilotIds.map((id) => ({ value: id, label: id }))} />
          </div>
        ) : <p className="muted">Scenario could not be loaded; type the pilot id instead.</p>}
        <div style={{ minWidth: 160 }}>
          <TextField id="tp-owner" label="Owner" value={owner} onChange={setOwner} placeholder="service owner" />
        </div>
        <div style={{ minWidth: 140 }}>
          <TextField id="tp-days" label="Duration (days)" value={days} onChange={setDays} inputMode="numeric" hint="1–365" />
        </div>
        <button className="btn btn-primary" onClick={() => void create()} disabled={busy || pilotIds.length === 0}>{busy ? 'Working…' : 'Generate plan'}</button>
      </div>
      {err != null && <ErrorNotice title="The plan could not be recorded." error={err} />}
      {done && (
        <p className="hint" role="status">
          Plan saved for <span className="mono">{done.plan.pilot_id}</span>: targets {formatPct(done.plan.adoption_target_pct ?? 0)} adoption / {formatPct(done.plan.quality_target_pct ?? 0)} quality,
          cost limit {formatINR(done.plan.cost_limit_inr ?? 0)} ({done.cost_limit_note}).
        </p>
      )}
    </div>
  )
}
