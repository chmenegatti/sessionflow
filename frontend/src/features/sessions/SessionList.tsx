import { useEffect, useState } from "react";
import { AlertTriangle, Loader2, RotateCw, type LucideIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/native-select";
import { EmptyState } from "@/components/layout/EmptyState";
import { useAsync } from "@/hooks/useAsync";
import {
  ProviderID,
  SessionService,
  list,
  sessionQuery,
  type SessionQuery,
} from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ProviderPill, providerActive, toggleProviders } from "./ProviderTag";
import { Segmented } from "@/components/ui/segmented";
import { SessionRows } from "./SessionRows";
import { SkeletonRows } from "@/components/ui/skeleton";

const PAGE = 50;
const MAX_LIMIT = 1000; // igual ao limite do backend (maxPageSize)
const allProviders = [ProviderID.ProviderClaude, ProviderID.ProviderCodex, ProviderID.ProviderOpenCode];

interface Filters {
  providers: ProviderID[];
  sort: "recent" | "oldest";
  tag: string;
  includeHidden: boolean;
}

interface SessionListProps {
  // Filtros fixos da página (projeto, favoritas).
  base?: Partial<SessionQuery>;
  showProject?: boolean;
  allowHidden?: boolean;
  empty: { icon: LucideIcon; title: string; description: string };
}

// Listagem com filtros (provider, ordem, tag, ocultas) e "carregar mais".
// Filtragem e ordenação são feitas no backend.
export function SessionList({ base = {}, showProject = true, allowHidden = true, empty }: SessionListProps) {
  const version = useAppStore((s) => s.dataVersion);
  const [filters, setFilters] = useState<Filters>({ providers: [], sort: "recent", tag: "", includeHidden: false });
  // "Carregar mais" aumenta a página em vez de acumular páginas: com o
  // watcher, os dados mudam a cada poucos segundos e a recarga traz de novo
  // tudo o que está na tela, sem encolher a lista nem pular a rolagem.
  const [limit, setLimit] = useState(PAGE);
  const filterKey = JSON.stringify(sessionQuery({ ...base, ...filters }));
  useEffect(() => setLimit(PAGE), [filterKey]);

  const query = sessionQuery({ ...base, ...filters, limit });
  const key = JSON.stringify(query);
  // A resposta leva a chave do pedido: esmaece só quando o filtro mudou,
  // não nas recargas de fundo.
  const first = useAsync(async () => ({ key, page: await SessionService.List(query) }), [key, version]);
  const tags = useAsync(() => SessionService.Tags(), [version]);

  const page = first.data?.page;
  const items = list(page?.items);
  const total = page?.total ?? 0;
  const hasMore = items.length < total && limit < MAX_LIMIT;
  const changing = first.loading && first.data?.key !== key;
  const loadingMore = changing && JSON.parse(first.data?.key ?? "{}").limit < limit;
  const loadMore = () => setLimit((l) => Math.min(l + PAGE, MAX_LIMIT));

  const toggleProvider = (p: ProviderID) => setFilters((f) => ({ ...f, providers: toggleProviders(f.providers, p, allProviders) }));
  const onlyOne = filters.providers.length === 1;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2.5 px-6 pt-5 pb-4" role="toolbar" aria-label="Filtros">
        <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Agentes">
          {allProviders.map((p) => (
            <ProviderPill
              key={p}
              provider={p}
              active={providerActive(filters.providers, p)}
              disabled={onlyOne && filters.providers[0] === p}
              onClick={() => toggleProvider(p)}
            />
          ))}
        </div>
        <Segmented
          size="sm"
          label="Ordenação"
          value={filters.sort}
          onChange={(sort) => setFilters((f) => ({ ...f, sort }))}
          options={[
            { value: "recent", label: "Mais recentes" },
            { value: "oldest", label: "Mais antigas" },
          ]}
        />
        <NativeSelect
          aria-label="Tag"
          value={filters.tag}
          onChange={(e) => setFilters((f) => ({ ...f, tag: e.target.value }))}
          disabled={!list(tags.data).length && !filters.tag}
        >
          <option value="">Todas as tags</option>
          {list(tags.data).map((t) => (
            <option key={t.name} value={t.name}>
              #{t.name} ({t.count})
            </option>
          ))}
        </NativeSelect>
        {allowHidden && (
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <input
              type="checkbox"
              className="accent-primary"
              checked={filters.includeHidden}
              onChange={(e) => setFilters((f) => ({ ...f, includeHidden: e.target.checked }))}
            />
            Mostrar subagentes e automáticas
          </label>
        )}
        <span className="ml-auto text-xs tabular-nums text-faint" aria-live="polite">
          {page ? `${total.toLocaleString("pt-BR")} ${total === 1 ? "sessão" : "sessões"}` : ""}
        </span>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6">
        {first.error && !page ? (
          <EmptyState
            icon={AlertTriangle}
            title="Não foi possível carregar as sessões"
            description={first.error}
            action={
              <Button variant="outline" size="sm" onClick={first.reload}>
                <RotateCw /> Tentar novamente
              </Button>
            }
          />
        ) : !page ? (
          <div className="surface overflow-hidden rounded-[18px]">
            <SkeletonRows rows={8} />
          </div>
        ) : items.length === 0 ? (
          <EmptyState icon={empty.icon} title={empty.title} description={empty.description} />
        ) : (
          <div className={cn("surface overflow-hidden rounded-[18px] transition-opacity duration-200", changing && !loadingMore && "opacity-60")}>
            <SessionRows items={items} showProject={showProject} />
            {hasMore && (
              <div className="flex flex-col items-center gap-1 border-t p-3">
                <Button variant="outline" size="sm" onClick={loadMore} disabled={loadingMore}>
                  {loadingMore && <Loader2 className="animate-spin" />} Carregar mais ({total - items.length})
                </Button>
              </div>
            )}
            {/* Falha numa recarga de fundo: a lista anterior fica. */}
            {first.error && <p className="p-3 text-center text-xs text-destructive">{first.error}</p>}
          </div>
        )}
      </div>
    </div>
  );
}
