// Formatação para exibição (datas em ms Unix; 0 = desconhecida).

const rtf = new Intl.RelativeTimeFormat("pt-BR", { numeric: "auto" });
const dtf = new Intl.DateTimeFormat("pt-BR", {
  dateStyle: "medium",
  timeStyle: "short",
});

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
];

export function relativeTime(ms: number, now = Date.now()): string {
  if (!ms) return "sem data";
  const diff = Math.round((ms - now) / 1000);
  for (const [unit, secs] of units) {
    if (Math.abs(diff) >= secs)
      return rtf.format(Math.round(diff / secs), unit);
  }
  return "agora";
}

export function dateTime(ms: number): string {
  return ms ? dtf.format(new Date(ms)) : "—";
}

export function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}
