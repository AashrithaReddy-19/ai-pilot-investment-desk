import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../../api/client'
import type { MeResponse } from '../../api/viewTypes'
import { Icon } from '../ui/Icon'

/** The signed-in account from GET /me. With AUTH_MODE=off there is no user: the gateway says so and the menu shows it. */
export function AccountMenu() {
  const [me, setMe] = useState<MeResponse | null>(null)
  const [failed, setFailed] = useState(false)
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => { api.me().then((r) => setMe(r.data)).catch(() => setFailed(true)) }, [])
  useEffect(() => {
    if (!open) return
    const down = (e: MouseEvent) => { if (box.current && !box.current.contains(e.target as Node)) setOpen(false) }
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', down)
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('mousedown', down); document.removeEventListener('keydown', key) }
  }, [open])

  const name = me?.user?.display_name || me?.user?.email || null
  const initials = name ? name.split(/[\s@.]+/).filter(Boolean).slice(0, 2).map((s) => s[0]?.toUpperCase()).join('') : null
  return (
    <div className="account pop-wrap" ref={box}>
      <button className="avatar" aria-haspopup="true" aria-expanded={open} aria-label="Account" onClick={() => setOpen((o) => !o)}>
        {initials ?? <Icon name="user" />}
      </button>
      {open && (
        <div className="popover" role="region" aria-label="Account details">
          {failed && <p className="caption">The account could not be loaded from the gateway.</p>}
          {me && (
            <>
              <strong>{name ?? (me.auth_mode === 'off' ? 'No sign-in (auth is off)' : 'Signed in')}</strong>
              <dl className="dl">
                <dt>Workspace</dt><dd>{me.workspace?.name ?? 'None chosen'}</dd>
                <dt>Role</dt><dd style={{ textTransform: 'capitalize' }}>{me.role}</dd>
                <dt>Authentication</dt><dd>{me.auth_mode === 'off' ? 'Off (local)' : 'Required'}</dd>
              </dl>
              {me.auth_mode === 'off' && <p className="hint">Everyone acts as admin in the default workspace. Turn on AUTH_MODE=required on the gateway for named users.</p>}
            </>
          )}
          <Link to="/settings" className="btn btn-sm" onClick={() => setOpen(false)}>Settings</Link>
        </div>
      )}
    </div>
  )
}
