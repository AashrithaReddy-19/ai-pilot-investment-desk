import { Navigate, Route, Routes } from 'react-router-dom'
import AppShell from './components/shell/AppShell'
import Workspace from './pages/Workspace'
import {
  BalanceSheetPage, ComparePage, EvidencePage, HistoryPage, InterviewsPage, MemoPage, ObservationsPage, OptimizePage,
  PortfoliosPage, ProposalsPage, TrackerPage, WhatIfPage,
} from './pages/SimplePages'
import { ChecksPage } from './features/system/ChecksPage'
import { AboutPage } from './features/system/AboutPage'
import Home from './pages/fynd/Home'
import Processing from './pages/fynd/Processing'
import Baseline from './pages/fynd/Baseline'
import Dashboard from './pages/fynd/Dashboard'
import AiProposals from './pages/fynd/AiProposals'
import Comparison from './pages/fynd/Comparison'
import Scenarios from './pages/fynd/Scenarios'
import Analysis from './pages/fynd/Analysis'
import Reports from './pages/fynd/Reports'
import Settings from './pages/fynd/Settings'

export default function App() {
  return (
    <Routes>
      <Route element={<AppShell />}>
        {/* FYND workflow */}
        <Route index element={<Home />} />
        <Route path="processing" element={<Processing />} />
        <Route path="processing/:id" element={<Processing />} />
        <Route path="dashboard" element={<Dashboard />} />
        <Route path="baseline" element={<Baseline />} />
        <Route path="ai-proposals" element={<AiProposals />} />
        <Route path="comparison" element={<Comparison />} />
        <Route path="scenarios" element={<Scenarios />} />
        <Route path="analysis" element={<Analysis />} />
        <Route path="reports" element={<Reports />} />
        <Route path="settings" element={<Settings />} />
        {/* Workbench: the existing tools, unchanged */}
        <Route path="workspace" element={<Workspace />} />
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
