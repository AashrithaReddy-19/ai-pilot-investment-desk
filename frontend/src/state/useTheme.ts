import { useCallback, useEffect, useState } from 'react'

export type Theme = 'light' | 'dark'
const KEY = 'c23-theme'

function storedTheme(): Theme | null {
  try { const t = localStorage.getItem(KEY); return t === 'light' || t === 'dark' ? t : null } catch { return null }
}

/** FYND is designed dark-first: dark on first visit; an explicit choice is remembered. Only the preference is stored, never business data. */
export function useTheme(): { theme: Theme; toggle: () => void } {
  const [explicit, setExplicit] = useState<Theme | null>(storedTheme)

  const theme = explicit ?? 'dark'
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
