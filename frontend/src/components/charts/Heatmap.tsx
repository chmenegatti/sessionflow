import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

const days = ["seg", "ter", "qua", "qui", "sex", "sáb", "dom"];

// Mapa de calor dia da semana × hora (horário local). values[d][h], segunda = 0.
export function Heatmap({ values, unit }: { values: number[][]; unit: string }) {
  const max = Math.max(0, ...values.flat());
  return (
    <div className="overflow-x-auto">
      <div className="inline-grid min-w-full grid-cols-[2.5rem_repeat(24,minmax(14px,1fr))] gap-[3px] text-[10px] text-faint">
        <span />
        {Array.from({ length: 24 }, (_, h) => (
          <span key={h} className="text-center">
            {h % 3 === 0 ? `${h}h` : ""}
          </span>
        ))}
        {values.map((row, d) => (
          <Row key={d} label={days[d]} row={row} max={max} unit={unit} />
        ))}
      </div>
      <div className="mt-3 flex items-center justify-end gap-2 text-[10.5px] text-faint">
        menos
        {[0.08, 0.3, 0.55, 0.8, 1].map((a) => (
          <span key={a} className="size-3 rounded-[3px]" style={{ background: `rgba(56,225,245,${a})` }} />
        ))}
        mais · {unit}
      </div>
    </div>
  );
}

function Row({ label, row, max, unit }: { label: string; row: number[]; max: number; unit: string }) {
  return (
    <>
      <span className="self-center pr-1 text-right">{label}</span>
      {row.map((n, h) => (
        <Tooltip key={h}>
          <TooltipTrigger asChild>
            <span
              className="h-5 rounded-[3px] border border-white/[0.04]"
              style={{
                background: n ? `rgba(56,225,245,${0.12 + 0.88 * (n / max)})` : "rgba(255,255,255,0.03)",
                boxShadow: n && n / max > 0.6 ? "0 0 8px rgba(56,225,245,0.45)" : undefined,
              }}
            />
          </TooltipTrigger>
          <TooltipContent>
            {label}, {h}h–{h + 1}h: {n.toLocaleString("pt-BR")} {unit}
          </TooltipContent>
        </Tooltip>
      ))}
    </>
  );
}
