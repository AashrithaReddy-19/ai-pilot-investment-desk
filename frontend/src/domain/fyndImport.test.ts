import { describe, expect, it } from 'vitest'
import { candidateFromEngineRow, importKind, parseJsonProposals } from './fyndImport'

describe('importing proposals', () => {
  it('routes file types: csv and xlsx to the engine, json to the browser, the rest refused', () => {
    expect(importKind('a.CSV')).toBe('engine')
    expect(importKind('a.xlsx')).toBe('engine')
    expect(importKind('a.json')).toBe('json')
    expect(importKind('a.pdf')).toBe('unsupported')
    expect(importKind('a.xls')).toBe('unsupported')
    expect(importKind('noext')).toBe('unsupported')
  })

  it('reads JSON in both shapes and flags what cannot be stored', () => {
    const r = parseJsonProposals(JSON.stringify({ proposals: [
      { name: 'Vendor A', notes: 'AP', inputs: { setup_cost_inr: 1, annual_benefit_inr: 2, one_time_costs: [{ amount_inr: 1 }] } },
      { name: 'Vendor B', setup_cost_inr: '2,00,000', annual_benefit_inr: 5, baseline_annual_cost_inr: 9 },
      { name: 'bad/name', setup_cost_inr: -1 },
      7,
    ] }))
    expect(r.problem).toBeNull()
    const [a, b, c, d] = r.candidates
    expect(a.problems).toEqual([])
    expect(a.inputs.one_time_costs).toEqual([{ amount_inr: 1 }])
    expect(b.problems).toEqual([])
    expect(b.inputs).toMatchObject({ setup_cost_inr: 200000, annual_benefit_inr: 5, baseline: { annual_cost_inr: 9 } })
    expect(c.problems.length).toBeGreaterThanOrEqual(2)
    expect(d.problems).toEqual(['This item is not an object.'])
  })

  it('refuses malformed files with a reason', () => {
    expect(parseJsonProposals('{').problem).toMatch(/not valid JSON/)
    expect(parseJsonProposals('{"a":1}').problem).toMatch(/array/)
    expect(parseJsonProposals('[]').problem).toMatch(/no proposals/)
  })

  it('maps an engine-accepted row, dropping the engine placeholder group and keeping provenance', () => {
    const c = candidateFromEngineRow({
      line: 3,
      pilot: { id: 'P1', task: 'Invoice bot', setup_cost_inr: 200000, annual_benefit_inr: 300000, annual_running_cost_inr: 60000, benefit_type: 'cashable', overlap_group: '__own_P1', ramp_months: 3 },
      extension: { id: 'P1', adoption_pct: 70, readiness: 'high' },
    }, 'p.csv')
    expect(c.problems).toEqual([])
    expect(c.name).toBe('Invoice bot')
    expect(c.notes).toBe('Imported from p.csv (id P1).')
    expect(c.inputs).toEqual({ setup_cost_inr: 200000, annual_benefit_inr: 300000, ai_running_cost_inr: 60000, benefit_type: 'cashable', readiness: 'high', adoption_pct: 70, ramp_months: 3 })
  })
})
