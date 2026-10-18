import { Link } from 'react-router-dom'
import { Empty, Skeleton } from '../ui/Bits'
import { ErrorNotice } from '../ui/ErrorNotice'
import { leftOutText } from '../../state/useStoredScenario'

/** Shared empty/loading/error state for tools that run on the workspace's stored proposals. */
export function NeedsProposals({ loaded, error, empty, leftOut, onRetry }: {
  loaded: boolean; error: unknown; empty: boolean; leftOut: { name: string; why: string }[]; onRetry: () => void
}) {
  if (error != null) return <ErrorNotice title="Proposals could not be loaded." error={error} onRetry={onRetry} />
  if (!loaded) return <Skeleton rows={3} height={44} />
  if (!empty) return null
  return (
    <Empty title="No usable proposal yet" actions={<Link to="/ai-proposals" className="btn btn-primary">Add or import proposals</Link>}>
      This tool works on your stored AI proposals. Add or import one with an implementation cost, an annual benefit and a benefit type (cashable or time freed). {leftOutText(leftOut) ?? ''}
    </Empty>
  )
}

export function LeftOutNote({ leftOut }: { leftOut: { name: string; why: string }[] }) {
  const t = leftOutText(leftOut)
  return t ? <p className="hint">{t} <Link to="/ai-proposals">Edit proposals</Link></p> : null
}
