import { NavLink, Outlet, useLocation } from "react-router";
import { Info } from "lucide-react";
import { motion } from "motion/react";
import { PageHeader } from "@/components/layout/PageHeader";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { AnalyticsFilters } from "./AnalyticsFilters";

const tabs = [
  { to: "/analytics", label: "Visão geral" },
  { to: "/analytics/usage", label: "Consumo" },
  { to: "/analytics/activity", label: "Atividade" },
  { to: "/analytics/costs", label: "Custos" },
  { to: "/analytics/patterns", label: "Padrões" },
];

// Texto obrigatório: separação de domínio entre consumo e atividade.
export const SCOPE_NOTE =
  "Consumo de tokens inclui subagentes e sessões automáticas. Atividade considera somente sessões principais e não automáticas.";

export function AnalyticsLayout() {
  const { pathname, search } = useLocation();
  return (
    <>
      <PageHeader title="Analytics" />
      <ScrollArea className="min-h-0 flex-1">
        <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-5 px-6 pt-6 pb-10">
          <header className="flex flex-wrap items-end gap-4">
            <div>
              <h2 className="title-gradient text-[30px] leading-tight font-semibold tracking-tight">
                Analytics
              </h2>
              <p className="mt-1 text-[13px] text-muted-foreground">
                Consumo de tokens, custos estimados e uso dos agentes, a partir
                dos históricos locais.
              </p>
            </div>
            <nav
              aria-label="Telas do Analytics"
              className="ml-auto flex gap-1 rounded-[12px] border bg-white/[0.03] p-1"
            >
              {tabs.map((t) => {
                const active =
                  t.to === "/analytics"
                    ? pathname === "/analytics"
                    : pathname.startsWith(t.to);
                return (
                  <NavLink
                    key={t.to}
                    to={{ pathname: t.to, search }}
                    end
                    className={cn(
                      "relative rounded-[9px] px-3.5 py-1.5 text-[13px] font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
                      active
                        ? "text-foreground"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {active && (
                      <motion.span
                        layoutId="analytics-tab"
                        className="absolute inset-0 rounded-[9px] border border-primary/30 bg-primary/[0.12] shadow-[0_0_16px_-6px_rgba(56,225,245,0.6)]"
                        transition={{
                          type: "spring",
                          bounce: 0.15,
                          duration: 0.35,
                        }}
                      />
                    )}
                    <span className="relative">{t.label}</span>
                  </NavLink>
                );
              })}
            </nav>
          </header>
          {/* A tabela de preços não depende do recorte. */}
          {!pathname.startsWith("/analytics/costs/prices") && (
            <>
              <AnalyticsFilters
                showModel={
                  pathname.startsWith("/analytics/usage") ||
                  pathname.startsWith("/analytics/costs")
                }
              />
              <p className="flex items-start gap-2 rounded-xl border border-primary/15 bg-primary/[0.05] px-3 py-2 text-[12px] text-muted-foreground">
                <Info className="mt-px size-3.5 shrink-0 text-primary" />
                {SCOPE_NOTE}
              </p>
            </>
          )}
          <Outlet />
        </div>
      </ScrollArea>
    </>
  );
}
