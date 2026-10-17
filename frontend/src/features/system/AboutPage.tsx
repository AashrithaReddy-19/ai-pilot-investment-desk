import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { DefaultsDoc } from '../../api/types'
import { SyntheticTag } from '../../components/ui/Bits'

export function AboutPage() {
  const [d, setD] = useState<DefaultsDoc | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => { api.defaults().then((r) => setD(r.data)).catch(() => setFailed(true)) }, [])
  const def = d?.extension.defaults

  return (
    <>
      <header className="page-head">
        <span className="eyebrow">About and limitations</span>
        <h1 className="h1">Know what the numbers are.</h1>
        <p className="lede">How the figures are produced, what is entered versus assumed versus measured, and what this product does not do.</p>
      </header>

      <section className="stage" aria-labelledby="a-how">
        <div className="stage-head" style={{ gridTemplateColumns: 'minmax(0,1fr)' }}><h2 className="h2" id="a-how">How an evaluation works</h2></div>
        <div className="split">
          <div className="prose">
            <p>Every figure comes from a deterministic rules engine. There is no machine-learning model, no forecast and no scoring: the same inputs always produce the same outputs.</p>
            <p><strong>Independent</strong> takes the benefit exactly as you entered it. Net benefit per year is benefit minus running cost. Payback is setup cost divided by net benefit, times 12 months, assuming the benefit is steady from day one.</p>
            <p><strong>Conservative</strong> multiplies the benefit by expected adoption, a readiness factor and a cash factor, because time saved is not cash unless a cost is removed. Then it applies the same net and payback arithmetic.</p>
            <p>A pilot that never pays back shows “Never”. A value you leave blank is never treated as zero: it stays unknown, and where the engine needs one it applies a stated default and labels the result <span className="tag tag-assumed">assumed</span>.</p>
          </div>
          <div className="prose">
            <p>The portfolio optimizer tests every combination of pilots with positive net benefit whose setup costs fit the budget, never funds two pilots that share a benefit group, and keeps the combination with the highest total net benefit. Ties are broken in a fixed, documented order, so a result is repeatable.</p>
            <p>The stop rule compares measured adoption and quality with thresholds and returns GO, REVISE or STOP with its reasons. REVISE usually means nothing has been measured yet.</p>
            <p>Four kinds of value appear in the product, kept visibly apart: <strong>entered</strong> by you, <strong>assumed</strong> by the engine, <strong>projected</strong> by calculation, and <strong>measured</strong> from real use.</p>
          </div>
        </div>
      </section>

      <section className="stage" aria-labelledby="a-limits">
        <div className="stage-head" style={{ gridTemplateColumns: 'minmax(0,1fr)' }}><h2 className="h2" id="a-limits">Limitations</h2></div>
        <ul className="stack prose" style={{ listStyle: 'disc', paddingLeft: 20 }}>
          <li><strong>Pilots are not stored.</strong> The service has no endpoint to create, edit, list or delete pilots. Pilots you type are session drafts held in this browser tab. They become durable only inside a saved portfolio.</li>
          <li><strong>Saved portfolios record the shipped case as their starting state</strong>, even when you supplied your own pilots. The pilots actually evaluated are stored in the negotiated scope.</li>
          <li><strong>Evaluation history cannot be read back.</strong> Each evaluation is logged by the service, but no endpoint returns that history, so none is shown.</li>
          <li><strong>Observation history is keyed by pilot ID only.</strong> The same ID used in two scenarios shares one history.</li>
          <li><strong>There are no accounts.</strong> The service has no sign-in, users or roles.</li>
          <li><strong>Inputs are assumptions.</strong> Nothing here is market evidence, a guarantee or a production result.</li>
        </ul>
      </section>

      <section className="stage" aria-labelledby="a-case">
        <div className="stage-head" style={{ gridTemplateColumns: 'minmax(0,1fr)' }}><h2 className="h2" id="a-case">The synthetic C23 case <SyntheticTag /></h2></div>
        {failed && <div className="notice notice-warn"><div>The case description could not be loaded because the backend did not answer.</div></div>}
        {d && (
          <div className="split">
            <div className="prose">
              <p>{d.starting_state.limit}</p>
              {d.starting_state.records.notes && <p>{d.starting_state.records.notes}</p>}
              <p>Case {d.starting_state.case_id}, data {d.starting_state.data_version}. {d.starting_state.synthetic ? 'Marked synthetic by its authors.' : ''}</p>
            </div>
            {def && (
              <div className="stack">
                <h3 className="h3">Team assumptions it ships with</h3>
                <dl className="ledger">
                  <dt>Assumed adoption when none is given</dt><dd>{def.assumed_adoption_pct}%</dd>
                  <dt>Assumed running cost, share of setup</dt><dd>{def.assumed_run_cost_pct_of_setup}%</dd>
                  <dt>Target adoption for the stop rule</dt><dd>{def.target_adoption_pct}%</dd>
                  <dt>Quality threshold for the stop rule</dt><dd>{def.quality_threshold_pct}%</dd>
                </dl>
                <p className="caption">These are the engine’s defaults. They apply to your pilots too wherever you leave a value blank.</p>
              </div>
            )}
          </div>
        )}
      </section>
    </>
  )
}
