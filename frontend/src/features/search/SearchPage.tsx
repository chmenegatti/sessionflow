import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { AlertTriangle, Bot, Loader2, RefreshCw, RotateCw, Search, SearchX, User, Wrench } from "lucide-react";
import { AnimatedBackground, hoverItem } from "@/components/ui/animated-background";
import { BlurFade } from "@/components/ui/blur-fade";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/native-select";
import { SkeletonRows } from "@/components/ui/skeleton";
import { EmptyState } from "@/components/layout/EmptyState";
import { PageHeader } from "@/components/layout/PageHeader";
import { useAsync } from "@/hooks/useAsync";
import {
  ProviderID,
  Role,
  SearchService,
  SessionService,
  SyncService,
  errorMessage,
  list,
  searchQuery,
  type SearchHit,
  type SearchQuery,
  type SnippetPart,
} from "@/lib/api";
import { relativeTime } from "@/lib/format";
import { SEARCH_INPUT_ID, sessionPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ProviderPill, ProviderTag, providerActive, providerLabel, toggleProviders } from "@/features/sessions/ProviderTag";

const PAGE = 30;
const DEBOUNCE_MS = 200;
const allProviders = [ProviderID.ProviderClaude, ProviderID.ProviderCodex, ProviderID.ProviderOpenCode];

interface Results {
  hits: SearchHit[];
  total: number;
  capped: boolean;
  hasMore: boolean;
}

// Estado da busca na URL: voltar do viewer restaura os resultados.
function useSearchState() {
  const [params, setParams] = useSearchParams();
  const providers = (params.get("p") ?? "").split(",").filter((p): p is ProviderID => allProviders.includes(p as ProviderID));
  const query = searchQuery({
    text: params.get("q") ?? "",
    providers,
    project: params.get("project") ?? "",
    includeHidden: params.get("hidden") === "1",
    limit: PAGE,
  });
  // Só os campos de patch mudam; o resto vem da URL.
  const update = (patch: Partial<SearchQuery>) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        const set = (k: string, v: string) => (v ? next.set(k, v) : next.delete(k));
        if (patch.text !== undefined) set("q", patch.text);
        if (patch.providers !== undefined) set("p", list(patch.providers).join(","));
        if (patch.project !== undefined) set("project", patch.project);
        if (patch.includeHidden !== undefined) set("hidden", patch.includeHidden ? "1" : "");
        return next;
      },
      { replace: true },
    );
  return { query, update };
}

