import { Donut } from "@/components/charts/Donut";
import { BorderBeam } from "@/components/ui/border-beam";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { providersOf, type AppStatus, type Dashboard } from "@/lib/api";
import { providerNote } from "@/features/settings/ProviderBadge";
import { cn } from "@/lib/utils";
import { agentKeys } from "./ActivityCard";

// Estado de detecção de cada agente na legenda.
const dot: Record<string, { cls: string; label: string }> = {
  ok: { cls: "bg-positive shadow-[0_0_8px_rgba(94,234,212,0.7)]", label: "Detectado" },
  pending: { cls: "bg-primary animate-pulse", label: "Verificando" },
  not_found: { cls: "bg-faint", label: "Não encontrado" },
  disabled: { cls: "bg-faint", label: "Desabilitado" },
  unsupported: { cls: "bg-amber-400", label: "Formato não suportado" },
  error: { cls: "bg-negative shadow-[0_0_8px_rgba(251,113,133,0.7)]", label: "Erro" },
};

// "Por agente": rosca das sessões e, na legenda, a situação de cada agente
// (o antigo card de Providers).
export function AgentsCard({ d, status }: { d: Dashboard; status: AppStatus }) {
  const providers = providersOf(status);
  const total = (d.byProvider ?? []).reduce((n, p) => n + p.sessions, 0);
  const sessionsOf = (id: string) => (d.byProvider ?? []).find((p) => p.provider === id)?.sessions ?? 0;
  return (
    <section className="surface relative overflow-hidden rounded-[18px] px-5 py-4" aria-label="Por agente">
      {status.scan.running && <BorderBeam size={90} duration={5} />}
      <h3 className="text-[15px] font-semibold tracking-tight">Por agente</h3>
      <p className="mt-0.5 text-[11.5px] text-faint">Sessões no período e situação de cada agente</p>
      <div className="mt-4 flex items-center gap-5">
        <Donut
          parts={agentKeys.map((k) => ({ value: sessionsOf(k.key), color: k.color }))}
          center={
            <>
              <span className="text-2xl font-semibold tabular-nums">{total.toLocaleString("pt-BR")}</span>
              <span className="text-[10.5px] text-faint">sessões</span>
            </>
          }
        />
        <ul className="flex min-w-0 flex-1 flex-col gap-2.5">
          {agentKeys.map((k) => {
            const p = providers.find((x) => x.id === k.key);
            const st = dot[p?.status ?? "pending"] ?? dot.pending;
            const note = p ? providerNote(p) : "";
            const n = sessionsOf(k.key);
            return (
              <li key={k.key}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div className="flex items-center gap-2 text-[13px]">
                      <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: k.color }} />
                      <span className="truncate text-muted-foreground">{k.label}</span>
                      <span className={cn("size-1.5 shrink-0 rounded-full", st.cls)} aria-label={st.label} />
                      <span className="ml-auto font-medium tabular-nums">{n.toLocaleString("pt-BR")}</span>
                      <span className="w-9 text-right text-[11px] text-faint tabular-nums">{total ? Math.round((n / total) * 100) : 0}%</span>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent side="left">
                    {st.label}
                    {p?.hidden ? ` · ${p.hidden} ocultas (subagentes e automáticas)` : ""}
                    {note ? ` · ${note}` : ""}
                  </TooltipContent>
                </Tooltip>
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
