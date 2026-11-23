import { Fragment, useState } from "react";
import { useNavigate } from "react-router";
import { ChevronDown, ChevronLeft, ChevronRight, Link2Off } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  AnalyticsService,
  list,
  type AnalyticsQuery,
  type OriginShare,
  type UsageSessionRow,
} from "@/lib/api";
import { sessionPath } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { AnalyticsCard } from "./AnalyticsCard";
import { SessionTimelineChart } from "./SessionTimelineChart";
import { TokenCell } from "./GroupTable";
import { originLabels } from "./format";
import { useAsync } from "@/hooks/useAsync";
import { useAppStore } from "@/lib/store";
import { dataView } from "./useAnalyticsData";
import { RefreshError } from "./OverviewPage";

function duration(r: UsageSessionRow) {
  if (r.firstTs == null || r.lastTs == null) return "—";
  const m = Math.round((r.lastTs - r.firstTs) / 60000);
  if (m < 60) return `${m} min`;
  if (m < 48 * 60) return `${Math.floor(m / 60)} h ${m % 60} min`;
  return `${Math.round(m / 1440)} dias`;
}

// Consumo por sessão: cada linha é uma sessão raiz com os descendentes
// associados somados; expandir mostra o detalhamento por origem.
export function SessionsUsageTable({ query }: { query: AnalyticsQuery }) {
  const [offset, setOffset] = useState(0);
  const key = JSON.stringify(query);
  const [lastKey, setLastKey] = useState(key);
  if (key !== lastKey) {
    setLastKey(key);
    setOffset(0); // novo filtro: volta à primeira página
  }
  const version = useAppStore((s) => s.dataVersion);
  const reqKey = `${key}#${offset}`;
  const res = useAsync(
    () =>
      AnalyticsService.UsageSessions(query, offset).then((data) => ({
        key: reqKey,
        data,
      })),
    [reqKey, version],
  );
  // Mesma regra das páginas (dataView): página de outro filtro só aparece
  // enquanto a nova carrega; falha na recarga em segundo plano mantém as
  // linhas atuais com o aviso "Falha ao atualizar".
  const r = { ...dataView(res, reqKey), reload: res.reload };
  const page = r.data;
  const rows = list(page?.rows);
  return (
    <AnalyticsCard
      title="Por sessão"
      hint="Sessão principal com subagentes associados somados; sessões automáticas e não associadas em linhas próprias"
      loading={r.loading}
      error={page ? null : r.error}
      onRetry={r.reload}
      empty={!!page && rows.length === 0}
      emptyText="Sem consumo registrado no período."
      actions={
        page && page.total > page.limit ? (
          <div className="flex items-center gap-1 text-[11.5px] text-faint">
            {offset + 1}–{Math.min(offset + page.limit, page.total)} de{" "}
            {page.total}
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Página anterior"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - page.limit))}
            >
              <ChevronLeft />
            </Button>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="Próxima página"
              disabled={offset + page.limit >= page.total}
              onClick={() => setOffset(offset + page.limit)}
            >
              <ChevronRight />
            </Button>
          </div>
        ) : undefined
      }
    >
      {r.error && page && <RefreshError error={r.error} />}
      <div
        className={cn(
          "overflow-x-auto transition-opacity",
          r.stale && "opacity-50",
        )}
      >
        <table className="w-full border-collapse text-[12.5px]">
          <thead>
            <tr className="border-b text-left text-[11px] text-faint">
              <th className="w-7" />
              <th className="py-2 pr-3 font-medium">Sessão</th>
              <th className="px-2 py-2 font-medium">Agente</th>
              <th className="px-2 py-2 font-medium">Projeto</th>
              <th className="px-2 py-2 font-medium">Origem</th>
              <th className="px-2 py-2 text-right font-medium">Total</th>
              <th className="px-2 py-2 text-right font-medium">Entrada</th>
              <th className="px-2 py-2 text-right font-medium">Saída</th>
              <th className="px-2 py-2 text-right font-medium">Cache lido</th>
              <th className="px-2 py-2 text-right font-medium">Duração</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <SessionRow key={row.sessionId} row={row} query={query} />
            ))}
          </tbody>
        </table>
      </div>
    </AnalyticsCard>
  );
}

