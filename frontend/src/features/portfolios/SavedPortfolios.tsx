import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../../api/client'
import type { PortfolioSummary, SavedPortfolio } from '../../api/types'
import { draftsFromScope } from '../../domain/draft'
import { formatDateTime, formatINR, formatPayback, formatPct } from '../../domain/format'
import { useWorkspace } from '../../state/useWorkspace'
import { AssumedTag, Empty, Skeleton, SyntheticTag } from '../../components/ui/Bits'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { ErrorNotice } from '../../components/ui/ErrorNotice'

/** Saved portfolios from the database: list, reopen and delete. Every change is confirmed by the service before it is reported. */
export function SavedPortfolios({ refreshKey = 0 }: { refreshKey?: number }) {
  const [list, setList] = useState<PortfolioSummary[] | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [openName, setOpenName] = useState<string | null>(null)
  const [doc, setDoc] = useState<SavedPortfolio | null>(null)
  const [docError, setDocError] = useState<unknown>(null)
  const [docLoading, setDocLoading] = useState(false)
  const [toDelete, setToDelete] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [delError, setDelError] = useState<unknown>(null)

  const refresh = useCallback(async () => {
    try { setList(await api.listPortfolios()); setError(null) } catch (e) { setError(e) }
  }, [])
  useEffect(() => { void refresh() }, [refresh, refreshKey])

  const open = async (name: string) => {
    if (openName === name) { setOpenName(null); setDoc(null); return }
    setOpenName(name); setDoc(null); setDocError(null); setDocLoading(true)
    try { setDoc((await api.getPortfolio(name)).data) } catch (e) { setDocError(e) } finally { setDocLoading(false) }
  }
  const remove = async () => {
    if (!toDelete) return
    setDeleting(true); setDelError(null)
    try {
      await api.deletePortfolio(toDelete)
      setMsg(`Deleted “${toDelete}”. The service confirmed it.`)
      if (openName === toDelete) { setOpenName(null); setDoc(null) }
      setToDelete(null)
      await refresh()
    } catch (e) { setDelError(e); setToDelete(null) } finally { setDeleting(false) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <div aria-live="polite">{msg && <div className="notice notice-ok"><div>{msg}</div></div>}</div>
      {delError != null && <ErrorNotice title="Nothing was deleted." error={delError} />}
      {error != null && <ErrorNotice title="Saved portfolios could not be loaded." error={error} onRetry={() => void refresh()} />}
      {error == null && list === null && <Skeleton rows={3} height={48} />}
      {error == null && list?.length === 0 && <Empty title="No saved portfolios">Optimize a portfolio in the decision workspace and save it. It will be listed here, ready to reopen.</Empty>}
      {list && list.length > 0 && (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr">Saved portfolios</caption>
            <thead><tr><th className="sticky" scope="col">Name</th><th scope="col">Last saved</th><th scope="col">Benefit view</th><th className="num" scope="col">Budget</th><th className="num" scope="col">Funded</th><th scope="col"><span className="sr">Actions</span></th></tr></thead>
            <tbody>
              {list.map((p) => (
                <tr key={p.id} aria-selected={openName === p.name}>
                  <td className="sticky strong">{p.name}</td>
                  <td className="caption">{formatDateTime(p.updated_at)}</td>
                  <td style={{ textTransform: 'capitalize' }}>{p.mode}</td>
                  <td className="num">{formatINR(p.budget_inr)}</td>
                  <td className="num">{p.selected_count}</td>
                  <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="btn btn-quiet btn-sm" aria-expanded={openName === p.name} onClick={() => void open(p.name)}>{openName === p.name ? 'Close' : 'Open'}</button>
                    <button className="btn btn-quiet btn-sm" onClick={() => setToDelete(p.name)} aria-label={`Delete ${p.name}`}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {docLoading && <Skeleton rows={3} height={60} />}
      {docError != null && <ErrorNotice title="That portfolio could not be opened." error={docError} />}
      {doc && <Viewer doc={doc} />}
      <ConfirmDialog open={toDelete !== null} danger confirmLabel="Delete portfolio" title={`Delete “${toDelete}”?`} busy={deleting}
        body={<p>This permanently removes the saved portfolio and its per-pilot rows from the database. It cannot be undone.</p>} onConfirm={() => void remove()} onCancel={() => setToDelete(null)} />
    </div>
  )
}

function Viewer({ doc }: { doc: SavedPortfolio }) {
  const loadScope = useWorkspace((s) => s.loadScope)
  const nav = useNavigate()
  const scope = doc.negotiated_scope
  const ext = new Map(scope.extension.pilots.map((p) => [p.id, p]))
  const pf = doc.final_evidence.portfolio
  const synthetic = doc.starting_state.synthetic === true

  const reopen = () => {
    loadScope(draftsFromScope(scope.fixture_as_used, scope.extension), doc.mode, doc.budget_inr)
    nav('/')
  }

  return (
    <div className="panel" style={{ gap: 24 }}>
      <div className="row" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div><h3 className="h2">{doc.name}</h3>
          <p className="caption">Created {formatDateTime(doc.created_at)} · last saved {formatDateTime(doc.updated_at)} · {doc.mode} view · budget {formatINR(doc.budget_inr)}</p></div>
        <button className="btn" onClick={reopen}>Load these pilots into the workspace</button>
      </div>

      <div className="stack">
        <h4 className="h3">Pilots used (negotiated scope)</h4>
        <div className="tbl-wrap"><table className="tbl">
          <caption className="sr">Pilots and assumptions used when this portfolio was saved</caption>
          <thead><tr><th className="sticky" scope="col">Pilot</th><th className="num" scope="col">Setup</th><th className="num" scope="col">Running / yr</th><th className="num" scope="col">Benefit / yr</th><th scope="col">Overlap group</th><th className="num" scope="col">Adoption</th><th scope="col">Readiness</th><th className="num" scope="col">Measured adoption</th><th className="num" scope="col">Measured quality</th></tr></thead>
          <tbody>{scope.fixture_as_used.records.pilots.map((p) => { const e = ext.get(p.id); const na = <span className="muted">Not provided</span>; return (
            <tr key={p.id}><td className="sticky"><div className="cell-pilot"><span className="id">{p.id}</span><span className="nm">{p.task}</span></div></td>
              <td className="num">{formatINR(p.setup_cost_inr)}</td><td className="num">{p.annual_running_cost_inr == null ? na : formatINR(p.annual_running_cost_inr)}</td><td className="num">{formatINR(p.annual_benefit_inr)}</td>
              <td>{p.overlap_group || na}</td><td className="num">{e?.adoption_pct == null ? na : formatPct(e.adoption_pct)}</td><td style={{ textTransform: 'capitalize' }}>{e?.readiness ?? na}</td>
              <td className="num">{e?.observed_adoption_pct == null ? na : formatPct(e.observed_adoption_pct)}</td><td className="num">{e?.observed_quality_pct == null ? na : formatPct(e.observed_quality_pct)}</td></tr>) })}</tbody>
        </table></div>
      </div>

      <div className="stack">
        <h4 className="h3">Final evidence</h4>
        <p>{pf.message}</p>
        <div className="tbl-wrap"><table className="tbl">
          <caption className="sr">Each pilot's outcome in this saved portfolio</caption>
          <thead><tr><th className="sticky" scope="col">Pilot</th><th scope="col">Decision</th><th className="num" scope="col">Setup</th><th className="num" scope="col">Running / yr</th><th className="num" scope="col">Net / yr</th><th className="num" scope="col">Payback</th><th scope="col">Reason</th></tr></thead>
          <tbody>{doc.pilots.map((r) => (
            <tr key={r.pilot_id}><td className="sticky"><span className="mono">{r.pilot_id}</span></td>
              <td><span className="badge"><span className={`dot ${r.selected ? 'dot-accent' : 'dot-off'}`} aria-hidden />{r.selected ? 'Funded' : 'Not funded'}</span></td>
              <td className="num">{formatINR(r.setup_inr)}</td><td className="num">{formatINR(r.run_inr)} {r.run_assumed && <AssumedTag />}</td>
              <td className={`num${r.net_inr !== null && r.net_inr <= 0 ? ' neg' : ''}`}>{formatINR(r.net_inr)}</td><td className="num">{formatPayback(r.payback_months)}</td>
              <td className="wrap-cell">{r.reason ?? <span className="muted">Selected</span>}</td></tr>))}</tbody>
        </table></div>
      </div>

      <div className="notice notice-info" role="note"><div>
        <strong>About the stored starting state.</strong> The service records its shipped case file {synthetic && <SyntheticTag />} as the “starting state” of every saved portfolio, even when you supplied your own pilots. The pilots that were actually evaluated are the ones listed above under negotiated scope.
      </div></div>
    </div>
  )
}
