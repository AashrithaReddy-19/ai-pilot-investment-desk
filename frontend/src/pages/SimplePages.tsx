import { useState } from 'react'
import { Link } from 'react-router-dom'
import { OptimizeStage } from '../features/workspace/OptimizeStage'
import { ObservationTools } from '../features/observations/ObservationTools'
import { SavedPortfolios } from '../features/portfolios/SavedPortfolios'
import { Interviews } from '../features/interviews/Interviews'

function Head({ eyebrow, title, children }: { eyebrow: string; title: string; children: React.ReactNode }) {
  return (
    <header className="page-head">
      <span className="eyebrow">{eyebrow}</span>
      <h1 className="h1">{title}</h1>
      <p className="lede">{children}</p>
    </header>
  )
}

export function OptimizePage() {
  return (
    <>
      <Head eyebrow="Portfolio optimization" title="Fund the best combination within budget.">
        The same optimizer as stage 3 of the <Link to="/">decision workspace</Link>, using the pilots in that workspace.
      </Head>
      <OptimizeStage standalone />
    </>
  )
}

export function PortfoliosPage() {
  const [k] = useState(0)
  return (
    <>
      <Head eyebrow="Saved portfolios" title="Decisions you have kept.">
        Portfolios stored in the database with the pilots used and the evidence returned by the rules engine. Open one to inspect it, or delete it.
      </Head>
      <SavedPortfolios refreshKey={k} />
    </>
  )
}

export function ObservationsPage() {
  return (
    <>
      <Head eyebrow="Observations and stop rules" title="What was actually measured.">
        Preview what the stop rule would decide, record a real measurement of adoption and quality, and review each pilot’s history.
      </Head>
      <ObservationTools />
    </>
  )
}

export function InterviewsPage() {
  return (
    <>
      <Head eyebrow="Interviews" title="What the people doing the work say.">
        Qualitative evidence from stakeholder interviews, stored in the database. Record the interviewee’s role, never a person’s name.
      </Head>
      <Interviews />
    </>
  )
}
