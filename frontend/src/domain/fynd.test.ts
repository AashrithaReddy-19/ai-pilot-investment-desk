import { describe, expect, it } from 'vitest'
import { EMPTY_FORM, engineProposal, formFromInputs, inputsFromForm, isEvaluable, outcomeLabel, outcomeTone, proposalIdByName } from './fynd'

describe('outcome labels', () => {
  it('labels known outcomes and never guesses unknown ones', () => {
    expect(outcomeLabel('gather_evidence')).toBe('Gather evidence')
    expect(outcomeTone('reject')).toBe('bad')
    expect(outcomeLabel('some_new_outcome')).toBe('Some new outcome')
    expect(outcomeTone('some_new_outcome')).toBe('off')
    expect(outcomeLabel(null)).toBe('Not available')
  })
})

describe('proposal form ↔ stored inputs', () => {
  it('requires setup and benefit, and leaves blanks out instead of sending zero', () => {
    const { inputs, errors } = inputsFromForm({ ...EMPTY_FORM, setup_cost_inr: '2,00,000', annual_benefit_inr: '300000' })
    expect(errors).toEqual({})
    expect(inputs).toEqual({ setup_cost_inr: 200000, annual_benefit_inr: 300000 })
    expect('ai_running_cost_inr' in inputs).toBe(false)
    expect(inputsFromForm(EMPTY_FORM).errors.setup_cost_inr).toMatch(/Required/)
  })

  it('writes the baseline as the object the engine accepts', () => {
    const { inputs } = inputsFromForm({ ...EMPTY_FORM, setup_cost_inr: '1', annual_benefit_inr: '2', baseline_annual_cost_inr: '600000' })
    expect(inputs.baseline).toEqual({ annual_cost_inr: 600000 })
  })

  it('rejects out-of-range values', () => {
    const { errors } = inputsFromForm({ ...EMPTY_FORM, setup_cost_inr: '-1', annual_benefit_inr: 'abc', adoption_pct: '120', ramp_months: '2.5' })
    expect(Object.keys(errors).sort()).toEqual(['adoption_pct', 'annual_benefit_inr', 'ramp_months', 'setup_cost_inr'])
  })

  it('keeps stored keys the form does not edit, and other baseline fields', () => {
    const previous = { setup_cost_inr: 5, annual_benefit_inr: 6, one_time_costs: [{ amount_inr: 5 }], baseline: { volume_per_month: 100, annual_cost_inr: 1 }, annual_running_cost_inr: 9 }
    const form = formFromInputs(previous)
    expect(form.ai_running_cost_inr).toBe('9')
    const { inputs } = inputsFromForm({ ...form, baseline_annual_cost_inr: '' }, previous)
    expect(inputs.one_time_costs).toEqual([{ amount_inr: 5 }])
    expect(inputs.baseline).toEqual({ volume_per_month: 100 })
    expect(inputs.ai_running_cost_inr).toBe(9)
    expect('annual_running_cost_inr' in inputs).toBe(false)
  })
})

describe('engine mapping', () => {
  it('mirrors the gateway: running-cost alias, known keys only, overrides last', () => {
    const p = engineProposal('A', { setup_cost_inr: 1, annual_benefit_inr: 2, annual_running_cost_inr: 3, unrelated: true }, { adoption_pct: 50 })
    expect(p).toEqual({ name: 'A', setup_cost_inr: 1, annual_benefit_inr: 2, ai_running_cost_inr: 3, adoption_pct: 50 })
  })
  it('knows when a proposal can be evaluated', () => {
    expect(isEvaluable({ setup_cost_inr: 1, annual_benefit_inr: 0 })).toBe(true)
    expect(isEvaluable({ setup_cost_inr: 1 })).toBe(false)
  })
  it('maps engine names back to stored ids', () => {
    expect(proposalIdByName([{ id: 'u1', name: 'Bot', version: 2 }], 'Bot')).toBe('u1')
    expect(proposalIdByName([], 'Bot')).toBeNull()
  })
})