export function SearchPage() {
  const navigate = useNavigate();
  const version = useAppStore((s) => s.dataVersion);
  const scanning = useAppStore((s) => s.status?.scan.running ?? false);
  const loadStatus = useAppStore((s) => s.loadStatus);
  const { query, update } = useSearchState();
  const projects = useAsync(() => SessionService.Projects(), [version]);

  // Texto digitado: vai para a URL (e dispara a busca) após uma pausa.
  const [text, setText] = useState(query.text);
  // O timer usa o update da renderização mais recente: o do momento da
  // digitação enxerga a URL antiga e desfaria um filtro mudado na espera.
  const latestUpdate = useRef(update);
  latestUpdate.current = update;
  useEffect(() => {
    if (text === query.text) return;
    const t = setTimeout(() => latestUpdate.current({ text }), DEBOUNCE_MS);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);

  const [res, setRes] = useState<Results | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [moreError, setMoreError] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  // Versão do índice da busca exibida. Os resultados não recarregam sozinhos
  // quando o índice muda (com o watcher, a cada poucos segundos): a lista
  // pularia durante a leitura. Um aviso oferece atualizar.
  const [searchedVersion, setSearchedVersion] = useState(version);
  const gen = useRef(0);
  const key = JSON.stringify(query);

  // Nova consulta: cancela a anterior no backend e ignora respostas velhas.
  useEffect(() => {
    const g = ++gen.current;
    setError(null);
    setMoreError(null);
    if (!query.text.trim()) {
      setRes(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setSearchedVersion(useAppStore.getState().dataVersion);
    const call = SearchService.Search(query);
    call.then(
      (r) => {
        if (g !== gen.current) return;
        setRes({ hits: list(r.hits), total: r.total, capped: r.totalCapped, hasMore: r.hasMore });
        setLoading(false);
      },
      (err) => {
        if (g !== gen.current) return;
        setError(errorMessage(err));
        setLoading(false);
      },
    );
    return () => void call.cancel();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, retry]);

  const loadMore = () => {
    if (!res) return;
    const g = gen.current;
    setLoading(true);
    setMoreError(null);
    SearchService.Search({ ...query, offset: res.hits.length }).then(
      (r) => {
        if (g !== gen.current) return;
        // Com a indexação rodando, o deslocamento pode repetir resultados.
        setRes((cur) => {
          if (!cur) return cur;
          const seen = new Set(cur.hits.map(hitKey));
          return { ...cur, hits: [...cur.hits, ...list(r.hits).filter((h) => !seen.has(hitKey(h)))], hasMore: r.hasMore };
        });
        setLoading(false);
      },
      (err) => {
        if (g !== gen.current) return;
        setMoreError(errorMessage(err));
        setLoading(false);
      },
    );
  };

  const refreshIndex = () =>
    SyncService.Start().then(
      () => void loadStatus(), // o fim da indexação chega pelo evento index:updated
      (err) => setError(errorMessage(err)),
    );

  const selected = list(query.providers);
  const toggleProvider = (p: ProviderID) => update({ providers: toggleProviders(selected, p, allProviders) });

  // Setas: do campo para os resultados e entre eles.
  const listRef = useRef<HTMLDivElement>(null);
  const focusResult = (from: Element | null, delta: number) => {
    const items = [...(listRef.current?.querySelectorAll<HTMLElement>("[data-result]") ?? [])];
    const i = from ? items.indexOf(from as HTMLElement) : -1;
    if (i + delta < 0) document.getElementById(SEARCH_INPUT_ID)?.focus();
    else items[Math.min(i + delta, items.length - 1)]?.focus();
  };

  let body;
  if (!query.text.trim()) {
    body = (
      <EmptyState
        icon={Search}
        title="Pesquise em todas as conversas"
        description='Palavras soltas precisam aparecer todas, em qualquer ordem (e casam como prefixo: "migra" acha "migração"). Use aspas para uma frase exata.'
      />
    );
  } else if (error) {
    body = (
      <EmptyState
        icon={AlertTriangle}
        title="Não foi possível pesquisar"
        description={error}
        action={
          <Button variant="outline" size="sm" onClick={() => setRetry((r) => r + 1)}>
            <RotateCw /> Tentar novamente
          </Button>
        }
      />
    );
  } else if (!res) {
    body = (
      <div className="surface overflow-hidden rounded-[18px]">
        <SkeletonRows rows={6} />
      </div>
    );
  } else if (res.hits.length === 0) {
    body = (
      <EmptyState
        icon={SearchX}
        title="Nada encontrado"
        description={
          scanning
            ? "A indexação ainda está em andamento; os resultados podem aparecer quando ela terminar."
            : "Tente menos palavras, outro filtro ou inclua subagentes e sessões automáticas."
        }
      />
    );
  } else {
    body = (
      <div
        ref={listRef}
        className={cn("surface overflow-hidden rounded-[18px] transition-opacity duration-200", loading && "opacity-60")}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            e.preventDefault();
            focusResult(document.activeElement, e.key === "ArrowDown" ? 1 : -1);
          }
        }}
      >
        <AnimatedBackground className="divide-y" highlightClassName="bg-white/[0.04]">
          {res.hits.map((h, i) => (
            <div key={hitKey(h)} {...hoverItem}>
              <BlurFade className="relative" delay={i < 12 ? i * 0.025 : 0} offset={4} blur="4px">
                <HitRow hit={h} onOpen={() => navigate(sessionPath(h.sessionId, h.seq))} />
              </BlurFade>
            </div>
          ))}
        </AnimatedBackground>
        {res.hasMore && (
          <div className="flex flex-col items-center gap-1 border-t p-3">
            <Button variant="outline" size="sm" onClick={loadMore} disabled={loading}>
              {loading && <Loader2 className="animate-spin" />} Carregar mais
            </Button>
            {moreError && <p className="text-xs text-destructive">{moreError}</p>}
          </div>
        )}
      </div>
    );
  }

  return (
    <>
      <PageHeader
        title="Busca"
        actions={
          <Button variant="ghost" size="sm" onClick={refreshIndex} disabled={scanning} title="Relê só os históricos que mudaram">
            <RefreshCw className={cn(scanning && "animate-spin")} /> {scanning ? "Indexando…" : "Atualizar índice"}
          </Button>
        }
      />
      <div className="space-y-3 px-6 pt-6 pb-4">
        <div className="group relative">
          <Search className="pointer-events-none absolute top-1/2 left-4 size-[18px] -translate-y-1/2 text-faint transition-colors group-focus-within:text-primary" />
          <input
            id={SEARCH_INPUT_ID}
            autoFocus
            type="search"
            value={text}
            maxLength={200}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                focusResult(null, 1);
              } else if (e.key === "Enter" && !loading && query.text === text && res?.hits[0]) {
                // Só resultados da consulta que está no campo.
                navigate(sessionPath(res.hits[0].sessionId, res.hits[0].seq));
              }
            }}
            placeholder='Pesquisar mensagens… (use "aspas" para frase exata)'
            aria-label="Pesquisar mensagens"
            className="surface h-13 w-full rounded-2xl pr-16 pl-12 text-[15px] outline-none transition-[border-color,box-shadow] duration-200 placeholder:text-faint focus:border-primary/50 focus:shadow-[0_0_0_4px_rgba(56,225,245,0.12),0_0_32px_-6px_rgba(56,225,245,0.45),var(--shadow-card)]"
          />
          {!text && (<kbd className="pointer-events-none absolute top-1/2 right-4 -translate-y-1/2 rounded border bg-white/[0.04] px-1.5 font-mono text-[10px] text-faint">Ctrl K</kbd>)}
        </div>
        <div className="flex flex-wrap items-center gap-2" role="toolbar" aria-label="Filtros da busca">
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Agentes">
            {allProviders.map((p) => (
              <ProviderPill
                key={p}
                provider={p}
                active={providerActive(selected, p)}
                disabled={selected.length === 1 && selected[0] === p}
                onClick={() => toggleProvider(p)}
              />
            ))}
          </div>
          <NativeSelect aria-label="Projeto" value={query.project} onChange={(e) => update({ project: e.target.value })}>
            <option value="">Todos os projetos</option>
            {/* Projeto vindo da URL que não está mais na lista continua selecionável. */}
            {query.project && !list(projects.data).some((p) => p.path === query.project) && (
              <option value={query.project}>{query.project}</option>
            )}
            {list(projects.data).map((p) => (
              <option key={p.path} value={p.path} title={p.path}>
                {p.name}
              </option>
            ))}
          </NativeSelect>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <input
              type="checkbox"
              className="accent-primary"
              checked={query.includeHidden}
              onChange={(e) => update({ includeHidden: e.target.checked })}
            />
            Incluir subagentes e automáticas
          </label>
          {res && version !== searchedVersion && !loading && (
            <Button variant="link" size="xs" className="h-auto p-0" onClick={() => setRetry((r) => r + 1)}>
              <RefreshCw /> Índice atualizado · atualizar resultados
            </Button>
          )}
          <span className="ml-auto text-xs tabular-nums text-faint" aria-live="polite">
            {res && query.text.trim() ? `${res.total}${res.capped ? "+" : ""} ${res.total === 1 && !res.capped ? "mensagem" : "mensagens"}` : ""}
          </span>
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-6" aria-busy={loading}>
        {body}
      </div>
    </>
  );
}

