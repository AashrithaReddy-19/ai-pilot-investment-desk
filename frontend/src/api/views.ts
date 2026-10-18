import type { CompareResponse } from './types'
import type {
  Approval, DashboardView, DecisionView, MeResponse, ReplayResult, Snapshot, SnapshotDetail, UsageForecast, ViewRequest,
  WorkspaceSettings, WorkspaceView, YearlyView,
} from './viewTypes'
import { V1, enc, file, json } from './http'
import type { Download } from './http'

/* ---- Workspace views over the stored proposals (each answer is kept as a snapshot) ---- */

export const workspaceDashboard = (b: ViewRequest) => json<WorkspaceView<DashboardView>>('POST', `${V1}/workspace/dashboard`, b)
export const workspaceDecision = (b: ViewRequest) => json<WorkspaceView<DecisionView>>('POST', `${V1}/workspace/decision`, b)
/** Needs 2 to 8 stored proposals. */
export const workspaceComparison = (b: ViewRequest) => json<WorkspaceView<CompareResponse>>('POST', `${V1}/workspace/comparison`, b)
export const workspaceYearly = (b: ViewRequest) => json<WorkspaceView<YearlyView>>('POST', `${V1}/workspace/yearly-savings`, b)
export const workspaceReport = (b: ViewRequest): Promise<Download> => file(`${V1}/workspace/export/decision-report`, b, 'decision-report.md')
export const workspaceYearlyCsv = (b: ViewRequest): Promise<Download> => file(`${V1}/workspace/export/yearly-csv`, b, 'yearly-savings.csv')

export const listSnapshots = async (kind?: Snapshot['kind'], limit = 20) =>
  (await json<{ snapshots: Snapshot[] | null }>('GET', `${V1}/snapshots?limit=${limit}${kind ? `&kind=${kind}` : ''}`)).data.snapshots ?? []
export const getSnapshot = <T = unknown>(id: string) => json<SnapshotDetail<T>>('GET', `${V1}/snapshots/${enc(id)}`)
export const replaySnapshot = (id: string) => json<ReplayResult>('POST', `${V1}/snapshots/${enc(id)}/replay`)

/* ---- Explicit proposal lists (cached pass-through, nothing stored): used for unsaved what-if runs ---- */

export interface ExplicitSet {
  proposals: Record<string, unknown>[]
  budget?: number | null
  mode?: string
  extension?: Record<string, unknown>
  budget_set?: Record<string, number>
  weights?: Record<string, number>
}
export const yearlySavings = (b: ExplicitSet) => json<YearlyView>('POST', `${V1}/views/yearly-savings`, b)
export const decisionView = (b: ExplicitSet) => json<DecisionView>('POST', `${V1}/decision`, b)
export const forecastUsage = (b: { history: number[]; months?: number; annual_usage_growth_pct?: number | null }) =>
  json<UsageForecast>('POST', `${V1}/forecast/usage`, b)

/* ---- Settings, approvals, account ---- */

export const getSettings = () => json<WorkspaceSettings>('GET', `${V1}/settings`)
export const putSettings = (s: WorkspaceSettings) => json<WorkspaceSettings>('PUT', `${V1}/settings`, s)
export const listApprovals = async (proposalId: string) =>
  (await json<{ approvals: Approval[] | null }>('GET', `${V1}/proposals/${enc(proposalId)}/approvals`)).data.approvals ?? []
export const addApproval = (proposalId: string, b: { decision: Approval['decision']; comment?: string | null; snapshot_id?: string | null }) =>
  json<Approval>('POST', `${V1}/proposals/${enc(proposalId)}/approvals`, b)
export const me = () => json<MeResponse>('GET', `${V1}/me`)
