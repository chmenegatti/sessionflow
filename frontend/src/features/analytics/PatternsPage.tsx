import { useState } from "react";
import { useNavigate } from "react-router";
import {
  CalendarDays,
  Download,
  Flame,
  Info,
  PlayCircle,
  RotateCcw,
} from "lucide-react";
import { AreaChart } from "@/components/charts/AreaChart";
import { CalendarGrid } from "@/components/charts/CalendarGrid";
import { Heatmap } from "@/components/charts/Heatmap";
import { Histogram } from "@/components/charts/Histogram";
import { Button } from "@/components/ui/button";
import {
  PatternsService,
  list,
  type AnalyticsQuery,
  type Distribution,
  type PatternsView,
} from "@/lib/api";
import { projectPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { KpiCard } from "@/features/dashboard/KpiCard";
import { AnalyticsCard } from "./AnalyticsCard";
import { tick, tipDate } from "./format";
import { RefreshError } from "./OverviewPage";
import { useAnalyticsData } from "./useAnalyticsData";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

// Texto fixo: indicadores de atividade, não de produtividade.
export const PATTERNS_NOTE =
  "Métricas de atividade observada nos históricos. Não medem produtividade, qualidade, esforço ou valor entregue.";

const fmt = (n: number) => n.toLocaleString("pt-BR");

export function duration(ms: number) {
  const m = Math.round(ms / 60000);
  if (m < 60) return `${m} min`;
  if (m < 48 * 60)
    return `${Math.floor(m / 60)} h ${m % 60 ? `${m % 60} min` : ""}`.trim();
  return `${Math.round(m / 1440)} dias`;
}

// Padrões: como os agentes são usados ao longo do tempo (sessões principais
// e não automáticas, regra do Dashboard).
export function PatternsPage() {
  const { withoutModel } = useAnalyticsQuery();
  const r = useAnalyticsData(withoutModel, (q) => PatternsService.Patterns(q));
  const v = r.data;
  const empty = !!v && v.frequency.activeDays === 0;
  return (
    <div
      className={cn(
        "flex flex-col gap-4 transition-opacity duration-200",
        r.stale && "opacity-50",
      )}
    >
      <div className="flex flex-wrap items-center gap-3">
        <p className="flex min-w-0 flex-1 items-start gap-2 text-[12px] text-muted-foreground">
          <Info className="mt-px size-3.5 shrink-0 text-accent2" />
          {PATTERNS_NOTE}
        </p>
        <ExportButton query={withoutModel} />
      </div>
      {r.error && v && <RefreshError error={r.error} />}
      {r.error && !v ? (
        <AnalyticsCard title="Padrões" error={r.error} onRetry={r.reload} />
      ) : (
        <>
          <Stats v={v} />
          <StartedCard v={v} empty={empty} />
          <AnalyticsCard
            title="Calendário"
            hint="Sessões por dia (até o último ano do período)"
            loading={!v}
            empty={empty}
            emptyText="Sem atividade no período."
          >
            {v && <CalendarGrid days={list(v.calendar)} />}
          </AnalyticsCard>
          <AnalyticsCard
            title="Dias e horários"
            hint="Mensagens por dia da semana e hora, no horário local"
            loading={!v}
            empty={empty}
            emptyText="Sem atividade no período."
          >
            {v && <Heatmap values={v.heatmap as number[][]} unit="mensagens" />}
          </AnalyticsCard>
          <div className="grid gap-4 lg:grid-cols-3">
            <DistCard
              title="Duração observada"
              hint="Tempo entre a primeira e a última mensagem, não tempo de trabalho"
              d={v?.duration}
              empty={empty}
              format={duration}
              unit="sessões"
            />
            <DistCard
              title="Mensagens por sessão"
              hint="Suas e dos agentes, no período"
              d={v?.messagesPerSession}
              empty={empty}
              format={fmt}
              unit="sessões"
            />
            <DistCard
              title="Intervalo entre sessões"
              hint="Entre o início de uma sessão e o da seguinte"
              d={v?.gaps}
              empty={!v || v.gaps.count === 0}
              format={duration}
              unit="intervalos"
            />
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <ProjectsCard v={v} empty={empty} />
            <ProvidersCard v={v} empty={empty} />
          </div>
        </>
      )}
    </div>
  );
}

function ExportButton({ query }: { query: AnalyticsQuery }) {
  const notify = useAppStore((s) => s.notify);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    try {
      const path = await PatternsService.ExportCSV(query);
      if (path) notify(`CSV salvo em ${path}`);
    } catch (e) {
      notify(`Não foi possível exportar: ${String(e)}`, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Button variant="outline" size="sm" disabled={busy} onClick={run}>
      <Download /> Exportar CSV
    </Button>
  );
}

function Stats({ v }: { v: PatternsView | null }) {
  if (!v) {
    return (
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="surface h-36 animate-pulse rounded-[18px]" />
        ))}
      </div>
    );
  }
  const f = v.frequency;
  return (
    <section
      aria-label="Indicadores de atividade"
      className="grid grid-cols-2 gap-4 xl:grid-cols-4"
    >
      <KpiCard
        icon={PlayCircle}
        label="Sessões iniciadas"
        value={v.started.value}
        metric={v.started}
        meta="criadas no período"
        series={list(v.startedSeries).map((p) => p.total)}
        color="var(--accent2)"
      />
      <KpiCard
        icon={RotateCcw}
        label="Sessões retomadas"
        value={v.resumed.value}
        metric={v.resumed}
        meta="voltaram após pausa de 30 min ou mais"
        series={[]}
        color="var(--primary)"
      />
      <KpiCard
        icon={CalendarDays}
        label="Dias com atividade"
        value={f.activeDays}
        meta={`de ${fmt(f.days)} ${f.days === 1 ? "dia" : "dias"} · ${fmt(f.activeWeeks)} ${f.activeWeeks === 1 ? "semana" : "semanas"} · ${String(f.sessionsPerActiveDay).replace(".", ",")} sessões por dia ativo`}
        series={[]}
        color="var(--positive)"
      />
      <KpiCard
        icon={Flame}
        label="Dias seguidos"
        value={f.currentStreak}
        meta={`sequência atual · maior: ${fmt(f.longestStreak)}`}
        series={[]}
        color="#fbbf24"
      />
    </section>
  );
}

