import type { Field, TokenSums } from "@/lib/api";

const compact = new Intl.NumberFormat("pt-BR", { notation: "compact", maximumFractionDigits: 1 });
const exact = new Intl.NumberFormat("pt-BR");

// "8,1 bi", "350 mil", "92". O valor exato vai na dica (exactTokens).
export const formatTokens = (n: number) => compact.format(n);
export const exactTokens = (n: number) => `${exact.format(n)} tokens`;

// Valor de um campo de consumo: nunca mostra 0 quando o provider não
// informou. records = registros no recorte.
export type FieldState =
  | { kind: "none" } // sem registros no recorte
  | { kind: "unavailable" } // nenhum registro informa o campo
  | { kind: "partial"; sum: number; coverage: number } // alguns informam
  | { kind: "value"; sum: number };

export function fieldState(f: Field, records: number): FieldState {
  if (records === 0) return { kind: "none" };
  if (f.count === 0) return { kind: "unavailable" };
  if (f.count < records) return { kind: "partial", sum: f.sum, coverage: f.count / records };
  return { kind: "value", sum: f.sum };
}

// Arredonda para baixo: cobertura de 99,6% não pode aparecer como 100%.
export const pct = (x: number) => `${x < 1 ? Math.min(99, Math.floor(x * 100)) : 100}%`;

// Variação contra o período anterior, só quando os dois períodos têm o campo
// completo (comparar parciais enganaria).
export function tokenDelta(cur: Field, curRecords: number, prev: Field, prevRecords: number): { text: string; up?: boolean } | null {
  if (!curRecords || !prevRecords || cur.count < curRecords || prev.count < prevRecords) return null;
  if (prev.sum === 0) return cur.sum > 0 ? { text: "novo", up: true } : null;
  if (cur.sum === prev.sum) return { text: "= 0%" };
  const p = ((cur.sum - prev.sum) / prev.sum) * 100;
  return { text: `${p > 0 ? "↑" : "↓"} ${Math.abs(p).toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%`, up: p > 0 };
}

export const fieldLabels: Record<string, string> = {
  input: "Entrada",
  output: "Saída",
  cache_read: "Cache lido",
  cache_creation: "Cache criado",
  reasoning: "Reasoning",
  total: "Total",
};

export const originLabels: Record<string, string> = {
  main_agent: "Agente principal",
  subagent: "Subagentes",
  automatic: "Sessões automáticas",
};

export type TokenKey = "input" | "output" | "cacheRead" | "cacheCreation" | "reasoning" | "total";
export const tokenField = (t: TokenSums, k: TokenKey): Field => t[k];

// Rótulo do eixo e do balão conforme o intervalo.
export function tick(bucket: string, ms: number) {
  const d = new Date(ms);
  if (bucket === "hour") return `${d.getHours()}h`;
  if (bucket === "month") return d.toLocaleDateString("pt-BR", { month: "short", year: "2-digit" }).replace(".", "");
  return d.toLocaleDateString("pt-BR", { day: "numeric", month: "short" }).replace(".", "");
}

export function tipDate(bucket: string, ms: number) {
  const d = new Date(ms);
  if (bucket === "hour") return `${d.toLocaleDateString("pt-BR", { day: "numeric", month: "short" }).replace(".", "")}, ${d.getHours()}h–${d.getHours() + 1}h`;
  if (bucket === "month") return d.toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
  const day = d.toLocaleDateString("pt-BR", { day: "numeric", month: "short" }).replace(".", "");
  if (bucket === "week") return `semana de ${day}`;
  return `${d.toLocaleDateString("pt-BR", { weekday: "short" }).replace(".", "")}, ${day}`;
}

// Texto do total de um grupo em rankings: "indisponível" quando nenhum
// registro do grupo informa o total (nunca "0").
export function groupTotal(t: TokenSums): { value: number; display?: string; title: string } {
  if (t.total.count === 0) return { value: 0, display: "indisponível", title: "Os registros deste grupo não informam o total de tokens." };
  const partial = t.total.count < t.records ? ` · total informado em ${t.total.count} de ${t.records} registros` : "";
  return { value: t.total.sum, display: partial ? `${formatTokens(t.total.sum)}*` : undefined, title: exactTokens(t.total.sum) + partial };
}

// Número em destaque (ex.: centro da rosca). Indisponível nunca vira 0;
// parcial leva a cobertura.
export function centerTotal(f: Field, records: number): { text: string; note?: string } {
  const s = fieldState(f, records);
  switch (s.kind) {
    case "none":
      return { text: "—" };
    case "unavailable":
      return { text: "indisponível" };
    case "partial":
      return { text: formatTokens(s.sum), note: `parcial · ${pct(s.coverage)}` };
    default:
      return { text: formatTokens(s.sum) };
  }
}

// Aviso de cobertura de um intervalo do gráfico de tokens: o gráfico só soma
// registros que informam o total, então o balão diz quando falta.
export function coverageNote(records: number, covered: number): { unavailable: boolean; text: string } | undefined {
  if (records === 0 || covered >= records) return undefined;
  if (covered === 0) return { unavailable: true, text: "total indisponível" };
  return { unavailable: false, text: `parcial · ${pct(covered / records)}` };
}
