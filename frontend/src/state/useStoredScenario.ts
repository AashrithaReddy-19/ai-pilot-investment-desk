import { useMemo } from 'react'
import type { Proposal, ScenarioBody } from '../api/types'
import { overridesFor } from '../domain/draft'
import type { DraftPilot } from '../domain/draft'
import { formFromInputs, isEvaluable } from '../domain/fynd'
import { useProposalDocs } from './useFynd'

/** The engine's pilot id for a stored proposal: stable (from the proposal's UUID) and valid under the gateway's pilot-id rule. */
export const pilotIdOf = (p: { id: string }) => `P-${p.id.slice(0, 8)}`

/** Why a stored proposal cannot be sent to the single-pilot tools, or null when it can. */
function whyLeftOut(p: Proposal): string | null {
  if (!isEvaluable(p.inputs)) return 'needs an implementation cost and an annual benefit'
  const t = formFromInputs(p.inputs).benefit_type
  if (t !== 'cashable' && t !== 'cashable assumption' && t !== 'time freed') return 'its benefit type must be “cashable” or “time freed”'
  return null
}

function toDraft(p: Proposal): DraftPilot {
  const f = formFromInputs(p.inputs)
  const num = (s: string): number | null => (s === '' ? null : Number(s))
  const inputs = (p.inputs ?? {}) as Record<string, unknown>
  const readiness = f.readiness === 'low' || f.readiness === 'medium' || f.readiness === 'high' ? f.readiness : null
  return {
    id: pilotIdOf(p), task: p.name, description: p.notes ?? '', setup_cost_inr: Number(f.setup_cost_inr),
    annual_running_cost_inr: num(f.ai_running_cost_inr), annual_benefit_inr: Number(f.annual_benefit_inr),
    benefit_type: f.benefit_type === 'time freed' ? 'time freed' : 'cashable assumption', overlap_group: f.overlap_group,
    adoption_pct: num(f.adoption_pct), readiness,
    observed_adoption_pct: typeof inputs.observed_adoption_pct === 'number' ? inputs.observed_adoption_pct : null,
    observed_quality_pct: typeof inputs.observed_quality_pct === 'number' ? inputs.observed_quality_pct : null,
  }
}

/**
 * The scenario for the single-pilot tools (evidence, what-if, memo, tracker), built from the workspace's stored
 * proposals instead of any shipped sample. Nothing is invented: a proposal that lacks what the engine needs is
 * listed as left out, with the reason.
 */
export function useStoredScenario() {
  const { docs, error, loading, reload } = useProposalDocs(false)
  return useMemo(() => {
    const active = (docs ?? []).filter((p) => p.status === 'active')
    const usable = active.filter((p) => whyLeftOut(p) === null)
    const leftOut = active.map((p) => ({ name: p.name, why: whyLeftOut(p) })).filter((x): x is { name: string; why: string } => x.why !== null)
    const drafts = usable.map(toDraft)
    const scenario: ScenarioBody | null = drafts.length > 0 ? overridesFor(drafts) : null
    return { scenario, pilots: drafts.map((d) => ({ id: d.id, task: d.task })), leftOut, loaded: docs !== null, error, loading, reload }
  }, [docs, error, loading, reload])
}

/** One line telling the person which stored proposals the tool could not use. */
export function leftOutText(leftOut: { name: string; why: string }[]): string | null {
  return leftOut.length === 0 ? null : `Left out: ${leftOut.map((x) => `${x.name} (${x.why})`).join('; ')}.`
}