function StartedCard({ v, empty }: { v: PatternsView | null; empty: boolean }) {
  const series = list(v?.startedSeries);
  const prev = list(v?.prevStarted);
  const bucket = v?.window.bucket ?? "day";
  const per =
    { hour: "hora", day: "dia", week: "semana", month: "mês" }[bucket] ?? "dia";
  return (
    <AnalyticsCard
      title={`Sessões iniciadas por ${per}`}
      hint={
        prev.length === series.length && prev.length
          ? "Tracejado = período anterior"
          : undefined
      }
      loading={!v}
      empty={empty}
      emptyText="Sem atividade no período."
    >
      <AreaChart
        points={series.map((p) => ({ start: p.start, byKey: { s: p.total } }))}
        keys={[{ key: "s", label: "Sessões", color: "var(--accent2)" }]}
        previous={prev.length === series.length ? prev : undefined}
        formatTick={(ms) => tick(bucket, ms)}
        formatTooltip={(ms) => tipDate(bucket, ms)}
        unit="sessões"
      />
    </AnalyticsCard>
  );
}

function DistCard({
  title,
  hint,
  d,
  empty,
  format,
  unit,
}: {
  title: string;
  hint: string;
  d?: Distribution;
  empty: boolean;
  format: (n: number) => string;
  unit: string;
}) {
  return (
    <AnalyticsCard
      title={title}
      hint={hint}
      loading={!d}
      empty={empty}
      emptyText="Sem atividade no período."
    >
      {d && (
        <div className="flex flex-col gap-3">
          <dl className="flex gap-6 text-[12px]">
            <div>
              <dt className="text-faint">Mediana</dt>
              <dd className="text-[18px] font-semibold tabular-nums">
                {format(d.median)}
              </dd>
            </div>
            <div>
              <dt className="text-faint">90% até</dt>
              <dd className="text-[18px] font-semibold tabular-nums">
                {format(d.p90)}
              </dd>
            </div>
          </dl>
          <Histogram buckets={list(d.buckets)} unit={unit} />
        </div>
      )}
    </AnalyticsCard>
  );
}

