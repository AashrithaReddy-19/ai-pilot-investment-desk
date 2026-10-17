import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/shell/AppShell'
import Workspace from './pages/Workspace'
import { InterviewsPage, ObservationsPage, OptimizePage, PortfoliosPage } from './pages/SimplePages'
import { ChecksPage } from './features/system/ChecksPage'
import { AboutPage } from './features/system/AboutPage'

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Workspace />} />
        <Route path="optimize" element={<OptimizePage />} />
        <Route path="portfolios" element={<PortfoliosPage />} />
        <Route path="observations" element={<ObservationsPage />} />
        <Route path="interviews" element={<InterviewsPage />} />
        <Route path="checks" element={<ChecksPage />} />
        <Route path="about-limits" element={<AboutPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
