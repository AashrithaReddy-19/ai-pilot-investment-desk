import { useEffect, useState } from 'react'
import { api, saveDownload } from '../../api/client'
import type { AdviceOutcome, CheckpointOutcome, EvidenceBand, PilotsResponse, ScenarioBody, StopDecision } from '../../api/types'
import { formatINR } from '../../domain/format'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { Skeleton, Status } from '../../components/ui/Bits'
import { LeftOutNote, NeedsProposals } from '../../components/fynd/NeedsProposals'
import { useStoredScenario } from '../../state/useStoredScenario'

/** Evidence and decision: the per-pilot evidence band, advice outcome, stop rule and checkpoint
 *  from one /pilots run on your stored proposals, plus the downloadable dossier and CSV. */
export function EvidenceDecision() {
  const stored = useStoredScenario()
  const scenario: ScenarioBody | null = stored.scenario
  const [pilots, setPilots] = useState<PilotsResponse | null>(null)
  const [err, setErr] = useState<unknown>(null)

  useEffect(() => { void reload() }, [scenario]) // eslint-disable-line react-hooks/exhaustive-deps
  const reload = async () => {
    setErr(null); setPilots(null)
    if (!scenario) return
    try { setPilots((await api.pilots(scenario)).data) } catch (e) { setErr(e) }
  }

  const download = async (kind: 'csv' | 'evidence') => {
    if (!scenario) return
    try { saveDownload(await (kind === 'csv' ? api.exportCsv(scenario) : api.exportEvidence(scenario))) } catch (e) { setErr(e) }
  }

  return (
    <div className="stack" style={{ gap: 20 }}>
      <div className="row" style={{ gap: 12 }}>
        <button className="btn" onClick={() => void download('evidence')} disabled={!scenario}>Download evidence dossier (MD)</button>
        <button className="btn" onClick={() => void download('csv')} disabled={!scenario}>Download pilot metrics (CSV)</button>
      </div>
      <NeedsProposals loaded={stored.loaded} error={stored.error} empty={stored.scenario === null} leftOut={stored.leftOut} onRetry={() => void stored.reload()} />
      <LeftOutNote leftOut={stored.leftOut} />
      {err != null && <ErrorNotice title="The pilots could not be loaded." error={err} onRetry={() => void reload()} />}
      {!err && scenario && !pilots && <Skeleton rows={4} />}
      {pilots?.pilots.map((p) => {
        const adv = p.advice
        const cp = p.checkpoint
        return (
          <div className="panel" key={p.id} data-testid={`pilot-${p.id}`}>
            <h3 className="h3">{p.id} — {p.task}</h3>
            <div className="row" style={{ flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
              <Status tone={verdictTone(p.stop_rule.decision)}>Stop rule: {p.stop_rule.decision}</Status>
              {adv && <Status tone={outcomeTone(adv.outcome)}>Advice: {adv.outcome.replace(/_/g, ' ')}</Status>}
              {adv && <Status tone={bandTone(adv.evidence.band)}>Evidence: {adv.evidence.band} ({adv.evidence.observed_weight}/{adv.evidence.total_weight} inputs real)</Status>}
              {cp && <Status tone={checkpointTone(cp.checkpoint)}>Checkpoint: {cp.checkpoint}</Status>}
              <Status tone={p.independent.net > 0 ? 'ok' : 'bad'}>Net {formatINR(p.independent.net)}/yr</Status>
            </div>
            {adv && (
              <>
                <p className="hint"><span className="strong">{adv.reasons[0]}</span></p>
                {adv.what_would_change_it.length > 0 && <p className="hint">Change it: {adv.what_would_change_it[0]}</p>}
                {adv.actions.length > 0 && (
                  <p className="hint">Next best action: <span className="strong">{adv.actions[0].action}</span> (owner hint: {adv.actions[0].owner_hint}).</p>
                )}
                <details>
                  <summary className="hint">All {adv.actions.length} next action(s), {adv.evidence.gaps.length} gap(s) and the per-input evidence list</summary>
                  <div className="stack" style={{ gap: 8, marginTop: 8 }}>
                    {adv.evidence.gaps.map((g) => (
                      <p key={g.input} className="hint">Gap ({g.impact}): {g.input} — {g.why}</p>
                    ))}
                    {adv.actions.map((a) => (
                      <p key={a.code} className="hint"><span className="mono">{a.code}</span> — {a.action} (owner hint: {a.owner_hint}; expected test: {a.expected_test})</p>
                    ))}
                    <div className="tbl-wrap"><table className="tbl">
                      <caption className="sr">Evidence inputs for {p.id}</caption>
                      <thead><tr><th scope="col">Input</th><th scope="col">Source</th><th scope="col">Observed</th><th scope="col">Weight</th></tr></thead>
                      <tbody>{adv.evidence.inputs.map((r) => (
                        <tr key={r.input}><th scope="row">{r.input}</th><td>{r.source}</td><td>{r.observed ? 'yes' : 'no'}</td><td>{r.weight}</td></tr>
                      ))}</tbody>
                    </table></div>
                  </div>
                </details>
              </>
            )}
            {cp && <p className="hint">{cp.reasons[0]}</p>}
            <p className="hint">{p.stop_rule.reasons[0]}</p>
          </div>
        )
      })}
      <button className="btn" onClick={() => void reload()}>Re-run</button>
    </div>
  )
}

type TagTone = 'ok' | 'warn' | 'bad' | 'accent' | 'off'
function verdictTone(d: StopDecision): TagTone { return d === 'GO' ? 'ok' : d === 'REVISE' ? 'warn' : 'bad' }
function outcomeTone(o: AdviceOutcome): TagTone { return o === 'fund' ? 'ok' : o === 'reject' ? 'bad' : o === 'insufficient_information' ? 'warn' : 'accent' }
function bandTone(b: EvidenceBand): TagTone { return b === 'high' ? 'ok' : b === 'medium' ? 'accent' : 'warn' }
function checkpointTone(c: CheckpointOutcome): TagTone { return c === 'scale' ? 'ok' : c === 'stop' || c === 'revise' ? 'bad' : c === 'extend' ? 'accent' : 'off' }
