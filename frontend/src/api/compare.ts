import type { BalanceSheetParse, BalanceSheetConfirm, BalanceUnit, CompareBody, CompareResponse, ObservationResult, PilotsImportParse, PilotsImportConfirm, ScenarioBody, StatementOptions } from './types'
import { V1, enc, json, jsonUpload } from './http'

export const recordObservation = (id: string, b: ScenarioBody & { observed_adoption_pct?: number; observed_quality_pct?: number; source?: string }) =>
  json<ObservationResult>('POST', `${V1}/pilots/${enc(id)}/observations`, b)

/** The choices that travel beside a balance-sheet document; empty ones are left out. */
function statementForm(fileV: File, unit: BalanceUnit, amountColumn?: string, opts?: StatementOptions): FormData {
  const f = new FormData()
  f.append('file', fileV)
  f.append('unit', unit)
  if (amountColumn) f.append('amount_column', amountColumn)
  if (opts?.sheet) f.append('sheet', opts.sheet)
  if (opts?.pages?.trim()) f.append('pages', opts.pages.trim())
  if (opts?.aiAssist && opts.aiAssist !== 'auto') f.append('ai_assist', opts.aiAssist)
  return f
}

/** Phase 4: uploads (multipart, nothing stored) and the three-proposal comparison. A balance sheet may be PDF, XLSX, XLS or CSV. */
export const balanceSheetParse = (fileV: File, unit: BalanceUnit = 'auto', amountColumn?: string, opts?: StatementOptions) =>
  jsonUpload<BalanceSheetParse>(`${V1}/balance-sheet/parse`, statementForm(fileV, unit, amountColumn, opts))
export const balanceSheetConfirm = (fileV: File, rows: number[], unit: BalanceUnit = 'auto', amountColumn?: string, defaults?: Record<string, unknown>, opts?: StatementOptions) => {
  const f = statementForm(fileV, unit, amountColumn, opts)
  f.append('rows', rows.join(','))
  if (defaults) f.append('defaults', JSON.stringify(defaults))
  return jsonUpload<BalanceSheetConfirm>(`${V1}/balance-sheet/confirm`, f)
}
export const importPilotsParse = (fileV: File, unit: BalanceUnit = 'auto') => {
  const f = new FormData()
  f.append('file', fileV)
  f.append('unit', unit)
  return jsonUpload<PilotsImportParse>(`${V1}/pilots/import/parse`, f)
}
export const importPilotsConfirm = (fileV: File, keep: string[], unit: BalanceUnit = 'auto') => {
  const f = new FormData()
  f.append('file', fileV)
  f.append('keep', keep.join(','))
  f.append('unit', unit)
  return jsonUpload<PilotsImportConfirm>(`${V1}/pilots/import/confirm`, f)
}
export const compare = (b: CompareBody) => json<CompareResponse>('POST', `${V1}/compare`, b)
