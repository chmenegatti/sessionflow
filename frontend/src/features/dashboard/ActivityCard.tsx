import { useNavigate } from "react-router";
import { AreaChart, type AreaKey } from "@/components/charts/AreaChart";
import type { Dashboard } from "@/lib/api";
import { providerColor, providerName } from "@/features/sessions/ProviderTag";

export type ChartMetric = "messages" | "sessions";

// Ordem de empilhamento: Claude embaixo.
export const agentKeys: AreaKey[] = ["claude", "opencode", "codex"].map((key) => ({
  key,
  color: providerColor(key),
  label: providerName(key),
}));

function tick(bucket: string, ms: number) {
  const d = new Date(ms);
  if (bucket === "month") return d.toLocaleDateString("pt-BR", { month: "short", year: "2-digit" }).replace(".", "");
  return d.toLocaleDateString("pt-BR", { day: "numeric", month: "short" }).replace(".", "");
}

function tip(bucket: string, ms: number) {
  const d = new Date(ms);
  if (bucket === "month") return d.toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
  const day = d.toLocaleDateString("pt-BR", { day: "numeric", month: "short" }).replace(".", "");
  if (bucket === "week") return `semana de ${day}`;
  return `${d.toLocaleDateString("pt-BR", { weekday: "short" }).replace(".", "")}, ${day}`;
}

// Gráfico grande: atividade por intervalo, empilhada por agente.
export function ActivityCard({ d, metric, providers }: { d: Dashboard; metric: ChartMetric; providers: string[] }) {
  const navigate = useNavigate();
  const series = metric === "messages" ? d.messagesSeries : d.sessionsSeries;
  const prev = (metric === "messages" ? d.prevMessages : d.prevSessions) ?? [];
  const keys = agentKeys.filter((k) => providers.includes(k.key));
  // Agente sem atividade não desenha: a linha zerada cobriria a de baixo.
  const drawn = keys.filter((k) => (series ?? []).some((p) => (p.byProvider as Record<string, number>)?.[k.key]));
  const unit = metric === "messages" ? "Mensagens" : "Sessões ativas";
  const per = d.bucket === "week" ? "semana" : d.bucket === "month" ? "mês" : "dia";
  return (
    <section className="surface rounded-[18px] px-5 pt-4 pb-3" aria-label="Atividade">
      <div className="flex items-start gap-4">
        <div>
          <h3 className="text-[15px] font-semibold tracking-tight">Atividade por {per}</h3>
          <p className="mt-0.5 text-[11.5px] text-faint">{unit} por agente. Clique para ver as sessões.</p>
        </div>
        <ul className="ml-auto flex flex-wrap justify-end gap-x-4 gap-y-1 text-[11.5px] text-muted-foreground">
          {keys.map((k) => (
            <li key={k.key} className="flex items-center gap-1.5">
              <span className="h-[3px] w-2.5 rounded-full" style={{ background: k.color, boxShadow: `0 0 6px ${k.color}` }} />
              {k.label}
            </li>
          ))}
          {prev.length > 0 && (
            <li className="flex items-center gap-1.5">
              <span className="h-px w-2.5 bg-faint" />
              Período anterior
            </li>
          )}
        </ul>
      </div>
      <div className="mt-3">
        <AreaChart
          points={(series ?? []).map((p) => ({ start: p.start, byKey: (p.byProvider ?? {}) as Record<string, number> }))}
          keys={drawn.length ? drawn : keys}
          previous={prev.length ? prev : undefined}
          formatTick={(ms) => tick(d.bucket, ms)}
          formatTooltip={(ms) => tip(d.bucket, ms)}
          onSelect={() => navigate("/sessions")}
        />
      </div>
    </section>
  );
}
