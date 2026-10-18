import { useState } from 'react'

/**
 * Small SVG charts with no extra dependency. They draw exactly the values they are given: no smoothing,
 * no extrapolation, a zero baseline for bars, and gaps where a value is missing (null is never drawn as 0).
 * Each chart carries a visually hidden table with the same numbers for screen readers.
 */

export const SERIES_COLORS = ['var(--accent)', 'var(--info)', 'var(--ok)', 'var(--warn)', 'var(--text-2)', 'var(--bad)', '#b39ddb', '#80cbc4']

export interface Series { name: string; values: (number | null)[]; color?: string; dashed?: boolean }

const W = 720
const PAD = { l: 64, r: 16, t: 12, b: 34 }

function niceTicks(lo: number, hi: number, n = 5): number[] {
  if (lo === hi) { hi = lo + 1 }
  const span = hi - lo
  const step0 = span / n
  const mag = Math.pow(10, Math.floor(Math.log10(step0)))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= step0) ?? 10 * mag
  const start = Math.floor(lo / step) * step
  const out: number[] = []
  for (let v = start; v <= hi + step * 0.5; v += step) out.push(Math.abs(v) < step / 1e6 ? 0 : v)
  return out
}

function extent(series: Series[], includeZero: boolean): [number, number] {
  const vals = series.flatMap((s) => s.values.filter((v): v is number => v !== null && Number.isFinite(v)))
  if (vals.length === 0) return [0, 1]
  let lo = Math.min(...vals)
  let hi = Math.max(...vals)
  if (includeZero) { lo = Math.min(lo, 0); hi = Math.max(hi, 0) }
  return [lo, hi]
}

function Legend({ series }: { series: Series[] }) {
  return (
    <div className="legend" aria-hidden>
      {series.map((s, i) => <span key={s.name}><span className="swatch" style={{ background: s.color ?? SERIES_COLORS[i % SERIES_COLORS.length] }} />{s.name}</span>)}
    </div>
  )
}

function SrTable({ caption, xLabel, x, series, fmt }: { caption: string; xLabel: string; x: (string | number)[]; series: Series[]; fmt: (n: number) => string }) {
  return (
    // a table ignores the 1px .sr width, so the hidden wrapper is a block
    <div className="sr">
      <table>
        <caption>{caption}</caption>
        <thead><tr><th scope="col">{xLabel}</th>{series.map((s) => <th key={s.name} scope="col">{s.name}</th>)}</tr></thead>
        <tbody>{x.map((xv, i) => <tr key={String(xv)}><th scope="row">{xv}</th>{series.map((s) => <td key={s.name}>{s.values[i] === null ? 'Not available' : fmt(s.values[i] as number)}</td>)}</tr>)}</tbody>
      </table>
    </div>
  )
}

export function GroupedBars({ caption, x, xLabel, series, fmt, axisFmt = fmt, height = 280 }: {
  caption: string; x: (string | number)[]; xLabel: string; series: Series[]; fmt: (n: number) => string; axisFmt?: (n: number) => string; height?: number
}) {
  const [tip, setTip] = useState<{ left: number; top: number; text: string } | null>(null)
  const [lo, hi] = extent(series, true)
  const ticks = niceTicks(lo, hi)
  const yMin = Math.min(ticks[0], 0)
  const yMax = Math.max(ticks[ticks.length - 1], 0)
  const ih = height - PAD.t - PAD.b
  const iw = W - PAD.l - PAD.r
  const y = (v: number) => PAD.t + ih - ((v - yMin) / (yMax - yMin || 1)) * ih
  const group = iw / Math.max(x.length, 1)
  const bw = Math.max(4, Math.min(28, (group * 0.72) / Math.max(series.length, 1)))
  return (
    <figure className="stack" style={{ gap: 8 }}>
      <Legend series={series} />
      <div className="chart-wrap" onMouseLeave={() => setTip(null)}>
        <svg className="chart" viewBox={`0 0 ${W} ${height}`} role="img" aria-label={caption}>
          {ticks.map((t) => (
            <g key={t}>
              <line className={t === 0 ? 'zero' : 'grid'} x1={PAD.l} x2={W - PAD.r} y1={y(t)} y2={y(t)} />
              <text x={PAD.l - 8} y={y(t) + 4} textAnchor="end">{axisFmt(t)}</text>
            </g>
          ))}
          {x.map((xv, gi) => {
            const gx = PAD.l + gi * group + (group - bw * series.length) / 2
            return (
              <g key={String(xv)}>
                {series.map((s, si) => {
                  const v = s.values[gi]
                  if (v === null || v === undefined || !Number.isFinite(v)) return null
                  const top = Math.min(y(v), y(0))
                  const h = Math.max(1, Math.abs(y(v) - y(0)))
                  const bx = gx + si * bw
                  return (
                    <rect key={s.name} x={bx + 1} y={top} width={bw - 2} height={h} rx={2} fill={s.color ?? SERIES_COLORS[si % SERIES_COLORS.length]}
                      onMouseEnter={() => setTip({ left: (bx / W) * 100, top: (top / height) * 100, text: `${xLabel} ${xv} · ${s.name}: ${fmt(v)}` })} />
                  )
                })}
                <text x={PAD.l + gi * group + group / 2} y={height - PAD.b + 18} textAnchor="middle">{xLabel === 'year' ? `Year ${xv}` : xv}</text>
              </g>
            )
          })}
        </svg>
        {tip && <div className="chart-tip" style={{ left: `${tip.left}%`, top: `calc(${tip.top}% - 36px)` }}>{tip.text}</div>}
      </div>
      <SrTable caption={caption} xLabel={xLabel} x={x} series={series} fmt={fmt} />
    </figure>
  )
}

