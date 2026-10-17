/** Empty means same origin (Vite proxies to the Go gateway in development). */
const BASE = ((import.meta.env.VITE_API_BASE as string | undefined) ?? '').replace(/\/$/, '')

export interface ErrorEnvelope { error?: { code?: string; message?: string; field?: string }; request_id?: string }

/** One error type for every failed call, carrying what the gateway reported. */
export class ApiError extends Error {
  status: number
  code: string
  field?: string
  requestId?: string
  constructor(message: string, o: { status?: number; code?: string; field?: string; requestId?: string } = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = o.status ?? 0
    this.code = o.code ?? 'network_error'
    this.field = o.field
    this.requestId = o.requestId
  }
}

/** Plain-language text for the gateway's error codes. The service's own message is kept as detail. */
export function describeError(e: unknown): string {
  if (!(e instanceof ApiError)) return e instanceof Error ? e.message : 'Something went wrong.'
  switch (e.code) {
    case 'network_error': return 'The gateway could not be reached. Check that the backend is running.'
    case 'rules_unavailable': return 'The rules engine is unreachable, so nothing could be calculated.'
    case 'rules_timeout': return 'The rules engine took too long to answer.'
    case 'rules_error': return 'The rules engine failed while calculating.'
    case 'database_error': return 'The database operation failed.'
    case 'database_timeout': return 'The database took too long to answer.'
    case 'payload_too_large': return 'That request is too large for the service.'
    case 'invalid_json': return 'The service could not read the request.'
    default: return e.message
  }
}

export function parseErrorEnvelope(status: number, body: unknown, headerRequestId?: string | null): ApiError {
  const env = (body ?? {}) as ErrorEnvelope
  const e = env.error
  return new ApiError(e?.message ?? `Request failed (${status})`, {
    status, code: e?.code ?? 'http_error', field: e?.field, requestId: env.request_id ?? headerRequestId ?? undefined,
  })
}

interface Meta { requestId?: string; cache?: string }
export interface Result<T> { data: T; meta: Meta }

async function send(method: string, path: string, body?: unknown): Promise<Response> {
  let res: Response
  try {
    res = await fetch(`${BASE}${path}`, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError('Network request failed')
  }
  return res
}

async function fail(res: Response): Promise<never> {
  let body: unknown = null
  try { body = await res.json() } catch { /* not JSON */ }
  throw parseErrorEnvelope(res.status, body, res.headers.get('X-Request-ID'))
}

const metaOf = (res: Response): Meta => ({ requestId: res.headers.get('X-Request-ID') ?? undefined, cache: res.headers.get('X-Cache') ?? undefined })

async function json<T>(method: string, path: string, body?: unknown): Promise<Result<T>> {
  const res = await send(method, path, body)
  if (!res.ok) return fail(res)
  if (res.status === 204) return { data: undefined as T, meta: metaOf(res) }
  return { data: (await res.json()) as T, meta: metaOf(res) }
}

/** Text responses (markdown memos/evidence). Used where the file itself is displayed in-page. */
async function text(path: string, body: unknown): Promise<Result<string>> {
  const res = await send('POST', path, body)
  if (!res.ok) return fail(res)
  return { data: await res.text(), meta: metaOf(res) }
}

/** Multipart upload: the browser sets the content-type and boundary; the gateway caps the size. */
async function jsonUpload<T>(path: string, form: FormData): Promise<Result<T>> {
  let res: Response
  try {
    res = await fetch(`${BASE}${path}`, { method: 'POST', body: form })
  } catch {
    throw new ApiError('Network request failed')
  }
  if (!res.ok) return fail(res)
  return { data: (await res.json()) as T, meta: metaOf(res) }
}

export interface Download { blob: Blob; filename: string; requestId?: string }

async function file(path: string, body: unknown, fallbackName: string): Promise<Download> {
  const res = await send('POST', path, body)
  if (!res.ok) return fail(res)
  const m = /filename="?([^";]+)"?/i.exec(res.headers.get('Content-Disposition') ?? '')
  return { blob: await res.blob(), filename: m?.[1] ?? fallbackName, requestId: res.headers.get('X-Request-ID') ?? undefined }
}

export const enc = encodeURIComponent
export const V1 = '/api/v1'

export { json, jsonUpload, file, text }
export { send }
export type { Meta }

/** Saves a downloaded file using the browser's normal download behaviour. */
export function saveDownload(d: Download): void {
  const url = URL.createObjectURL(d.blob)
  const a = document.createElement('a')
  a.href = url
  a.download = d.filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}