function SessionRow({
  row,
  query,
}: {
  row: UsageSessionRow;
  query: AnalyticsQuery;
}) {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const version = useAppStore((s) => s.dataVersion);
  // Refeito a cada recorte (e atualização do índice) enquanto aberto: o
  // detalhe precisa fechar com a linha do mesmo recorte.
  const origins = useAsync(
    () =>
      open
        ? AnalyticsService.SessionOrigins(query, row.sessionId).then(list)
        : Promise.resolve(null as OriginShare[] | null),
    [open, JSON.stringify(query), row.sessionId, version],
  );
  const parts = origins.loading ? null : origins.data;
  const error = origins.error;
  const toggle = () => setOpen(!open);
  const id = `${row.key.provider}:${row.key.nativeId}`;
  return (
    <Fragment>
      <tr className="border-b hover:bg-white/[0.03]">
        <td className="py-2">
          <Button
            variant="ghost"
            size="icon-xs"
            aria-expanded={open}
            aria-label={
              open ? "Recolher detalhe" : "Detalhar por origem e linha do tempo"
            }
            onClick={toggle}
          >
            <ChevronDown
              className={cn("transition-transform", !open && "-rotate-90")}
            />
          </Button>
        </td>
        <td className="max-w-72 py-2 pr-3">
          <button
            type="button"
            className="block max-w-full truncate text-left font-medium outline-none hover:text-primary focus-visible:text-primary"
            title="Abrir a conversa"
            onClick={() => navigate(sessionPath(id))}
          >
            {row.title || "Sem título"}
          </button>
        </td>
        <td className="px-2 py-2">
          <ProviderTag provider={row.key.provider} />
        </td>
        <td
          className="max-w-40 truncate px-2 py-2 font-mono text-[11.5px] text-muted-foreground"
          title={row.project}
        >
          {row.project ? row.project.split("/").pop() : "—"}
        </td>
        <td className="px-2 py-2 text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            {originLabels[row.origin] ?? row.origin}
            {!row.associated && (
              <span
                className="inline-flex items-center gap-1 rounded-md bg-amber-400/12 px-1.5 py-px text-[10.5px] text-amber-300"
                title="A sessão principal desta subordinada não está no índice"
              >
                <Link2Off className="size-3" /> não associada
              </span>
            )}
          </span>
        </td>
        <td className="px-2 py-2 text-right font-medium tabular-nums">
          <TokenCell t={row.tokens} k="total" />
        </td>
        <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
          <TokenCell t={row.tokens} k="input" />
        </td>
        <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
          <TokenCell t={row.tokens} k="output" />
        </td>
        <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
          <TokenCell t={row.tokens} k="cacheRead" />
        </td>
        <td className="px-2 py-2 text-right text-muted-foreground tabular-nums">
          {duration(row)}
        </td>
      </tr>
      {open && (
        <tr className="border-b bg-white/[0.02]">
          <td />
          <td colSpan={9} className="py-2 pr-3">
            <div className="flex flex-col gap-3">
              {error ? (
                <span className="text-[12px] text-negative">{error}</span>
              ) : !parts ? (
                <span className="text-[12px] text-faint">Carregando…</span>
              ) : (
                <ul className="flex flex-wrap gap-x-6 gap-y-1 text-[12px]">
                  {parts.map((p) => (
                    <li
                      key={p.origin}
                      className="flex items-center gap-2 text-muted-foreground"
                    >
                      {originLabels[p.origin] ?? p.origin}
                      <span className="text-faint">
                        ({p.sessions} {p.sessions === 1 ? "sessão" : "sessões"})
                      </span>
                      <span className="font-medium text-foreground tabular-nums">
                        <TokenCell t={p.tokens} k="total" />
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              <SessionTimelineChart query={query} rootID={row.sessionId} />
            </div>
          </td>
        </tr>
      )}
    </Fragment>
  );
}
