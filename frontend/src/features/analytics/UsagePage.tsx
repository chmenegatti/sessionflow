import { BarList } from "@/components/charts/BarList";
import { AnalyticsService, list, type UsageView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { providerName } from "@/features/sessions/ProviderTag";
import { AnalyticsCard } from "./AnalyticsCard";
import { GroupTable } from "./GroupTable";
import { SessionsUsageTable } from "./SessionsUsageTable";
import { TokenStat } from "./TokenStat";
import {
  exactTokens,
  fieldLabels,
  formatTokens,
  groupTotal,
  originLabels,
  pct,
} from "./format";
import { RefreshError, derivedNote, reasonOf } from "./OverviewPage";
import { useAnalyticsData } from "./useAnalyticsData";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

export function UsagePage() {
  const { query } = useAnalyticsQuery();
  const r = useAnalyticsData(query, (q) => AnalyticsService.Usage(q));
  const v = r.data;
  if (r.error && !v)
    return <AnalyticsCard title="Consumo" error={r.error} onRetry={r.reload} />;
  const empty = !!v && v.totals.records === 0;
  return (
    <div
      className={cn(
        "flex flex-col gap-4 transition-opacity duration-200",
        r.stale && "opacity-50",
      )}
    >
      {r.error && v && <RefreshError error={r.error} />}
      {v ? (
        <section
          aria-label="Indicadores de consumo"
          className="grid grid-cols-2 gap-4 lg:grid-cols-3 xl:grid-cols-6"
        >
          <TokenStat
            big
            label="Total"
            field={v.totals.total}
            records={v.totals.records}
            derived={derivedNote(v.totals.records, v.totals.derived)}
          />
          <TokenStat
            label={fieldLabels.input}
            field={v.totals.input}
            records={v.totals.records}
          />
          <TokenStat
            label={fieldLabels.output}
            field={v.totals.output}
            records={v.totals.records}
          />
          <TokenStat
            label={fieldLabels.cache_read}
            field={v.totals.cacheRead}
            records={v.totals.records}
          />
          <TokenStat
            label={fieldLabels.cache_creation}
            field={v.totals.cacheCreation}
            records={v.totals.records}
            reason={reasonOf(v, "cache_creation")}
          />
          <TokenStat
            label={fieldLabels.reasoning}
            field={v.totals.reasoning}
            records={v.totals.records}
            reason={reasonOf(v, "reasoning")}
          />
        </section>
      ) : (
        <div className="surface h-28 animate-pulse rounded-[18px]" />
      )}
      <div className="grid gap-4 lg:grid-cols-2">
        <AnalyticsCard
          title="Por agente"
          hint="Tokens por campo; — = o agente não informa"
          loading={!v}
          empty={empty}
          emptyText="Sem consumo registrado no período."
        >
          {v && (
            <GroupTable
              groups={list(v.byProvider)}
              label="Agente"
              name={(g) => providerName(g.key)}
            />
          )}
        </AnalyticsCard>
        <div className="flex flex-col gap-4">
          <CacheCard v={v} empty={empty} />
          <AnalyticsCard
            title="Por origem"
            hint="Agente principal, subagentes e sessões automáticas"
            loading={!v}
            empty={empty}
            emptyText="Sem consumo registrado no período."
          >
            <BarList
              color="accent2"
              format={formatTokens}
              items={list(v?.byOrigin).map((g) => {
                const t = groupTotal(g.tokens);
                return {
                  label: originLabels[g.key] ?? g.key,
                  value: t.value,
                  display: t.display,
                  title: t.title,
                };
              })}
            />
          </AnalyticsCard>
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <AnalyticsCard
          title="Por modelo"
          loading={!v}
          empty={empty}
          emptyText="Sem consumo registrado no período."
        >
          {v && (
            <GroupTable
              groups={list(v.byModel)}
              label="Modelo"
              name={(g) => g.label || "(sem modelo)"}
            />
          )}
        </AnalyticsCard>
        <AnalyticsCard
          title="Por projeto"
          loading={!v}
          empty={empty}
          emptyText="Sem consumo registrado no período."
        >
          {v && (
            <GroupTable
              groups={list(v.byProject)}
              label="Projeto"
              name={(g) => g.label || "(sem projeto)"}
            />
          )}
        </AnalyticsCard>
      </div>
      <SessionsUsageTable query={query} />
    </div>
  );
}

// Cache: lido e criado e a parte da entrada total servida pelo cache (só
// quando os três campos vieram completos).
function CacheCard({ v, empty }: { v: UsageView | null; empty: boolean }) {
  const t = v?.totals;
  const complete =
    !!t &&
    t.records > 0 &&
    [t.input, t.cacheRead, t.cacheCreation].every((f) => f.count === t.records);
  const totalIn = t ? t.input.sum + t.cacheRead.sum + t.cacheCreation.sum : 0;
  return (
    <AnalyticsCard
      title="Cache"
      hint="Entrada total = entrada + cache lido + cache criado"
      loading={!v}
      empty={empty}
      emptyText="Sem consumo registrado no período."
    >
      {t && (
        <dl className="grid grid-cols-3 gap-3 text-[12px]">
          <div>
            <dt className="text-faint">Cache lido</dt>
            <dd
              className="text-[18px] font-semibold tabular-nums"
              title={exactTokens(t.cacheRead.sum)}
            >
              {t.cacheRead.count ? formatTokens(t.cacheRead.sum) : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-faint">Cache criado</dt>
            <dd
              className="text-[18px] font-semibold tabular-nums"
              title={
                reasonOf(v!, "cache_creation") ??
                exactTokens(t.cacheCreation.sum)
              }
            >
              {t.cacheCreation.count ? formatTokens(t.cacheCreation.sum) : "—"}
            </dd>
          </div>
          <div>
            <dt className="text-faint">Lido / entrada total</dt>
            <dd
              className="text-[18px] font-semibold tabular-nums"
              title={
                complete
                  ? undefined
                  : "Indisponível: algum agente do recorte não informa todos os campos de entrada"
              }
            >
              {complete && totalIn ? pct(t.cacheRead.sum / totalIn) : "—"}
            </dd>
          </div>
        </dl>
      )}
    </AnalyticsCard>
  );
}
