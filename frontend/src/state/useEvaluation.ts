import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api/client'
import type { Proposal } from '../api/types'
import type { DashboardView, DecisionView, ViewRequest, WorkspaceView } from '../api/viewTypes'
import { SET_MIN, evaluationSet, isEvaluable } from '../domain/fynd'
import { useFynd, useProposalDocs } from './useFynd'

export interface Evaluation {
  dashboard: WorkspaceView<DashboardView>
  decision: WorkspaceView<DecisionView>
  request: ViewRequest
}

/**
 * One evaluation of the workspace's evaluation set: the decision view and the dashboard view, requested with
 * the identical body at the same moment, so every card on a screen reads the same inputs versions and engine
 * version. Both answers are stored by the gateway as snapshots.
 */
export function useEvaluation() {
  const { docs, error: docsError, reload: reloadDocs } = useProposalDocs(false)
  const burn = useFynd((s) => s.burn)
  const [data, setData] = useState<Evaluation | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(false)
  const seq = useRef(0)

  const { set: evaluable, left } = useMemo(() => evaluationSet(docs ?? []), [docs])
  const drafts = useMemo(() => (docs ?? []).filter((p) => p.status === 'active' && !isEvaluable(p.inputs)), [docs])

  const run = useCallback(async () => {
    if (evaluable.length < SET_MIN) { setData(null); return }
    const request: ViewRequest = { proposal_ids: evaluable.map((p) => p.id) }
    if (typeof burn?.value === 'number') request.ai_burn_to_date = burn.value
    const my = ++seq.current
    setLoading(true); setError(null)
    try {
      const [decision, dashboard] = await Promise.all([api.workspaceDecision(request), api.workspaceDashboard(request)])
      if (my === seq.current) setData({ decision: decision.data, dashboard: dashboard.data, request })
    } catch (e) {
      if (my === seq.current) { setError(e); setData(null) }
    } finally { if (my === seq.current) setLoading(false) }
  }, [evaluable, burn])

  useEffect(() => { if (docs !== null) void run() }, [docs, run])

  return { docs, docsError, reloadDocs, evaluable, left, drafts, data, error, loading, run }
}

/** The engine names a proposal by its stored name; the snapshot maps each name back to the stored id. */
export function engineNames(e: Evaluation | null, evaluable: Proposal[]): { key: string; name: string; id: string | null; notes: string | null }[] {
  if (!e) return []
  const used = e.decision.snapshot.proposals
  return used.map((u) => ({ key: u.id, name: u.name, id: u.id, notes: evaluable.find((p) => p.id === u.id)?.notes ?? null }))
}
