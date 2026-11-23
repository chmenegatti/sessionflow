// Calendário de atividade: uma célula por dia (colunas = semanas, começando
// na segunda), intensidade pelo número de sessões. days vem denso, em ordem.
const weekdays = ["seg", "", "qua", "", "sex", "", "dom"];

export function CalendarGrid({ days, unit = "sessões" }: { days: { day: string; sessions: number; messages: number }[]; unit?: string }) {
  if (!days.length) return null;
  const max = Math.max(0, ...days.map((d) => d.sessions));
  const first = new Date(`${days[0].day}T12:00:00`);
  const offset = (first.getDay() + 6) % 7; // segunda = 0
  const cells: ({ day: string; sessions: number; messages: number } | null)[] = [...Array(offset).fill(null), ...days];
  const fmt = (day: string) => new Date(`${day}T12:00:00`).toLocaleDateString("pt-BR", { weekday: "short", day: "numeric", month: "short", year: "numeric" });
  return (
    <div className="overflow-x-auto">
      <div className="flex gap-2">
        <div className="grid grid-rows-7 gap-[3px] text-[10px] text-faint">
          {weekdays.map((w, i) => (
            <span key={i} className="flex h-3.5 items-center">
              {w}
            </span>
          ))}
        </div>
        <div className="grid grid-flow-col grid-rows-7 gap-[3px]" role="img" aria-label={`Calendário de atividade: ${days.filter((d) => d.sessions).length} dias com sessões`}>
          {cells.map((c, i) =>
            c ? (
              <span
                key={c.day}
                title={`${fmt(c.day)}: ${c.sessions.toLocaleString("pt-BR")} ${unit}, ${c.messages.toLocaleString("pt-BR")} mensagens`}
                className="size-3.5 rounded-[3px] border border-white/[0.04]"
                style={{
                  background: c.sessions ? `rgba(56,225,245,${0.15 + 0.85 * (c.sessions / max)})` : "rgba(255,255,255,0.03)",
                  boxShadow: c.sessions && c.sessions / max > 0.6 ? "0 0 8px rgba(56,225,245,0.45)" : undefined,
                }}
              />
            ) : (
              <span key={`pad-${i}`} className="size-3.5" />
            ),
          )}
        </div>
      </div>
      <div className="mt-3 flex items-center justify-end gap-2 text-[10.5px] text-faint">
        menos
        {[0.15, 0.4, 0.65, 1].map((a) => (
          <span key={a} className="size-3 rounded-[3px]" style={{ background: `rgba(56,225,245,${a})` }} />
        ))}
        mais · {unit} por dia
      </div>
    </div>
  );
}
