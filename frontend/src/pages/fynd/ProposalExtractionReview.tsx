import { useEffect, useMemo, useState } from 'react'
import { api, ApiError } from '../../api/client'
import type { ProposalExtraction, ProposalExtractionField } from '../../api/uploads'
import { Card, Pill } from '../../components/fynd/Page'
import { ErrorNotice } from '../../components/ui/ErrorNotice'
import { BENEFIT_TYPES, READINESS } from '../../domain/fynd'
import { formatINR } from '../../domain/format'

const FIELD_ORDER = [
  'id', 'task', 'setup_cost_inr', 'annual_benefit_inr', 'annual_running_cost_inr',
  'benefit_type', 'overlap_group', 'adoption_pct', 'readiness', 'ramp_months', 'delay_months',
] as const
type FieldName = typeof FIELD_ORDER[number]
const LABELS: Record<FieldName, string> = {
  id: 'Proposal identifier',
  task: 'Task or business process',
  setup_cost_inr: 'Implementation cost (one-time)',
  annual_benefit_inr: 'Expected annual benefit',
  annual_running_cost_inr: 'Annual running cost',
  benefit_type: 'Benefit type',
  overlap_group: 'Overlap group',
  adoption_pct: 'Expected adoption',
  readiness: 'Team data readiness',
  ramp_months: 'Ramp-up',
  delay_months: 'Delay before go-live',
}
const OPTIONAL = new Set<FieldName>(['annual_running_cost_inr', 'benefit_type', 'overlap_group', 'adoption_pct', 'readiness', 'ramp_months', 'delay_months'])
const MONEY = new Set<FieldName>(['setup_cost_inr', 'annual_benefit_inr', 'annual_running_cost_inr'])
const MONTHS = new Set<FieldName>(['ramp_months', 'delay_months'])

function displayValue(name: FieldName, value: unknown): string {
  if (value === null || value === undefined) return ''
  if (MONEY.has(name) && typeof value === 'number') return String(value)
  return String(value)
}

function fieldError(name: FieldName, value: string): string | null {
  if (value.trim() === '') return OPTIONAL.has(name) ? null : 'Required before confirmation.'
  if (MONEY.has(name)) {
    const n = Number(value.replace(/[,\s]/g, ''))
    if (!Number.isFinite(n) || n < 0) return 'Enter a non-negative INR amount.'
  } else if (name === 'adoption_pct') {
    const n = Number(value)
    if (!Number.isFinite(n) || n < 0 || n > 100) return 'Enter a percentage from 0 to 100.'
  } else if (MONTHS.has(name)) {
    const n = Number(value)
    if (!Number.isInteger(n) || n < 0 || n > 120) return 'Enter a whole number from 0 to 120.'
  } else if (name === 'task' && !/^[A-Za-z0-9_\- ]{1,80}$/.test(value.trim())) {
    return 'Use 1–80 letters, digits, spaces, hyphens or underscores.'
  } else if (name === 'overlap_group' && value.length > 40) {
    return 'Use at most 40 characters.'
  } else if (name === 'benefit_type' && !(BENEFIT_TYPES as readonly string[]).includes(value)) {
    return 'Choose a supported benefit type.'
  } else if (name === 'readiness' && !(READINESS as readonly string[]).includes(value)) {
    return 'Choose low, medium or high.'
  }
  return null
}

function statusPill(field: ProposalExtractionField) {
  if (field.status === 'missing') return <Pill tone="warn">Missing</Pill>
  if (field.status === 'ambiguous') return <Pill tone="warn">Ambiguous</Pill>
  if (field.status === 'derived') return <Pill tone="warn">Derived — review required</Pill>
  if (field.evidence?.some((e) => e.source === 'human correction')) return <Pill tone="info">User-confirmed</Pill>
  return <Pill tone="ok">Extracted</Pill>
}

