import { useEffect, useState } from 'react'
import { api, saveDownload } from '../../api/client'
import type { ScenarioBody } from '../../api/types'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { SelectField } from '../../components/ui/Fields'
import { LeftOutNote, NeedsProposals } from '../../components/fynd/NeedsProposals'
import { useStoredScenario } from '../../state/useStoredScenario'

/** One-page decision memo: generate from the engine (the gateway embeds the stored plan
 *  and the latest actuals), read it in-page and download it as memo.md. */
export function Memo() {
  const stored = useStoredScenario()
  const scenario: ScenarioBody | null = stored.scenario
  const pilotOptions = stored.pilots
  const [pilotId, setPilotId] = useState('')
  const [text, setText] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<unknown>(null)

  useEffect(() => { if (pilotOptions.length > 0 && !pilotOptions.some((p) => p.id === pilotId)) setPilotId(pilotOptions[0].id) }, [pilotOptions, pilotId])

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
          {pilotOptions.length > 0 && (
            <div style={{ minWidth: 260 }}>
              <SelectField id="memo-pilot" label="Proposal" value={pilotId} onChange={setPilotId} required options={pilotOptions.map((p) => ({ value: p.id, label: `${p.task} (${p.id})` }))} />
            </div>
          )}
          <button className="btn btn-primary" onClick={() => void run()} disabled={busy || !pilotId}>{busy ? 'Writing…' : 'Generate'}</button>
          {text && <button className="btn" onClick={() => void download()}>Download memo.md</button>}
        </div>
      </div>
      <NeedsProposals loaded={stored.loaded} error={stored.error} empty={stored.scenario === null} leftOut={stored.leftOut} onRetry={() => void stored.reload()} />
      <LeftOutNote leftOut={stored.leftOut} />
      {err != null && <ErrorNotice title="The memo could not be generated." error={err} />}
      {text && (
        <div className="panel" data-testid="memo-text">
          <pre className="hint" style={{ whiteSpace: 'pre-wrap', fontFamily: 'var(--mono, monospace)' }}>{text}</pre>
        </div>
      )}
    </div>
  )
}
