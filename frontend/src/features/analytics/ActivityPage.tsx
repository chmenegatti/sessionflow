import { useNavigate } from "react-router";
import {
  CalendarDays,
  MessagesSquare,
  Repeat,
  TerminalSquare,
} from "lucide-react";
import { AreaChart } from "@/components/charts/AreaChart";
import { BarList } from "@/components/charts/BarList";
import { Heatmap } from "@/components/charts/Heatmap";
import { AnalyticsService, list, type ActivityView } from "@/lib/api";
import { projectPath } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { providerName } from "@/features/sessions/ProviderTag";
import { agentKeys } from "@/features/dashboard/ActivityCard";
import { KpiCard } from "@/features/dashboard/KpiCard";
import { AnalyticsCard } from "./AnalyticsCard";
import { tick, tipDate } from "./format";
import { useAnalyticsData } from "./useAnalyticsData";
import { RefreshError } from "./OverviewPage";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

const fmt = (n: number) => n.toLocaleString("pt-BR");

// Atividade: mesmas regras e consultas do Dashboard (só sessões principais e
// não automáticas), com filtro de projeto e períodos do Analytics.
export function ActivityPage() {
  const { withoutModel } = useAnalyticsQuery();
  const r = useAnalyticsData(withoutModel, (q) => AnalyticsService.Activity(q));
  const v = r.data;
  if (r.error && !v)
    return (
      <AnalyticsCard title="Atividade" error={r.error} onRetry={r.reload} />
    );
  const empty = !!v && v.messages.value === 0;
  return (
    <div
      className={cn(
        "flex flex-col gap-4 transition-opacity duration-200",
        r.stale && "opacity-50",
      )}
    >
      {r.error && v && <RefreshError error={r.error} />}
      <Stats v={v} />
      <SessionsCard v={v} empty={empty} />
      <div className="grid gap-4 lg:grid-cols-2">
        <AnalyticsCard
          title="Sessões por agente"
          hint="Sessões com mensagens no período"
          loading={!v}
          empty={empty}
          emptyText="Sem atividade no período."
        >
          <BarList
            color="accent"
            items={list(v?.byProvider).map((p) => ({
              label: providerName(p.provider),
              value: p.sessions,
            }))}
          />
        </AnalyticsCard>
        <ProjectsCard v={v} empty={empty} />
      </div>
      <AnalyticsCard
        title="Dias e horários"
        hint="Mensagens por dia da semana e hora, no horário local"
        loading={!v}
        empty={empty}
        emptyText="Sem atividade no período."
      >
        {v && <Heatmap values={v.heatmap as number[][]} unit="mensagens" />}
      </AnalyticsCard>
    </div>
  );
}

function Stats({ v }: { v: ActivityView | null }) {
  if (!v) {
    return (
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="surface h-36 animate-pulse rounded-[18px]" />
        ))}
      </div>
    );
  }
  const perDay = v.activeDays ? v.sessions.value / v.activeDays : 0;
  return (
    <section
      aria-label="Indicadores de atividade"
      className="grid grid-cols-2 gap-4 xl:grid-cols-4"
    >
      <KpiCard
        icon={MessagesSquare}
        label="Sessões"
        value={v.sessions.value}
        metric={v.sessions}
        meta="com atividade no período"
        series={list(v.sessionsSeries).map((p) => p.total)}
        color="var(--accent2)"
      />
      <KpiCard
        icon={TerminalSquare}
        label="Mensagens"
        value={v.messages.value}
        metric={v.messages}
        meta="suas e dos agentes"
        series={list(v.messagesSeries).map((p) => p.total)}
        color="var(--primary)"
      />
      <KpiCard
        icon={CalendarDays}
        label="Dias com atividade"
        value={v.activeDays}
        meta={`de ${fmt(v.days)} ${v.days === 1 ? "dia" : "dias"} no período`}
        series={[]}
        color="var(--positive)"
      />
      <KpiCard
        icon={Repeat}
        label="Sessões por dia ativo"
        value={Math.round(perDay * 10) / 10}
        decimals={1}
        meta="média nos dias com atividade"
        series={[]}
        color="#fbbf24"
      />
    </section>
  );
}

function SessionsCard({
  v,
  empty,
}: {
  v: ActivityView | null;
  empty: boolean;
}) {
  const navigate = useNavigate();
  const series = list(v?.sessionsSeries);
  const bucket = v?.window.bucket ?? "day";
  const per =
    { hour: "hora", day: "dia", week: "semana", month: "mês" }[bucket] ?? "dia";
  const prev = list(v?.prevSessions);
  return (
    <AnalyticsCard
      title={`Sessões por ${per}`}
      hint={
        prev.length ? "Por agente; tracejado = período anterior" : "Por agente"
      }
      loading={!v}
      empty={empty}
      emptyText="Sem atividade no período."
    >
      <AreaChart
        points={series.map((p) => ({
          start: p.start,
          byKey: (p.byProvider ?? {}) as Record<string, number>,
        }))}
        keys={agentKeys.filter((k) =>
          series.some(
            (p) => (p.byProvider as Record<string, number> | null)?.[k.key],
          ),
        )}
        previous={prev.length === series.length ? prev : undefined}
        formatTick={(ms) => tick(bucket, ms)}
        formatTooltip={(ms) => tipDate(bucket, ms)}
        unit="sessões"
        onSelect={() => navigate("/sessions")}
      />
    </AnalyticsCard>
  );
}

function ProjectsCard({
  v,
  empty,
}: {
  v: ActivityView | null;
  empty: boolean;
}) {
  const navigate = useNavigate();
  return (
    <AnalyticsCard
      title="Sessões por projeto"
      hint="Sessões com mensagens no período"
      loading={!v}
      empty={empty}
      emptyText="Sem atividade no período."
    >
      <BarList
        color="accent2"
        items={list(v?.byProject).map((p) => ({
          label: p.name,
          value: p.count,
          title: p.path,
          onClick: () => navigate(projectPath(p.path)),
        }))}
      />
    </AnalyticsCard>
  );
}
