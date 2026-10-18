import type { EvaluationSummary, Evidence, EvidenceKind, EvidenceQuality, Proposal, ProposalSummary } from './types'
import { V1, enc, json } from './http'

export const createProposal = (b: { name: string; inputs?: unknown; notes?: string | null }) => json<Proposal>('POST', `${V1}/proposals`, b)
export const listProposals = async (includeArchived = false) =>
  (await json<{ proposals: ProposalSummary[] | null }>('GET', `${V1}/proposals${includeArchived ? '?include_archived=true' : ''}`)).data.proposals ?? []
export const getProposal = (id: string) => json<Proposal>('GET', `${V1}/proposals/${enc(id)}`)
export const updateProposal = (id: string, b: { name?: string; inputs?: unknown; notes?: string | null }) => json<Proposal>('PATCH', `${V1}/proposals/${enc(id)}`, b)
export const archiveProposal = (id: string) => json<Proposal>('POST', `${V1}/proposals/${enc(id)}/archive`)
export const restoreProposal = (id: string) => json<Proposal>('POST', `${V1}/proposals/${enc(id)}/restore`)
export const deleteProposal = (id: string) => json<void>('DELETE', `${V1}/proposals/${enc(id)}`)
export const addEvidence = (id: string, b: { kind: EvidenceKind; source: string; quality?: EvidenceQuality | null; note?: string | null }) =>
  json<Evidence>('POST', `${V1}/proposals/${enc(id)}/evidence`, b)
export const listEvidence = async (id: string) => (await json<{ evidence: Evidence[] | null }>('GET', `${V1}/proposals/${enc(id)}/evidence`)).data.evidence ?? []
/** Evaluations run for this proposal, each with the inputs version it used. */
export const listProposalEvaluations = async (id: string) =>
  (await json<{ evaluations: EvaluationSummary[] | null }>('GET', `${V1}/proposals/${enc(id)}/evaluations`)).data.evaluations ?? []
