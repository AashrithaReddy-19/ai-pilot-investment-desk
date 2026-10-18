import { useMemo, useState } from 'react'
import { api, ApiError, describeError } from '../../api/client'
import type { BalanceUnit, ImportRecord, PilotsImportParse } from '../../api/types'
import { Pill } from '../../components/fynd/Page'
import { Dropzone, fmtBytes } from '../../components/fynd/Dropzone'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { formatINR } from '../../domain/format'
import {
  IMPORT_ACCEPT, IMPORT_TEMPLATE_CSV, PROPOSAL_NAME_RE, candidateFromEngineRow, importKind, parseJsonProposals,
} from '../../domain/fyndImport'
import type { ImportCandidate } from '../../domain/fyndImport'

const MAX_IMPORT_BYTES = 1_048_576 // the gateway's MAX_BODY_BYTES default for uploads that go to the engine
const UNITS: { value: BalanceUnit; label: string }[] = [
  { value: 'auto', label: 'Detect from the file' }, { value: 'rupees', label: 'Rupees' }, { value: 'thousands', label: 'Thousands' },
  { value: 'lakh', label: 'Lakh' }, { value: 'crore', label: 'Crore' },
]
const NAME_MARK = 'The name must'

type Outcome = { state: 'created' | 'failed'; message: string }

