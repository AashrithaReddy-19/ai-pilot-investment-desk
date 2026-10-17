import { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useTheme } from '../../state/useTheme'
import { Icon } from '../ui/Icon'
import { ServiceProvider } from './ServiceContext'
import { StatusIndicator } from './StatusIndicator'

interface Item { to: string; label: string; icon: string; end?: boolean; title: string }
const GROUPS: { label: string; items: Item[] }[] = [
  { label: 'Decide', items: [
    { to: '/', label: 'Decision workspace', icon: 'workspace', end: true, title: 'Decision workspace' },
    { to: '/optimize', label: 'Portfolio optimization', icon: 'optimize', title: 'Portfolio optimization' },
    { to: '/compare', label: 'Compare proposals', icon: 'compare', title: 'Compare proposals' },
    { to: '/balance-sheet', label: 'Balance sheet', icon: 'compare', title: 'Balance sheet' },
    { to: '/what-if', label: 'What-if and sweep', icon: 'checks', title: 'What-if and sweep' },
    { to: '/evidence', label: 'Evidence and decision', icon: 'about', title: 'Evidence and decision' },
  ] },
  { label: 'Lifecycle', items: [
    { to: '/tracker', label: 'Pilot tracker', icon: 'observe', title: 'Pilot tracker' },
    { to: '/memo', label: 'Decision memo', icon: 'checks', title: 'Decision memo' },
  ] },
  { label: 'Records', items: [
    { to: '/portfolios', label: 'Saved portfolios', icon: 'saved', title: 'Saved portfolios' },
    { to: '/proposals', label: 'Proposals', icon: 'optimize', title: 'Proposals' },
    { to: '/observations', label: 'Observations and stop rules', icon: 'observe', title: 'Observations and stop rules' },
    { to: '/interviews', label: 'Interviews', icon: 'interviews', title: 'Interviews' },
  ] },
  { label: 'System', items: [
    { to: '/checks', label: 'API and system checks', icon: 'checks', title: 'API and system checks' },
    { to: '/about-limits', label: 'About and limitations', icon: 'about', title: 'About and limitations' },
  ] },
]
const ALL = GROUPS.flatMap((g) => g.items)

export default function AppShell() {
  const { theme, toggle } = useTheme()
  const { pathname } = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const menuBtn = useRef<HTMLButtonElement>(null)
  const rail = useRef<HTMLElement>(null)

  const here = ALL.find((i) => (i.end ? pathname === i.to : pathname.startsWith(i.to)))?.title ?? 'Decision workspace'

  useEffect(() => { document.title = `${here} · C23 AI Pilot Investment Desk` }, [here])
  useEffect(() => { setMobileOpen(false); window.scrollTo(0, 0) }, [pathname])
  useEffect(() => {
    if (!mobileOpen) return
    rail.current?.querySelector<HTMLElement>('a')?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { setMobileOpen(false); menuBtn.current?.focus() } }
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('keydown', onKey) }
  }, [mobileOpen])

  return (
    <ServiceProvider>
      <a className="skip" href="#main">Skip to content</a>
      <div className={`app${collapsed ? ' collapsed' : ''}`}>
        <aside ref={rail} className={`rail${mobileOpen ? ' open' : ''}`} aria-label="Application navigation">
          <div className="rail-brand">
            <span className="brand-mark" aria-hidden />
            <span className="brand-text"><span className="brand-name">C23</span><span className="brand-desc">AI Pilot Investment Desk</span></span>
          </div>
          <nav aria-label="Primary" className="stack" style={{ gap: 14 }}>
            {GROUPS.map((g) => (
              <div className="nav-group" key={g.label}>
                <span className="nav-label">{g.label}</span>
                {g.items.map((i) => (
                  <NavLink key={i.to} to={i.to} end={i.end} title={collapsed ? i.title : undefined} className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}>
                    <Icon name={i.icon} /><span className="nav-text">{i.label}</span>
                  </NavLink>
                ))}
              </div>
            ))}
          </nav>
          <div className="rail-foot">
            <button className="nav-item collapse-btn" onClick={() => setCollapsed((c) => !c)} aria-pressed={collapsed} title={collapsed ? 'Expand navigation' : 'Collapse navigation'}>
              <Icon name={collapsed ? 'expand' : 'collapse'} /><span className="nav-text">{collapsed ? 'Expand navigation' : 'Collapse navigation'}</span>
            </button>
            <p className="hint foot-text" style={{ padding: '0 12px' }}>Rule-based calculations. No AI model is used.</p>
          </div>
        </aside>
        <div className={`scrim${mobileOpen ? ' show' : ''}`} onClick={() => setMobileOpen(false)} aria-hidden />

        <div className="main-col">
          <header className="topbar">
            <button ref={menuBtn} className="btn btn-quiet icon-btn menu-btn" aria-label="Open navigation" aria-expanded={mobileOpen} onClick={() => setMobileOpen((o) => !o)}>
              <Icon name="menu" />
            </button>
            <div className="topbar-title">
              <span className="where">{here}</span>
              <span className="what">C23 · AI Pilot Investment Decision Desk</span>
            </div>
            <div className="topbar-actions">
              <StatusIndicator />
              <button className="btn btn-quiet icon-btn" onClick={toggle} aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'} title={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}>
                <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
              </button>
            </div>
          </header>
          <main id="main" className="content" tabIndex={-1}>
            <Outlet />
          </main>
          <footer className="footer">
            <span>Figures come from the inputs you enter, calculated by the rules engine. They are assumptions, not market evidence.</span>
            <span>Case C23 is synthetic teaching data.</span>
          </footer>
        </div>
      </div>
    </ServiceProvider>
  )
}
