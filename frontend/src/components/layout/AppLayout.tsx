import { useEffect } from "react";
import { Outlet, useLocation } from "react-router";
import { PageErrorBoundary } from "./PageErrorBoundary";
import { AnimatePresence, motion } from "motion/react";
import { Events } from "@wailsio/runtime";
import { CostsUpdated, IndexUpdated } from "@/lib/api";
import { Sidebar } from "./Sidebar";
import { DetailPanel } from "./DetailPanel";
import { useShortcuts } from "@/hooks/useShortcuts";
import { NoticeToast, ResumeConfirm } from "@/features/resume/ResumeOverlays";
import { useAppStore } from "@/lib/store";

export function AppLayout() {
  useShortcuts();
  const loadStatus = useAppStore((s) => s.loadStatus);
  const detailOpen = useAppStore((s) => s.detailOpen);
  const touch = useAppStore((s) => s.touch);
  const location = useLocation();

  useEffect(() => {
    void loadStatus();
    // O backend avisa quando a indexação termina.
    const offIndex = Events.On(IndexUpdated, () => {
      void loadStatus();
      touch();
    });
    // Estimativas recalculadas (depois da sync ou de mudar um preço).
    const offCosts = Events.On(CostsUpdated, () => touch());
    return () => {
      offIndex();
      offCosts();
    };
  }, [loadStatus, touch]);

  return (
    <div className="flex h-screen overflow-hidden text-foreground">
      <div className="aurora-bg" aria-hidden />
      <Sidebar />
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        {/* Entrada suave a cada troca de página (só entrada: navegar não espera animação). */}
        <motion.div
          // Seção (1º segmento): trocar de aba dentro do Analytics não remonta a página.
          key={"/" + location.pathname.split("/")[1]}
          className="flex min-h-0 flex-1 flex-col"
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.22, ease: "easeOut" }}
        >
          <PageErrorBoundary>
            <Outlet />
          </PageErrorBoundary>
        </motion.div>
      </main>
      <AnimatePresence initial={false}>
        {detailOpen && (
          <motion.div
            key="detail"
            className="flex shrink-0 overflow-hidden"
            initial={{ width: 0, opacity: 0 }}
            animate={{ width: 320, opacity: 1 }}
            exit={{ width: 0, opacity: 0 }}
            transition={{ type: "spring", bounce: 0, duration: 0.3 }}
          >
            <DetailPanel />
          </motion.div>
        )}
      </AnimatePresence>
      <ResumeConfirm />
      <NoticeToast />
    </div>
  );
}