export function ProposalExtractionReview({ uploadId, filename }: { uploadId: string; filename: string }) {
  const [extraction, setExtraction] = useState<ProposalExtraction | null>(null)
  const [draft, setDraft] = useState<Partial<Record<FieldName, string>>>({})
  const [busy, setBusy] = useState(false)
  const [starting, setStarting] = useState(false)
  const [err, setErr] = useState<unknown>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [submitted, setSubmitted] = useState(false)

  const load = async () => {
    try {
      const result = (await api.getProposalExtraction(uploadId)).data
      setExtraction(result)
      setDraft((previous) => Object.fromEntries(FIELD_ORDER.map((name) => [name, previous[name] ?? displayValue(name, result.fields[name]?.value)])))
      setErr(null)
    } catch (e) {
      if (e instanceof ApiError && e.code === 'not_found') {
        setExtraction(null)
      } else {
        setErr(e)
      }
    }
  }
  useEffect(() => { void load() }, [uploadId])
  useEffect(() => {
    if (!extraction || (extraction.status !== 'processing')) return
    const timer = window.setTimeout(() => { void load() }, 1000)
    return () => window.clearTimeout(timer)
  }, [extraction, uploadId])

  const errors = useMemo(() => Object.fromEntries(FIELD_ORDER.map((name) => [name, fieldError(name, draft[name] ?? '')])), [draft])
  const invalid = FIELD_ORDER.some((name) => errors[name] !== null)

  const start = async () => {
    setStarting(true); setErr(null); setMessage(null)
    try { await api.startProposalExtraction(uploadId); await load() } catch (e) { setErr(e) } finally { setStarting(false) }
  }

  const persistCorrections = async () => {
    if (!extraction) return
      for (const name of FIELD_ORDER) {
        const raw = draft[name]?.trim() ?? ''
        const value = raw === '' && OPTIONAL.has(name) ? null
          : MONEY.has(name) ? Number(raw.replace(/[,\s]/g, ''))
          : MONTHS.has(name) || name === 'adoption_pct' ? Number(raw)
          : raw
        const original = displayValue(name, extraction.fields[name]?.value)
        if (String(value ?? '') !== original) await api.correctProposalExtraction(uploadId, name, value, 'Reviewed in the proposal extraction screen')
      }
  }

  const saveCorrections = async () => {
    if (!extraction || invalid || busy) return
    setBusy(true); setErr(null); setMessage(null)
    try {
      await persistCorrections()
      setMessage('Corrections saved. Review the evidence, then confirm the proposal.')
      await load()
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  const confirm = async () => {
    if (!extraction || invalid || busy || submitted) return
    setBusy(true); setErr(null); setMessage(null)
    try {
      await persistCorrections()
      const confirmed = (await api.confirmProposalExtraction(uploadId)).data
      const created = (await api.createProposal({
        name: confirmed.proposal.name,
        inputs: confirmed.proposal.inputs,
        notes: `Imported from ${filename}. Evidence remains attached to the extraction review.`,
      })).data
      setSubmitted(true)
      setMessage(`Proposal “${created.name}” was created and is ready for assessment.`)
    } catch (e) { setErr(e) } finally { setBusy(false) }
  }

  if (!extraction) {
    return <Card title="Proposal extraction" sub="Review extracted assessment fields before adding this proposal.">
      {err != null && <ErrorNotice title="Extraction status could not be loaded." error={err} />}
      <p className="caption">No extraction has been started for this document.</p>
      <button className="btn btn-primary" onClick={() => void start()} disabled={starting}>{starting ? 'Starting extraction…' : 'Extract proposal fields'}</button>
    </Card>
  }
  if (extraction.status === 'processing') return <Card title="Proposal extraction" sub={filename}><p className="caption" role="status">Reading the document and preserving source evidence…</p></Card>
  if (extraction.status === 'failed') return <Card title="Proposal extraction failed" sub={filename}><ErrorNotice title="The document could not be extracted." error={extraction.error ?? 'No readable proposal fields were found.'} onRetry={() => void start()} /></Card>

  return <Card title="Review extracted proposal" sub={`${filename} · values are not submitted until you confirm`}>
    {err != null && <ErrorNotice title="The proposal was not submitted." error={err} />}
    {message && <div className="notice notice-ok" role="status"><div>{message}</div></div>}
    {extraction.warnings?.some((warning) => warning === 'extraction_mode: bedrock') &&
      <div className="notice notice-info" role="status"><div>Extracted with Amazon Bedrock. Review every value and source before confirming.</div></div>}
    {extraction.warnings?.some((warning) => warning === 'extraction_mode: deterministic') &&
      <div className="notice notice-info" role="status"><div>Extracted deterministically from the document text. Review every value and source before confirming.</div></div>}
    <div className="stack">
      {FIELD_ORDER.map((name) => {
        const field = extraction.fields[name]
        const value = draft[name] ?? ''
        const error = errors[name]
        const evidence = field?.evidence ?? []
        const label = LABELS[name]
        const isSelect = name === 'benefit_type' || name === 'readiness'
        return <div key={name} className="card" style={{ padding: 14 }}>
          <div className="row" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <div><strong>{label}</strong><div className="caption">{name}</div></div>
            {statusPill(field)}
          </div>
          <div className="form-grid" style={{ marginTop: 10 }}>
            {isSelect ? <select className="select" aria-label={label} value={value} onChange={(e) => setDraft((x) => ({ ...x, [name]: e.target.value }))}>
              <option value="">Not stated</option>
              {(name === 'benefit_type' ? BENEFIT_TYPES : READINESS).map((option) => <option key={option} value={option}>{option}</option>)}
            </select> : <input className="input" aria-label={label} inputMode={MONEY.has(name) ? 'decimal' : MONTHS.has(name) || name === 'adoption_pct' ? 'numeric' : 'text'}
              value={value} onChange={(e) => setDraft((x) => ({ ...x, [name]: e.target.value }))} />}
          </div>
          {MONEY.has(name) && value && <div className="caption" style={{ marginTop: 5 }}>{formatINR(Number(value.replace(/[,\s]/g, '')))} · {name === 'setup_cost_inr' ? 'one-time' : 'per year'}</div>}
          {error && <div className="field-error" role="alert">{error}</div>}
          {field?.explanation && <div className="hint">{field.explanation}</div>}
          {evidence.length > 0 && <details className="more"><summary>Source evidence</summary><div className="more-body caption">{evidence.map((item, i) => <p key={`${item.source}-${i}`}><strong>{item.source}</strong>: {item.excerpt}</p>)}</div></details>}
        </div>
      })}
    </div>
    <div className="row" style={{ marginTop: 14 }}>
      <button className="btn" onClick={() => void saveCorrections()} disabled={busy || invalid || submitted}>{busy ? 'Saving…' : 'Save corrections'}</button>
      <button className="btn btn-primary" onClick={() => void confirm()} disabled={busy || invalid || submitted}>{submitted ? 'Submitted' : busy ? 'Submitting…' : 'Confirm and add proposal'}</button>
    </div>
    <p className="hint">Missing financial values remain blank. No productivity or coverage value is converted into annual cash benefit.</p>
  </Card>
}
