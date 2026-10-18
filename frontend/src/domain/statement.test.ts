import { describe, expect, it } from 'vitest'
import { aiPlan, describeSource, extraFindings, readSummary, sheetOptionLabel, statementFormat, unitSummary, validateStatementFile, visibleWarnings, whyFlagged } from './statement'

describe('statementFormat', () => {
  it('recognises the four formats whatever the case', () => {
    expect(statementFormat('Annual Report.PDF')).toBe('pdf')
    expect(statementFormat('book.xlsx')).toBe('xlsx')
    expect(statementFormat('old.XLS')).toBe('xls')
    expect(statementFormat('export.csv')).toBe('csv')
  })
  it('rejects everything else', () => {
    for (const n of ['notes.docx', 'photo.png', 'archive.zip', 'noextension', 'report.pdf.exe', '.csv.bak']) expect(statementFormat(n)).toBeNull()
  })
})

describe('validateStatementFile', () => {
  it('accepts a normal file', () => {
    expect(validateStatementFile({ name: 'a.pdf', size: 5 * 1024 * 1024 })).toBeNull()
    expect(validateStatementFile({ name: 'a.csv', size: 25 * 1024 * 1024 })).toBeNull() // exactly the limit
  })
  it('names the problem in plain words', () => {
    expect(validateStatementFile({ name: 'a.docx', size: 10 })).toMatch(/PDF, an Excel workbook \(\.xlsx or \.xls\) or a CSV/)
    expect(validateStatementFile({ name: 'a.pdf', size: 0 })).toMatch(/empty/)
    const big = validateStatementFile({ name: 'big.pdf', size: 30 * 1024 * 1024 })
    expect(big).toMatch(/30\.0 MB/)
    expect(big).toMatch(/limit is 25 MB/)
  })
})

describe('what the reviewer reads', () => {
  it('describes where a row came from', () => {
    expect(describeSource({ page: 15 })).toBe('Page 15')
    expect(describeSource({ sheet: 'Statement', row: 6 })).toBe('Sheet “Statement”, row 6')
    expect(describeSource({ sheet: 'Statement' })).toBe('Sheet “Statement”')
    expect(describeSource(undefined)).toBe('')
  })
  it('says why a row was flagged', () => {
    expect(whyFlagged({ reason: 'Matched keyword: software', matched: ['software'] })).toBe('Matched keyword: software')
    expect(whyFlagged({ matched: ['ai', 'tokens'] })).toBe('Matched keyword: ai, tokens')
  })
  it('marks an assumed unit and leaves a stated one alone', () => {
    expect(unitSummary({ choice: 'auto', unit: 'rupees', source: 'default (no unit detected)' })).toEqual({ text: 'rupees (assumed: the file states no unit)', assumed: true })
    expect(unitSummary({ choice: 'auto', unit: 'lakh', source: 'amount-column header' })).toEqual({ text: 'lakh (amount-column header)', assumed: false })
    expect(unitSummary({ choice: 'crore', unit: 'crore', source: 'your choice' }).assumed).toBe(false)
  })
  it('does not repeat a message the warnings already show', () => {
    const f = [{ severity: 'warning', code: 'PARSE_WARNING', line: null, field: null, message: 'same' }, { severity: 'error', code: 'ROW', line: 4, field: 'amount', message: 'different' }]
    expect(extraFindings(f, ['same'])).toEqual([f[1]])
    expect(extraFindings(undefined, [])).toEqual([])
  })
  it('hides the engine column warning only when the column control replaces it', () => {
    const w = ["Multiple numeric amount columns (Note, 2026); using 'Note'.", 'Another warning']
    expect(visibleWarnings(w, [{ code: 'AMOUNT_COLUMN_CHOSEN', message: '' }])).toEqual(['Another warning'])
    expect(visibleWarnings(w, [])).toEqual(w)
  })
  it('summarises how the document was read', () => {
    expect(readSummary({ source_format: 'pdf', sources_matched: true, pages: { total: 16, selected: Array.from({ length: 16 }, (_, i) => i + 1), with_tables: [15], no_text: [], units: {} } })).toBe('PDF: 16 of 16 pages read, tables on 15')
    expect(readSummary({ source_format: 'xlsx', sources_matched: true, sheet: 'Statement', sheets: [{ name: 'Cover', hidden: false, rows: 2, usable: false, suggested: false }, { name: 'Statement', hidden: false, rows: 9, usable: true, suggested: true }] })).toBe('Excel (.xlsx): sheet “Statement” of 2')
    expect(readSummary({ source_format: 'csv', sources_matched: false })).toBe('CSV')
    expect(readSummary(undefined)).toBe('')
  })
  it('labels sheets so a hidden or empty one is obvious', () => {
    expect(sheetOptionLabel({ name: 'Statement', hidden: false, usable: true, suggested: true })).toBe('Statement (suggested)')
    expect(sheetOptionLabel({ name: 'Cover', hidden: false, usable: false, suggested: false })).toBe('Cover (no table found)')
    expect(sheetOptionLabel({ name: 'Calc', hidden: true, usable: false, suggested: false })).toBe('Calc (hidden, no table found)')
    expect(sheetOptionLabel({ name: 'Plain', hidden: false, usable: true, suggested: false })).toBe('Plain')
  })
})

describe('aiPlan', () => {
  const base = {
    ai_burn_to_date: 4_000_000, source: 'observed', rows_confirmed: [12], confirmations: [], amount_column_used: 'Amount',
    unit: { choice: 'auto', unit: 'rupees', multiplier: 1, source: 'default' }, warnings: [], flags: [],
    next_fy: { inputs: {}, ai_run_cost_inr: 170_000, next_fy_cost_inr: 182_000, inflation_flag: true },
  }
  it('plans from the confirmed rows when the engine sends them', () => {
    const p = aiPlan({ ...base, observed_forecast: { basis: 'confirmed_rows', ai_run_cost_inr: 4_000_000, next_fy_cost_inr: 4_320_000, inflation_flag: true,
      inputs: { annual_usage_growth_pct: 20, annual_price_drift_pct: -10 }, note: '' } })
    expect(p).toMatchObject({ run: 4_000_000, next: 4_320_000, fromStatement: true, runSub: 'confirmed rows, annualised' })
    expect(p.nextSub).toBe('this year with +20% usage, −10% price · spend carries into next FY')
  })
  it('falls back to the token estimate, labelled, for an older engine', () => {
    const p = aiPlan(base)
    expect(p).toMatchObject({ run: 170_000, next: 182_000, fromStatement: false, runSub: 'token assumptions, not the statement' })
  })
})
