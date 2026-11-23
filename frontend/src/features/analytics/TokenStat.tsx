import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { Field } from "@/lib/api";
import { cn } from "@/lib/utils";
import {
  exactTokens,
  fieldState,
  formatTokens,
  pct,
  tokenDelta,
} from "./format";

// Indicador de consumo: valor, "parcial" com a cobertura, "indisponível"
// com o motivo, variação contra o anterior (quando ambos completos) e
// marca de derivado.
export function TokenStat({
  label,
  field,
  records,
  prev,
  prevRecords,
  reason,
  derived,
  big,
}: {
  label: string;
  field: Field;
  records: number;
  prev?: Field;
  prevRecords?: number;
  reason?: string; // por que fica indisponível
  derived?: string; // explicação quando o valor inclui derivados
  big?: boolean;
}) {
  const st = fieldState(field, records);
  const delta =
    prev && prevRecords !== undefined
      ? tokenDelta(field, records, prev, prevRecords)
      : null;
  return (
    <div
      className={cn(
        "surface flex flex-col rounded-[18px] px-4 py-3.5",
        big && "surface-sel",
      )}
    >
      <span className="text-[13px] font-medium text-muted-foreground">
        {label}
      </span>
      {st.kind === "none" ? (
        <span className="mt-2 text-[26px] leading-none font-semibold text-faint">
          —
        </span>
      ) : st.kind === "unavailable" ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              tabIndex={0}
              className="mt-2 w-fit cursor-help text-[15px] leading-[26px] font-medium text-faint outline-none"
            >
              indisponível
            </span>
          </TooltipTrigger>
          <TooltipContent>
            {reason ?? "Os providers do recorte não informam este campo."}
          </TooltipContent>
        </Tooltip>
      ) : (
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              tabIndex={0}
              className={cn(
                "mt-2 w-fit leading-none font-semibold tracking-tight tabular-nums outline-none",
                big ? "text-[32px]" : "text-[26px]",
              )}
            >
              {formatTokens(st.sum)}
            </span>
          </TooltipTrigger>
          <TooltipContent>{exactTokens(st.sum)}</TooltipContent>
        </Tooltip>
      )}
      <span className="mt-2 flex min-h-5 flex-wrap items-center gap-1.5 text-[11px] text-faint">
        {delta && (
          <span
            className={cn(
              "rounded-md px-1.5 py-0.5 font-semibold",
              delta.up === undefined
                ? "bg-white/[0.06] text-muted-foreground"
                : delta.up
                  ? "bg-positive/15 text-positive"
                  : "bg-negative/15 text-negative",
            )}
          >
            {delta.text}
          </span>
        )}
        {st.kind === "partial" && (
          <span
            className="rounded-md bg-amber-400/12 px-1.5 py-0.5 font-medium text-amber-300"
            title={`Informado em ${field.count} de ${records} registros`}
          >
            parcial · {pct(st.coverage)}
          </span>
        )}
        {derived && st.kind !== "unavailable" && st.kind !== "none" && (
          <span
            className="rounded-md bg-accent2/12 px-1.5 py-0.5 font-medium text-accent2"
            title={derived}
          >
            derivado
          </span>
        )}
        {st.kind !== "none" && st.kind !== "unavailable" && <span>tokens</span>}
      </span>
    </div>
  );
}
