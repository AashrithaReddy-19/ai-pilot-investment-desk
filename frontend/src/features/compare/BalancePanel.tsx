import { useState } from 'react'
import { api } from '../../api/client'
import type { BalanceSheetConfirm, BalanceSheetParse, BalanceUnit, StatementOptions } from '../../api/types'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField, TextField } from '../../components/ui/Fields'
import {
  FORMAT_LABEL, MAX_STATEMENT_MB, STATEMENT_ACCEPT, describeSource, extraFindings, readSummary, sheetOptionLabel,
  statementFormat, unitSummary, validateStatementFile, visibleWarnings, whyFlagged,
} from '../../domain/statement'
import { Stat, inr } from './shared'

const UNITS: { value: BalanceUnit; label: string }[] = [
  { value: 'auto', label: 'Detect from the file (₹ / thousand / lakh / crore)' },
  { value: 'rupees', label: 'Rupees' }, { value: 'thousands', label: 'Thousands' },
  { value: 'lakh', label: 'Lakh' }, { value: 'crore', label: 'Crore' },
]

/** The choices that shape how the document is read; changing one reads it again. */
interface Choices { unit: BalanceUnit; amountColumn: string; sheet: string; pages: string }

/* -------------------------------------------------- balance sheet */

export function BalancePanel({ onConfirmed }: { onConfirmed: (b: BalanceSheetConfirm) => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [fileError, setFileError] = useState<string | null>(null)
  const [choices, setChoices] = useState<Choices>({ unit: 'auto', amountColumn: '', sheet: '', pages: '' })
  const [parse, setParse] = useState<BalanceSheetParse | null>(null)
  const [checked, setChecked] = useState<Set<number>>(new Set())
  const [res, setRes] = useState<BalanceSheetConfirm | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)

  const opts = (c: Choices): StatementOptions => ({ sheet: c.sheet || undefined, pages: c.pages || undefined })

  const doParse = async (next: Choices = choices, f: File | null = file) => {
    if (!f) return
    setChoices(next)
    setBusy(true); setErr(null); setParse(null); setRes(null); setChecked(new Set())
    try {
      setParse((await api.balanceSheetParse(f, next.unit, next.amountColumn || undefined, opts(next))).data)
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }
  const change = (patch: Partial<Choices>) => { void doParse({ ...choices, ...patch }) }

  const doConfirm = async () => {
    if (!file || checked.size === 0) return
    setBusy(true); setErr(null)
    try {
      // confirm re-reads the document with the same choices, so the row numbers still match
      // and with the column parse used, even when the person never picked one, so both calls total the same figures
      const column = choices.amountColumn || parse?.amount_column_used || undefined
      const r = (await api.balanceSheetConfirm(file, [...checked], choices.unit, column, undefined, opts(choices))).data
      setRes(r); onConfirmed(r)
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  const onPick = (f: File | null) => {
    setParse(null); setRes(null); setErr(null); setChecked(new Set())
    setChoices((c) => ({ ...c, amountColumn: '', sheet: '', pages: '' }))
    const problem = f ? validateStatementFile(f) : null
    setFileError(problem)
    setFile(problem ? null : f)
  }

  const x = parse?.extraction
  const flags = parse?.flags ?? res?.flags ?? []
  const warnings = parse ? visibleWarnings(parse.warnings, parse.flags) : []
  const findings = parse ? extraFindings(parse.findings, parse.warnings) : []
  const unit = parse ? unitSummary(parse.unit) : null
  const format = file ? statementFormat(file.name) : null

  return (
    <div className="panel">
      <h3 className="h3">Balance sheet: what the AI has burned</h3>
      <p className="hint">Upload the balance sheet, profit and loss statement or annual report as a <strong>PDF</strong>, an <strong>Excel</strong> workbook (.xlsx or .xls) or a <strong>CSV</strong>, up to {MAX_STATEMENT_MB} MB.
        The service only <em>proposes</em> rows that look like AI spend (keywords: ai, software, cloud, licence, token): <em>you</em> decide which count, so a software or cloud line is never counted on its own.
        Nothing is stored. A PDF must contain real text: a scanned (picture-only) PDF cannot be read, and the desk will say so.</p>
      <div className="row" style={{ flexWrap: 'wrap', gap: 12, alignItems: 'flex-end' }}>
        <div className="field">
          <label className="label" htmlFor="bs-file">Statement file</label>
          <input id="bs-file" type="file" accept={STATEMENT_ACCEPT} onChange={(e) => onPick(e.target.files?.[0] ?? null)} aria-describedby={fileError ? 'bs-file-err' : undefined} />
        </div>
        <div style={{ minWidth: 230 }}>
          <SelectField id="bs-unit" label="Money unit" value={choices.unit} onChange={(v) => change({ unit: v as BalanceUnit })} options={UNITS}
            hint={parse ? undefined : 'Amounts are never multiplied unless the file states a unit or you choose one.'} />
        </div>
        <button className="btn" onClick={() => void doParse()} disabled={!file || busy}>{busy ? 'Reading…' : parse ? 'Read again' : 'Read the file'}</button>
        {parse && parse.candidates.length > 0 && (
          <button className="btn btn-primary" onClick={() => void doConfirm()} disabled={busy || checked.size === 0}>Confirm selected rows</button>
        )}
      </div>
      {fileError && <div id="bs-file-err" className="notice notice-bad" role="alert"><div>{fileError}</div></div>}
      {file && !parse && !busy && err == null && format && <p className="hint">{FORMAT_LABEL[format]} selected: {file.name} ({(file.size / 1024).toLocaleString('en-IN', { maximumFractionDigits: 0 })} KB). Choose “Read the file”.</p>}

      {err != null && <ErrorNotice title="The file could not be read." error={err} />}

      {x && (
        <div className="stack" style={{ gap: 8 }} data-testid="extraction">
          <p className="hint"><span className="strong">Read as:</span> {readSummary(x)}</p>
          {x.sheets && x.sheets.length > 1 && (
            <div style={{ maxWidth: 360 }}>
              <SelectField id="bs-sheet" label="Worksheet" value={x.sheet ?? ''} onChange={(v) => change({ sheet: v })}
                options={x.sheets.map((s) => ({ value: s.name, label: sheetOptionLabel(s) }))}
                hint="The workbook has several sheets. Pick the one that holds the statement." />
            </div>
          )}
          {x.pages && x.pages.total > 1 && (
            <div className="row" style={{ gap: 12, alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div style={{ maxWidth: 260 }}>
                <TextField id="bs-pages" label="Pages to read" value={choices.pages} onChange={(v) => setChoices((c) => ({ ...c, pages: v }))}
                  placeholder="e.g. 120-124, 130" hint={`Tables were found on page${x.pages.with_tables.length === 1 ? '' : 's'} ${x.pages.with_tables.join(', ') || 'none'}. Leave empty to read every page.`} />
              </div>
              <button className="btn" onClick={() => void doParse()} disabled={busy}>Read these pages</button>
            </div>
          )}
          {x.dropped_columns && x.dropped_columns.length > 0 && (
            <p className="hint">Left out: {x.dropped_columns.map((d) => `“${d.header}” (${d.reason})`).join('; ')}.</p>
          )}
          {x.notes?.map((n, i) => <p key={i} className="hint">{n}</p>)}
        </div>
      )}

      {flags.map((f, i) => <div key={i} className="notice notice-warn"><div><span className="mono">{f.code}</span> — {f.message}</div></div>)}
      {warnings.map((w, i) => <div key={`w${i}`} className="notice notice-info"><div>{w}</div></div>)}
      {findings.map((f, i) => <div key={`f${i}`} className="notice notice-info"><div><span className="mono">{f.code}</span>{f.line ? ` (line ${f.line})` : ''} — {f.message}</div></div>)}

      {parse && parse.candidates.length === 0 && (
        <p className="muted">No line item matched the AI-spend keywords, so the AI burn is unknown. Rename the relevant line items or extend the keyword set (visible in /defaults).</p>
      )}

      {parse && parse.candidates.length > 0 && (
        <>
          <p className="hint">
            Unit: <span className={unit?.assumed ? 'strong tag tag-assumed' : 'strong'}>{unit?.text}</span>
            {x?.currency && <> · Currency: <span className="strong">{x.currency}</span></>}
            {' '}· Amount column <span className="mono">{parse.amount_column_used}</span>
          </p>
          {parse.amount_columns.length > 1 && (
            <div style={{ maxWidth: 360 }}>
              <SelectField id="bs-column" label="Amount column" value={choices.amountColumn || parse.amount_column_used} onChange={(v) => change({ amountColumn: v })}
                options={parse.amount_columns.map((c) => ({ value: c, label: c }))}
                hint="The file has more than one column of amounts (for example this year and last year). Choose the one to review." />
            </div>
          )}
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Candidate AI-spend rows</caption>
            <thead><tr>
              <th scope="col">Source</th><th scope="col">Line item</th><th scope="col">Why flagged</th>
              <th scope="col">As printed</th><th scope="col">Amount in ₹</th><th scope="col">AI spend?</th>
            </tr></thead>
            <tbody>{parse.candidates.map((c) => {
              const src = describeSource(c.source)
              return (
                <tr key={c.row}>
                  <td title={c.source?.text}>{src || <span className="mono">line {c.row}</span>}</td>
                  <td className="strong wrap-cell">{c.label}</td>
                  <td className="wrap-cell">{whyFlagged(c)}</td>
                  <td className="mono">{c.raw ?? '—'}</td>
                  <td>{c.amount_inr === null ? <span className="tag" title="No readable amount on this row">no readable amount</span> : inr(c.amount_inr)}</td>
                  <td><input type="checkbox" aria-label={`Count ${c.label} (${src || `line ${c.row}`}) as AI spend`}
                    checked={checked.has(c.row)}
                    onChange={(e) => { const n = new Set(checked); if (e.target.checked) n.add(c.row); else n.delete(c.row); setChecked(n) }} /></td>
                </tr>)
            })}</tbody>
          </table></div>
          <p className="hint">Tick only the rows that really are AI spending. A row with no readable amount cannot be counted until it is fixed in the source file.</p>
        </>
      )}

      {res && (
        <>
          <div className="row" style={{ flexWrap: 'wrap', gap: 8 }}>
            <Stat label="AI burn to date (observed)" value={`₹ ${inr(res.ai_burn_to_date)}`} sub={`rows ${res.rows_confirmed.join(', ')}`} />
            <Stat label="AI run cost this year" value={res.next_fy.ai_run_cost_inr === null ? 'unknown' : `₹ ${inr(res.next_fy.ai_run_cost_inr)}`}
              sub={res.next_fy.ai_run_cost_inr === null ? 'tokens or price not recorded' : 'from token assumptions'} />
            <Stat label="Next-FY AI plan" value={res.next_fy.next_fy_cost_inr === null ? 'unknown' : `₹ ${inr(res.next_fy.next_fy_cost_inr)}`}
              sub={res.next_fy.inflation_flag === null ? undefined : (res.next_fy.inflation_flag ? 'inflation: spend carries into next FY' : 'usage falls; no inflation')} />
          </div>
          {res.confirmations.length > 0 && (
            <div className="tbl-wrap"><table className="tbl">
              <caption className="sr">Rows counted as AI spend</caption>
              <thead><tr><th scope="col">Source</th><th scope="col">Counted line item</th><th scope="col">Amount in ₹</th></tr></thead>
              <tbody>{res.confirmations.map((c) => (
                <tr key={c.row}>
                  <td>{describeSource(c.source) || <span className="mono">line {c.row}</span>}</td>
                  <td className="wrap-cell">{c.label}</td>
                  <td>{c.amount_inr === null ? <span className="tag">no readable amount</span> : inr(c.amount_inr)}</td>
                </tr>))}</tbody>
            </table></div>
          )}
        </>
      )}
      {res && res.next_fy.ai_run_cost_inr === null && (
        <p className="hint">Record monthly token usage and the price per 1M tokens (team assumptions in /defaults) to see the next-FY plan; it is never guessed as zero.</p>
      )}
    </div>
  )
}
