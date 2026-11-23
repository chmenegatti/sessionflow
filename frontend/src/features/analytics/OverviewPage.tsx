import { useNavigate } from "react-router";
import { AreaChart } from "@/components/charts/AreaChart";
import { BarList } from "@/components/charts/BarList";
import { Donut } from "@/components/charts/Donut";
import { StackedBar } from "@/components/charts/StackedBar";
import { AnalyticsService, list, type Overview } from "@/lib/api";
import { projectPath } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { providerColor, providerName } from "@/features/sessions/ProviderTag";
import { agentKeys } from "@/features/dashboard/ActivityCard";
import { AnalyticsCard } from "./AnalyticsCard";
import { TokenStat } from "./TokenStat";
import {
  centerTotal,
  coverageNote,
  fieldLabels,
  formatTokens,
  groupTotal,
  pct,
  tick,
  tipDate,
} from "./format";
import { useAnalyticsData } from "./useAnalyticsData";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

export const reasonOf = (
  o: {
    unavailable: { provider: string; field: string; reason: string }[] | null;
  },
  field: string,
) =>
  list(o.unavailable)
    .filter((u) => u.field === field)
    .map((u) => u.reason)
    .join(" ") || undefined;

export const derivedNote = (records: number, derived: number) =>
  derived
    ? `${derived} de ${records} totais calculados (Claude Code: entrada + saída + cache; OpenCode sem total informado).`
    : undefined;

export function OverviewPage() {
  const { withoutModel } = useAnalyticsQuery();
  const r = useAnalyticsData(withoutModel, (q) => AnalyticsService.Overview(q));
  const o = r.data;
  return (
    <div
      className={cn(
        "flex flex-col gap-4 transition-opacity duration-200",
        r.stale && "opacity-50",
      )}
    >
      {r.error && o && <RefreshError error={r.error} />}
      {r.error && !o ? (
        <AnalyticsCard title="Consumo" error={r.error} onRetry={r.reload} />
      ) : (
        <>
          <Stats o={o} />
          <div className="grid gap-4 lg:grid-cols-3">
            <SeriesCard o={o} className="lg:col-span-2" />
            <ProvidersCard o={o} />
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <CompositionCard o={o} />
            <ProjectsCard o={o} />
          </div>
        </>
      )}
    </div>
  );
}

function Stats({ o }: { o: Overview | null }) {
  if (!o) {
    return (
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-5">
        {[0, 1, 2, 3, 4].map((i) => (
          <div key={i} className="surface h-28 animate-pulse rounded-[18px]" />
        ))}
      </div>
    );
  }
  const t = o.totals;
  const p = o.window.hasPrev ? o.prev : undefined;
  return (
    <section
      aria-label="Indicadores de consumo"
      className="grid grid-cols-2 gap-4 xl:grid-cols-5"
    >
      <TokenStat
        big
        label="Total de tokens"
        field={t.total}
        records={t.records}
        prev={p?.total}
        prevRecords={p?.records}
        derived={derivedNote(t.records, t.derived)}
      />
      <TokenStat
        label={fieldLabels.input}
        field={t.input}
        records={t.records}
        prev={p?.input}
        prevRecords={p?.records}
      />
      <TokenStat
        label={fieldLabels.output}
        field={t.output}
        records={t.records}
        prev={p?.output}
        prevRecords={p?.records}
      />
      <TokenStat
        label={fieldLabels.cache_read}
        field={t.cacheRead}
        records={t.records}
        prev={p?.cacheRead}
        prevRecords={p?.records}
      />
      <TokenStat
        label={fieldLabels.cache_creation}
        field={t.cacheCreation}
        records={t.records}
        prev={p?.cacheCreation}
        prevRecords={p?.records}
        reason={reasonOf(o, "cache_creation")}
      />
    </section>
  );
}

function SeriesCard({
  o,
  className,
}: {
  o: Overview | null;
  className?: string;
}) {
  const series = list(o?.series);
  const empty = !!o && series.every((p) => p.records === 0);
  const gaps = series.filter((p) => p.records > p.covered).length;
  const bucket = o?.window.bucket ?? "day";
  const per =
    { hour: "hora", day: "dia", week: "semana", month: "mês" }[bucket] ?? "dia";
  return (
    <AnalyticsCard
      title={`Tokens por ${per}`}
      hint="Total de tokens por agente (registros que informam o total)"
      loading={!o}
      empty={empty}
      emptyText="Sem consumo registrado no período."
      className={className}
    >
      <AreaChart
        points={series.map((p) => ({
          start: p.start,
          byKey: (p.byProvider ?? {}) as Record<string, number>,
          note: coverageNote(p.records, p.covered),
        }))}
        keys={agentKeys.filter((k) =>
          series.some(
            (p) => (p.byProvider as Record<string, number> | null)?.[k.key],
          ),
        )}
        formatTick={(ms) => tick(bucket, ms)}
        formatTooltip={(ms) => tipDate(bucket, ms)}
        formatValue={formatTokens}
        unit="tokens"
        padLeft={56}
      />
      {gaps > 0 && (
        <p className="mt-2 text-[11.5px] text-amber-300">
          Em {gaps} {gaps === 1 ? "intervalo" : "intervalos"} há registros sem
          total informado: o gráfico mostra só os que informam.
        </p>
      )}
    </AnalyticsCard>
  );
}

