import { AnalyticsService, type AnalyticsQuery } from "@/lib/api";
import { useAsync } from "@/hooks/useAsync";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { fieldLabels, formatTokens } from "./format";
import { timelineView, type TimelineKey } from "./timeline";

const colors: Record<TimelineKey, string> = {
  input: "var(--codex)",
  output: "var(--accent2)",
  cacheRead: "var(--positive)",
  cacheCreation: "#fbbf24",
};
const labels: Record<TimelineKey, string> = {
  input: fieldLabels.input,
  output: fieldLabels.output,
  cacheRead: fieldLabels.cache_read,
  cacheCreation: fieldLabels.cache_creation,
};

const stepText = (ms: number) =>
  ms >= 86_400_000
    ? "dia"
    : ms >= 3_600_000
      ? `${ms / 3_600_000} h`
      : `${ms / 60_000} min`;
const when = (ms: number, step: number) =>
  new Date(ms).toLocaleString(
    "pt-BR",
    step >= 86_400_000
      ? { day: "2-digit", month: "2-digit" }
      : {
          day: "2-digit",
          month: "2-digit",
          hour: "2-digit",
          minute: "2-digit",
        },
  );
const n = (v: number) => v.toLocaleString("pt-BR");

// Consumo da sessão por intervalo (com subagentes associados), a partir dos
// registros indexados: só intervalos com registro, retomadas marcadas.
export function SessionTimelineChart({
  query,
  rootID,
}: {
  query: AnalyticsQuery;
  rootID: number;
}) {
  const version = useAppStore((s) => s.dataVersion);
  const res = useAsync(
    () => AnalyticsService.SessionTimeline(query, rootID),
    [JSON.stringify(query), rootID, version],
  );
  if (res.error)
    return <p className="text-[12px] text-negative">{res.error}</p>;
  if (!res.data)
    return (
      <p className="text-[12px] text-faint">Carregando a linha do tempo…</p>
    );
  const v = timelineView(res.data);
  if (v.points.length === 0) {
    return (
      <p className="text-[12px] text-faint">
        {v.untimedRecords
          ? `Sem horário nos ${n(v.untimedRecords)} registros de consumo: não há linha do tempo.`
          : "Sem consumo registrado nesta sessão."}
      </p>
    );
  }
  const partial = v.points.filter((p) => p.partial).length;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-[12px] text-muted-foreground">
        <span className="font-medium text-foreground">
          Consumo ao longo da sessão
        </span>
        <span>intervalos de {stepText(v.stepMs)}</span>
        {v.resumes > 0 && (
          <span className="text-amber-300">
            {v.resumes} {v.resumes === 1 ? "retomada" : "retomadas"} (pausa de
            30 min ou mais)
          </span>
        )}
        <span className="ml-auto">
          Total acumulado{" "}
          <span className="font-medium text-foreground tabular-nums">
            {formatTokens(v.total)}
          </span>
        </span>
      </div>
      {/* Largura das barras (até 24 px + 3 px de espaço): os rótulos ficam embaixo delas. */}
      <div
        className="flex flex-col gap-1"
        style={{ maxWidth: v.points.length * 27 }}
      >
        <div
          className="flex h-28 items-end gap-[3px]"
          role="img"
          aria-label={`Consumo por intervalo: ${v.points.length} intervalos, total ${n(v.total)} tokens`}
        >
          {v.points.map((p) => {
            const sum = p.stack.reduce((a, b) => a + b.value, 0);
            const tip = [
              when(p.start, v.stepMs) + (p.resumed ? " · retomada" : ""),
              ...p.stack.map((s) => `${labels[s.key]}: ${n(s.value)}`),
              ...(p.reasoning != null
                ? [`Reasoning (dentro da saída ou à parte): ${n(p.reasoning)}`]
                : []),
              `Registros: ${n(p.records)}${p.partial ? " · dados parciais" : ""}`,
              `Acumulado: ${n(p.cumulative)}`,
            ].join("\n");
            return (
              <div
                key={p.start}
                className={cn(
                  "flex h-full max-w-6 min-w-[3px] flex-1 flex-col justify-end",
                  p.resumed &&
                    "border-l border-dashed border-amber-300/70 pl-px",
                )}
                title={tip}
              >
                <div
                  className="flex w-full flex-col-reverse overflow-hidden rounded-t-[3px]"
                  style={{
                    height: `${v.max ? Math.max(3, (sum / v.max) * 100) : 3}%`,
                    opacity: p.partial ? 0.55 : 1,
                  }}
                >
                  {p.stack.map((s) => (
                    <span
                      key={s.key}
                      style={{
                        height: sum ? `${(s.value / sum) * 100}%` : 0,
                        background: colors[s.key],
                      }}
                    />
                  ))}
                </div>
              </div>
            );
          })}
        </div>
        <div className="flex justify-between text-[10.5px] text-faint tabular-nums">
          <span>{when(v.points[0].start, v.stepMs)}</span>
          {v.points.length > 1 && (
            <span>{when(v.points[v.points.length - 1].start, v.stepMs)}</span>
          )}
        </div>
      </div>
      <ul className="flex flex-wrap gap-x-4 gap-y-1 text-[11.5px] text-muted-foreground">
        {v.keys.map((k) => (
          <li key={k} className="flex items-center gap-1.5">
            <span
              className="size-2 rounded-[3px]"
              style={{ background: colors[k] }}
            />
            {labels[k]}
          </li>
        ))}
      </ul>
      {(partial > 0 || v.untimedRecords > 0) && (
        <p className="text-[11.5px] text-amber-300">
          {partial > 0 &&
            `${partial} ${partial === 1 ? "intervalo com dados parciais (barra mais clara)" : "intervalos com dados parciais (barras mais claras)"}. `}
          {v.untimedRecords > 0 &&
            `${n(v.untimedRecords)} ${v.untimedRecords === 1 ? "registro sem horário entra" : "registros sem horário entram"} no total, fora do gráfico.`}
        </p>
      )}
    </div>
  );
}