/** Import proposals from a file: CSV and XLSX through the engine's importer, JSON read in the browser. Nothing is stored until the person confirms. */
export function ProposalImport({ switcher, onClose, onImported }: { switcher: React.ReactNode; onClose: () => void; onImported: (created: number) => void }) {
  const [file, setFile] = useState<File | null>(null)
  const [unit, setUnit] = useState<BalanceUnit>('auto')
  const [candidates, setCandidates] = useState<ImportCandidate[] | null>(null)
  const [rejected, setRejected] = useState<ImportRecord[]>([])
  const [info, setInfo] = useState<{ unit?: string; warnings: string[] }>({ warnings: [] })
  const [names, setNames] = useState<Record<string, string>>({})
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({})
  const [finished, setFinished] = useState(false)

  const reset = () => { setCandidates(null); setRejected([]); setInfo({ warnings: [] }); setNames({}); setPicked(new Set()); setOutcomes({}); setFinished(false); setErr(null); setNotice(null) }

  const read = async (f: File, u: BalanceUnit) => {
    reset()
    const k = importKind(f.name)
    if (k === 'unsupported') {
      setNotice(`“${f.name}” cannot be imported as proposals. PDF and the older .xls format are not read for proposals: save the table as CSV or XLSX (or write JSON) and import that.`)
      setFile(null); return
    }
    if (f.size === 0) { setNotice(`“${f.name}” is empty.`); setFile(null); return }
    if (f.size > MAX_IMPORT_BYTES) { setNotice(`“${f.name}” is ${fmtBytes(f.size)}; the import limit is ${fmtBytes(MAX_IMPORT_BYTES)}.`); setFile(null); return }
    setFile(f); setBusy(true)
    try {
      let list: ImportCandidate[]
      if (k === 'json') {
        const r = parseJsonProposals(await f.text())
        if (r.problem) { setNotice(r.problem); setFile(null); return }
        list = r.candidates
      } else {
        const p: PilotsImportParse = (await api.importPilotsParse(f, u)).data
        setInfo({ unit: `${p.unit.unit} (${p.unit.source})`, warnings: [...p.warnings, ...p.flags.map((x) => x.message)] })
        setRejected(p.records.filter((r) => !r.pilot))
        list = p.records.filter((r): r is ImportRecord & { pilot: NonNullable<ImportRecord['pilot']> } => !!r.pilot)
          .map((r) => candidateFromEngineRow({ line: r.line, pilot: r.pilot as unknown as Record<string, unknown>, extension: r.extension }, f.name))
      }
      setCandidates(list)
      setNames(Object.fromEntries(list.map((c) => [c.key, c.name])))
      setPicked(new Set(list.filter((c) => c.problems.every((x) => x.startsWith(NAME_MARK)) ? PROPOSAL_NAME_RE.test(c.name) : false).map((c) => c.key)))
    } catch (e) { setErr(e); setFile(null) } finally { setBusy(false) }
  }

  const fieldProblems = (c: ImportCandidate) => c.problems.filter((x) => !x.startsWith(NAME_MARK))
  const nameOk = (c: ImportCandidate) => PROPOSAL_NAME_RE.test((names[c.key] ?? c.name).trim())
  const importable = (c: ImportCandidate) => fieldProblems(c).length === 0 && nameOk(c)
  const selected = useMemo(() => (candidates ?? []).filter((c) => picked.has(c.key) && importable(c)), [candidates, picked, names]) // eslint-disable-line react-hooks/exhaustive-deps
  const dupInFile = useMemo(() => {
    const seen = new Map<string, number>()
    for (const c of candidates ?? []) { const n = (names[c.key] ?? c.name).trim().toLowerCase(); seen.set(n, (seen.get(n) ?? 0) + 1) }
    return seen
  }, [candidates, names])

  const run = async () => {
    setBusy(true); setErr(null)
    const out: Record<string, Outcome> = {}
    let created = 0
    for (const c of selected) {
      try {
        const saved = (await api.createProposal({ name: (names[c.key] ?? c.name).trim(), inputs: c.inputs, notes: c.notes })).data
        out[c.key] = { state: 'created', message: `Stored (inputs v${saved.version})` }
        created++
      } catch (e) {
        out[c.key] = { state: 'failed', message: e instanceof ApiError && e.code === 'duplicate_name' ? 'A proposal with this name already exists. Rename it and import again.' : describeError(e) }
      }
      setOutcomes({ ...out })
    }
    setBusy(false); setFinished(true)
    if (created > 0) onImported(created)
  }

  const download = () => {
    const url = URL.createObjectURL(new Blob([IMPORT_TEMPLATE_CSV], { type: 'text/csv' }))
    const a = document.createElement('a'); a.href = url; a.download = 'proposals-template.csv'; document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(url)
  }
  const createdCount = Object.values(outcomes).filter((o) => o.state === 'created').length

  return (
    <div className="dlg">
      <div className="dlg-head">
        <div>
          <h2 className="h2" id="pf-title">Import proposals</h2>
          <p className="caption">Read proposals from a file, check them, then store the ones you choose.</p>
        </div>
        <button type="button" className="btn btn-quiet icon-btn" onClick={onClose} aria-label="Close">×</button>
      </div>
      <div className="dlg-body">
        {switcher}
        {!candidates && (
          <>
            <Dropzone id="pi-file" accept={IMPORT_ACCEPT} onFile={(f) => void read(f, unit)} disabled={busy}
              hint={<><strong>CSV</strong> and <strong>Excel (.xlsx)</strong> are read by the rules engine; <strong>JSON</strong> is read here. Up to {fmtBytes(MAX_IMPORT_BYTES)}. PDF and the older .xls cannot be imported as proposals.</>} />
            <div className="toolbar">
              <div className="field"><label className="label" htmlFor="pi-unit">Amounts in (CSV and Excel)</label>
                <select id="pi-unit" className="select" value={unit} onChange={(e) => { const u = e.target.value as BalanceUnit; setUnit(u); if (file) void read(file, u) }}>
                  {UNITS.map((u) => <option key={u.value} value={u.value}>{u.label}</option>)}
                </select></div>
              <button type="button" className="btn" onClick={download}>Download a blank CSV template</button>
            </div>
            <details className="more"><summary>What each format needs</summary>
              <div className="more-body caption">
                <p><strong>CSV / Excel:</strong> one row per proposal. An <span className="mono">id</span> column (or pilot_id, pilot, proposal_id) is required; <span className="mono">task</span> becomes the proposal name. Columns: setup_cost_inr, annual_benefit_inr (required), annual_running_cost_inr, benefit_type, overlap_group, adoption_pct, readiness, ramp_months, delay_months.</p>
                <p><strong>JSON:</strong> an array, or {'{ "proposals": [...] }'}. Each item is <span className="mono">{'{ "name", "notes"?, "inputs": { ... } }'}</span> or the same fields written flat beside the name. Optional <span className="mono">baseline_annual_cost_inr</span> sets the current annual cost of the process.</p>
                <p>Blank cells stay blank: they are never turned into zero.</p>
              </div>
            </details>
          </>
        )}
        {busy && !candidates && <p className="caption" role="status">Reading the file…</p>}
        {notice && <div className="notice notice-bad" role="alert"><div>{notice}</div></div>}
        {err != null && <ErrorNotice title="The file could not be read." error={err} />}

        {candidates && file && (
          <>
            <div className="file-row">
              <div className="grow"><div className="strong">{file.name}</div>
                <div className="caption">{candidates.length} proposal{candidates.length === 1 ? '' : 's'} found{rejected.length > 0 ? ` · ${rejected.length} row${rejected.length === 1 ? '' : 's'} the importer rejected` : ''}{info.unit ? ` · amounts in ${info.unit}` : ''}</div></div>
              <button type="button" className="btn btn-sm" onClick={() => { reset(); setFile(null) }} disabled={busy}>Choose another file</button>
            </div>
            {info.warnings.length > 0 && <div className="notice notice-warn"><div><ul>{info.warnings.map((w) => <li key={w}>{w}</li>)}</ul></div></div>}
            {candidates.length === 0 && <div className="notice notice-warn"><div>No importable proposal was found in the file.</div></div>}
            {candidates.length > 0 && (
              <div className="tbl-wrap"><table className="tbl">
                <caption className="sr">Proposals found in the file</caption>
                <thead><tr><th scope="col"><span className="sr">Import</span></th><th scope="col">Name</th><th scope="col">Source</th><th scope="col" className="num">Implementation</th><th scope="col" className="num">Annual benefit</th><th scope="col">Result</th></tr></thead>
                <tbody>{candidates.map((c) => {
                  const o = outcomes[c.key]
                  const fp = fieldProblems(c)
                  const num = (k: string) => (typeof c.inputs[k] === 'number' ? formatINR(c.inputs[k] as number) : <span className="muted">Not provided</span>)
                  const dup = (dupInFile.get((names[c.key] ?? c.name).trim().toLowerCase()) ?? 0) > 1
                  return (
                    <tr key={c.key}>
                      <td><input type="checkbox" aria-label={`Import ${names[c.key] ?? c.name}`} checked={picked.has(c.key) && importable(c)} disabled={!importable(c) || busy || finished}
                        onChange={(e) => setPicked((s) => { const n = new Set(s); if (e.target.checked) n.add(c.key); else n.delete(c.key); return n })} /></td>
                      <td style={{ minWidth: 200 }}>
                        <input className="input" style={{ minHeight: 32 }} aria-label={`Name for ${c.origin}`} value={names[c.key] ?? c.name} maxLength={80} disabled={busy || finished}
                          aria-invalid={!nameOk(c) || undefined} onChange={(e) => setNames((x) => ({ ...x, [c.key]: e.target.value }))} />
                        {!nameOk(c) && <span className="field-error">Use letters, digits, spaces, hyphens or underscores (1–80).</span>}
                        {dup && <span className="field-error">Another row has the same name.</span>}
                      </td>
                      <td className="caption">{c.origin}</td>
                      <td className="num">{num('setup_cost_inr')}</td>
                      <td className="num">{num('annual_benefit_inr')}</td>
                      <td style={{ minWidth: 200 }}>
                        {o ? <Pill tone={o.state === 'created' ? 'ok' : 'bad'}>{o.state === 'created' ? 'Stored' : 'Not stored'}</Pill> : fp.length === 0 ? <Pill tone="info">Ready</Pill> : <Pill tone="warn">Needs fixing</Pill>}
                        {o && <span className="cell-sub" style={{ whiteSpace: 'normal' }}>{o.message}</span>}
                        {!o && fp.map((m) => <span key={m} className="cell-sub" style={{ whiteSpace: 'normal' }}>{m}</span>)}
                      </td>
                    </tr>
                  )
                })}</tbody>
              </table></div>
            )}
            {rejected.length > 0 && (
              <details className="more"><summary>{rejected.length} row{rejected.length === 1 ? '' : 's'} the importer rejected</summary>
                <ul className="more-body caption">{rejected.map((r) => <li key={r.line}>Line {r.line}: {(r.errors ?? []).map((e) => `${e.field} — ${e.reason}`).join('; ') || 'rejected'}</li>)}</ul>
              </details>
            )}
            <p className="hint">Proposals that need fixing cannot be imported from here: correct the file and choose it again, or fix the name in the table. Imported proposals are stored exactly as read; calculated figures come later from the engine.</p>
          </>
        )}
      </div>
      <div className="dlg-foot">
        <span className="grow hint" role="status">{finished ? `${createdCount} stored${Object.keys(outcomes).length - createdCount > 0 ? `, ${Object.keys(outcomes).length - createdCount} not stored` : ''}.` : candidates ? `${selected.length} selected` : ''}</span>
        {finished
          ? <button type="button" className="btn btn-primary" onClick={onClose}>Done</button>
          : <>
              <button type="button" className="btn" onClick={onClose} disabled={busy}>Cancel</button>
              <button type="button" className="btn btn-primary" onClick={() => void run()} disabled={busy || selected.length === 0}>{busy && candidates ? 'Storing…' : `Store ${selected.length || ''} proposal${selected.length === 1 ? '' : 's'}`}</button>
            </>}
      </div>
    </div>
  )
}
