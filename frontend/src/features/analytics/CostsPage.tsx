import { Fragment, useState } from "react";
import { useNavigate } from "react-router";
import { Calculator, Download, Info, Tags } from "lucide-react";
import { AreaChart } from "@/components/charts/AreaChart";
import { StackedBar, type BarPart } from "@/components/charts/StackedBar";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { useAsync } from "@/hooks/useAsync";
import {
  CostService,
  list,
  type AnalyticsQuery,
  type CostGroup,
  type CostSums,
  type CostView,
} from "@/lib/api";
import { decimalShares, formatMoney } from "@/lib/money";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { providerName } from "@/features/sessions/ProviderTag";
import { AnalyticsCard } from "./AnalyticsCard";
import {
  CostSessionsTable,
  CurrencySectionRow,
  UncalculableTable,
} from "./CostSessionsTable";
import {
  COST_NOTE,
  amount,
  costDelta,
  coverageText,
  currencySections,
} from "./costFormat";
import { tick, tipDate } from "./format";
import { RecalculatePanel } from "./RecalculatePanel";
import { RefreshError } from "./OverviewPage";
import { dataView } from "./useAnalyticsData";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

type BucketChoice = "" | "day" | "week" | "month";

const currencyColors = [
  "var(--primary)",
  "var(--accent2)",
  "#fbbf24",
  "var(--positive)",
];

// categoryParts: composição por categoria. A legenda (participação e
// valor) sai das strings decimais do backend; o número só desenha a barra.
export const categoryParts = (s: CostSums): BarPart[] => {
  const raw = [
    { label: "Entrada", v: String(s.input), color: "var(--primary)" },
    { label: "Saída", v: String(s.output), color: "var(--accent2)" },
    { label: "Cache lido", v: String(s.cacheRead), color: "var(--positive)" },
    { label: "Cache criado", v: String(s.cacheCreation), color: "#fbbf24" },
  ];
  const shares = decimalShares(raw.map((p) => p.v));
  return raw.map((p, i) => ({
    label: p.label,
    value: amount(p.v),
    color: p.color,
    share: shares[i],
    text: formatMoney(p.v, s.currency),
  }));
};

