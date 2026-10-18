/** Small pieces both the balance-sheet panel and the comparison panel use. */

export const inr = (n: number | null | undefined) => (n === null || n === undefined ? '—' : n.toLocaleString('en-IN', { maximumFractionDigits: 0 }))

export function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div style={{ flex: '1 1 160px', minWidth: 150 }}>
      <div className="caption">{label}</div>
      <div className="strong" style={{ fontSize: 'var(--t-lg)' }}>{value}</div>
      {sub && <div className="caption">{sub}</div>}
    </div>
  )
}
