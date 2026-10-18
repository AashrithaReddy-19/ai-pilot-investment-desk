import { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useTheme } from '../../state/useTheme'
import { Icon } from '../ui/Icon'
import { AccountMenu } from './AccountMenu'
import { ServiceProvider } from './ServiceContext'
import { StatusIndicator } from './StatusIndicator'

interface Item { to: string; label: string; icon: string; end?: boolean; title: string }

/** The eight FYND workflow screens, in the wireframe's order. */
const PRIMARY: Item[] = [
  { to: '/', label: 'Home', icon: 'home', end: true, title: 'Home' },
  { to: '/dashboard', label: 'Dashboard', icon: 'dashboard', title: 'Executive dashboard' },
  { to: '/baseline', label: 'Financial Baseline', icon: 'baseline', title: 'Financial baseline' },
  { to: '/ai-proposals', label: 'AI Proposals', icon: 'proposals', title: 'AI proposals' },
  { to: '/comparison', label: 'Comparison', icon: 'compare', title: 'Proposal comparison' },
  { to: '/scenarios', label: 'Forecast & Scenarios', icon: 'forecast', title: 'Forecast and scenarios' },
  { to: '/analysis', label: 'Decision Analysis', icon: 'analysis', title: 'Decision analysis' },
  { to: '/reports', label: 'Reports', icon: 'reports', title: 'Reports' },
]

/** The existing tools stay reachable at their original routes, grouped out of the way. */
const WORKBENCH: Item[] = [
  { to: '/workspace', label: 'Decision workspace', icon: 'workspace', title: 'Decision workspace' },
  { to: '/optimize', label: 'Portfolio optimization', icon: 'optimize', title: 'Portfolio optimization' },
  { to: '/compare', label: 'Compare proposals', icon: 'compare', title: 'Compare proposals' },
  { to: '/balance-sheet', label: 'Balance sheet', icon: 'compare', title: 'Balance sheet' },
  { to: '/what-if', label: 'What-if and sweep', icon: 'checks', title: 'What-if and sweep' },
  { to: '/evidence', label: 'Evidence and decision', icon: 'about', title: 'Evidence and decision' },
  { to: '/tracker', label: 'Pilot tracker', icon: 'observe', title: 'Pilot tracker' },
  { to: '/memo', label: 'Decision memo', icon: 'checks', title: 'Decision memo' },
  { to: '/portfolios', label: 'Saved portfolios', icon: 'saved', title: 'Saved portfolios' },
  { to: '/proposals', label: 'Proposals (records)', icon: 'optimize', title: 'Proposals' },
  { to: '/observations', label: 'Observations and stop rules', icon: 'observe', title: 'Observations and stop rules' },
  { to: '/interviews', label: 'Interviews', icon: 'interviews', title: 'Interviews' },
  { to: '/history', label: 'History and audit', icon: 'saved', title: 'History and audit' },
  { to: '/checks', label: 'API and system checks', icon: 'checks', title: 'API and system checks' },
  { to: '/about-limits', label: 'About and limitations', icon: 'about', title: 'About and limitations' },
]
const EXTRA: Item[] = [
  { to: '/settings', label: 'Settings', icon: 'settings', title: 'Settings' },
  { to: '/processing', label: 'Processing', icon: 'refresh', title: 'Processing and review' },
]
const ALL = [...PRIMARY, ...WORKBENCH, ...EXTRA]

export default function AppShell() {
  const { theme, toggle } = useTheme()
  const { pathname } = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const menuBtn = useRef<HTMLButtonElement>(null)
  const rail = useRef<HTMLElement>(null)

  // segment-wise, so /compare never matches /comparison
  const under = (i: Item) => pathname === i.to || (!i.end && pathname.startsWith(`${i.to}/`))
  const here = ALL.find(under)?.title ?? 'Home'
  const inWorkbench = WORKBENCH.some(under)

  useEffect(() => { document.title = `${here} · FYND` }, [here])
  useEffect(() => { setMobileOpen(false); window.scrollTo(0, 0) }, [pathname])
  useEffect(() => {
    if (!mobileOpen) return
    rail.current?.querySelector<HTMLElement>('a')?.focus()
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { setMobileOpen(false); menuBtn.current?.focus() } }
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('keydown', onKey) }
  }, [mobileOpen])

  const link = (i: Item) => (
    <NavLink key={i.to} to={i.to} end={i.end} title={collapsed ? i.title : undefined} className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}>
      <Icon name={i.icon} /><span className="nav-text">{i.label}</span>
    </NavLink>
  )

  return (
    <ServiceProvider>
      <a className="skip" href="#main">Skip to content</a>
      <div className={`app${collapsed ? ' collapsed' : ''}`}>
        <aside ref={rail} className={`rail${mobileOpen ? ' open' : ''}`} aria-label="Application navigation">
          <div className="wordmark">
            <span className="wordmark-mark" aria-hidden>F</span>
            <span className="wordmark-text"><span className="wordmark-name">FYND</span><span className="wordmark-desc">AI Investment Decision Desk</span></span>
          </div>
          <nav aria-label="Primary" className="stack" style={{ gap: 12 }}>
            <div className="nav-group">{PRIMARY.map(link)}</div>
            <details className="nav-group nav-more" open={inWorkbench || undefined}>
              <summary className="nav-item" title={collapsed ? 'Workbench' : undefined}>
                <Icon name="tools" /><span className="nav-text">Workbench</span><Icon name="chevron" />
              </summary>
              <div className="nav-sub">{WORKBENCH.map(link)}</div>
            </details>
          </nav>
          <div className="rail-foot">
            {link(EXTRA[0])}
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
              <span className="what">FYND · AI Investment Decision Desk</span>
            </div>
            <div className="topbar-actions">
              <StatusIndicator />
              <button className="btn btn-quiet icon-btn" onClick={toggle} aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'} title={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}>
                <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
              </button>
              <AccountMenu />
            </div>
          </header>
          <main id="main" className="content" tabIndex={-1}>
            <Outlet />
          </main>
          <footer className="footer">
            <span>Every figure is calculated by the rules engine from the inputs and documents you supply. Assumptions are labelled as such.</span>
          </footer>
        </div>
      </div>
    </ServiceProvider>
  )
}
