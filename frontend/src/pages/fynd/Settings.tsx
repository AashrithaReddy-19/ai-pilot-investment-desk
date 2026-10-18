import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../../api/client'
import type { Mode } from '../../api/types'
import type { MeResponse, WorkspaceSettings } from '../../api/viewTypes'
import { Card, PageHeader } from '../../components/fynd/Page'
import { Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { TextField } from '../../components/ui/Fields'
import { useTheme } from '../../state/useTheme'

/** The keys PUT /settings accepts (views.go parseSettings); anything else is refused by the gateway. */
const BUDGET_SET: { key: string; label: string; unit: string }[] = [
  { key: 'total_inr', label: 'Total budget over the horizon', unit: '₹' },
  { key: 'first_year_inr', label: 'First-year budget', unit: '₹' },
  { key: 'recurring_inr_per_year', label: 'Recurring budget', unit: '₹/yr' },
  { key: 'implementation_inr', label: 'Implementation budget', unit: '₹' },
  { key: 'contingency_pct', label: 'Contingency', unit: '%' },
]
const DEFAULTS: { key: string; label: string; unit: string }[] = [
  { key: 'horizon_months', label: 'Horizon', unit: 'months' },
  { key: 'discount_rate', label: 'Discount rate', unit: 'e.g. 0.1' },
  { key: 'assumed_run_cost_pct_of_setup', label: 'Assumed running cost when not stated', unit: '% of setup' },
  { key: 'assumed_adoption_pct', label: 'Assumed adoption when not stated', unit: '%' },
]

const toForm = (m: Record<string, number> | undefined, keys: { key: string }[]) => Object.fromEntries(keys.map((k) => [k.key, m?.[k.key] != null ? String(m[k.key]) : '']))

export default function Settings() {
  const { theme, toggle } = useTheme()
  const [me, setMe] = useState<MeResponse | null>(null)
  const [s, setS] = useState<WorkspaceSettings | null>(null)
  const [loadErr, setLoadErr] = useState<unknown>(null)
  const [budget, setBudget] = useState('')
  const [mode, setMode] = useState<Mode | ''>('')
  const [bset, setBset] = useState<Record<string, string>>({})
  const [defs, setDefs] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const [bad, setBad] = useState<string | null>(null)

  const load = () => {
    setLoadErr(null)
    api.getSettings().then((r) => {
      const v = r.data
      setS(v); setBudget(v.budget == null ? '' : String(v.budget)); setMode(v.mode ?? '')
      setBset(toForm(v.budget_set, BUDGET_SET)); setDefs(toForm(v.defaults, DEFAULTS))
    }).catch(setLoadErr)
  }
  useEffect(() => { load(); api.me().then((r) => setMe(r.data)).catch(() => setMe(null)) }, [])

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const num = (t: string) => { const x = t.trim().replace(/[,\s]/g, ''); return x === '' ? null : Number(x) }
    const b = num(budget)
    if (b !== null && (!Number.isFinite(b) || b < 0)) { setBad('Budget must be a non-negative number, or blank for none.'); return }
    const pack = (vals: Record<string, string>, keys: { key: string; label: string }[], nonNeg: boolean) => {
      const out: Record<string, number> = {}
      for (const k of keys) {
        const n = num(vals[k.key] ?? '')
        if (n === null) continue
        if (!Number.isFinite(n) || (nonNeg && n < 0)) throw new Error(`${k.label} must be a${nonNeg ? ' non-negative' : ''} number.`)
        out[k.key] = n
      }
      return out
    }
    let body: WorkspaceSettings
    try {
      // keep stored keys this form does not show (other engine defaults, weights)
      body = { budget: b, ...(mode ? { mode } : {}), budget_set: pack(bset, BUDGET_SET, true), defaults: { ...(s?.defaults ?? {}), ...pack(defs, DEFAULTS, false) }, ...(s?.weights ? { weights: s.weights } : {}) }
      for (const k of DEFAULTS) if ((defs[k.key] ?? '').trim() === '') delete body.defaults![k.key]
    } catch (x) { setBad((x as Error).message); return }
    setBad(null); setBusy(true); setErr(null); setMsg(null)
    try { const r = (await api.putSettings(body)).data; setS(r); setMsg('Settings saved. The service confirmed it. New dashboard and comparison runs use them.') } catch (x) { setErr(x) } finally { setBusy(false) }
  }

  const notConfigured = loadErr instanceof ApiError && loadErr.code === 'not_configured'
  return (
    <>
      <PageHeader title="Settings" subtitle="Workspace decision context, account and display preferences." />
      <div className="grid-main">
        <Card title="Decision context" sub="Used by every workspace view (dashboard, decision, comparison, yearly savings). Saving needs the admin role.">
          {notConfigured && <p className="caption">Workspace settings are not available: workspace features are not configured on this gateway.</p>}
          {loadErr != null && !notConfigured && <ErrorNotice title="Settings could not be loaded." error={loadErr} onRetry={load} />}
          {!loadErr && s === null && <Skeleton rows={4} height={40} />}
          {s && (
            <form className="stack" onSubmit={save} noValidate>
              <div className="form-grid">
                <TextField id="st-budget" label="Budget (selection)" unit="₹" inputMode="decimal" value={budget} onChange={setBudget} hint="Blank: no budget, every surviving proposal is funded." />
                <div className="field"><label className="label" htmlFor="st-mode">Default mode</label>
                  <select id="st-mode" className="select" value={mode} onChange={(e) => setMode(e.target.value as Mode | '')}>
                    <option value="">Engine default</option><option value="independent">Independent</option><option value="conservative">Conservative</option>
                  </select></div>
              </div>
              <fieldset><legend>Budget lines</legend>
                <div className="form-grid">{BUDGET_SET.map((k) => <TextField key={k.key} id={`st-${k.key}`} label={k.label} unit={k.unit} inputMode="decimal" value={bset[k.key] ?? ''} onChange={(v) => setBset((x) => ({ ...x, [k.key]: v }))} />)}</div>
              </fieldset>
              <fieldset><legend>Engine defaults</legend>
                <div className="form-grid">{DEFAULTS.map((k) => <TextField key={k.key} id={`st-${k.key}`} label={k.label} unit={k.unit} inputMode="decimal" value={defs[k.key] ?? ''} onChange={(v) => setDefs((x) => ({ ...x, [k.key]: v }))} />)}</div>
                <p className="hint">Blank keeps the engine's own documented default. Missing is never sent as zero.</p>
              </fieldset>
              {bad && <div className="field-error" role="alert">{bad}</div>}
              <div className="row"><button className="btn btn-primary" disabled={busy}>{busy ? 'Saving…' : 'Save settings'}</button></div>
              <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
              {err != null && <ErrorNotice title="Settings were not saved." error={err} />}
            </form>
          )}
        </Card>
        <div className="stack">
          <Card title="Account">
            {me ? (
              <dl className="dl">
                <dt>User</dt><dd>{me.user?.display_name || me.user?.email || 'No sign-in'}</dd>
                <dt>Workspace</dt><dd>{me.workspace?.name ?? 'None'}</dd>
                <dt>Role</dt><dd style={{ textTransform: 'capitalize' }}>{me.role}</dd>
                <dt>Authentication</dt><dd>{me.auth_mode === 'off' ? 'Off (local)' : 'Required'}</dd>
              </dl>
            ) : <p className="caption">The account could not be loaded.</p>}
            {me?.auth_mode === 'off' && <p className="hint">With authentication off there are no members and everyone acts as admin. The browser does not send sign-in tokens yet.</p>}
          </Card>
          <Card title="Display">
            <div className="row"><span className="grow">Theme: {theme === 'dark' ? 'Dark' : 'Light'}</span><button className="btn btn-sm" onClick={toggle}>Switch to {theme === 'dark' ? 'light' : 'dark'}</button></div>
          </Card>
          <Card title="System">
            <div className="row"><Link className="btn btn-sm" to="/checks">API and system checks</Link><Link className="btn btn-sm" to="/about-limits">About and limitations</Link></div>
          </Card>
        </div>
      </div>
    </>
  )
}