export function Lines({ caption, x, xLabel, series, fmt, axisFmt = fmt, height = 260, band }: {
  caption: string; x: (string | number)[]; xLabel: string; series: Series[]; fmt: (n: number) => string; axisFmt?: (n: number) => string; height?: number
  band?: { low: number[]; high: number[]; label: string }
}) {
  const [hover, setHover] = useState<number | null>(null)
  const all = band ? [...series, { name: band.label, values: [...band.low, ...band.high] }] : series
  const [lo, hi] = extent(all, false)
  const ticks = niceTicks(lo, hi)
  const yMin = ticks[0]
  const yMax = ticks[ticks.length - 1]
  const ih = height - PAD.t - PAD.b
  const iw = W - PAD.l - PAD.r
  const y = (v: number) => PAD.t + ih - ((v - yMin) / (yMax - yMin || 1)) * ih
  const xp = (i: number) => PAD.l + (x.length <= 1 ? iw / 2 : (i / (x.length - 1)) * iw)
  const path = (vals: (number | null)[]) => {
    let d = ''
    let pen = false
    vals.forEach((v, i) => {
      if (v === null || !Number.isFinite(v)) { pen = false; return }
      d += `${pen ? 'L' : 'M'}${xp(i).toFixed(1)},${y(v).toFixed(1)}`
      pen = true
    })
    return d
  }
  const every = Math.max(1, Math.ceil(x.length / 12))
  return (
    <figure className="stack" style={{ gap: 8 }}>
      <Legend series={band ? [...series, { name: band.label, values: [], color: 'color-mix(in srgb, var(--info) 30%, transparent)' }] : series} />
      <div className="chart-wrap" onMouseLeave={() => setHover(null)}>
        <svg className="chart" viewBox={`0 0 ${W} ${height}`} role="img" aria-label={caption}
          onMouseMove={(e) => {
            const r = (e.currentTarget as SVGSVGElement).getBoundingClientRect()
            const px = ((e.clientX - r.left) / r.width) * W
            const i = Math.round(((px - PAD.l) / iw) * (x.length - 1))
            setHover(i >= 0 && i < x.length ? i : null)
          }}>
          {ticks.map((t) => (
            <g key={t}>
              <line className={t === 0 ? 'zero' : 'grid'} x1={PAD.l} x2={W - PAD.r} y1={y(t)} y2={y(t)} />
              <text x={PAD.l - 8} y={y(t) + 4} textAnchor="end">{axisFmt(t)}</text>
            </g>
          ))}
          {band && band.low.length === x.length && (
            <path d={`${band.high.map((v, i) => `${i ? 'L' : 'M'}${xp(i)},${y(v)}`).join('')}${band.low.map((_, i) => `L${xp(band.low.length - 1 - i)},${y(band.low[band.low.length - 1 - i])}`).join('')}Z`}
              fill="color-mix(in srgb, var(--info) 18%, transparent)" stroke="none" />
          )}
          {series.map((s, si) => (
            <path key={s.name} d={path(s.values)} fill="none" stroke={s.color ?? SERIES_COLORS[si % SERIES_COLORS.length]} strokeWidth={2} strokeDasharray={s.dashed ? '5 4' : undefined} />
          ))}
          {x.map((xv, i) => (i % every === 0 || i === x.length - 1) && (
            <text key={String(xv)} x={xp(i)} y={height - PAD.b + 18} textAnchor="middle">{xv}</text>
          ))}
          {hover !== null && <line className="axis" x1={xp(hover)} x2={xp(hover)} y1={PAD.t} y2={PAD.t + ih} />}
        </svg>
        {hover !== null && (
          <div className="chart-tip" style={{ left: `${Math.min(70, (xp(hover) / W) * 100)}%`, top: 4 }}>
            <strong>{xLabel} {x[hover]}</strong>
            {series.map((s) => <div key={s.name}>{s.name}: {s.values[hover] === null || s.values[hover] === undefined ? 'Not available' : fmt(s.values[hover] as number)}</div>)}
            {band && <div>{band.label}: {fmt(band.low[hover])} to {fmt(band.high[hover])}</div>}
          </div>
        )}
      </div>
      <SrTable caption={caption} xLabel={xLabel} x={x} series={band ? [...series, { name: `${band.label} low`, values: band.low }, { name: `${band.label} high`, values: band.high }] : series} fmt={fmt} />
    </figure>
  )
}
