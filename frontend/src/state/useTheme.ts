import { useCallback, useEffect, useState } from 'react'

export type Theme = 'light' | 'dark'
const KEY = 'c23-theme'

function systemTheme(): Theme {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}
function storedTheme(): Theme | null {
  try { const t = localStorage.getItem(KEY); return t === 'light' || t === 'dark' ? t : null } catch { return null }
}

/** Follows the system on first visit; an explicit choice is remembered. Only the preference is stored, never business data. */
export function useTheme(): { theme: Theme; toggle: () => void } {
  const [explicit, setExplicit] = useState<Theme | null>(storedTheme)
  const [system, setSystem] = useState<Theme>(systemTheme)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const on = () => setSystem(mq.matches ? 'dark' : 'light')
    mq.addEventListener('change', on)
    return () => { mq.removeEventListener('change', on) }
  }, [])

  const theme = explicit ?? system
  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme)
  }, [theme])

  const toggle = useCallback(() => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark'
    setExplicit(next)
    try { localStorage.setItem(KEY, next) } catch { /* private mode: the choice just lasts for this visit */ }
  }, [theme])

  return { theme, toggle }
}
