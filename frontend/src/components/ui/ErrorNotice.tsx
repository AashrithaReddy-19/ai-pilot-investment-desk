import { ApiError, describeError } from '../../api/client'

interface Props {
  title: string
  error: unknown
  onRetry?: () => void
  retryLabel?: string
}

/** An honest failure message: what happened, the service's own wording, and the request id for tracing. */
export function ErrorNotice({ title, error, onRetry, retryLabel = 'Try again' }: Props) {
  const api = error instanceof ApiError ? error : null
  const friendly = describeError(error)
  const raw = api && api.message !== friendly ? api.message : null
  return (
    <div className="notice notice-bad" role="alert">
      <div>
        <strong>{title}</strong> {friendly}
        {(raw || api?.requestId || api?.code) && (
          <div className="hint" style={{ marginTop: 4 }}>
            {raw && <>Service said: “{raw}”. </>}
            {api?.code && api.code !== 'network_error' && <>Code {api.code}. </>}
            {api?.requestId && <>Request ID <span className="mono">{api.requestId}</span>.</>}
          </div>
        )}
      </div>
      {onRetry && <button className="btn btn-sm" onClick={onRetry}>{retryLabel}</button>}
    </div>
  )
}
