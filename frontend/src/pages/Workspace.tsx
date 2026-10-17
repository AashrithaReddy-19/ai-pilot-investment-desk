import { currentEvalKey, currentOptKey, useWorkspace } from '../state/useWorkspace'
import { EvaluateStage } from '../features/workspace/EvaluateStage'
import { OptimizeStage } from '../features/workspace/OptimizeStage'
import { PilotsStage } from '../features/workspace/PilotsStage'
import { ReviewStage } from '../features/workspace/ReviewStage'

export default function Workspace() {
  const s = useWorkspace()
  const n = s.source === 'drafts' ? s.drafts.length : s.source === 'synthetic' ? s.synthetic?.starting_state.records.pilots.length ?? 0 : 0
  const evaluated = s.evaluation.status === 'done' && s.evaluation.key === currentEvalKey(s)
  const optimized = s.optimization.status === 'done' && s.optimization.key === currentOptKey(s)
  const steps = [
    { n: 1, t: 'Add pilots', s: n > 0 ? `${n} in this scenario${s.source === 'synthetic' ? ' (synthetic)' : ''}` : 'Start here', done: n > 0 },
    { n: 2, t: 'Evaluate', s: evaluated ? 'Evaluated' : n > 0 ? 'Ready' : 'Needs a pilot', done: evaluated },
    { n: 3, t: 'Optimize', s: optimized ? 'Portfolio chosen' : n > 0 ? 'Ready' : 'Needs a pilot', done: optimized },
    { n: 4, t: 'Review and save', s: optimized ? 'Ready to save' : 'After optimizing', done: false },
  ]
  return (
    <>
      <header className="page-head">
        <span className="eyebrow">C23 · AI Pilot Investment Decision Desk</span>
        <h1 className="h1" style={{ fontSize: 'clamp(32px, 5vw, 52px)', letterSpacing: '-0.035em' }}>Invest in AI with evidence.</h1>
        <p className="lede">Add the AI pilots you are considering, see what each would return under two views, fund the best combination within your budget, and keep the evidence behind the decision.</p>
      </header>
      <nav aria-label="Workflow stages">
        <ol className="steps">
          {steps.map((x) => (
            <li key={x.n} className={x.done ? 'done' : undefined}><a href={`#stage-${x.n}`}><span className="s-n">Stage {x.n}</span><span className="s-t">{x.t}</span><span className="s-s">{x.s}</span></a></li>
          ))}
        </ol>
      </nav>
      <PilotsStage />
      <EvaluateStage />
      <OptimizeStage />
      <ReviewStage />
    </>
  )
}
