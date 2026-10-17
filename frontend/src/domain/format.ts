const MINUS = '−'
const NOT_PROVIDED = 'Not provided'

const cache = new Map<number, Intl.NumberFormat>()
function inrFormat(digits: number): Intl.NumberFormat {
  let f = cache.get(digits)
  if (!f) { f = new Intl.NumberFormat('en-IN', { style: 'currency', currency: 'INR', minimumFractionDigits: digits, maximumFractionDigits: digits }); cache.set(digits, f) }
  return f
}

/** Indian digit grouping. Whole rupees show no decimals; fractional amounts keep two so no precision is hidden. Null is "Not provided", never zero. */
export function formatINR(n: number | null | undefined): string {
  if (n === null || n === undefined || Number.isNaN(n)) return NOT_PROVIDED
  const digits = Number.isInteger(n) ? 0 : 2
  return n < 0 ? `${MINUS}${inrFormat(digits).format(-n)}` : inrFormat(digits).format(n)
}

/** Compact lakh/thousand form for tight spaces. */
export function formatINRCompact(n: number): string {
  const a = Math.abs(n)
  const sign = n < 0 ? MINUS : ''
  if (a >= 100000) return `${sign}₹${+(a / 100000).toFixed(2)}L`
  if (a >= 1000) return `${sign}₹${+(a / 1000).toFixed(1)}k`
  return `${sign}₹${a}`
}

/** A null payback means the pilot never pays back under the stated figures. */
export const formatPayback = (m: number | null | undefined): string => (m === null || m === undefined ? 'Never' : `${m.toFixed(1)} months`)
export const formatPct = (n: number | null | undefined, digits = 0): string => (n === null || n === undefined ? NOT_PROVIDED : `${n.toFixed(digits)}%`)
export const orNotProvided = (s: string | null | undefined): string => (s && s.trim() !== '' ? s : NOT_PROVIDED)
export const isUnknownGroup = (g: string): boolean => g.startsWith('__own_')
export const groupLabel = (g: string): string => (isUnknownGroup(g) ? 'Not provided' : g)

export function formatDateTime(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
export function formatDate(iso: string): string {
  const d = new Date(`${iso.slice(0, 10)}T00:00:00`)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { dateStyle: 'medium' })
}