function ProvidersCard({ o }: { o: Overview | null }) {
  const groups = list(o?.byProvider);
  const center = o ? centerTotal(o.totals.total, o.totals.records) : null;
  return (
    <AnalyticsCard
      title="Por agente"
      hint="Total de tokens no período"
      loading={!o}
      empty={!!o && groups.length === 0}
      emptyText="Sem consumo registrado no período."
    >
      <div className="flex items-center gap-5">
        <Donut
          parts={groups.map((g) => ({
            value: g.tokens.total.sum,
            color: providerColor(g.key),
          }))}
          center={
            <>
              <span
                className={
                  center?.text === "indisponível"
                    ? "text-sm text-faint"
                    : "text-xl font-semibold tabular-nums"
                }
              >
                {center?.text}
              </span>
              <span className="text-[10.5px] text-faint">tokens</span>
              {center?.note && (
                <span className="text-[10.5px] text-amber-300">
                  {center.note}
                </span>
              )}
            </>
          }
        />
        <ul className="flex min-w-0 flex-1 flex-col gap-2.5">
          {groups.map((g) => (
            <li
              key={g.key}
              className="flex items-center gap-2 text-[13px]"
              title={groupTotal(g.tokens).title}
            >
              <span
                className="size-2.5 shrink-0 rounded-[3px]"
                style={{ background: providerColor(g.key) }}
              />
              <span className="truncate text-muted-foreground">
                {providerName(g.key)}
              </span>
              {g.tokens.total.count === 0 ? (
                <span className="ml-auto text-[11.5px] text-faint">
                  indisponível
                </span>
              ) : (
                <span className="ml-auto font-medium tabular-nums">
                  {formatTokens(g.tokens.total.sum)}
                </span>
              )}
              {g.tokens.total.count > 0 &&
                g.tokens.total.count < g.tokens.records && (
                  <span className="text-[10.5px] text-amber-300">
                    parcial · {pct(g.tokens.total.count / g.tokens.records)}
                  </span>
                )}
            </li>
          ))}
        </ul>
      </div>
    </AnalyticsCard>
  );
}

const compColors = {
  input: "var(--codex)",
  output: "var(--accent2)",
  cacheRead: "var(--positive)",
  cacheCreation: "#fbbf24",
};

function CompositionCard({ o }: { o: Overview | null }) {
  const t = o?.totals;
  const parts = t
    ? (
        [
          ["input", fieldLabels.input],
          ["output", fieldLabels.output],
          ["cacheRead", fieldLabels.cache_read],
          ["cacheCreation", fieldLabels.cache_creation],
        ] as const
      )
        .filter(([k]) => t[k].count > 0)
        .map(([k, label]) => ({ label, value: t[k].sum, color: compColors[k] }))
    : [];
  const missing = o ? list(o.unavailable) : [];
  return (
    <AnalyticsCard
      title="Composição"
      hint="Entrada, saída e cache, somados de cada campo informado"
      loading={!o}
      empty={!!t && t.records === 0}
      emptyText="Sem consumo registrado no período."
    >
      <StackedBar parts={parts} format={formatTokens} />
      {missing.length > 0 && (
        <ul className="mt-3 space-y-1 text-[11.5px] text-faint">
          {missing.map((m) => (
            <li key={m.provider + m.field}>
              <span className="text-muted-foreground">
                {fieldLabels[m.field] ?? m.field} indisponível
              </span>{" "}
              · {m.reason}
            </li>
          ))}
        </ul>
      )}
    </AnalyticsCard>
  );
}

function ProjectsCard({ o }: { o: Overview | null }) {
  const navigate = useNavigate();
  const groups = list(o?.topProjects);
  return (
    <AnalyticsCard
      title="Projetos com maior consumo"
      hint="Total de tokens no período"
      loading={!o}
      empty={!!o && groups.length === 0}
      emptyText="Sem consumo registrado no período."
    >
      <BarList
        color="accent"
        format={formatTokens}
        items={groups.map((g) => {
          const t = groupTotal(g.tokens);
          return {
            label: g.label || "(sem projeto)",
            value: t.value,
            display: t.display,
            title: g.key ? `${g.key} · ${t.title}` : t.title,
            onClick: g.key ? () => navigate(projectPath(g.key)) : undefined,
          };
        })}
      />
    </AnalyticsCard>
  );
}

// Falha numa recarga de fundo: os números ficam (são do mesmo recorte), com aviso.
export function RefreshError({ error }: { error: string }) {
  return (
    <p className="rounded-lg border border-negative/30 bg-negative/[0.06] px-3 py-1.5 text-[12px] text-negative">
      Falha ao atualizar: {error}
    </p>
  );
}
