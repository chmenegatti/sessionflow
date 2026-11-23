import type { Dashboard } from "@/lib/api";

export interface Segment {
  text: string;
  bold?: boolean;
}

const fmt = (n: number) => n.toLocaleString("pt-BR");
const plural = (n: number, one: string, many: string) => `${fmt(n)} ${n === 1 ? one : many}`;

// Rótulo do período em frase ("Nos últimos 30 dias").
export function periodPhrase(period: string): string {
  const days: Record<string, number> = { "7d": 7, "30d": 30, "90d": 90 };
  return days[period] ? `nos últimos ${days[period]} dias` : "em todo o histórico";
}

// Quando foi o pico, conforme o tamanho do intervalo.
function peakPhrase(d: Dashboard): string {
  const at = new Date(d.peak);
  if (d.bucket === "month") return `em ${at.toLocaleDateString("pt-BR", { month: "long", year: "numeric" })}`;
  const day = at.toLocaleDateString("pt-BR", { day: "numeric", month: "long" });
  return d.bucket === "week" ? `na semana de ${day}` : `em ${day}`;
}

// Frase-resumo do Dashboard em segmentos (os números vão em negrito). Partes
// que não se aplicam somem: sem atividade, sem concentração com um agente
// só, sem "abertas" quando não há.
export function summary(d: Dashboard, names: Record<string, string>): Segment[] {
  const when = periodPhrase(d.period);
  if (d.messages.value === 0) {
    return [{ text: `Nenhuma atividade ${when}.` }];
  }
  const out: Segment[] = [
    { text: `${when[0].toUpperCase()}${when.slice(1)} você trabalhou em ` },
    { text: plural(d.sessions.value, "sessão", "sessões"), bold: true },
    { text: d.activeProjects.value === 1 ? " em " : " distribuídas por " },
    { text: plural(d.activeProjects.value, "projeto", "projetos"), bold: true },
    { text: ", com " },
    { text: plural(d.messages.value, "mensagem", "mensagens"), bold: true },
    { text: " trocadas." },
  ];
  // Partes opcionais, ligadas por vírgula e "e"; a primeira começa a frase.
  const extra: Segment[][] = [];
  const shares = (d.byProvider ?? []).filter((p) => p.sessions > 0);
  if (shares.length >= 2) {
    const top = [...shares].sort((a, b) => b.sessions - a.sessions)[0];
    const total = shares.reduce((n, p) => n + p.sessions, 0);
    extra.push([
      { text: "o " },
      { text: names[top.provider] ?? top.provider, bold: true },
      { text: ` concentrou ${Math.round((top.sessions / total) * 100)}% das sessões` },
    ]);
  }
  if (d.peak > 0) extra.push([{ text: `o pico foi ${peakPhrase(d)}` }]);
  if (d.openNow > 0) {
    extra.push([
      { text: plural(d.openNow, "sessão", "sessões"), bold: true },
      { text: d.openNow === 1 ? " está aberta agora" : " estão abertas agora" },
    ]);
  }
  if (extra.length) {
    const first = extra[0][0];
    if (!first.bold) extra[0][0] = { text: first.text[0].toUpperCase() + first.text.slice(1) };
    out.push({ text: " " });
    extra.forEach((part, i) => {
      if (i > 0) out.push({ text: i === extra.length - 1 ? " e " : ", " });
      out.push(...part);
    });
    out.push({ text: "." });
  }
  return out;
}
