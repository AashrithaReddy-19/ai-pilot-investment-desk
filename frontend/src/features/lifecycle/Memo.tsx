import { useEffect, useState } from 'react'
import { api, saveDownload } from '../../api/client'
import type { ScenarioBody } from '../../api/types'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField } from '../../components/ui/Fields'

/** One-page decision memo: generate from the engine (the gateway embeds the stored plan
 *  and the latest actuals), read it in-page and download it as memo.md. */
export function Memo() {
  const [scenario, setScenario] = useState<ScenarioBody | null>(null)
  const [pilotIds, setPilotIds] = useState<string[]>([])
  const [pilotId, setPilotId] = useState('')
  const [text, setText] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)

  useEffect(() => { void (async () => {
    try {
      const doc = (await api.defaults()).data
      setScenario({ fixture: doc.starting_state, extension: doc.extension })
      const ids = doc.starting_state.records.pilots.map((p) => p.id)
      setPilotIds(ids)
      if (ids[0]) setPilotId(ids[0])
    } catch (e) { setErr(e) }
  })() }, [])

  const run = async () => {
    if (!scenario || !pilotId) return
    setBusy(true); setErr(null)
    try { setText((await api.memoText({ ...scenario, pilot_id: pilotId })).data) } catch (e) { setErr(e) } finally { setBusy(false) }
  }
  const download = async () => {
    if (!scenario || !pilotId) return
    try { saveDownload(await api.memoDownload({ ...scenario, pilot_id: pilotId })) } catch (e) { setErr(e) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <div className="panel">
        <h3 className="h3">Decision memo</h3>
        <p className="hint">One page: recommendation, key numbers with provenance tags, evidence band, gaps, next actions,
          the 30-day plan and the audit trail. The stored tracker plan and the latest observations are included automatically.</p>
        <div className="row" style={{ gap: 12, alignItems: 'flex-end' }}>
          {pilotIds.length > 0 && (
            <div style={{ minWidth: 260 }}>
              <SelectField id="memo-pilot" label="Pilot" value={pilotId} onChange={setPilotId} required options={pilotIds.map((id) => ({ value: id, label: id }))} />
            </div>
          )}
          <button className="btn btn-primary" onClick={() => void run()} disabled={busy || !pilotId}>{busy ? 'Writing…' : 'Generate'}</button>
          {text && <button className="btn" onClick={() => void download()}>Download memo.md</button>}
        </div>
      </div>
      {err != null && <ErrorNotice title="The memo could not be generated." error={err} />}
      {text && (
        <div className="panel" data-testid="memo-text">
          <pre className="hint" style={{ whiteSpace: 'pre-wrap', fontFamily: 'var(--mono, monospace)' }}>{text}</pre>
        </div>
      )}
    </div>
  )
}
