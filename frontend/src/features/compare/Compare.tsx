import { useState } from 'react'
import { api } from '../../api/client'
import type { BalanceSheetConfirm, CompareProposalRow, CompareResponse, Mode, PilotsImportParse } from '../../api/types'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { aiPlan } from '../../domain/statement'
import { BalancePanel } from './BalancePanel'
import { Stat, inr } from './shared'
import { SelectField, TextField } from '../../components/ui/Fields'

const num = (s: string): number | null => {
  if (s === '') return null
  const t = s.replace(/[,\s₹]/g, '')
  if (!Number.isFinite(Number(t))) return null
  return Number(t)
}
const TYPES = [
  { value: 'cashable', label: 'Cash savings' },
  { value: 'time freed', label: 'Time freed (capacity)' },
  { value: 'non-cashable', label: 'Non-cashable' },
]
const LINE_COLORS = ['#1d4ed8', '#b45309', '#15803d', '#9333ea', '#dc2626', '#0891b2', '#65a30d', '#db2777']

interface CfRow { name: string; setup: string; benefit: string; run: string; type: string; group: string }
const blankRow = (): CfRow => ({ name: '', setup: '', benefit: '', run: '', type: 'cashable', group: '' })

export { BalancePanel }

/** Compare proposals: balance-sheet burn beside a section-2 ranking made by the same rules engine. */
export function Compare() {
  const [burn, setBurn] = useState<BalanceSheetConfirm | null>(null)
  return (
    <div className="stack" style={{ gap: 20 }}>
      <BalancePanel onConfirmed={setBurn} />
      <ComparePanel burn={burn} />
    </div>
  )
}

/* -------------------------------------------------- CSV import + compare table */

