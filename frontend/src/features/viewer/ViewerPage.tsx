import { useCallback, useEffect, useMemo, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router";
import { useVirtualizer } from "@tanstack/react-virtual";
import { AlertTriangle, ArrowLeft, FolderGit2, MessageSquareOff, RotateCw, TerminalSquare } from "lucide-react";
import { TextShimmer } from "@/components/ui/text-shimmer";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/layout/EmptyState";
import { PageHeader } from "@/components/layout/PageHeader";
import { useAsync } from "@/hooks/useAsync";
import { SessionService } from "@/lib/api";
import { plural, relativeTime } from "@/lib/format";
import { projectPath, sessionPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { FavoriteButton } from "@/features/sessions/FavoriteButton";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { SessionTitle } from "@/features/sessions/SessionRow";
import { RowView, buildRows, rowKey, toolNames } from "./MessageRow";
import { useTranscript } from "./useTranscript";
import { useResume } from "@/features/resume/useResume";

// Visualizador de conversa. Só as linhas visíveis são renderizadas
// (virtualização) e as páginas seguintes são pedidas ao chegar perto do fim.
// Mensagens anteriores ao alvo da busca exibidas como contexto.
const TARGET_CONTEXT = 4;

export function ViewerPage() {
  const [params] = useSearchParams();
  const id = params.get("id") ?? "";
  const seq = params.get("seq") ?? "";
  const target = /^\d+$/.test(seq) ? Number(seq) : null;
  return id ? <Viewer key={`${id}#${seq}`} id={id} target={target} /> : <EmptyState icon={MessageSquareOff} title="Nenhuma sessão informada" />;
}

function Viewer({ id, target }: { id: string; target: number | null }) {
  const navigate = useNavigate();
  const version = useAppStore((s) => s.dataVersion);
  const select = useAppStore((s) => s.select);
  const detail = useAsync(() => SessionService.Get(id), [id, version]);
  const from = target === null ? 0 : Math.max(0, target - TARGET_CONTEXT);
  const t = useTranscript(id, from);
  const { resume } = useResume();

  useEffect(() => select(id), [id, select]);

  const rows = useMemo(() => buildRows(t.messages), [t.messages]);
  const names = useMemo(() => toolNames(t.messages), [t.messages]);
  const s = detail.data?.session;

  const scrollRef = useRef<HTMLDivElement>(null);
  const footer = t.done ? 0 : 1; // linha de "carregando"/erro no fim
  const getItemKey = useCallback((i: number) => (i < rows.length ? rowKey(rows[i]) : "footer"), [rows]);
  const virtual = useVirtualizer({
    count: rows.length + footer,
    getScrollElement: () => scrollRef.current,
    // Estimativa por tipo de linha: menos correção ao medir a linha real.
    estimateSize: (i) => (i < rows.length && rows[i].kind === "system" ? 30 : 140),
    overscan: 6,
    getItemKey,
  });
  const items = virtual.getVirtualItems();
  const lastIndex = items.length ? items[items.length - 1].index : -1;

  // Resultado de busca: rola até a mensagem uma vez, quando ela chega.
  const targetIndex = useMemo(
    () => (target === null ? -1 : rows.findIndex((r) => r.kind === "message" && r.message.seq === target)),
    [rows, target],
  );
  const scrolled = useRef(false);
  useEffect(() => {
    if (targetIndex < 0 || scrolled.current) return;
    scrolled.current = true;
    virtual.scrollToIndex(targetIndex, { align: "center" });
  }, [targetIndex, virtual]);

  // Perto do fim do que já foi carregado: pede a próxima página.
  useEffect(() => {
    if (!t.done && !t.loading && !t.pageError && !t.openError && lastIndex >= rows.length - 3) t.loadMore();
  }, [lastIndex, rows.length, t]);

  const header = (
    <PageHeader
      title={s ? s.title || "Sem título" : "Conversa"}
      actions={
        <>
          {s && !s.subagent && (
            <Button variant="ghost" size="sm" title="Retomar no terminal (Ctrl+Enter)" onClick={() => void resume(id)}>
              <TerminalSquare /> Retomar
            </Button>
          )}
          <Button variant="ghost" size="sm" onClick={() => (window.history.length > 1 ? navigate(-1) : navigate("/sessions"))}>
            <ArrowLeft /> Voltar
          </Button>
        </>
      }
    />
  );

  if (t.openError) {
    return (
      <>
        {header}
        <EmptyState
          icon={AlertTriangle}
          title="Não foi possível abrir a conversa"
          description={t.openError}
          action={
            <Button variant="outline" size="sm" onClick={t.reopen}>
              <RotateCw /> Tentar novamente
            </Button>
          }
        />
      </>
    );
  }

  return (
    <>
      {header}
      {s && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b bg-white/[0.015] px-5 py-2.5 text-xs text-faint">
          <FavoriteButton id={s.id} favorite={s.favorite} />
          <SessionTitle title={s.title} className="max-w-md text-[14px] font-semibold tracking-tight text-foreground" />
          <ProviderTag provider={s.provider} />
          {s.projectPath && (
            <Button variant="link" size="xs" className="h-auto p-0" title={s.projectPath} onClick={() => navigate(projectPath(s.projectPath))}>
              <FolderGit2 /> {s.projectName}
            </Button>
          )}
          <span>{relativeTime(s.updatedAt)}</span>
          {s.model && <span className="rounded-md border bg-white/[0.03] px-1.5 py-px font-mono text-[11px] text-muted-foreground">{s.model}</span>}
          <span className="ml-auto tabular-nums" aria-live="polite">
            {plural(t.dropped + t.messages.length, "registro lido", "registros lidos")}
            {t.done ? "" : "…"}
          </span>
        </div>
      )}
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto" role="feed" aria-busy={t.loading}>
        {from > 0 && (
          <div className="flex items-center justify-center gap-2 border-b bg-primary/[0.04] py-2 text-xs text-muted-foreground">
            Aberta no resultado da busca; mensagens anteriores não foram carregadas.
            <Button variant="link" size="xs" className="h-auto p-0" onClick={() => navigate(sessionPath(id), { replace: true })}>
              Ver desde o início
            </Button>
          </div>
        )}
        {t.dropped > 0 && (
          <div className="flex items-center justify-center gap-2 border-b bg-primary/[0.04] py-2 text-xs text-muted-foreground">
            Conteúdo de {plural(t.dropped, "registro anterior foi liberado", "registros anteriores foram liberados")} da memória.
            <Button variant="link" size="xs" className="h-auto p-0" onClick={t.reopen}>
              Reabrir do início
            </Button>
          </div>
        )}
        {t.done && rows.length === 0 ? (
          <EmptyState icon={MessageSquareOff} title="Conversa vazia" description="O histórico não tem mensagens desta sessão." />
        ) : (
          <div className="relative mx-auto w-full max-w-4xl" style={{ height: virtual.getTotalSize() }}>
            {items.map((it) => (
              <div
                key={it.key}
                data-index={it.index}
                data-key={String(it.key)}
                ref={virtual.measureElement}
                className="absolute top-0 left-0 w-full"
                style={{ transform: `translateY(${it.start}px)` }}
              >
                {it.index < rows.length ? (
                  <RowView row={rows[it.index]} provider={s?.provider ?? ""} names={names} highlight={it.index === targetIndex} />
                ) : (
                  <Footer error={t.pageError} onRetry={t.loadMore} onReopen={t.reopen} />
                )}
              </div>
            ))}
          </div>
        )}
        {t.done && rows.length > 0 && <p className="flex items-center justify-center gap-3 py-8 text-xs text-faint">
            <span className="h-px w-16 bg-linear-to-r from-transparent to-border" />
            Fim da conversa
            <span className="h-px w-16 bg-linear-to-l from-transparent to-border" />
          </p>}
      </div>
    </>
  );
}

function Footer({ error, onRetry, onReopen }: { error: string | null; onRetry: () => void; onReopen: () => void }) {
  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 py-6 text-xs">
        <p className="text-destructive">{error}</p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RotateCw /> Tentar de novo
          </Button>
          <Button variant="ghost" size="sm" onClick={onReopen}>
            Reabrir do início
          </Button>
        </div>
      </div>
    );
  }
  return (
    <div className="flex justify-center py-6 text-xs" role="status">
      <TextShimmer>Carregando mensagens…</TextShimmer>
    </div>
  );
}
