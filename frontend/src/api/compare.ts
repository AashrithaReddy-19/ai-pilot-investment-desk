import type { BalanceSheetParse, BalanceSheetConfirm, BalanceUnit, CompareBody, CompareResponse, ObservationResult, PilotsImportParse, PilotsImportConfirm, ScenarioBody } from './types'
import { V1, enc, json, jsonUpload } from './http'

export const recordObservation = (id: string, b: ScenarioBody & { observed_adoption_pct?: number; observed_quality_pct?: number; source?: string }) =>
  json<ObservationResult>('POST', `${V1}/pilots/${enc(id)}/observations`, b)

/** Phase 4: uploads (multipart, nothing stored) and the three-proposal comparison */
export const balanceSheetParse = (fileV: File, unit: BalanceUnit = 'auto', amountColumn?: string) => {
  const f = new FormData()
  f.append('file', fileV)
  f.append('unit', unit)
  if (amountColumn) f.append('amount_column', amountColumn)
  return jsonUpload<BalanceSheetParse>(`${V1}/balance-sheet/parse`, f)
}
export const balanceSheetConfirm = (fileV: File, rows: number[], unit: BalanceUnit = 'auto', amountColumn?: string, defaults?: Record<string, unknown>) => {
  const f = new FormData()
  f.append('file', fileV)
  f.append('rows', rows.join(','))
  f.append('unit', unit)
  if (amountColumn) f.append('amount_column', amountColumn)
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
