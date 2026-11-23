import type { CostSums } from "@/lib/api";
import { pct } from "./format";

// Valores monetários chegam como string decimal (Nanos); Number só para
// desenhar e calcular variação, nunca para exibir o valor.
export const amount = (n: unknown) => Number(n ?? 0);

// Texto fixo das telas de custo.
export const COST_NOTE = "Calculado com os preços que você cadastrou. Não é o valor cobrado; planos por assinatura não são cobrados por token.";

// Variação do total contra o período anterior, na mesma moeda.
export function costDelta(cur: CostSums, prev: CostSums[]): { text: string; up?: boolean } | null {
  const p = prev.find((s) => s.currency === cur.currency);
  if (!p) return null;
  const a = amount(cur.total);
  const b = amount(p.total);
  if (b === 0) return a > 0 ? { text: "novo", up: true } : null;
  if (a === b) return { text: "= 0%" };
  const d = ((a - b) / b) * 100;
  return { text: `${d > 0 ? "↑" : "↓"} ${Math.abs(d).toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%`, up: d > 0 };
}

// Cobertura da estimativa: quanto das respostas tem custo calculado.
export function coverageText(q: CostSums): string | undefined {
  if (!q.records) return undefined;
  const covered = q.complete + q.partial;
  if (covered < q.records) return `estimativa sobre ${pct(covered / q.records)} das respostas`;
  if (q.partial > 0) return `${q.partial} ${q.partial === 1 ? "resposta" : "respostas"} com custo parcial`;
  return undefined;
}

export function unitLabel(unit: number): string {
  if (unit === 1_000_000) return "por 1 milhão de tokens";
  if (unit === 1000) return "por mil tokens";
  return "por token";
}

// AAAA-MM-DD local de um horário ISO.
export const localDate = (iso: string) => new Date(iso).toLocaleDateString("sv-SE");

// Fim de vigência (exclusivo, meia-noite seguinte) como último dia inclusivo.
export function inclusiveEnd(iso: string | null | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  d.setDate(d.getDate() - 1);
  return d.toLocaleDateString("sv-SE");
}

export const shortDate = (iso: string) => new Date(iso).toLocaleDateString("pt-BR", { day: "numeric", month: "short", year: "numeric" }).replace(".", "");

// primaryCurrency: a moeda pela qual a linha entra no ranking (a primeira em
// ordem alfabética com custo; "" = sem custo). Igual ao backend
// (sqlite.CompareCostSums), que já devolve as linhas nessa ordem.
export function primaryCurrency(sums: CostSums[] | null | undefined): string {
  let best = "";
  for (const s of sums ?? []) if (s.currency && (!best || s.currency < best)) best = s.currency;
  return best;
}

// currencySections: o cabeçalho de seção de cada linha (null = sem
// cabeçalho). Com mais de uma moeda, cada moeda abre a sua seção; linhas
// sem custo abrem a seção "" depois das que têm custo. paged: a lista é uma
// página de várias (as outras podem ter outras moedas), então toda seção
// tem cabeçalho.
export function currencySections(rows: { sums: CostSums[] | null }[], paged = false): (string | null)[] {
  const keys = rows.map((r) => primaryCurrency(r.sums));
  const priced = new Set(keys.filter((k) => k !== ""));
  let prev: string | undefined;
  return keys.map((k) => {
    const open = k !== prev && (paged || (k === "" ? priced.size > 0 : priced.size > 1));
    prev = k;
    return open ? k : null;
  });
}
