import type { BalanceUnit } from './types'
import type { Correction, Upload, UploadDetail } from './viewTypes'
import { ApiError, V1, enc, fileGet, json, jsonUpload } from './http'
import type { Download } from './http'

/* ---- Stored uploads: kept with their original bytes, parsed by the gateway's worker, reviewed, confirmed ---- */

/** 202 for a new upload, 200 with duplicate: true when the same file was already stored. CSV and XLSX only. */
async function sha256(file: File): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
  return [...new Uint8Array(digest)].map((n) => n.toString(16).padStart(2, '0')).join('')
}

function uploadToPresigned(url: string, headers: Record<string, string>, file: File, onProgress?: (value: number) => void): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', url)
    for (const [key, value] of Object.entries(headers)) xhr.setRequestHeader(key, value)
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress?.(event.loaded / event.total)
    }
    xhr.onerror = () => reject(new ApiError('The file could not be uploaded to document storage.'))
    xhr.onload = () => xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new ApiError(`Document storage rejected the upload (${xhr.status}).`, { status: xhr.status, code: 'storage_upload_failed' }))
    xhr.send(file)
  })
}

export async function createUpload(fileV: File, kind: Upload['kind'], unit: BalanceUnit = 'auto', onProgress?: (value: number) => void) {
  const checksum = await sha256(fileV)
  type Session = { upload: Upload; duplicate: boolean; upload_url?: string; headers?: Record<string, string> }
  let session: { data: Session }
  try {
    session = await json<Session>('POST', `${V1}/documents/upload-session`,
      { filename: fileV.name, kind, size_bytes: fileV.size, content_type: fileV.type, sha256: checksum, ...(unit !== 'auto' ? { unit } : {}) })
  } catch (error: unknown) {
    if (!(error instanceof ApiError) || error.code !== 'not_configured') throw error
    const f = new FormData()
    f.append('file', fileV)
    f.append('kind', kind)
    if (unit !== 'auto') f.append('unit', unit)
    session = await jsonUpload<Session>(`${V1}/uploads`, f)
  }
  if (session.data.duplicate || !session.data.upload_url || !session.data.headers) return session
  onProgress?.(0)
  await uploadToPresigned(session.data.upload_url, session.data.headers, fileV, onProgress)
  return json<{ upload: Upload; duplicate: boolean }>('POST', `${V1}/documents/${enc(session.data.upload.id)}/complete`)
}
export const listUploads = async (limit = 50) => (await json<{ uploads: Upload[] | null }>('GET', `${V1}/uploads?limit=${limit}`)).data.uploads ?? []
export const getUpload = (id: string) => json<UploadDetail>('GET', `${V1}/uploads/${enc(id)}`)
export const retryUpload = (id: string) => json<{ upload: Upload }>('POST', `${V1}/uploads/${enc(id)}/retry`)
export const correctUpload = (id: string, b: { row_ref: string | number; field: string; value: unknown; note?: string | null }) =>
  json<Correction>('POST', `${V1}/uploads/${enc(id)}/corrections`, b)
export const confirmUpload = (id: string, b: { rows: number[] } | { keep: string[] }) =>
  json<{ upload: Upload; result: unknown }>('POST', `${V1}/uploads/${enc(id)}/confirm`, b)
export const downloadUpload = (id: string, name: string): Promise<Download> => fileGet(`${V1}/uploads/${enc(id)}/file`, name)

export type ProposalExtractionField = {
  value: unknown
  status: 'explicit' | 'derived' | 'missing' | 'ambiguous'
  explanation?: string
  evidence?: { source: string; excerpt: string; original: string }[]
}
export type ProposalExtraction = {
  id: string
  upload_id: string
  status: 'processing' | 'review' | 'confirmed' | 'failed'
  fields: Record<string, ProposalExtractionField>
  warnings: string[]
  error?: string
}
export const startProposalExtraction = (id: string) =>
  json<ProposalExtraction>('POST', `${V1}/uploads/${enc(id)}/proposal-extraction`)
export const getProposalExtraction = (id: string) =>
  json<ProposalExtraction>('GET', `${V1}/uploads/${enc(id)}/proposal-extraction`)
export const correctProposalExtraction = (id: string, field: string, value: unknown, note?: string) =>
  json<ProposalExtraction>('PATCH', `${V1}/uploads/${enc(id)}/proposal-extraction`, { field, value, note })
export const confirmProposalExtraction = (id: string) =>
  json<{ extraction: ProposalExtraction; proposal: { id: string; name: string; inputs: Record<string, unknown> } }>(
    'POST', `${V1}/uploads/${enc(id)}/proposal-extraction/confirm`)
