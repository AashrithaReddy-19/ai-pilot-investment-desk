import type { BalanceCandidate, BalanceSheetConfirm, Extraction, Flag, ParseFinding, StatementFormat, StatementSource } from '../api/types'

/** What the balance-sheet upload accepts. The gateway checks the bytes again; this only gives instant feedback. */
export const STATEMENT_ACCEPT = '.pdf,.xlsx,.xls,.csv,application/pdf,text/csv,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
export const MAX_STATEMENT_MB = 25

export const FORMAT_LABEL: Record<StatementFormat, string> = { pdf: 'PDF', xlsx: 'Excel (.xlsx)', xls: 'Excel (.xls)', csv: 'CSV' }

export function statementFormat(name: string): StatementFormat | null {
  const m = /\.([A-Za-z0-9]+)$/.exec(name.trim())
  const ext = m?.[1].toLowerCase()
  return ext === 'pdf' || ext === 'xlsx' || ext === 'xls' || ext === 'csv' ? ext : null
}

/** A plain-language reason the file cannot be used, or null when it can be tried. */
export function validateStatementFile(f: { name: string; size: number }, maxMB = MAX_STATEMENT_MB): string | null {
  if (!statementFormat(f.name)) {
    return `“${f.name}” is not a supported file. Upload a PDF, an Excel workbook (.xlsx or .xls) or a CSV file.`
  }
  if (f.size === 0) return `“${f.name}” is empty.`
  if (f.size > maxMB * 1024 * 1024) {
    return `“${f.name}” is ${(f.size / 1024 / 1024).toFixed(1)} MB and the limit is ${maxMB} MB. Split the document or export only the pages that hold the statement.`
  }
  return null
}

/** "Sheet “Statement”, row 6", "Page 15" or "" when the file is a CSV (the row number is then the CSV line). */
export function describeSource(src: StatementSource | undefined): string {
  if (!src) return ''
  if (src.page) return `Page ${src.page}`
  if (src.sheet) return `Sheet “${src.sheet}”${src.row ? `, row ${src.row}` : ''}`
  return ''
}

/** Why a row was flagged: the gateway's wording, or built from the matched keywords. */
export function whyFlagged(c: Pick<BalanceCandidate, 'reason' | 'matched'>): string {
  if (c.reason) return c.reason
  return c.matched.length > 0 ? `Matched keyword: ${c.matched.join(', ')}` : 'Matched the AI-spend rules'
}

/** The unit line, with the state that matters: stated by the file, chosen by the person, or assumed. */
export function unitSummary(unit: { choice: string; unit: string; source: string }): { text: string; assumed: boolean } {
  const assumed = unit.choice === 'auto' && unit.source.startsWith('default')
  return { text: assumed ? `${unit.unit} (assumed: the file states no unit)` : `${unit.unit} (${unit.source})`, assumed }
}

/** Engine findings that say something the warnings above them do not already say. */
export function extraFindings(findings: ParseFinding[] | undefined, warnings: string[]): ParseFinding[] {
  return (findings ?? []).filter((f) => !warnings.includes(f.message))
}

/** The engine's own warning about the amount column, which the "choose a column" control replaces. */
export function visibleWarnings(warnings: string[], flags: Flag[]): string[] {
  const columnFlag = flags.some((f) => f.code === 'AMOUNT_COLUMN_CHOSEN')
  return warnings.filter((w) => !(columnFlag && w.startsWith('Multiple numeric amount columns')))
}

/** "3 of 16 pages", "sheet “Statement”", or the format alone. */
export function readSummary(x: Extraction | undefined): string {
  if (!x) return ''
  const label = FORMAT_LABEL[x.source_format]
  if (x.pages) return `${label}: ${x.pages.selected.length} of ${x.pages.total} page${x.pages.total === 1 ? '' : 's'} read, tables on ${x.pages.with_tables.join(', ') || 'none'}`
  if (x.sheet) return `${label}: sheet “${x.sheet}”${x.sheets && x.sheets.length > 1 ? ` of ${x.sheets.length}` : ''}`
  return label
}

export function sheetOptionLabel(s: { name: string; hidden: boolean; usable: boolean; suggested: boolean }): string {
  const tags = [s.hidden ? 'hidden' : '', s.suggested ? 'suggested' : '', s.usable ? '' : 'no table found'].filter(Boolean)
  return tags.length ? `${s.name} (${tags.join(', ')})` : s.name
}

const pct = (v: number | null | undefined) => (v === null || v === undefined ? null : `${v > 0 ? '+' : v < 0 ? '−' : ''}${Math.abs(v)}%`)

/** The AI plan to show after confirming: from the confirmed rows when the engine sends it, otherwise (an
 *  older engine) the token-assumption estimate, labelled as such. */
export function aiPlan(r: BalanceSheetConfirm): { run: number | null; next: number | null; runSub: string; nextSub?: string; fromStatement: boolean } {
  const o = r.observed_forecast
  if (o) {
    const g = pct(o.inputs.annual_usage_growth_pct), d = pct(o.inputs.annual_price_drift_pct)
    const moves = [g && `${g} usage`, d && `${d} price`].filter(Boolean).join(', ')
    return {
      run: o.ai_run_cost_inr, next: o.next_fy_cost_inr, fromStatement: true,
      runSub: o.ai_run_cost_inr === null ? 'no confirmed row has an amount' : 'confirmed rows, annualised',
      nextSub: o.next_fy_cost_inr === null ? undefined : `this year ${moves ? `with ${moves}` : 'unchanged'}${o.inflation_flag ? ' · spend carries into next FY' : ''}`,
    }
  }
  return {
    run: r.next_fy.ai_run_cost_inr, next: r.next_fy.next_fy_cost_inr, fromStatement: false,
    runSub: r.next_fy.ai_run_cost_inr === null ? 'tokens or price not recorded' : 'token assumptions, not the statement',
    nextSub: r.next_fy.inflation_flag === null ? undefined : (r.next_fy.inflation_flag ? 'inflation: spend carries into next FY' : 'usage falls; no inflation'),
  }
}
