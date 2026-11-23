import type { SessionTimeline, TokenSums } from "@/lib/api";

// Componentes empilhados do gráfico. Reasoning fica de fora da pilha: no
// Codex vem dentro da saída e no OpenCode não se sabe (docs/ANALYTICS.md),
// então aparece só no detalhe, nunca somado de novo.
export const timelineKeys = [
  "input",
  "output",
  "cacheRead",
  "cacheCreation",
] as const;
export type TimelineKey = (typeof timelineKeys)[number];

export type TimelineBar = {
  start: number;
  stack: { key: TimelineKey; value: number }[];
  records: number;
  reasoning: number | null;
  cumulative: number;
  partial: boolean; // algum componente ou o total não veio em todos os registros
  resumed: boolean;
};

// timelineView prepara a linha do tempo da sessão para o gráfico: só os
// componentes que algum registro informou, um ponto por intervalo com
// registros (o backend não manda intervalo vazio).
export function timelineView(tl: SessionTimeline) {
  const total = tl.total as TokenSums;
  const keys = timelineKeys.filter((k) => total[k].count > 0);
  const points: TimelineBar[] = (tl.points ?? []).map((p) => {
    const t = p.tokens as TokenSums;
    return {
      start: p.start,
      stack: keys.map((key) => ({ key, value: t[key].sum })),
      records: t.records,
      reasoning: t.reasoning.count > 0 ? t.reasoning.sum : null,
      cumulative: p.cumulative,
      partial:
        keys.some((k) => t[k].count < t.records) || t.total.count < t.records,
      resumed: p.resumed,
    };
  });
  const max = Math.max(
    0,
    ...points.map((p) => p.stack.reduce((a, b) => a + b.value, 0)),
  );
  return {
    keys,
    points,
    max,
    total: total.total.sum,
    untimedRecords: (tl.untimed as TokenSums).records,
    resumes: tl.resumes,
    stepMs: tl.stepMs,
  };
}
