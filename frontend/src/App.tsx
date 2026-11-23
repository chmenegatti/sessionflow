import { HashRouter, Route, Routes } from "react-router";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppLayout } from "@/components/layout/AppLayout";
import { DashboardPage } from "@/features/dashboard/DashboardPage";
import { SessionsPage } from "@/features/sessions/SessionsPage";
import { FavoritesPage } from "@/features/sessions/FavoritesPage";
import { ProjectsPage } from "@/features/projects/ProjectsPage";
import { ProjectSessionsPage } from "@/features/projects/ProjectSessionsPage";
import { SearchPage } from "@/features/search/SearchPage";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { AnalyticsLayout } from "@/features/analytics/AnalyticsLayout";
import { OverviewPage } from "@/features/analytics/OverviewPage";
import { UsagePage } from "@/features/analytics/UsagePage";
import { ActivityPage } from "@/features/analytics/ActivityPage";
import { CostsPage } from "@/features/analytics/CostsPage";
import { PricesPage } from "@/features/analytics/PricesPage";
import { PatternsPage } from "@/features/analytics/PatternsPage";
import { Suspense, lazy } from "react";
import { MotionConfig } from "motion/react";
import { StartupGate } from "@/features/startup/StartupGate";

// O viewer traz Markdown e destaque de sintaxe (~500 KB): carregado sob demanda.
const ViewerPage = lazy(() => import("@/features/viewer/ViewerPage").then((m) => ({ default: m.ViewerPage })));

// HashRouter: o asset server do Wails serve arquivos estáticos; rotas por
// hash não dependem de fallback para index.html.
export default function App() {
  return (
    // reducedMotion="user": respeita "reduzir movimento" do sistema.
    <StartupGate>
    <MotionConfig reducedMotion="user">
    <TooltipProvider delayDuration={300}>
      <HashRouter>
        <Routes>
          <Route element={<AppLayout />}>
            <Route index element={<DashboardPage />} />
            <Route path="sessions" element={<SessionsPage />} />
            <Route path="projects" element={<ProjectsPage />} />
            <Route path="projects/view" element={<ProjectSessionsPage />} />
            <Route path="favorites" element={<FavoritesPage />} />
            <Route path="search" element={<SearchPage />} />
            <Route path="settings" element={<SettingsPage />} />
            <Route path="analytics" element={<AnalyticsLayout />}>
              <Route index element={<OverviewPage />} />
              <Route path="usage" element={<UsagePage />} />
              <Route path="activity" element={<ActivityPage />} />
              <Route path="costs" element={<CostsPage />} />
              <Route path="costs/prices" element={<PricesPage />} />
              <Route path="patterns" element={<PatternsPage />} />
            </Route>
            <Route path="session" element={<Suspense fallback={null}><ViewerPage /></Suspense>} />
          </Route>
        </Routes>
      </HashRouter>
    </TooltipProvider>
    </MotionConfig>
    </StartupGate>
  );
}
