import { describe, expect, it } from 'vitest'
import { describeDetails, describeEvent } from './History'

describe('describeEvent', () => {
  it('describes a stop-rule decision with the earlier verdict and the measurement', () => {
    const d = describeEvent({
      id: 1, kind: 'decision', at: '2026-10-18T00:00:00Z', pilot_id: 'AI1', verdict: 'STOP', previous_verdict: 'REVISE',
      observed_adoption_pct: 20, observed_quality_pct: 90, reason: 'Adoption below break-even',
    })
    expect(d.what).toBe('Stop rule STOP for AI1 (was REVISE)')
    expect(d.detail).toBe('adoption 20.0%, quality 90.0% · Adoption below break-even')
  })

  it('never invents a measurement that was not recorded', () => {
    const d = describeEvent({ id: 2, kind: 'decision', at: '2026-10-18T00:00:00Z', pilot_id: 'AI2', verdict: 'GO' })
    expect(d.what).toBe('Stop rule GO for AI2')
    expect(d.detail).toBe('No measurement recorded')
  })

  it('prefers the change summary for an evaluation', () => {
    const d = describeEvent({ id: 3, kind: 'evaluation', at: '2026-10-18T00:00:00Z', change_summary: 'funded AI4; dropped AI2', selected: ['AI4'] })
    expect(d).toEqual({ what: 'Evaluation', detail: 'funded AI4; dropped AI2' })
  })

  it('falls back to the funded set and says when the budget is not stated', () => {
    expect(describeEvent({ id: 4, kind: 'evaluation', at: '2026-10-18T00:00:00Z', selected: [] }).detail)
      .toBe('nothing funded; budget not stated')
  })
})

describe('describeDetails', () => {
  it('shows identifiers and statuses as short pairs, skipping empty values', () => {
    expect(describeDetails({ fields: ['inputs', 'notes'], version: 2, proposal_id: null, empty: [] }))
      .toBe('fields: inputs, notes · version: 2')
  })
  it('is empty when there are no details', () => {
    expect(describeDetails({})).toBe('')
    expect(describeDetails(null)).toBe('')
  })
})
