import { useState } from "react";
import { ArrowRight, Calculator, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAsync } from "@/hooks/useAsync";
import {
  CostService,
  list,
  type AnalyticsQuery,
  type CostSums,
} from "@/lib/api";
import { formatMoney } from "@/lib/money";
import { useAppStore } from "@/lib/store";

// isStalePreview: o backend recusou a confirmação porque a prévia ficou
// desatualizada (ErrStalePreview).
export const isStalePreview = (msg: string) =>
  msg.includes("mudaram desde a prévia");

// Recalcular: mostra o efeito antes (quantas estimativas mudam e o total
// por moeda) e só grava ao confirmar. É o único caminho que muda uma
// estimativa já calculada com preço.
export function RecalculatePanel({
  query,
  onClose,
}: {
  query: AnalyticsQuery;
  onClose: () => void;
}) {
  const notify = useAppStore((s) => s.notify);
  const preview = useAsync(
    () => CostService.PreviewRecalculate(query),
    [JSON.stringify(query)],
  );
  const [busy, setBusy] = useState(false);
  const [stale, setStale] = useState(false);
  const p = preview.data;
  const currencies = [
    ...new Set([...list(p?.before), ...list(p?.after)].map((s) => s.currency)),
  ];
  const find = (xs: CostSums[] | null | undefined, c: string) =>
    list(xs).find((s) => s.currency === c);
  const run = async () => {
    if (!p) return;
    setBusy(true);
    try {
      const done = await CostService.Recalculate(query, p.token);
      notify(
        `${done.changes.toLocaleString("pt-BR")} estimativas recalculadas`,
      );
      onClose();
    } catch (e) {
      const msg = String(e instanceof Error ? e.message : e);
      if (isStalePreview(msg)) {
        // Os dados mudaram desde a prévia: nada foi gravado; mostra a nova.
        setStale(true);
        preview.reload();
      } else {
        notify(`Não foi possível recalcular: ${msg}`, "error");
      }
      setBusy(false);
    }
  };
  return (
    <section
      aria-label="Recalcular estimativas"
      className="surface flex flex-col gap-3 rounded-[18px] border border-primary/25 px-5 py-4"
    >
      <div className="flex items-start gap-3">
        <div>
          <h3 className="text-[15px] font-semibold tracking-tight">
            Recalcular com os preços vigentes
          </h3>
          <p className="mt-0.5 text-[12px] text-muted-foreground">
            Aplica os preços atuais a todas as respostas do recorte (filtros
            acima), substituindo as estimativas guardadas. Fora do recorte nada
            muda.
          </p>
        </div>
        <Button
          variant="ghost"
          size="icon-xs"
          className="ml-auto"
          aria-label="Fechar"
          onClick={onClose}
        >
          <X />
        </Button>
      </div>
      {preview.loading && (
        <p className="text-[12.5px] text-faint">Calculando o efeito…</p>
      )}
      {preview.error && (
        <p className="text-[12.5px] text-negative">
          Não foi possível calcular a prévia: {preview.error}
        </p>
      )}
      {stale && (
        <p
          role="status"
          className="rounded-md border border-amber-400/40 bg-amber-400/10 px-3 py-2 text-[12.5px] text-amber-300"
        >
          Os dados mudaram desde a prévia (sincronização ou preço). Nada foi
          gravado; confira a prévia atualizada abaixo e confirme de novo.
        </p>
      )}
      {p && (
        <>
          <p className="text-[13px]">
            {p.changes === 0
              ? "Nenhuma estimativa muda: todas já usam os preços vigentes."
              : `${p.changes.toLocaleString("pt-BR")} estimativas mudam.`}
          </p>
          {currencies.length > 0 && (
            <ul className="flex flex-col gap-1 text-[13px] tabular-nums">
              {currencies.map((c) => {
                const b = find(p.before, c);
                const a = find(p.after, c);
                return (
                  <li key={c} className="flex items-center gap-2">
                    <span className="text-muted-foreground">
                      {b ? formatMoney(String(b.total), c) : "sem preço"}
                    </span>
                    <ArrowRight className="size-3.5 text-faint" />
                    <span className="font-medium">
                      {a ? formatMoney(String(a.total), c) : "sem preço"}
                    </span>
                  </li>
                );
              })}
            </ul>
          )}
          <div className="flex gap-2">
            <Button size="sm" disabled={busy || p.changes === 0} onClick={run}>
              <Calculator /> Recalcular{" "}
              {p.changes ? p.changes.toLocaleString("pt-BR") : ""}
            </Button>
            <Button variant="ghost" size="sm" onClick={onClose}>
              Cancelar
            </Button>
          </div>
        </>
      )}
    </section>
  );
}