function ComparePanel({ burn }: { burn: BalanceSheetConfirm | null }) {
  const [rows, setRows] = useState<CfRow[]>([blankRow(), blankRow(), blankRow()])
  const [budgetStr, setBudgetStr] = useState('')
  const [mode, setMode] = useState<Mode>('independent')
  const [result, setResult] = useState<CompareResponse | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)

  // CSV import review state
  const [impFile, setImpFile] = useState<File | null>(null)
  const [imp, setImp] = useState<PilotsImportParse | null>(null)
  const [keep, setKeep] = useState<Set<string>>(new Set())
  const [impBusy, setImpBusy] = useState(false)
  const [impErr, setImpErr] = useState<unknown>(null)
  const [impMsg, setImpMsg] = useState<string | null>(null)

  const setCell = (i: number, patch: Partial<CfRow>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...patch } : r)))

  const doImpParse = async () => {
    if (!impFile) return
    setImpBusy(true); setImpErr(null); setImp(null); setImpMsg(null)
    try {
      const p = (await api.importPilotsParse(impFile)).data
      setImp(p); setKeep(new Set(p.pilots.map((x) => x.id)))
    } catch (e) { setImpErr(e) } finally { setImpBusy(false) }
  }
  const doImpConfirm = async () => {
    if (!impFile || keep.size === 0) return
    setImpBusy(true); setImpErr(null)
    try {
      const c = (await api.importPilotsConfirm(impFile, [...keep])).data
      setRows(c.pilots.map((p) => ({
        name: p.id, setup: String(p.setup_cost_inr), benefit: String(p.annual_benefit_inr),
        run: p.annual_running_cost_inr === undefined ? '' : String(p.annual_running_cost_inr),
        type: p.benefit_type ?? 'cashable', group: p.overlap_group ?? '',
      })))
      setImpMsg(`Imported ${c.pilots.length} proposal(s) into the comparison. Review the numbers below — they are not stored anywhere.`)
      setImp(null); setKeep(new Set())
    } catch (e) { setImpErr(e) } finally { setImpBusy(false) }
  }

  const compare = async () => {
    setErr(null); setResult(null)
    const filled = rows.filter((r) => r.name.trim() !== '' || r.setup.trim() !== '' || r.benefit.trim() !== '')
    if (filled.length < 2) { setErr(new Error('Compare needs 2 to 8 proposals with a name; the plan’s worked example has three. Import a CSV or fill the rows.')); return }
    const names = filled.map((r) => r.name.trim())
    const dup = names.filter((v, i) => names.indexOf(v) !== i)
    if (names.some((n) => n === '')) { setErr(new Error('Every proposal needs a name (or remove the empty row).')); return }
    if (dup.length) { setErr(new Error(`Proposal names must be unique: ${dup.join(', ')}.`)); return }
    for (const r of filled) {
      if (num(r.setup) === null || num(r.setup)! < 0 || num(r.benefit) === null || num(r.benefit)! < 0) {
        setErr(new Error(`One-time cost and yearly savings must be non-negative numbers (check ${r.name.trim()}).`)); return
      }
      if (r.run.trim() !== '' && (num(r.run) === null || num(r.run)! < 0)) {
        setErr(new Error(`AI running cost must be a non-negative number or blank for the engine to assume it (check ${r.name.trim()}).`)); return
      }
    }
    const budget = budgetStr.trim() === '' ? null : num(budgetStr)
    if (budgetStr.trim() !== '' && (budget === null || budget! < 0)) { setErr(new Error('Budget must be a non-negative INR amount, or blank for no budget limit.')); return }
    setBusy(true)
    try {
      setResult((await api.compare({
        mode,
        proposals: filled.map((r) => ({
          name: r.name.trim(), setup_cost_inr: num(r.setup)!, annual_benefit_inr: num(r.benefit)!,
          ai_running_cost_inr: r.run.trim() === '' ? null : num(r.run), benefit_type: r.type,
          ...(r.group.trim() ? { overlap_group: r.group.trim() } : {}),
        })),
        ...(budget === null ? {} : { budget }),
      })).data)
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  return (
    <div className="panel">
      <h3 className="h3">Import proposals from CSV</h3>
      <p className="hint">Headers are flexible (id, task, setup_cost, savings or benefit, running cost, type, adoption, readiness, ramp, delay, overlap group).
        Rows with problems are listed for review; duplicate ids are blocked. Nothing is stored — confirming sends the file again.</p>
      <div className="row" style={{ flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
        <input type="file" accept=".csv,text/csv" onChange={(e) => { setImpFile(e.target.files?.[0] ?? null); setImp(null); setImpMsg(null) }} />
        <button className="btn" onClick={() => void doImpParse()} disabled={!impFile || impBusy}>{impBusy ? 'Working…' : 'Review CSV'}</button>
        {imp && imp.valid_rows > 0 && (
          <button className="btn btn-primary" onClick={() => void doImpConfirm()} disabled={impBusy || keep.size === 0}>Import {keep.size} selected</button>
        )}
      </div>
      {impErr != null && <ErrorNotice title="The CSV could not be read." error={impErr} />}
      {impMsg && <div className="notice notice-ok"><div>{impMsg}</div></div>}
      {imp && (
        <>
          {imp.error_rows > 0 && <p className="hint">{imp.error_rows} of {imp.rows} rows need fixing before they can be imported:</p>}
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">CSV import review</caption>
            <thead><tr><th scope="col">Line</th><th scope="col">Id</th><th scope="col">Task</th><th scope="col">Setup</th><th scope="col">Yearly savings</th><th scope="col">Import?</th><th scope="col">Problems</th></tr></thead>
            <tbody>{imp.records.map((r) => (
              <tr key={r.line}>
                <td className="mono">{r.line}</td>
                <td className={r.pilot ? 'strong' : 'muted'}>{r.pilot?.id ?? '—'}</td>
                <td>{r.pilot?.task ?? '—'}</td>
                <td>{r.pilot?.setup_cost_inr !== undefined ? inr(r.pilot.setup_cost_inr) : '—'}</td>
                <td>{r.pilot?.annual_benefit_inr !== undefined ? inr(r.pilot.annual_benefit_inr) : '—'}</td>
                <td>{r.pilot && <input type="checkbox" aria-label={`Import row ${r.line}`} checked={keep.has(r.pilot.id)}
                  onChange={(e) => { const n = new Set(keep); if (e.target.checked) n.add(r.pilot!.id); else n.delete(r.pilot!.id); setKeep(n) }} />}</td>
                <td>{r.errors && r.errors.map((x, i) => <div key={i} className="caption"><span className="mono">{x.field}</span>: {x.reason}</div>)}</td>
              </tr>))}</tbody>
          </table></div>
        </>
      )}

      <h3 className="h3">Proposals to compare</h3>
      <p className="hint">One-time cost, yearly savings and the AI running cost — the three numbers from the plan. A blank AI running cost is assumed
        (20% of setup) and flagged by the engine. Give two proposals the same overlap group to make them mutually exclusive.</p>
      <div className="tbl-wrap"><table className="tbl">
        <caption className="sr">Proposals</caption>
        <thead><tr><th scope="col">Name</th><th scope="col">One-time cost ₹</th><th scope="col">Yearly savings ₹</th><th scope="col">AI running ₹/yr</th><th scope="col">Benefit type</th><th scope="col">Overlap group</th><th scope="col"><span className="sr">Actions</span></th></tr></thead>
        <tbody>{rows.map((r, i) => (
          <tr key={i}>
            <td><input aria-label={`Name of proposal ${i + 1}`} value={r.name} maxLength={80} onChange={(e) => setCell(i, { name: e.target.value })} style={{ width: 130 }} /></td>
            <td><input aria-label={`One-time cost of proposal ${i + 1}`} value={r.setup} inputMode="decimal" onChange={(e) => setCell(i, { setup: e.target.value })} style={{ width: 120 }} /></td>
            <td><input aria-label={`Yearly savings of proposal ${i + 1}`} value={r.benefit} inputMode="decimal" onChange={(e) => setCell(i, { benefit: e.target.value })} style={{ width: 120 }} /></td>
            <td><input aria-label={`AI running cost of proposal ${i + 1}`} value={r.run} inputMode="decimal" placeholder="blank = assumed" onChange={(e) => setCell(i, { run: e.target.value })} style={{ width: 120 }} /></td>
            <td><select aria-label={`Benefit type of proposal ${i + 1}`} value={r.type} onChange={(e) => setCell(i, { type: e.target.value })}>
              {TYPES.map((t) => <option key={t.value} value={t.value}>{t.label}</option>)}</select></td>
            <td><input aria-label={`Overlap group of proposal ${i + 1}`} value={r.group} maxLength={40} onChange={(e) => setCell(i, { group: e.target.value })} style={{ width: 110 }} /></td>
            <td><button className="btn btn-quiet btn-sm" aria-label={`Remove proposal ${i + 1}`} onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}>Remove</button></td>
          </tr>))}</tbody>
      </table></div>
      <div className="row" style={{ flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
        <button className="btn" onClick={() => setRows((rs) => (rs.length >= 8 ? rs : [...rs, blankRow()]))} disabled={rows.length >= 8}>Add proposal</button>
        <div style={{ minWidth: 210 }}>
          <TextField id="cmp-budget" label="Budget ₹ (optional)" value={budgetStr} onChange={setBudgetStr} hint="Blank = fund every survivor." />
        </div>
        <div style={{ minWidth: 210 }}>
          <SelectField id="cmp-mode" label="Mode" value={mode} onChange={(v) => setMode(v as Mode)}
            options={[{ value: 'independent', label: 'Independent (full benefit)' }, { value: 'conservative', label: 'Conservative (cash factor)' }]} />
        </div>
        <button className="btn btn-primary" onClick={() => void compare()} disabled={busy}>{busy ? 'Comparing…' : 'Compare'}</button>
      </div>

      {err != null && (err instanceof Error
        ? <div className="notice notice-warn"><div>{err.message}</div></div>
        : <ErrorNotice title="The comparison could not be run." error={err} />)}
      {result && <CompareResult res={result} burn={burn} />}
    </div>
  )
}

/* -------------------------------------------------- result */

function CompareResult({ res, burn }: { res: CompareResponse; burn: BalanceSheetConfirm | null }) {
  const sel = res.proposals.filter((r) => r.status === 'recommended' || r.status === 'kept')
  const skip = res.proposals.filter((r) => r.status !== 'recommended' && r.status !== 'kept')
  const horizon = Number(res.recommendation.assumptions.horizon_months ?? 36)
  return (
    <div className="stack" style={{ gap: 16 }}>
      <div className="notice notice-ok" style={{ whiteSpace: 'pre-line' }}><div>{res.recommendation.text}</div></div>

      <div className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
        <Stat label="Net annual savings of the funded set" value={`₹ ${inr(res.selection.total_net_inr)}`} sub={res.selection.message} />
        <Stat label="Budget left" value={res.selection.budget_left === null ? 'no budget given' : `₹ ${inr(res.selection.budget_left)}`} />
        <Stat label="Token value vs price" value={res.token_value_vs_price.value_vs_price_ratio === null ? 'unknown' : `${res.token_value_vs_price.value_vs_price_ratio}×`}
          sub={res.token_value_vs_price.value_vs_price_ratio === null
            ? (res.token_value_vs_price.flags[0]?.message ?? 'record tokens and price')
            : `₹${res.token_value_vs_price.value_per_1m_tokens} value of ₹${res.token_value_vs_price.price_per_1m_tokens} price per 1M tokens`} />
        <Stat label="Funded" value={res.selection.selected.join(', ') || 'none'} />
      </div>

      <CumulativeChart rows={sel} />
      {sel.length > 0 && (
        <div className="row" style={{ flexWrap: 'wrap', gap: 20, alignItems: 'flex-start' }}>
          <div style={{ flex: 1, minWidth: 260 }}>
            <BarList title="ROI over the horizon" pct
              items={sel.filter((r) => r.roi_pct !== null).map((r) => ({ name: r.name, value: Math.max(0, r.roi_pct!), text: `${r.roi_pct!.toFixed(0)}%` }))} />
          </div>
          <div style={{ flex: 1, minWidth: 260 }}>
            <BarList title="Break-even month" max={horizon}
              items={sel.filter((r) => r.breakeven_month !== null).map((r) => ({ name: r.name, value: r.breakeven_month!, text: `month ${r.breakeven_month}` }))} />
          </div>
        </div>
      )}

      <div className="tbl-wrap"><table className="tbl">
        <caption className="sr">Comparison summary</caption>
        <thead><tr><th scope="col">Rank</th><th scope="col">Proposal</th><th scope="col">Net ₹/yr</th><th scope="col">Payback</th><th scope="col">Horizon profit</th><th scope="col">ROI</th><th scope="col">NPV</th><th scope="col">Break-even</th><th scope="col">Cash share</th><th scope="col">Status</th><th scope="col">Note</th></tr></thead>
        <tbody>{res.proposals.map((r) => (
          <tr key={r.id}>
            <td className="mono">{r.rank ?? '—'}</td>
            <td className="strong">{r.name}</td>
            <td>{fmtSigned(r.net_annual_inr)}</td>
            <td>{r.payback_months === null ? 'never' : `${r.payback_months} months`}</td>
            <td>{r.profit_inr === null ? '—' : fmtSigned(r.profit_inr)}</td>
            <td>{r.roi_pct === null ? '—' : `${r.roi_pct.toFixed(0)}%`}</td>
            <td>{r.npv_inr === null ? '—' : fmtSigned(r.npv_inr)}</td>
            <td>{r.breakeven_month === null ? (r.breakeven_beyond_horizon ? `beyond ${horizon} mo` : '—') : `month ${r.breakeven_month}`}</td>
            <td className="caption" title={`cash ₹${inr(r.cash_vs_capacity.cash_benefit_inr)} · capacity ₹${inr(r.cash_vs_capacity.capacity_benefit_inr)}`}>{r.cash_vs_capacity.cash_share_pct}%</td>
            <td><span className={`badge${r.status === 'recommended' ? ' badge-ok' : ''}`}>{r.status}</span></td>
            <td className="caption">{r.reason ?? (r.flags[0]?.message ?? '')}</td>
          </tr>))}</tbody>
      </table></div>

      <div className="panel" style={{ padding: 12 }}>
        <div className="h3">Why invest — beside what has already been spent</div>
        <div className="row" style={{ flexWrap: 'wrap', gap: 16, alignItems: 'flex-start' }}>
          <div style={{ flex: 2, minWidth: 260 }} className="stack">
            {res.recommendation.text.split('. ').filter((s) => s.trim()).map((s, i) => <div key={i} className="caption">• {s.trim()}.</div>)}
          </div>
          <div style={{ flex: 1, minWidth: 220 }}>
            {burn ? (
              <div className="row" style={{ flexWrap: 'wrap', gap: 4 }}>
                <Stat label="AI burned to date" value={`₹ ${inr(burn.ai_burn_to_date)}`} sub="confirmed from the balance sheet" />
                <Stat label="Next-FY AI plan" value={aiPlan(burn).next === null ? 'unknown' : `₹ ${inr(aiPlan(burn).next!)}`} sub={aiPlan(burn).nextSub} />
              </div>
            ) : (
              <p className="caption">Confirm rows in the balance-sheet card above to place the money already burned on AI beside this recommendation.</p>
            )}
          </div>
        </div>
      </div>

      {skip.length > 0 && (
        <div className="stack" style={{ gap: 4 }}>
          {skip.map((r) => <div key={r.id} className="caption"><span className="strong">{r.name}</span>: {r.reason}</div>)}
        </div>
      )}
      {res.warnings.map((w, i) => <div key={i} className="caption">Note: {w}</div>)}
    </div>
  )
}

const fmtSigned = (n: number) => (n < 0 ? `−₹ ${inr(-n)}` : `₹ ${inr(n)}`)

function CumulativeChart({ rows }: { rows: CompareProposalRow[] }) {
  const series = rows.filter((r) => Array.isArray(r.cumulative) && r.cumulative.length > 1)
  if (!series.length) return null
  const W = 620, H = 180, PAD = 12
  const all = series.flatMap((r) => r.cumulative as number[])
  const lo = Math.min(0, ...all), hi = Math.max(0, ...all)
  const span = hi - lo || 1
  const n = (series[0].cumulative as number[]).length
  const x = (i: number) => PAD + (i / (n - 1)) * (W - 2 * PAD)
  const y = (v: number) => PAD + (1 - (v - lo) / span) * (H - 2 * PAD)
  const zero = y(0)
  return (
    <div className="stack" style={{ gap: 6 }} role="img"
      aria-label={`Cumulative cash flow by month for ${series.map((s) => s.name).join(', ')}. The dashed line is break-even.`}>
      <div className="caption strong">Cumulative cash flow by month (₹) — a line above the dashes is a funded pilot that has paid back</div>
      <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto' }} focusable="false" aria-hidden>
        <line x1={PAD} x2={W - PAD} y1={zero} y2={zero} stroke="currentColor" strokeDasharray="4 4" opacity={0.4} />
        {series.map((r, idx) => (
          <polyline key={r.id} fill="none" stroke={LINE_COLORS[idx % LINE_COLORS.length]} strokeWidth={2}
            points={(r.cumulative as number[]).map((v, i) => `${x(i)},${y(v)}`).join(' ')} />
        ))}
      </svg>
      <div className="row" style={{ gap: 12, flexWrap: 'wrap' }}>
        {series.map((r, idx) => (
          <span key={r.id} className="caption"><span style={{ color: LINE_COLORS[idx % LINE_COLORS.length], fontWeight: 700 }}>▮</span> {r.name}</span>
        ))}
      </div>
    </div>
  )
}

function BarList({ title, items, max: given, pct }: { title: string; items: { name: string; value: number; text: string }[]; max?: number; pct?: boolean }) {
  if (items.length === 0) return null
  const max = given ?? Math.max(...items.map((i) => i.value), 1)
  return (
    <div className="stack" style={{ gap: 4 }} role="img" aria-label={title}>
      <div className="caption strong">{title}</div>
      {items.map((it, idx) => (
        <div key={it.name} className="row" style={{ gap: 8, alignItems: 'center' }}>
          <span className="caption" style={{ width: 70, overflow: 'hidden', textOverflow: 'ellipsis' }}>{it.name}</span>
          <div style={{ flex: 1, height: 12, background: 'var(--bg-muted)', borderRadius: 6, overflow: 'hidden' }}>
            <div style={{ width: `${Math.min(100, (it.value / max) * 100)}%`, height: '100%', background: LINE_COLORS[idx % LINE_COLORS.length] }} />
          </div>
          <span className="caption" style={{ width: 76, textAlign: 'right' }}>{it.text}</span>
        </div>
      ))}
      {pct && <div className="caption">ROI = horizon profit ÷ one-time cost, at the engine’s horizon and discount rate.</div>}
    </div>
  )
}
