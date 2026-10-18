import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Tone } from '../../domain/fynd'
import { outcomeLabel, outcomeTone } from '../../domain/fynd'
import { Icon } from '../ui/Icon'

export function PageHeader({ title, subtitle, actions, crumbs }: {
  title: string
  subtitle?: React.ReactNode
  actions?: React.ReactNode
  crumbs?: { to?: string; label: string }[]
}) {
  return (
    <header className="ph">
      <div className="ph-text">
        {crumbs && crumbs.length > 0 && (
          <nav className="crumbs" aria-label="Breadcrumb">
            {crumbs.map((c, i) => (
              <span key={c.label} className="row" style={{ gap: 6 }}>
                {c.to ? <Link to={c.to}>{c.label}</Link> : <span aria-current="page">{c.label}</span>}
                {i < crumbs.length - 1 && <span aria-hidden>/</span>}
              </span>
            ))}
          </nav>
        )}
        <h1 className="ph-title">{title}</h1>
        {subtitle && <p className="ph-sub">{subtitle}</p>}
      </div>
      {actions && <div className="ph-actions">{actions}</div>}
    </header>
  )
}

export function Card({ title, sub, actions, children, className = '', tight, ...rest }: {
  title?: React.ReactNode
  sub?: React.ReactNode
  actions?: React.ReactNode
  children?: React.ReactNode
  className?: string
  tight?: boolean
} & Omit<React.HTMLAttributes<HTMLElement>, 'title'>) {
  return (
    <section className={`card${tight ? ' card-tight' : ''} ${className}`} {...rest}>
      {(title || actions) && (
        <div className="card-head">
          <div>
            {title && <h2 className="card-title">{title}</h2>}
            {sub && <p className="card-sub">{sub}</p>}
          </div>
          {actions && <div className="row" style={{ gap: 8 }}>{actions}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

export function Pill({ tone = 'off', children, title }: { tone?: Tone; children: React.ReactNode; title?: string }) {
  return <span className={`pill pill-${tone}`} title={title}>{children}</span>
}

export function OutcomePill({ outcome }: { outcome: string | null | undefined }) {
  return <Pill tone={outcomeTone(outcome)} title={outcome ? `Engine outcome: ${outcome}` : undefined}>{outcomeLabel(outcome)}</Pill>
}

/** A figure with its provenance. A null value reads "Not available" with the reason, never zero. */
export function Kpi({ label, value, foot, reason }: { label: string; value: string | null; foot?: React.ReactNode; reason?: string }) {
  return (
    <div className="card card-tight kpi">
      <span className="kpi-label">{label}</span>
      {value === null ? <span className="kpi-value na">Not available</span> : <span className="kpi-value">{value}</span>}
      {value === null && reason && <span className="kpi-foot">{reason}</span>}
      {foot && <span className="kpi-foot">{foot}</span>}
    </div>
  )
}

/** Contextual row actions: a small menu that closes on outside click and Escape. */
export function RowMenu({ label, items }: { label: string; items: { label: string; onClick: () => void; danger?: boolean; disabled?: boolean; title?: string }[] }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => { if (box.current && !box.current.contains(e.target as Node)) setOpen(false) }
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key) }
  }, [open])
  return (
    <div className="menu-wrap" ref={box}>
      <button className="btn btn-quiet btn-sm icon-btn" aria-haspopup="menu" aria-expanded={open} aria-label={label} onClick={() => setOpen((o) => !o)}>
        <Icon name="more" />
      </button>
      {open && (
        <div className="menu" role="menu">
          {items.map((it) => (
            <button key={it.label} role="menuitem" className={it.danger ? 'danger' : ''} disabled={it.disabled} title={it.title}
              onClick={() => { setOpen(false); it.onClick() }}>{it.label}</button>
          ))}
        </div>
      )}
    </div>
  )
}

/** Three workflow stages whose state comes from real application data. */
export function FlowSteps({ steps }: { steps: { title: string; sub: string; state: 'done' | 'current' | 'todo'; to?: string }[] }) {
  return (
    <ol className="flow" aria-label="Workflow">
      {steps.map((s, i) => {
        const body = (
          <>
            <span className="flow-n" aria-hidden>{s.state === 'done' ? '✓' : i + 1}</span>
            <span className="stack" style={{ gap: 0 }}>
              <span className="flow-t">{s.title}</span>
              <span className="flow-s">{s.sub}<span className="sr"> ({s.state === 'done' ? 'done' : s.state === 'current' ? 'current step' : 'not started'})</span></span>
            </span>
          </>
        )
        return (
          <li key={s.title} className={`flow-step ${s.state}`}>
            {s.to ? <Link to={s.to} className="row" style={{ gap: 12, color: 'inherit', textDecoration: 'none', flexWrap: 'nowrap' }}>{body}</Link> : body}
          </li>
        )
      })}
    </ol>
  )
}

export type StepState = 'waiting' | 'active' | 'done' | 'review' | 'failed'
export function Timeline({ items }: { items: { title: string; sub?: React.ReactNode; state: StepState; aside?: React.ReactNode }[] }) {
  const icon = (s: StepState) => (s === 'done' ? 'checks' : s === 'failed' ? 'close' : s === 'review' ? 'alert' : s === 'active' ? 'refresh' : 'more')
  const word: Record<StepState, string> = { waiting: 'Waiting', active: 'In progress', done: 'Completed', review: 'Needs review', failed: 'Failed' }
  return (
    <ol className="timeline">
      {items.map((it) => (
        <li key={it.title} className={`tl-item ${it.state}`}>
          <span className="tl-dot" aria-hidden><span className={it.state === 'active' ? 'spin' : ''} style={{ display: 'grid' }}><Icon name={icon(it.state)} /></span></span>
          <span className="stack" style={{ gap: 2 }}>
            <span className="tl-t">{it.title}</span>
            {it.sub && <span className="tl-s">{it.sub}</span>}
          </span>
          <span className="caption">{it.aside ?? word[it.state]}</span>
        </li>
      ))}
    </ol>
  )
}
