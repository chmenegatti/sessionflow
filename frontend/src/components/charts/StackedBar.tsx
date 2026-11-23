import { sharePct } from "./scale";

// Parte da barra. value (número) só desenha a largura; share e text, quando
// vêm prontos (valores monetários, calculados em decimal), são a legenda.
export type BarPart = {
  label: string;
  value: number;
  color: string;
  share?: string;
  text?: string;
};

// Barra horizontal 100% com legenda (composição de um total).
export function StackedBar({
  parts,
  format,
}: {
  parts: BarPart[];
  format: (n: number) => string;
}) {
  const total = parts.reduce((n, p) => n + p.value, 0);
  return (
    <div className="flex flex-col gap-3">
      <div
        className="flex h-3 overflow-hidden rounded-full bg-white/[0.07]"
        role="img"
        aria-label={parts
          .map((p) => `${p.label}: ${p.text ?? format(p.value)}`)
          .join(", ")}
      >
        {total > 0 &&
          parts.map((p) => (
            <span
              key={p.label}
              className="h-full first:rounded-l-full last:rounded-r-full"
              style={{
                width: `${(p.value / total) * 100}%`,
                background: p.color,
                boxShadow: `0 0 10px ${p.color}`,
              }}
            />
          ))}
      </div>
      <ul className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-[12px] sm:grid-cols-4">
        {parts.map((p) => (
          <li
            key={p.label}
            className="flex items-center gap-2 text-muted-foreground"
          >
            <span
              className="size-2 shrink-0 rounded-[3px]"
              style={{ background: p.color }}
            />
            <span className="truncate">{p.label}</span>
            <span className="ml-auto font-medium text-foreground tabular-nums">
              {p.share ?? sharePct(p.value, total)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