function ProjectsCard({
  v,
  empty,
}: {
  v: PatternsView | null;
  empty: boolean;
}) {
  const navigate = useNavigate();
  const projects = list(v?.topProjects);
  return (
    <AnalyticsCard
      title="Projetos mais ativos"
      hint={
        v
          ? `Por sessões no período · ${fmt(v.recurringProjects)} ${v.recurringProjects === 1 ? "projeto recorrente" : "projetos recorrentes"} (2 dias ou mais)`
          : undefined
      }
      loading={!v}
      empty={empty || projects.length === 0}
      emptyText="Sem atividade no período."
    >
      <table className="w-full border-collapse text-[12.5px]">
        <thead>
          <tr className="border-b text-left text-[11px] text-faint">
            <th className="py-2 pr-3 font-medium">Projeto</th>
            <th className="px-2 py-2 text-right font-medium">Sessões</th>
            <th className="px-2 py-2 text-right font-medium">Mensagens</th>
            <th className="px-2 py-2 text-right font-medium">Dias ativos</th>
            <th className="py-2 pl-2 text-right font-medium">Última</th>
          </tr>
        </thead>
        <tbody>
          {projects.map((p) => (
            <tr key={p.path} className="border-b last:border-0">
              <td className="max-w-48 py-2 pr-3">
                <button
                  type="button"
                  className="block max-w-full truncate text-left outline-none hover:text-primary focus-visible:text-primary"
                  title={p.path}
                  onClick={() => navigate(projectPath(p.path))}
                >
                  {p.name || p.path}
                </button>
              </td>
              <td className="px-2 py-2 text-right tabular-nums">
                {fmt(p.sessions)}
              </td>
              <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
                {fmt(p.messages)}
              </td>
              <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
                {fmt(p.activeDays)}
              </td>
              <td className="py-2 pl-2 text-right text-muted-foreground">
                {p.daysSinceLast < 0
                  ? "—"
                  : p.daysSinceLast === 0
                    ? "hoje"
                    : `há ${fmt(p.daysSinceLast)} ${p.daysSinceLast === 1 ? "dia" : "dias"}`}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </AnalyticsCard>
  );
}

function ProvidersCard({
  v,
  empty,
}: {
  v: PatternsView | null;
  empty: boolean;
}) {
  const multi = list(v?.multiProvider);
  return (
    <AnalyticsCard
      title="Comparação entre agentes"
      hint="Sessões, mensagens, dias ativos e duração mediana no período"
      loading={!v}
      empty={empty}
      emptyText="Sem atividade no período."
    >
      <div className="flex flex-col gap-4">
        <table className="w-full border-collapse text-[12.5px]">
          <thead>
            <tr className="border-b text-left text-[11px] text-faint">
              <th className="py-2 pr-3 font-medium">Agente</th>
              <th className="px-2 py-2 text-right font-medium">Sessões</th>
              <th className="px-2 py-2 text-right font-medium">Mensagens</th>
              <th className="px-2 py-2 text-right font-medium">Dias ativos</th>
              <th className="py-2 pl-2 text-right font-medium">
                Duração mediana
              </th>
            </tr>
          </thead>
          <tbody>
            {list(v?.byProvider).map((p) => (
              <tr key={p.provider} className="border-b last:border-0">
                <td className="py-2 pr-3">
                  <ProviderTag provider={p.provider} />
                </td>
                <td className="px-2 py-2 text-right tabular-nums">
                  {fmt(p.sessions)}
                </td>
                <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
                  {fmt(p.messages)}
                </td>
                <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
                  {fmt(p.activeDays)}
                </td>
                <td className="py-2 pl-2 text-right text-muted-foreground tabular-nums">
                  {duration(p.medianDuration)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <div>
          <h4 className="mb-2 text-[12px] font-medium text-muted-foreground">
            Projetos com mais de um agente
          </h4>
          {multi.length === 0 ? (
            <p className="text-[12px] text-faint">
              Nenhum projeto usou mais de um agente no período.
            </p>
          ) : (
            <ul className="flex flex-col gap-2 text-[12.5px]">
              {multi.map((m) => (
                <li
                  key={m.path}
                  className="flex flex-wrap items-center gap-2"
                  title={m.path}
                >
                  <span className="min-w-0 flex-1 truncate">
                    {m.name || m.path}
                  </span>
                  {list(m.providers).map((p) => (
                    <span
                      key={p.provider}
                      className="inline-flex items-center gap-1 text-muted-foreground tabular-nums"
                    >
                      <ProviderTag provider={p.provider} /> {fmt(p.sessions)}
                    </span>
                  ))}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </AnalyticsCard>
  );
}
