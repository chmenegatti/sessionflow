import { Fragment, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { ChevronLeft, ChevronRight, Link2Off } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAsync } from "@/hooks/useAsync";
import { CostService, list, type AnalyticsQuery } from "@/lib/api";
import { formatMoney } from "@/lib/money";
import { sessionPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { AnalyticsCard } from "./AnalyticsCard";
import { RefreshError } from "./OverviewPage";
import { currencySections } from "./costFormat";
import { dataView } from "./useAnalyticsData";

// Página de uma lista do servidor: volta à primeira ao trocar de filtro.
function usePaged<T extends { total: number; limit: number }>(
  query: AnalyticsQuery,
  fetch: (q: AnalyticsQuery, offset: number) => Promise<T>,
) {
  const [offset, setOffset] = useState(0);
  const key = JSON.stringify(query);
  const [lastKey, setLastKey] = useState(key);
  if (key !== lastKey) {
    setLastKey(key);
    setOffset(0);
  }
  const version = useAppStore((s) => s.dataVersion);
  const reqKey = `${key}#${offset}`;
  const res = useAsync(
    () => fetch(query, offset).then((data) => ({ key: reqKey, data })),
    [reqKey, version],
  );
  return { ...dataView(res, reqKey), reload: res.reload, offset, setOffset };
}

function Pager({
  offset,
  page,
  setOffset,
}: {
  offset: number;
  page: { total: number; limit: number } | null;
  setOffset: (n: number) => void;
}): ReactNode {
  if (!page || page.total <= page.limit) return undefined;
  return (
    <div className="flex items-center gap-1 text-[11.5px] text-faint">
      {offset + 1}–{Math.min(offset + page.limit, page.total)} de {page.total}
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
  );
}

function SessionLink({
  provider,
  nativeId,
  title,
}: {
  provider: string;
  nativeId: string;
  title: string;
}) {
  const navigate = useNavigate();
  const id = `${provider}:${nativeId}`;
  return (
    <button
      type="button"
      className="block max-w-full truncate text-left font-medium outline-none hover:text-primary focus-visible:text-primary"
      title={`Abrir a conversa (${nativeId})`}
      onClick={() => navigate(sessionPath(id))}
    >
      {title || "Sem título"}
    </button>
  );
}

const projectCell = (p: string) => (
  <td
    className="max-w-40 truncate px-2 py-2 font-mono text-[11.5px] text-muted-foreground"
    title={p}
  >
    {p ? p.split("/").pop() : "—"}
  </td>
);

// Custo por sessão raiz (com os subagentes associados), por moeda.
// Cabeçalho de seção de um ranking: moedas diferentes não se comparam,
// então cada moeda tem a sua ordem.
export function CurrencySectionRow({
  currency,
  cols,
}: {
  currency: string;
  cols: number;
}) {
  return (
    <tr className="border-b bg-white/[0.02]">
      <td
        colSpan={cols}
        className="py-1.5 text-[11px] font-medium tracking-wide text-faint uppercase"
      >
        {currency ? `Em ${currency}` : "Sem custo calculado"}
      </td>
    </tr>
  );
}

export function CostSessionsTable({ query }: { query: AnalyticsQuery }) {
  const r = usePaged(query, (q, o) => CostService.CostSessions(q, o));
  const page = r.data;
  const rows = list(page?.rows);
  const sections = currencySections(rows, !!page && page.total > page.limit); // página já em ordem de moeda (backend)
  return (
    <AnalyticsCard
      title="Por sessão"
      hint="Sessão principal com subagentes associados somados; clique para abrir a conversa"
      loading={r.loading}
      error={page ? null : r.error}
      onRetry={r.reload}
      empty={!!page && rows.length === 0}
      emptyText="Sem consumo no período."
      actions={<Pager offset={r.offset} page={page} setOffset={r.setOffset} />}
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
              <th className="py-2 pr-3 font-medium">Sessão</th>
              <th className="px-2 py-2 font-medium">Agente</th>
              <th className="px-2 py-2 font-medium">Projeto</th>
              <th className="px-2 py-2 text-right font-medium">
                Custo estimado
              </th>
              <th className="py-2 pl-2 text-right font-medium">Sem custo</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const sums = list(row.sums);
              const priced = sums.filter((s) => s.currency !== "");
              const missing = sums
                .filter((s) => s.currency === "")
                .reduce((n, s) => n + s.records, 0);
              return (
                <Fragment key={row.sessionId}>
                  {sections[i] != null && (
                    <CurrencySectionRow currency={sections[i]} cols={5} />
                  )}
                  <tr className="border-b hover:bg-white/[0.03]">
                    <td className="max-w-64 py-2 pr-3">
                      <span className="flex items-center gap-1.5">
                        <SessionLink
                          provider={row.key.provider}
                          nativeId={row.key.nativeId}
                          title={row.title}
                        />
                        {!row.associated && (
                          <span title="A sessão principal desta subordinada não está no índice">
                            <Link2Off className="size-3 text-amber-300" />
                          </span>
                        )}
                      </span>
                    </td>
                    <td className="px-2 py-2">
                      <ProviderTag provider={row.key.provider} />
                    </td>
                    {projectCell(row.project)}
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
                    <td
                      className={cn(
                        "py-2 pl-2 text-right tabular-nums",
                        missing ? "text-amber-300" : "text-faint",
                      )}
                    >
                      {missing || "—"}
                    </td>
                  </tr>
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </AnalyticsCard>
  );
}

// Sessões com respostas sem custo calculável, com o motivo.
export function UncalculableTable({ query }: { query: AnalyticsQuery }) {
  const r = usePaged(query, (q, o) => CostService.Uncalculable(q, o));
  const page = r.data;
  const rows = list(page?.rows);
  if (page && rows.length === 0 && !r.error) return null;
  return (
    <AnalyticsCard
      title="Sessões sem custo calculável"
      hint="Respostas sem preço vigente ou sem dados suficientes (horário, modelo, tokens)"
      loading={r.loading}
      error={page ? null : r.error}
      onRetry={r.reload}
      actions={<Pager offset={r.offset} page={page} setOffset={r.setOffset} />}
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
              <th className="py-2 pr-3 font-medium">Sessão</th>
              <th className="px-2 py-2 font-medium">Agente</th>
              <th className="px-2 py-2 font-medium">Projeto</th>
              <th className="px-2 py-2 text-right font-medium">Sem preço</th>
              <th className="px-2 py-2 text-right font-medium">Sem dados</th>
              <th className="py-2 pl-2 font-medium">Motivos</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr
                key={row.sessionId}
                className="border-b hover:bg-white/[0.03]"
              >
                <td className="max-w-64 py-2 pr-3">
                  <SessionLink
                    provider={row.key.provider}
                    nativeId={row.key.nativeId}
                    title={row.title}
                  />
                </td>
                <td className="px-2 py-2">
                  <ProviderTag provider={row.key.provider} />
                </td>
                {projectCell(row.project)}
                <td className="px-2 py-2 text-right tabular-nums">
                  {row.noPrice + row.unestimated || "—"}
                </td>
                <td className="px-2 py-2 text-right tabular-nums">
                  {row.insufficient || "—"}
                </td>
                <td className="py-2 pl-2 text-[11.5px] text-muted-foreground">
                  {list(row.reasons).join(", ") ||
                    (row.unestimated ? "ainda não calculado" : "—")}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </AnalyticsCard>
  );
}
