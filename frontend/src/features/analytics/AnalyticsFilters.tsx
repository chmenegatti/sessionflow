import { Segmented } from "@/components/ui/segmented";
import { NativeSelect } from "@/components/ui/native-select";
import { useAsync } from "@/hooks/useAsync";
import { AnalyticsService, SessionService, list } from "@/lib/api";
import { useAppStore } from "@/lib/store";
import {
  ProviderPill,
  providerActive,
  toggleProviders,
} from "@/features/sessions/ProviderTag";
import {
  allProviders,
  type Period,
  useAnalyticsQuery,
} from "./useAnalyticsQuery";

const periodOptions: { value: Period; label: string }[] = [
  { value: "today", label: "Hoje" },
  { value: "7d", label: "7 dias" },
  { value: "30d", label: "30 dias" },
  { value: "90d", label: "90 dias" },
  { value: "month", label: "Este mês" },
  { value: "prev_month", label: "Mês anterior" },
  { value: "all", label: "Tudo" },
  { value: "custom", label: "Personalizado" },
];

const dateInput =
  "h-8 rounded-[10px] border bg-white/[0.03] px-2.5 text-[12.5px] text-foreground outline-none [color-scheme:dark] focus-visible:border-primary/50 focus-visible:shadow-[0_0_0_3px_rgba(56,225,245,0.12)]";

// Barra de filtros compartilhada pelas três telas. showModel: só em Usage.
export function AnalyticsFilters({ showModel }: { showModel?: boolean }) {
  const { query, update } = useAnalyticsQuery();
  const version = useAppStore((s) => s.dataVersion);
  const projects = useAsync(() => SessionService.Projects(), [version]);
  const models = useAsync(
    () =>
      showModel ? AnalyticsService.Models() : Promise.resolve([] as string[]),
    [version, showModel],
  );
  const selected = list(query.providers);
  return (
    <div
      className="flex flex-col gap-2.5"
      role="toolbar"
      aria-label="Filtros do Analytics"
    >
      <div className="flex flex-wrap items-center gap-2.5">
        <Segmented
          size="sm"
          label="Período"
          value={query.period as Period}
          onChange={(period) => update({ period })}
          options={periodOptions}
        />
        {query.period === "custom" && (
          <span className="flex items-center gap-1.5 text-[12px] text-faint">
            <input
              type="date"
              aria-label="Início"
              className={dateInput}
              value={query.from}
              max={query.to}
              // Digitado no teclado, o início pode passar do fim: o fim acompanha.
              onChange={(e) => {
                const from = e.target.value;
                if (from)
                  update(
                    query.to && from > query.to ? { from, to: from } : { from },
                  );
              }}
            />
            até
            <input
              type="date"
              aria-label="Fim"
              className={dateInput}
              value={query.to}
              min={query.from}
              onChange={(e) => {
                const to = e.target.value;
                if (to)
                  update(
                    query.from && to < query.from ? { from: to, to } : { to },
                  );
              }}
            />
          </span>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-2.5">
        <div
          className="flex flex-wrap items-center gap-1.5"
          role="group"
          aria-label="Agentes"
        >
          {allProviders.map((p) => (
            <ProviderPill
              key={p}
              provider={p}
              active={providerActive(selected, p)}
              disabled={selected.length === 1 && selected[0] === p}
              title={
                selected.length === 1 && selected[0] === p
                  ? "Pelo menos um agente fica ligado"
                  : undefined
              }
              onClick={() =>
                update({
                  providers: toggleProviders(selected, p, allProviders),
                })
              }
            />
          ))}
        </div>
        <NativeSelect
          aria-label="Projeto"
          value={query.project}
          onChange={(e) => update({ project: e.target.value })}
        >
          <option value="">Todos os projetos</option>
          {query.project &&
            !list(projects.data).some((p) => p.path === query.project) && (
              <option value={query.project}>{query.project}</option>
            )}
          {list(projects.data).map((p) => (
            <option key={p.path} value={p.path} title={p.path}>
              {p.name}
            </option>
          ))}
        </NativeSelect>
        {showModel && (
          <NativeSelect
            aria-label="Modelo"
            value={query.model}
            onChange={(e) => update({ model: e.target.value })}
          >
            <option value="">Todos os modelos</option>
            {/* Modelo da URL que a lista ainda não trouxe (ou saiu do índice) continua visível. */}
            {query.model && !list(models.data).includes(query.model) && (
              <option value={query.model}>{query.model}</option>
            )}
            {list(models.data).map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </NativeSelect>
        )}
      </div>
    </div>
  );
}
