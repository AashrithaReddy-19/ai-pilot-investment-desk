import { useState } from 'react'
import { Link } from 'react-router-dom'
import { BalancePanel, Compare } from '../features/compare/Compare'
import { WhatIf } from '../features/lifecycle/WhatIf'
import { EvidenceDecision } from '../features/lifecycle/EvidenceDecision'
import { Tracker } from '../features/lifecycle/Tracker'
import { Memo } from '../features/lifecycle/Memo'
import { OptimizeStage } from '../features/workspace/OptimizeStage'
import { ObservationTools } from '../features/observations/ObservationTools'
import { SavedPortfolios } from '../features/portfolios/SavedPortfolios'
import { Interviews } from '../features/interviews/Interviews'
import { Proposals } from '../features/proposals/Proposals'
import { History } from '../features/history/History'

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

export function ComparePage() {
  return (
    <>
      <Head eyebrow="Compare proposals" title="What was spent on AI vs what the proposals promise.">
        Upload the previous-year balance sheet to see how much the AI has burned and what next year costs, import proposal CSVs or type them in, then run the same engine the workspace uses to rank and recommend.
      </Head>
      <Compare />
    </>
  )
}

export function BalanceSheetPage() {
  return (
    <>
      <Head eyebrow="Balance sheet" title="What the AI has burned so far">
        Upload the previous-year balance sheet or annual report as a PDF, an Excel workbook or a CSV. The service only proposes rows that look like AI spend — you decide which count. Nothing is stored, and the AI burn stays unknown until you confirm a row.
      </Head>
      <BalancePanel onConfirmed={() => { /* the panel shows its own results; this page is balance-sheet-only */ }} />
    </>
  )
}

export function WhatIfPage() {
  return (
    <>
      <Head eyebrow="What-if and sweep" title="Change one input, see the verdict move.">
        Base versus changed for one pilot with the same cash-flow rules the decisions use, plus the one-variable sensitivity sweep. Overrides never change stored data.
      </Head>
      <WhatIf />
    </>
  )
}

export function EvidencePage() {
  return (
    <>
      <Head eyebrow="Evidence and decision" title="How strong the evidence is, and what the rule says.">
        One run of the engine on the shipped scenario: the evidence band, the decision outcome and the next-best action per pilot, the stop rule, and the Phase 6 checkpoint — with the dossier and CSV downloads.
      </Head>
      <EvidenceDecision />
    </>
  )
}

export function TrackerPage() {
  return (
    <>
      <Head eyebrow="Pilot tracker" title="The 30-day plan, the gap to the actuals, and what happens on a stop.">
        Plans are generated from the engine, actuals come from the linked observations, the checkpoint verdict is re-run with real numbers, and stopping frees the pilot's budget for reallocation — always as a recommendation you accept yourself.
      </Head>
      <Tracker />
    </>
  )
}

export function MemoPage() {
  return (
    <>
      <Head eyebrow="Decision memo" title="One page for the decision record.">
        Recommendation, key numbers with provenance tags, evidence band and gaps, next actions, the 30-day plan and the audit trail — the stored plan and latest observations are folded in automatically.
      </Head>
      <Memo />
    </>
  )
}

export function PortfoliosPage() {  const [k] = useState(0)
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

export function ProposalsPage() {
  return (
    <>
      <Head eyebrow="Proposals" title="AI pilot candidates on the desk.">
        Each proposal is a named pilot candidate with versioned inputs and its own evidence. Later observations and decisions attach to the proposal, so measurements stay comparable over time.
      </Head>
      <Proposals />
    </>
  )
}

export function HistoryPage() {
  return (
    <>
      <Head eyebrow="History and audit" title="What changed, and when.">
        The evaluation and decision timeline, every stored evaluation with the proposal version it used, and the audit trail of changes and downloads.
      </Head>
      <History />
    </>
  )
}
