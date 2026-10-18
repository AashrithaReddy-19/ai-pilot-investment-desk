import { useEffect, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { api } from '../../api/client'
import type { BalanceSheetConfirm } from '../../api/types'
import type { Upload } from '../../api/viewTypes'
import { Card, Kpi, PageHeader } from '../../components/fynd/Page'
import { Empty, Skeleton } from '../../components/ui/Bits'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { BalancePanel } from '../../features/compare/BalancePanel'
import { formatDateTime, formatINR } from '../../domain/format'
import { describeSource } from '../../domain/statement'
import { useFynd } from '../../state/useFynd'

/**
 * Today's AI spend as the documents state it: confirmed balance-sheet rows from stored uploads, or a document
 * read once without storing. Only rows the person confirmed count; nothing is categorised by the browser.
 */
export default function Baseline() {
  const location = useLocation() as { state?: { file?: File } }
  const { burn, setBurn } = useFynd()
  const [uploads, setUploads] = useState<Upload[] | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const handed = location.state?.file ?? null

  const load = () => { setErr(null); api.listUploads(50).then(setUploads).catch(setErr) }
  useEffect(load, [])

  const confirmedUploads = (uploads ?? []).filter((u) => u.kind === 'balance_sheet' && u.status === 'confirmed' && u.confirmed)
  const latest = confirmedUploads[0]
  const latestResult = latest ? (latest.confirmed as BalanceSheetConfirm) : null
  const waiting = (uploads ?? []).filter((u) => u.kind === 'balance_sheet' && u.status === 'parsed')

  return (
    <>
      <PageHeader title="Financial Baseline" subtitle="What is already being spent on AI, taken only from document rows you confirmed."
        actions={<><Link to="/" className="btn">Upload another document</Link><Link to="/dashboard" className="btn btn-primary">Continue to dashboard</Link></>} />

      <div className="grid-3">
        <Kpi label="AI burn to date (this session)" value={burn ? formatINR(burn.value) : null}
          reason="No balance-sheet row has been confirmed in this session." foot={burn ? `Source: ${burn.source}` : undefined} />
        <Kpi label="Latest stored confirmation" value={latestResult ? formatINR(latestResult.ai_burn_to_date) : null}
          reason="No stored upload has been confirmed yet." foot={latest ? `${latest.filename} · confirmed ${formatDateTime(latest.updated_at)}` : undefined} />
        <Kpi label="Next-FY AI cost (engine)" value={latestResult?.next_fy?.next_fy_cost_inr != null ? formatINR(latestResult.next_fy.next_fy_cost_inr) : null}
          reason="Calculated by the engine once rows are confirmed." foot={latestResult?.next_fy?.inflation_flag ? 'The engine flagged cost inflation.' : undefined} />
      </div>

      {latestResult && burn?.source !== `confirmed upload “${latest!.filename}”` && typeof latestResult.ai_burn_to_date === 'number' && (
        <div className="notice notice-info">
          <div>Use the latest stored confirmation ({formatINR(latestResult.ai_burn_to_date)} from “{latest!.filename}”) as the AI burn for the dashboard?</div>
          <button className="btn btn-sm" onClick={() => setBurn({ value: latestResult.ai_burn_to_date as number, source: `confirmed upload “${latest!.filename}”` })}>Use it</button>
        </div>
      )}

      {waiting.length > 0 && (
        <div className="notice notice-warn"><div>{waiting.length} stored document{waiting.length === 1 ? ' is' : 's are'} waiting for review: {waiting.map((u, i) => <span key={u.id}>{i > 0 && ', '}<Link to={`/processing/${u.id}`}>{u.filename}</Link></span>)}.</div></div>
      )}

      <Card title="Confirmed expenditure rows" sub="From the most recent confirmed stored upload. Reporting period: as stated in the document; the engine does not return a period for these rows.">
        {err != null && <ErrorNotice title="Stored uploads could not be listed." error={err} onRetry={load} />}
        {err == null && uploads === null && <Skeleton rows={3} height={40} />}
        {uploads && !latestResult && <Empty title="No confirmed rows yet">Store a CSV or XLSX balance sheet from Home and confirm its AI-spend rows, or read a document below without storing it.</Empty>}
        {latestResult && (
          <div className="tbl-wrap"><table className="tbl">
            <caption className="sr">Confirmed AI-spend rows</caption>
            <thead><tr><th scope="col" className="num">Row</th><th scope="col">Line item</th><th scope="col" className="num">Amount (INR)</th><th scope="col">Source</th></tr></thead>
            <tbody>{latestResult.confirmations.map((c) => (
              <tr key={c.row}>
                <td className="num mono">{c.row}</td>
                <td className="strong">{c.label}</td>
                <td className="num">{formatINR(c.amount_inr)}</td>
                <td className="caption">{describeSource(c.source) || `${latest!.filename}, row ${c.row}`}</td>
              </tr>))}</tbody>
          </table></div>
        )}
        {latestResult && <p className="hint">Unit: {latestResult.unit.unit} ({latestResult.unit.source}) · amount column “{latestResult.amount_column_used}” · currency INR. {latestResult.warnings.length > 0 && <>Warnings: {latestResult.warnings.join(' ')}</>}</p>}
      </Card>

      <section className="stack" style={{ gap: 12 }} aria-label="Read a document without storing it">
        <h2 className="card-title">Read a document without storing it</h2>
        <BalancePanel initialFile={handed} onConfirmed={(r) => { if (typeof r.ai_burn_to_date === 'number') setBurn({ value: r.ai_burn_to_date, source: `${r.source} (read without storing)` }) }} />
      </section>
    </>
  )
}
