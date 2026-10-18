import { describe, expect, it } from 'vitest'
import { ApiError, describeError, parseErrorEnvelope } from '../api/client'
import { emptyForm, nextId, overridesFor, parseAmount, toExtension, toFixture, validateDraft } from './draft'
import { formatINR, formatINRCompact, formatPayback, formatPct, groupLabel } from './format'
import { currentEvalKey, currentOptKey, overridesOf, parseBudget, scenarioKey } from '../state/useWorkspace'

const form = (over: Partial<ReturnType<typeof emptyForm>> = {}) => ({ ...emptyForm('P1'), task: 'Invoice extraction', setup: '2,00,000', benefit: '180000', ...over })

describe('draft validation', () => {
  it('parses Indian-grouped amounts and rejects text and blanks', () => {
    expect(parseAmount('2,00,000')).toBe(200000)
    expect(Number.isNaN(parseAmount('abc'))).toBe(true)
    expect(Number.isNaN(parseAmount(''))).toBe(true)
  })
  it('keeps blank optional fields as null, never zero', () => {
    const { errors, draft } = validateDraft(form(), [])
    expect(errors).toEqual({})
    expect(draft).toMatchObject({ annual_running_cost_inr: null, adoption_pct: null, readiness: null, observed_adoption_pct: null, observed_quality_pct: null })
  })
  it('distinguishes a typed zero from blank', () => {
    const { draft } = validateDraft(form({ running: '0', adoption: '0' }), [])
    expect(draft?.annual_running_cost_inr).toBe(0)
    expect(draft?.adoption_pct).toBe(0)
  })
  it('requires name, setup cost and benefit', () => {
    const { errors, draft } = validateDraft(emptyForm('P1'), [])
    expect(draft).toBeNull()
    expect(Object.keys(errors).sort()).toEqual(['benefit', 'setup', 'task'])
  })
  it('rejects negative money, out-of-range percentages, bad and duplicate ids', () => {
    const r = validateDraft(form({ setup: '-1', running: 'x', adoption: '101', obsQuality: '-5', id: 'bad id' }), [])
    expect(r.errors.setup).toMatch(/negative/)
    expect(r.errors.running).toMatch(/number/)
    expect(r.errors.adoption).toMatch(/0 and 100/)
    expect(r.errors.obsQuality).toMatch(/0 and 100/)
    expect(r.errors.id).toMatch(/letters/)
    expect(validateDraft(form({ id: 'P1' }), ['P1']).errors.id).toMatch(/already uses/)
  })
  it('suggests the next free id', () => {
    expect(nextId([])).toBe('P1')
    expect(nextId(['P1', 'P2', 'P4'])).toBe('P3')
  })
})

describe('request payloads', () => {
  const draft = validateDraft(form({ group: 'support', adoption: '60', readiness: 'high' }), []).draft!
  it('builds the fixture in the engine shape (records.pilots[])', () => {
    expect(toFixture([draft])).toEqual({ records: { pilots: [{ id: 'P1', task: 'Invoice extraction', setup_cost_inr: 200000, annual_running_cost_inr: null, annual_benefit_inr: 180000, benefit_type: 'cashable assumption', overlap_group: 'support' }] } })
  })
  it('builds the extension with empty defaults so the engine uses its own', () => {
    const e = toExtension([draft])
    expect(e.defaults).toEqual({})
    expect(e.pilots[0]).toMatchObject({ id: 'P1', adoption_pct: 60, readiness: 'high', observed_adoption_pct: null })
  })
  it('sends nothing at all with no scenario', () => {
    expect(overridesOf('none', [])).toBeNull()
    expect(overridesOf('drafts', [])).toBeNull()
    expect(overridesOf('drafts', [draft])).toEqual(overridesFor([draft]))
  })
})

describe('staleness keys', () => {
  const d = validateDraft(form(), []).draft!
  it('change whenever the inputs change, so old results are never shown as current', () => {
    const base = { source: 'drafts' as const, drafts: [d], budgetInput: '500000', mode: 'independent' as const }
    expect(currentEvalKey(base)).toBe(currentEvalKey({ ...base }))
    expect(currentEvalKey(base)).not.toBe(currentEvalKey({ ...base, budgetInput: '400000' }))
    expect(currentOptKey(base)).not.toBe(currentOptKey({ ...base, mode: 'conservative' }))
    expect(scenarioKey('drafts', [d])).not.toBe(scenarioKey('drafts', [{ ...d, setup_cost_inr: 1 }]))
  })
  it('reads budgets', () => {
    expect(parseBudget('')).toBeNull()
    expect(parseBudget(' 6,00,000 ')).toBe(600000)
    expect(parseBudget('-1')).toBeUndefined()
    expect(parseBudget('lots')).toBeUndefined()
  })
})

describe('formatting', () => {
  it('uses Indian grouping, a true minus, and never turns unknown into zero', () => {
    expect(formatINR(200000).replace(/\s/g, '')).toBe('₹2,00,000')
    expect(formatINR(-16800).replace(/\s/g, '')).toBe('−₹16,800')
    expect(formatINR(1234.5).replace(/\s/g, '')).toBe('₹1,234.50')
    expect(formatINR(null)).toBe('Not provided')
    expect(formatPct(null)).toBe('Not provided')
  })
  it('shows Never for a null payback and hides internal group keys', () => {
    expect(formatPayback(null)).toBe('Never')
    expect(formatPayback(20)).toBe('20.0 months')
    expect(groupLabel('__own_P1')).toBe('Not provided')
    expect(formatINRCompact(120000)).toBe('₹1.2L')
  })
})

describe('error handling', () => {
  it('reads the gateway error envelope', () => {
    const e = parseErrorEnvelope(422, { error: { code: 'invalid_name', message: 'bad name', field: 'name' }, request_id: 'abc123' })
    expect(e).toBeInstanceOf(ApiError)
    expect(e).toMatchObject({ status: 422, code: 'invalid_name', field: 'name', requestId: 'abc123', message: 'bad name' })
  })
  it('falls back to the status when the body is not an envelope', () => {
    expect(parseErrorEnvelope(502, null, 'hdr1')).toMatchObject({ status: 502, requestId: 'hdr1' })
  })
  it('gives plain-language text for infrastructure failures', () => {
    expect(describeError(new ApiError('x', { code: 'rules_unavailable' }))).toMatch(/rules engine is unreachable/)
    expect(describeError(new ApiError('x'))).toMatch(/could not be reached/)
    expect(describeError(new ApiError('Budget missing', { code: 'rules_rejected', status: 422 }))).toBe('Budget missing')
  })
})
