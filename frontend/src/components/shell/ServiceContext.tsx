import { createContext, useContext } from 'react'
import { ServiceStatus, useServiceStatus } from '../../api/useHealth'

const Ctx = createContext<ServiceStatus | null>(null)

/** One shared health check for the whole app, so the header and the system pages always agree. */
export function ServiceProvider({ children }: { children: React.ReactNode }) {
  const status = useServiceStatus()
  return <Ctx.Provider value={status}>{children}</Ctx.Provider>
}

export function useService(): ServiceStatus {
  const v = useContext(Ctx)
  if (!v) throw new Error('useService must be used inside ServiceProvider')
  return v
}
