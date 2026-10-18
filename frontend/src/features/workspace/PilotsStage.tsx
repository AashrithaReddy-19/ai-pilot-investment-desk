import { useState } from 'react'
import { DraftPilot } from '../../domain/draft'
import { formatINR, formatPct } from '../../domain/format'
import { useWorkspace } from '../../state/useWorkspace'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog'
import { Icon } from '../../components/ui/Icon'
import { Empty } from '../../components/ui/Bits'
import { PilotDialog } from './PilotDialog'

const missing = <span className="muted">Not provided</span>

export function StageHead({ n, title, children, actions }: { n: number; title: string; children?: React.ReactNode; actions?: React.ReactNode }) {
  return (
    <div className="stage-head">
      <span className="stage-n">0{n}</span>
      <div><h2 className="h2">{title}</h2>{children && <p className="caption">{children}</p>}</div>
      {actions && <div className="stage-actions">{actions}</div>}
    </div>
  )
}

/** Stage 1. Pilots typed in here are session drafts: the gateway has no endpoint that stores pilots. */
export function PilotsStage() {
  const { source, drafts, addDraft, updateDraft, removeDraft, startFresh } = useWorkspace()
  const [dialog, setDialog] = useState<{ open: boolean; editing: DraftPilot | null }>({ open: false, editing: null })
  const [confirm, setConfirm] = useState<null | { kind: 'remove'; id: string } | { kind: 'clear' }>(null)
  const usedIds = drafts.filter((d) => d.id !== dialog.editing?.id).map((d) => d.id)
  const openAdd = () => setDialog({ open: true, editing: null })

  const submit = (d: DraftPilot, prev: string | null) => {
    if (prev) updateDraft(prev, d)
    else addDraft(d)
    setDialog({ open: false, editing: null })
  }
  const doConfirm = () => {
    if (confirm?.kind === 'remove') removeDraft(confirm.id)
    else startFresh()
    setConfirm(null)
  }

  return (
    <section className="stage" id="stage-1" aria-labelledby="h-stage-1">
      <StageHead n={1} title="Add pilots" actions={source === 'drafts' && <button className="btn btn-primary" onClick={openAdd}><Icon name="plus" />Add pilot</button>}>
        Describe each AI pilot you are considering. You enter the figures; the rules engine does the calculating.
      </StageHead>
      <h2 id="h-stage-1" className="sr">Add pilots</h2>

      {source === 'none' && (
        <>
          <Empty title="No pilots yet" actions={<button className="btn btn-primary" onClick={openAdd}><Icon name="plus" />Add a pilot</button>}>
            Start by adding the first pilot you want to evaluate. You need its name, setup cost, expected yearly benefit and benefit type.
            To work from your stored proposals instead, use the AI Proposals screen.
          </Empty>
        </>
      )}

      {source === 'drafts' && (
        <>
          <div className="notice notice-info" role="note"><div><strong>Session drafts.</strong> These pilots exist only in this browser tab and are sent to the rules engine with each request. The service has no endpoint that stores pilots, so they become durable only inside a saved portfolio.</div></div>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr">Draft pilots in this scenario</caption>
              <thead><tr>
                <th className="sticky" scope="col">Pilot</th><th className="num" scope="col">Setup</th><th className="num" scope="col">Running / yr</th><th className="num" scope="col">Benefit / yr</th>
                <th scope="col">Type</th><th scope="col">Overlap group</th><th className="num" scope="col">Adoption</th><th scope="col">Readiness</th><th scope="col"><span className="sr">Actions</span></th>
              </tr></thead>
              <tbody>
                {drafts.map((d) => (
                  <tr key={d.id}>
                    <td className="sticky"><div className="cell-pilot"><span className="id">{d.id}</span><span className="nm">{d.task}</span></div></td>
                    <td className="num">{formatINR(d.setup_cost_inr)}</td>
                    <td className="num">{d.annual_running_cost_inr === null ? missing : formatINR(d.annual_running_cost_inr)}</td>
                    <td className="num">{formatINR(d.annual_benefit_inr)}</td>
                    <td>{d.benefit_type}</td>
                    <td>{d.overlap_group || missing}</td>
                    <td className="num">{d.adoption_pct === null ? missing : formatPct(d.adoption_pct)}</td>
                    <td style={{ textTransform: 'capitalize' }}>{d.readiness ?? missing}</td>
                    <td style={{ whiteSpace: 'nowrap' }}>
                      <button className="btn btn-quiet btn-sm" onClick={() => setDialog({ open: true, editing: d })} aria-label={`Edit ${d.id} ${d.task}`}>Edit</button>
                      <button className="btn btn-quiet btn-sm" onClick={() => setConfirm({ kind: 'remove', id: d.id })} aria-label={`Remove ${d.id} ${d.task}`}>Remove</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="row"><button className="link" onClick={() => setConfirm({ kind: 'clear' })}>Start over and clear all pilots</button></div>
        </>
      )}

      <PilotDialog open={dialog.open} editing={dialog.editing} usedIds={usedIds} onClose={() => setDialog({ open: false, editing: null })} onSubmit={submit} />
      <ConfirmDialog
        open={confirm !== null} danger confirmLabel={confirm?.kind === 'remove' ? 'Remove pilot' : 'Clear all'}
        title={confirm?.kind === 'remove' ? 'Remove this pilot?' : 'Clear every pilot?'}
        body={<p>{confirm?.kind === 'remove' ? 'It is removed from this scenario. Results already calculated will be marked out of date.' : 'All session drafts are discarded. They are not stored anywhere else, so this cannot be undone.'}</p>}
        onConfirm={doConfirm} onCancel={() => setConfirm(null)}
      />
    </section>
  )
}
