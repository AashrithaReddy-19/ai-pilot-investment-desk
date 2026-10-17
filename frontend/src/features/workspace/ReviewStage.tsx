import { Link } from 'react-router-dom'
import { ObservationTools } from '../observations/ObservationTools'
import { SavePortfolio } from '../portfolios/SavePortfolio'
import { StageHead } from './PilotsStage'

/** Stage 4. Three separate concerns: preview, recording a real observation, and saving the portfolio. */
export function ReviewStage() {
  return (
    <section className="stage" id="stage-4" aria-labelledby="h-stage-4">
      <StageHead n={4} title="Review, record and save">
        Check what the stop rule would decide, record what has really been measured, then keep the portfolio and its evidence.
      </StageHead>
      <h2 id="h-stage-4" className="sr">Review, record and save</h2>
      <div className="stack" style={{ gap: 36 }}>
        <div className="stack">
          <h3 className="h3">Stop rule and observations</h3>
          <ObservationTools />
        </div>
        <SavePortfolio />
        <p className="caption">Saved portfolios are listed under <Link to="/portfolios">Saved portfolios</Link>. A pilot’s full observation history is under <Link to="/observations">Observations and stop rules</Link>.</p>
      </div>
    </section>
  )
}
