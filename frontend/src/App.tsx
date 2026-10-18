import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/shell/AppShell'
import Workspace from './pages/Workspace'
import {
  BalanceSheetPage, ComparePage, EvidencePage, HistoryPage, InterviewsPage, MemoPage, ObservationsPage, OptimizePage,
  PortfoliosPage, ProposalsPage, TrackerPage, WhatIfPage,
} from './pages/SimplePages'
import { ChecksPage } from './features/system/ChecksPage'
import { AboutPage } from './features/system/AboutPage'

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Workspace />} />
        <Route path="optimize" element={<OptimizePage />} />
        <Route path="compare" element={<ComparePage />} />
        <Route path="balance-sheet" element={<BalanceSheetPage />} />
        <Route path="what-if" element={<WhatIfPage />} />
        <Route path="evidence" element={<EvidencePage />} />
        <Route path="tracker" element={<TrackerPage />} />
        <Route path="memo" element={<MemoPage />} />
        <Route path="portfolios" element={<PortfoliosPage />} />
        <Route path="observations" element={<ObservationsPage />} />
        <Route path="interviews" element={<InterviewsPage />} />
        <Route path="proposals" element={<ProposalsPage />} />
        <Route path="history" element={<HistoryPage />} />
        <Route path="checks" element={<ChecksPage />} />
        <Route path="about-limits" element={<AboutPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
