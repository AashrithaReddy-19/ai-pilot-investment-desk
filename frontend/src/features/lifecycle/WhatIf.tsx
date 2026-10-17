import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { Readiness, ScenarioBody, SweepParameter, WhatIfOverrides, WhatIfResponse, SweepResponse } from '../../api/types'
import { formatINR, formatPayback, formatPct } from '../../domain/format'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField, TextField } from '../../components/ui/Fields'

const READINESS: { value: Readiness; label: string }[] = [
  { value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' },
]
const SWEEP_PARAMS: { value: SweepParameter; label: string }[] = [
  { value: 'adoption_pct', label: 'Adoption % (need a pilot)' },
  { value: 'annual_benefit_inr', label: 'Annual benefit ×0.5–1.5 (need a pilot)' },
  { value: 'annual_running_cost_inr', label: 'Running cost ×0.5–1.5 (need a pilot)' },
  { value: 'price_per_1m_tokens', label: 'Token price ×0.5–1.5 (org level)' },
  { value: 'annual_usage_growth_pct', label: 'Usage growth −20%…+30% (org level)' },
]
const PILOT_LEVEL: SweepParameter[] = ['adoption_pct', 'annual_benefit_inr', 'annual_running_cost_inr']

/** Text inputs for the numeric override fields; '' stays "not overridden". */
type Strs = Record<string, string>
const num = (v?: string): number | undefined => (v === undefined || v.trim() === '' ? undefined : Number(v))

export function WhatIf() {
  const [scenario, setScenario] = useState<ScenarioBody | null>(null)
  const [pilotId, setPilotId] = useState('')
  const [ready, setReady] = useState(false)
  const [loadErr, setLoadErr] = useState<unknown>(null)

  useEffect(() => { void load() }, [])
  const load = async () => {
    setLoadErr(null); setReady(false)
    try {
      const doc = (await api.defaults()).data
      const body: ScenarioBody = { fixture: doc.starting_state, extension: doc.extension }
      setScenario(body)
      const first = doc.starting_state.records.pilots[0]
      if (first) setPilotId(first.id)
      setReady(true)
    } catch (e) { setLoadErr(e) }
  }

  return ready && scenario
    ? <WhatIfInner scenario={scenario} pilotId={pilotId} setPilotId={setPilotId} />
    : loadErr != null
      ? <ErrorNotice title="The default scenario could not be loaded." error={loadErr} onRetry={() => void load()} />
      : <p className="muted">Loading the shipped scenario…</p>
}

function WhatIfInner({ scenario, pilotId, setPilotId }: { scenario: ScenarioBody; pilotId: string; setPilotId: (v: string) => void }) {
  const pilots = scenario.fixture?.records?.pilots ?? []
  const [o, setO] = useState<Strs>({})
  const [res, setRes] = useState<WhatIfResponse | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)

  const [param, setParam] = useState<SweepParameter>('adoption_pct')
  const [sweepRes, setSweepRes] = useState<SweepResponse | null>(null)
  const [sweepBusy, setSweepBusy] = useState(false)
  const [sweepErr, setSweepErr] = useState<unknown>(null)

  const set = (k: string) => (v: string) => setO((prev) => ({ ...prev, [k]: v }))

  const overridesOf = (): WhatIfOverrides => {
    const x: WhatIfOverrides = {}
    const put = (k: keyof WhatIfOverrides, v?: number) => { if (v !== undefined && !Number.isNaN(v)) { (x as Record<string, unknown>)[k] = v } }
    put('setup_cost_inr', num(o.setup_cost_inr))
    if ((o.annual_running_cost_inr ?? '').trim() !== '') {
      const t = o.annual_running_cost_inr.trim().toLowerCase()
      x.annual_running_cost_inr = t === 'null' || t === 'none' ? null : num(o.annual_running_cost_inr) ?? null
    }
    put('annual_benefit_inr', num(o.annual_benefit_inr))
    put('adoption_pct', num(o.adoption_pct))
    if (o.readiness !== undefined && o.readiness !== '') (x as Record<string, unknown>).readiness = o.readiness
    put('observed_adoption_pct', num(o.observed_adoption_pct))
    put('observed_quality_pct', num(o.observed_quality_pct))
    put('ramp_months', num(o.ramp_months))
    put('delay_months', num(o.delay_months))
    put('budget', num(o.budget))
    put('horizon_months', num(o.horizon_months))
    put('discount_rate', num(o.discount_rate))
    put('tokens_per_month', num(o.tokens_per_month))
    put('price_per_1m_tokens', num(o.price_per_1m_tokens))
    put('annual_usage_growth_pct', num(o.annual_usage_growth_pct))
    put('annual_price_drift_pct', num(o.annual_price_drift_pct))
    return x
  }

  const run = async () => {
    setBusy(true); setErr(null)
    try {
      setRes((await api.whatIf({ ...scenario, pilot_id: pilotId, overrides: overridesOf() })).data)
    } catch (e) { setErr(e); setRes(null) } finally { setBusy(false) }
  }
  const doSweep = async () => {
    setSweepBusy(true); setSweepErr(null)
    try {
      const body: Parameters<typeof api.sweep>[0] = { ...scenario, parameter: param }
      if (PILOT_LEVEL.includes(param) && pilotId) body.pilot_id = pilotId
      setSweepRes((await api.sweep(body)).data)
    } catch (e) { setSweepErr(e); setSweepRes(null) } finally { setSweepBusy(false) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <div className="panel">
        <h3 className="h3">Pilot and overrides</h3>
        <p className="hint">Defaults come from the shipped scenario. Leave a field empty to keep its current value; type
          <code> null</code> for the running cost to withdraw a recorded figure. Overrides never change stored data.</p>
        <div className="row" style={{ flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
          <div style={{ minWidth: 260 }}>
            <SelectField id="wi-pilot" label="Pilot" value={pilotId}
              onChange={setPilotId} required
              options={pilots.map((p) => ({ value: p.id, label: `${p.id} — ${p.task}` }))} />
          </div>
          <button className="btn btn-primary" onClick={() => void run()} disabled={busy || !pilotId}>{busy ? 'Working…' : 'Run what-if'}</button>
        </div>
        <div className="row" style={{ flexWrap: 'wrap', gap: 12 }}>
          <NumForm id="wi-setup" label="Setup cost (₹)" value={o.setup_cost_inr} onChange={set('setup_cost_inr')} />
          <NumForm id="wi-run" label="Annual running cost (₹)" value={o.annual_running_cost_inr} onChange={set('annual_running_cost_inr')} hint="null = withdraw a recorded figure" />
          <NumForm id="wi-benefit" label="Annual benefit (₹)" value={o.annual_benefit_inr} onChange={set('annual_benefit_inr')} />
          <NumForm id="wi-adopt" label="Adoption %" value={o.adoption_pct} onChange={set('adoption_pct')} />
          <NumForm id="wi-obs-adopt" label="Observed adoption %" value={o.observed_adoption_pct} onChange={set('observed_adoption_pct')} />
          <NumForm id="wi-obs-qual" label="Observed quality %" value={o.observed_quality_pct} onChange={set('observed_quality_pct')} />
          <NumForm id="wi-ramp" label="Ramp-up (months)" value={o.ramp_months} onChange={set('ramp_months')} />
          <NumForm id="wi-delay" label="Start delay (months)" value={o.delay_months} onChange={set('delay_months')} />
          <NumForm id="wi-budget" label="Budget (₹)" value={o.budget} onChange={set('budget')} />
          <NumForm id="wi-horizon" label="Horizon (months)" value={o.horizon_months} onChange={set('horizon_months')} />
          <NumForm id="wi-disc" label="Discount rate (%)" value={o.discount_rate} onChange={set('discount_rate')} />
          <NumForm id="wi-tokens" label="Tokens per month" value={o.tokens_per_month} onChange={set('tokens_per_month')} />
          <NumForm id="wi-price" label="Price per 1M tokens (₹)" value={o.price_per_1m_tokens} onChange={set('price_per_1m_tokens')} />
          <NumForm id="wi-growth" label="Usage growth (%/yr)" value={o.annual_usage_growth_pct} onChange={set('annual_usage_growth_pct')} />
          <NumForm id="wi-drift" label="Price drift (%/yr)" value={o.annual_price_drift_pct} onChange={set('annual_price_drift_pct')} />
          <div style={{ minWidth: 180 }}>
            <SelectField id="wi-readiness" label="Readiness" value={o.readiness ?? ''} onChange={set('readiness')}
              options={[{ value: '', label: 'Keep current' }, ...READINESS.map((r) => ({ value: r.value, label: r.label }))]} />
          </div>
        </div>
      </div>

      {err != null && <ErrorNotice title="The what-if could not be run." error={err} />}
      {res && (
        <div className="panel">
          <h3 className="h3">Base vs changed</h3>
          {res.verdict_flip && (
            <div className="notice notice-ok" role="status"><div>{res.flip_statement ?? `Verdict flips: ${res.verdict_flip.from} becomes ${res.verdict_flip.to}.`}</div></div>
          )}
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">What-if base against changed</caption>
            <thead><tr><th scope="col">Number</th><th scope="col">Base</th><th scope="col">Changed</th><th scope="col">Delta</th></tr></thead>
            <tbody>
              <Row label="Annual benefit (₹/yr)" base={res.base.benefit_inr} changed={res.changed.benefit_inr} delta={res.deltas.benefit_inr} />
              <Row label="Net annual (₹/yr)" base={res.base.net} changed={res.changed.net} delta={res.deltas.net} />
              <Row label="Payback" base={res.base.payback_months} changed={res.changed.payback_months} delta={res.deltas.payback_months} payback />
              <Row label="Profit over horizon (₹)" base={res.base.profit_inr} changed={res.changed.profit_inr} delta={res.deltas.profit_inr} />
              <Row label="ROI" base={res.base.roi_pct} changed={res.changed.roi_pct} delta={res.deltas.roi_pct} pct />
              <Row label="NPV (₹)" base={res.base.npv_inr} changed={res.changed.npv_inr} delta={res.deltas.npv_inr} />
              <Row label="Break-even month" base={res.base.breakeven_month} changed={res.changed.breakeven_month} delta={res.deltas.breakeven_month} />
              <tr><th scope="row">Verdict</th><td>{res.base.verdict}</td><td>{res.changed.verdict}</td><td /></tr>
            </tbody>
          </table></div>
          <p className="hint">{res.base.stop_rule.reasons[0]} → changed: {res.changed.stop_rule.reasons[0]}</p>
          <p className="hint">Portfolio effect on the changed view: {res.changed.portfolio.message}</p>
        </div>
      )}

      <div className="panel">
        <h3 className="h3">Sensitivity sweep</h3>
        <p className="hint">One variable at a time, deterministic steps, with a monotonicity check.</p>
        <div className="row" style={{ gap: 12, alignItems: 'flex-end' }}>
          <div style={{ minWidth: 320 }}>
            <SelectField id="wi-param" label="Parameter" value={param} onChange={(v) => setParam(v as SweepParameter)} options={SWEEP_PARAMS} />
          </div>
          <button className="btn" onClick={() => void doSweep()} disabled={sweepBusy || (PILOT_LEVEL.includes(param) && !pilotId)}>{sweepBusy ? 'Working…' : 'Run sweep'}</button>
        </div>
        {PILOT_LEVEL.includes(param) && !pilotId && <p className="hint">Pick a pilot above for this parameter.</p>}
        {sweepErr != null && <ErrorNotice title="The sweep could not be run." error={sweepErr} />}
        {sweepRes && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Sweep steps for {sweepRes.parameter}</caption>
            <thead><tr><th scope="col">Step</th><th scope="col">Net (₹/yr)</th><th scope="col">Payback</th><th scope="col">NPV (₹)</th></tr></thead>
            <tbody>{sweepRes.steps.map((s, i) => (
              <tr key={i}>
                <td>{paramValue(sweepRes.parameter, s.value)}</td>
                <td>{s.net === undefined || s.net === null ? '—' : formatINR(s.net)}</td>
                <td>{s.payback_months === undefined ? '—' : formatPayback(s.payback_months)}</td>
                <td>{s.npv_inr === undefined ? '—' : formatINR(s.npv_inr)}</td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
        {sweepRes && <p className="hint">Direction: {sweepRes.direction.replace('_', ' ')} · monotonic: {sweepRes.monotonic === null ? 'n/a' : sweepRes.monotonic ? 'yes' : 'no'}.</p>}
      </div>
    </div>
  )
}

function paramValue(p: SweepParameter, v: number): string {
  if (p === 'adoption_pct') return formatPct(v)
  if (p === 'annual_usage_growth_pct') return formatPct(v, 0)
  return `${v}`
}

function NumForm({ id, label, value, onChange, hint }: { id: string; label: string; value: string | undefined; onChange: (v: string) => void; hint?: string }) {
  return (
    <div style={{ minWidth: 170, flex: 0 }}>
      <TextField id={id} label={label} value={value ?? ''} onChange={onChange} inputMode="decimal" unit="" hint={hint} />
    </div>
  )
}

function Row({ label, base, changed, delta, payback, pct }: { label: string; base: number | null; changed: number | null; delta: number | null | undefined; payback?: boolean; pct?: boolean }) {
  const fmt = (n: number | null | undefined) => n === null || n === undefined ? '—' : payback ? formatPayback(n) : pct ? formatPct(n, 1) : formatINR(n)
  const fmtDelta = (n: number | null | undefined) => {
    if (n === null || n === undefined) return '—'
    const sign = n > 0 ? '+' : ''
    if (pct) return `${sign}${n.toFixed(1)} pp`
    if (payback) return `${sign}${n.toFixed(1)} mo`
    return `${sign}${formatINR(n)}`
  }
  return (
    <tr>
      <th scope="row">{label}</th>
      <td>{fmt(base)}</td>
      <td>{fmt(changed)}</td>
      <td>{fmtDelta(delta)}</td>
    </tr>
  )
}
