import { useCallback, useEffect, useState } from 'react'
import { create } from 'zustand'
import { api } from '../api/client'
import type { Proposal } from '../api/types'

/**
 * The selection the FYND screens share: which stored proposal is "the service" being evaluated, and the
 * AI burn the person confirmed from a balance sheet in this session. Kept in memory only; the stored
 * records (proposals, uploads, snapshots) live in the database.
 */
interface FyndState {
  proposalId: string | null
  setProposalId: (id: string | null) => void
  burn: { value: number; source: string } | null
  setBurn: (b: { value: number; source: string } | null) => void
}

export const useFynd = create<FyndState>((set) => ({
  proposalId: null,
  setProposalId: (proposalId) => set({ proposalId }),
  burn: null,
  setBurn: (burn) => set({ burn }),
}))

/* ---- stored proposals with their inputs ----
   The list endpoint returns summaries, so each proposal is opened for its inputs. That is one request per
   proposal, so loads are shared: screens that mount together join one in-flight load, a result is reused for a
   few seconds (8), and at most CONCURRENCY requests run at once (the gateway rate-limits per client). A `force`
   load (after the person changes a proposal) always goes to the service. */
const CONCURRENCY = 6
const FRESH_MS = 8000
const inflight = new Map<boolean, Promise<Proposal[]>>()
const cached = new Map<boolean, { at: number; docs: Proposal[] }>()

async function loadDocs(includeArchived: boolean, force: boolean): Promise<Proposal[]> {
  const hit = cached.get(includeArchived)
  if (!force && hit && Date.now() - hit.at < FRESH_MS) return hit.docs
  const running = inflight.get(includeArchived)
  if (running && !force) return running
  const p = (async () => {
    const list = await api.listProposals(includeArchived)
    const out: Proposal[] = new Array(list.length)
    let next = 0
    await Promise.all(Array.from({ length: Math.min(CONCURRENCY, list.length) }, async () => {
      while (next < list.length) {
        const i = next++
        out[i] = (await api.getProposal(list[i].id)).data
      }
    }))
    cached.set(includeArchived, { at: Date.now(), docs: out })
    return out
  })()
  inflight.set(includeArchived, p)
  try { return await p } finally { if (inflight.get(includeArchived) === p) inflight.delete(includeArchived) }
}

/** Every stored proposal with its inputs. `reload` always asks the service again. */
export function useProposalDocs(includeArchived = false) {
  const [docs, setDocs] = useState<Proposal[] | null>(null)
  const [error, setError] = useState<unknown>(null)
  const [loading, setLoading] = useState(false)
  const load = useCallback(async (force: boolean) => {
    setLoading(true)
    try { setDocs(await loadDocs(includeArchived, force)); setError(null) } catch (e) { setError(e) } finally { setLoading(false) }
  }, [includeArchived])
  useEffect(() => { void load(false) }, [load])
  const reload = useCallback(() => load(true), [load])
  return { docs, error, loading, reload }
}
