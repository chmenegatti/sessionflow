import { cn } from "@/lib/utils";

// Ranking em barras horizontais (projetos, ferramentas).
export function BarList({
  items,
  color,
  format = (n: number) => n.toLocaleString("pt-BR"),
}: {
  items: { label: string; value: number; title?: string; onClick?: () => void; display?: string }[];
  color: "accent" | "accent2";
  format?: (n: number) => string;
}) {
  const max = Math.max(0, ...items.map((i) => i.value));
  const bar =
    color === "accent"
      ? "from-primary/60 to-primary shadow-[0_0_14px_rgba(56,225,245,0.45)]"
      : "from-accent2/60 to-accent2 shadow-[0_0_14px_rgba(167,139,250,0.45)]";
  return (
    <ul className="flex flex-col gap-2.5">
      {items.map((it) => {
        const Row = it.onClick ? "button" : "div";
        return (
          <li key={it.title ?? it.label}>
            <Row
              type={it.onClick ? "button" : undefined}
              onClick={it.onClick}
              title={it.title ?? it.label}
              className={cn(
                "grid w-full grid-cols-[minmax(0,8rem)_1fr_3.5rem] items-center gap-3 rounded-md text-left text-[13px]",
                it.onClick && "outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
              )}
            >
              <span className="truncate text-muted-foreground">{it.label}</span>
              <span className="h-2 overflow-hidden rounded-full bg-white/[0.07]">
                <span className={cn("block h-full rounded-full bg-linear-to-r", bar)} style={{ width: `${max ? (it.value / max) * 100 : 0}%` }} />
              </span>
              <span className={cn("text-right font-medium tabular-nums", it.display && "text-[11.5px] font-normal text-faint")}>{it.display ?? format(it.value)}</span>
            </Row>
          </li>
        );
      })}
    </ul>
  );
}