const hitKey = (h: SearchHit) => `${h.sessionId}#${h.seq}`;

const roleMeta = {
  [Role.RoleUser]: { icon: User, label: "Você" },
  [Role.RoleTool]: { icon: Wrench, label: "Ferramenta" },
} as Record<string, { icon: typeof User; label: string } | undefined>;

function HitRow({ hit: h, onOpen }: { hit: SearchHit; onOpen: () => void }) {
  const role = roleMeta[h.role];
  const RoleIcon = role?.icon ?? Bot;
  return (
    <button
      type="button"
      data-result
      onClick={onOpen}
      className="flex w-full flex-col gap-1.5 px-5 py-3.5 text-left outline-none focus-visible:bg-primary/[0.06]"
    >
      <div className="flex min-w-0 items-center gap-2 text-xs text-faint">
        <ProviderTag provider={h.provider} />
        <span className="truncate text-sm font-medium text-foreground">{h.title || "Sem título"}</span>
        {(h.subagent || h.automated) && <span className="shrink-0 rounded border px-1 text-[10px]">{h.subagent ? "subagente" : "automática"}</span>}
        {h.projectName && (
          <span className="shrink-0 truncate" title={h.projectPath}>
            · {h.projectName}
          </span>
        )}
        <span className="ml-auto shrink-0">{h.timestamp ? relativeTime(h.timestamp) : ""}</span>
      </div>
      <p className="line-clamp-2 text-[13px] leading-relaxed text-muted-foreground">
        <RoleIcon className="mr-1.5 inline size-3.5 -translate-y-px" aria-label={role?.label ?? providerLabel(h.provider)} />
        <Snippet parts={list(h.snippet)} />
      </p>
    </button>
  );
}

// Trecho com os termos destacados. Só texto: o conteúdo da conversa nunca
// é interpretado como HTML.
function Snippet({ parts }: { parts: SnippetPart[] }) {
  return (
    <>
      {parts.map((p, i) =>
        p.match ? (
          <mark key={i} className="rounded-sm bg-primary/20 px-0.5 text-primary shadow-[0_0_10px_-2px_rgba(56,225,245,0.5)]">
            {p.text}
          </mark>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}