// Custos: estimativa com os preços cadastrados. Nunca é o valor cobrado.
export function CostsPage() {
  const { query, search } = useAnalyticsQuery();
  const navigate = useNavigate();
  const [bucket, setBucket] = useState<BucketChoice>("");
  const [recalc, setRecalc] = useState(false);
  const version = useAppStore((s) => s.dataVersion);
  const key = JSON.stringify({ query, bucket });
  const res = useAsync(
    () => CostService.Costs(query, bucket).then((data) => ({ key, data })),
    [key, version],
  );
  const r = { ...dataView(res, key), reload: res.reload };
  const v = r.data;
  const toPrices = () =>
    navigate({ pathname: "/analytics/costs/prices", search });
  return (
    <div
      className={cn(
        "flex flex-col gap-4 transition-opacity duration-200",
        r.stale && "opacity-50",
      )}
    >
      <div className="flex flex-wrap items-center gap-3">
        <span className="rounded-md border border-amber-400/40 bg-amber-400/10 px-2 py-0.5 text-[11.5px] font-semibold tracking-wide text-amber-300 uppercase">
          Estimativa
        </span>
        <p className="flex min-w-0 flex-1 items-start gap-2 text-[12px] text-muted-foreground">
          <Info className="mt-px size-3.5 shrink-0 text-amber-300" />
          {COST_NOTE}
        </p>
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={toPrices}>
            <Tags /> Preços
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!v?.hasPrices}
            onClick={() => setRecalc(true)}
          >
            <Calculator /> Recalcular
          </Button>
          <ExportButton query={query} />
        </div>
      </div>
      {recalc && (
        <RecalculatePanel query={query} onClose={() => setRecalc(false)} />
      )}
      {r.error && v && <RefreshError error={r.error} />}
      {r.error && !v ? (
        <AnalyticsCard title="Custos" error={r.error} onRetry={r.reload} />
      ) : v && !v.hasPrices ? (
        <AnalyticsCard title="Nenhum preço cadastrado">
          <div className="flex flex-col items-center gap-3 py-6 text-center text-[13px] text-muted-foreground">
            <p className="max-w-lg">
              O SessionFlow não consulta preços na internet. Cadastre o preço
              por token de cada modelo que você usa; as estimativas aparecem
              assim que houver preço vigente.
            </p>
            <Button size="sm" onClick={toPrices}>
              <Tags /> Cadastrar preços
            </Button>
          </div>
        </AnalyticsCard>
      ) : (
        <>
          <Totals v={v} />
          <SeriesCard v={v} bucket={bucket} onBucket={setBucket} />
          <div className="grid gap-4 lg:grid-cols-2">
            <AnalyticsCard
              title="Por agente"
              loading={!v}
              empty={!!v && !list(v.byProvider).length}
              emptyText="Sem consumo no período."
            >
              {v && (
                <CostGroups
                  groups={list(v.byProvider)}
                  name={(g) => providerName(g.key)}
                />
              )}
            </AnalyticsCard>
            <CompositionCard v={v} />
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <AnalyticsCard
              title="Por modelo"
              loading={!v}
              empty={!!v && !list(v.byModel).length}
              emptyText="Sem consumo no período."
            >
              {v && (
                <CostGroups
                  groups={list(v.byModel)}
                  name={(g) => g.label || "(sem modelo)"}
                />
              )}
            </AnalyticsCard>
            <AnalyticsCard
              title="Por projeto"
              loading={!v}
              empty={!!v && !list(v.byProject).length}
              emptyText="Sem consumo no período."
            >
              {v && (
                <CostGroups
                  groups={list(v.byProject)}
                  name={(g) => g.label || "(sem projeto)"}
                  title={(g) => g.key}
                />
              )}
            </AnalyticsCard>
          </div>
          <CostSessionsTable query={query} />
          <UncalculableTable query={query} />
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
      const path = await CostService.ExportCSV(query);
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

function Totals({ v }: { v: CostView | null }) {
  if (!v) return <div className="surface h-32 animate-pulse rounded-[18px]" />;
  const totals = list(v.totals);
  const q = v.quality;
  const cover = coverageText(q);
  return (
    <section
      aria-label="Custo estimado"
      className="grid gap-4 lg:grid-cols-[2fr_3fr]"
    >
      <div className="surface flex flex-col gap-2 rounded-[18px] px-5 py-4">
        <h3 className="text-[13px] text-muted-foreground">
          Custo estimado no período
        </h3>
        {totals.length === 0 ? (
          <p
            className="text-[26px] font-semibold text-faint"
            title="Nenhuma resposta do recorte tem preço vigente"
          >
            sem preço
          </p>
        ) : (
          totals.map((s) => {
            const d = v.window.hasPrev ? costDelta(s, list(v.prev)) : null;
            return (
              <div key={s.currency} className="flex items-baseline gap-3">
                <span className="text-[28px] leading-tight font-semibold tabular-nums">
                  {formatMoney(String(s.total), s.currency)}
                </span>
                {d && (
                  <span
                    className={cn(
                      "text-[12px] font-medium",
                      d.up ? "text-amber-300" : "text-positive",
                    )}
                  >
                    {d.text}
                  </span>
                )}
              </div>
            );
          })
        )}
        {totals.length > 1 && (
          <p className="text-[11.5px] text-faint">
            Moedas diferentes não são somadas nem convertidas.
          </p>
        )}
        {cover && <p className="text-[11.5px] text-amber-300">{cover}</p>}
      </div>
      <div className="surface rounded-[18px] px-5 py-4">
        <h3 className="mb-3 text-[13px] text-muted-foreground">
          Qualidade da estimativa
        </h3>
        <dl className="grid grid-cols-2 gap-3 text-[12px] sm:grid-cols-4">
          {[
            {
              label: "Completas",
              n: q.complete,
              hint: "Todos os componentes com preço e tokens",
              tone: "text-positive",
            },
            {
              label: "Parciais",
              n: q.partial,
              hint: "Alguma categoria sem preço ou sem tokens informados",
              tone: "text-amber-300",
            },
            {
              label: "Sem preço",
              n: q.noPrice + q.unestimated,
              hint: "Nenhum preço vigente para o agente, modelo e data",
              tone: "text-muted-foreground",
            },
            {
              label: "Sem dados",
              n: q.insufficient,
              hint: "Sem horário, sem modelo ou sem tokens",
              tone: "text-muted-foreground",
            },
          ].map((x) => (
            <div key={x.label} title={x.hint}>
              <dt className="text-faint">{x.label}</dt>
              <dd
                className={cn("text-[20px] font-semibold tabular-nums", x.tone)}
              >
                {x.n.toLocaleString("pt-BR")}
              </dd>
            </div>
          ))}
        </dl>
        <p className="mt-2 text-[11.5px] text-faint">
          Respostas do recorte: {q.records.toLocaleString("pt-BR")}
        </p>
      </div>
    </section>
  );
}

function SeriesCard({
  v,
  bucket,
  onBucket,
}: {
  v: CostView | null;
  bucket: BucketChoice;
  onBucket: (b: BucketChoice) => void;
}) {
  const series = list(v?.series);
  const currencies = list(v?.totals).map((s) => s.currency);
  const b = v?.window.bucket ?? "day";
  const single = currencies.length === 1 ? currencies[0] : null;
  const compact = single
    ? new Intl.NumberFormat("pt-BR", {
        style: "currency",
        currency: single,
        notation: "compact",
        maximumFractionDigits: 2,
      })
    : new Intl.NumberFormat("pt-BR", {
        notation: "compact",
        maximumFractionDigits: 2,
      });
  const gaps = series.filter((p) => p.records > p.covered).length;
  return (
    <AnalyticsCard
      title="Evolução do custo"
      hint={
        single
          ? `Custo estimado por intervalo (${single})`
          : "Custo estimado por intervalo e moeda"
      }
      loading={!v}
      empty={!!v && series.every((p) => p.records === 0)}
      emptyText="Sem consumo no período."
      actions={
        <Segmented
          size="sm"
          label="Intervalo"
          value={bucket}
          onChange={onBucket}
          options={[
            { value: "", label: "Auto" },
            { value: "day", label: "Dia" },
            { value: "week", label: "Semana" },
            { value: "month", label: "Mês" },
          ]}
        />
      }
    >
      <AreaChart
        points={series.map((p) => ({
          start: p.start,
          byKey: Object.fromEntries(
            Object.entries(p.byCurrency ?? {}).map(([c, n]) => [c, amount(n)]),
          ),
          note:
            p.records > p.covered
              ? {
                  text: p.covered
                    ? `custo em ${Math.floor((p.covered / p.records) * 100)}% das respostas`
                    : "sem custo calculável",
                  unavailable: p.covered === 0,
                }
              : undefined,
        }))}
        keys={currencies.map((c, i) => ({
          key: c,
          label: c,
          color: currencyColors[i % currencyColors.length],
        }))}
        formatTick={(ms) => tick(b, ms)}
        formatTooltip={(ms) => tipDate(b, ms)}
        formatValue={(n) => compact.format(n)}
        padLeft={64}
      />
      {gaps > 0 && (
        <p className="mt-2 text-[11.5px] text-amber-300">
          Em {gaps} {gaps === 1 ? "intervalo" : "intervalos"} há respostas sem
          custo calculável: o gráfico mostra só as que têm.
        </p>
      )}
    </AnalyticsCard>
  );
}

function CompositionCard({ v }: { v: CostView | null }) {
  const totals = list(v?.totals);
  return (
    <AnalyticsCard
      title="Por categoria de token"
      hint="Parte do custo de cada categoria"
      loading={!v}
      empty={!!v && !totals.length}
      emptyText="Sem custo calculado no período."
    >
      <div className="flex flex-col gap-5">
        {totals.map((s) => (
          <div key={s.currency} className="flex flex-col gap-2">
            {totals.length > 1 && (
              <span className="text-[11.5px] font-medium text-faint">
                {s.currency}
              </span>
            )}
            <StackedBar parts={categoryParts(s)} format={(n) => String(n)} />
          </div>
        ))}
      </div>
    </AnalyticsCard>
  );
}

// Ranking de custo: valor por moeda e quantas respostas ficaram sem custo.
// O backend ordena por moeda (cada linha na seção da sua primeira moeda).
export function CostGroups({
  groups,
  name,
  title,
}: {
  groups: CostGroup[];
  name: (g: CostGroup) => string;
  title?: (g: CostGroup) => string;
}) {
  const sections = currencySections(groups);
  return (
    <table className="w-full border-collapse text-[12.5px]">
      <thead>
        <tr className="border-b text-left text-[11px] text-faint">
          <th className="py-2 pr-3 font-medium">Nome</th>
          <th className="px-2 py-2 text-right font-medium">Custo estimado</th>
          <th className="px-2 py-2 text-right font-medium">Respostas</th>
          <th className="py-2 pl-2 text-right font-medium">Sem custo</th>
        </tr>
      </thead>
      <tbody>
        {groups.map((g, i) => {
          const sums = list(g.sums);
          const priced = sums.filter((s) => s.currency !== "");
          const records = sums.reduce((n, s) => n + s.records, 0);
          const missing = sums
            .filter((s) => s.currency === "")
            .reduce((n, s) => n + s.records, 0);
          return (
            <Fragment key={g.key}>
              {sections[i] != null && (
                <CurrencySectionRow currency={sections[i]} cols={4} />
              )}
              <tr className="border-b last:border-0">
                <td
                  className="max-w-56 truncate py-2 pr-3"
                  title={title?.(g) ?? name(g)}
                >
                  {name(g)}
                </td>
                <td className="px-2 py-2 text-right font-medium tabular-nums">
                  {priced.length ? (
                    priced.map((s) => (
                      <div key={s.currency}>
                        {formatMoney(String(s.total), s.currency)}
                        {s.partial > 0 && (
                          <span
                            className="text-amber-300"
                            title={`${s.partial} com custo parcial`}
                          >
                            *
                          </span>
                        )}
                      </div>
                    ))
                  ) : (
                    <span className="text-[11.5px] font-normal text-faint">
                      sem preço
                    </span>
                  )}
                </td>
                <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
                  {records.toLocaleString("pt-BR")}
                </td>
                <td
                  className={cn(
                    "py-2 pl-2 text-right tabular-nums",
                    missing ? "text-amber-300" : "text-faint",
                  )}
                >
                  {missing ? missing.toLocaleString("pt-BR") : "—"}
                </td>
              </tr>
            </Fragment>
          );
        })}
      </tbody>
    </table>
  );
}
