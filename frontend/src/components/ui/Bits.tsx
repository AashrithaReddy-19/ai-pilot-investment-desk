import type { Flag } from '../../api/types'

export function Skeleton({ rows = 3, height = 40 }: { rows?: number; height?: number }) {
  return <div className="stack" aria-hidden>{Array.from({ length: rows }, (_, i) => <div key={i} className="skeleton" style={{ height }} />)}</div>
}

export function Empty({ title, children, actions }: { title: string; children?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="empty">
      <strong>{title}</strong>
      {children && <span style={{ maxWidth: '58ch' }}>{children}</span>}
      {actions && <div className="row">{actions}</div>}
    </div>
  )
}

/** Marks a value the service filled in because the user left it blank. */
export const AssumedTag = () => <span className="tag tag-assumed" title="Filled in by the service because it was left blank">assumed</span>
export const SyntheticTag = () => <span className="tag tag-synthetic" title="Teaching data supplied with the case">synthetic</span>

export const hasFlag = (flags: Flag[], code: string): boolean => flags.some((f) => f.code === code)

export function Status({ tone, children }: { tone: 'ok' | 'warn' | 'bad' | 'accent' | 'off' | 'wait'; children: React.ReactNode }) {
  return <span className="badge"><span className={`dot dot-${tone}`} aria-hidden />{children}</span>
}
