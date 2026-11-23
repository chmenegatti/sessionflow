import type { LucideIcon } from "lucide-react";
import { NumberTicker } from "@/components/ui/number-ticker";
import { Sparkline } from "@/components/charts/Sparkline";
import type { Metric } from "@/lib/api";
import { cn } from "@/lib/utils";

// Variação contra o período anterior: % (ou "novo" se antes era zero).
function delta(m: Metric): { text: string; up?: boolean } | null {
  if (!m.hasPrev) return null;
  if (m.prev === 0) return m.value > 0 ? { text: "novo", up: true } : null;
  if (m.value === m.prev) return { text: "= 0%" };
  const pct = ((m.value - m.prev) / m.prev) * 100;
  return { text: `${pct > 0 ? "↑" : "↓"} ${Math.abs(pct).toLocaleString("pt-BR", { maximumFractionDigits: 1 })}%`, up: pct > 0 };
}

export function KpiCard({
  icon: Icon,
  label,
  value,
  metric,
  badge,
  meta,
  series,
  color,
  selected,
  onSelect,
  decimals,
}: {
  icon: LucideIcon;
  label: string;
  value: number;
  metric?: Metric; // variação percentual
  badge?: string; // variação pronta (ex.: favoritas "↑ 2")
  meta: string;
  series: number[];
  color: string;
  selected?: boolean;
  onSelect?: () => void;
  decimals?: number;
}) {
  const d = metric ? delta(metric) : badge ? { text: badge, up: true } : null;
  const Root = onSelect ? "button" : "div";
  return (
    <Root
      type={onSelect ? "button" : undefined}
      onClick={onSelect}
      aria-pressed={onSelect ? selected : undefined}
      className={cn(
        "surface flex flex-col overflow-hidden rounded-[18px] px-4 pt-3.5 text-left transition-[border-color,box-shadow,transform] duration-200",
        onSelect && "outline-none hover:-translate-y-0.5 hover:border-[var(--border-strong)] focus-visible:ring-2 focus-visible:ring-ring/50",
        selected && "surface-sel",
      )}
    >
      <span className="flex items-center gap-2 text-[13px] font-medium text-muted-foreground">
        <Icon className="size-4 opacity-70" /> {label}
      </span>
      <NumberTicker value={value} decimals={decimals} className="mt-2 text-[32px] leading-none font-semibold tracking-tight" />
      <span className="mt-2 flex items-center gap-2 text-[11.5px] text-faint">
        {d && (
          <span className={cn("rounded-md px-1.5 py-0.5 text-[11px] font-semibold", d.up === undefined ? "bg-white/[0.06] text-muted-foreground" : d.up ? "bg-positive/15 text-positive" : "bg-negative/15 text-negative")}>
            {d.text}
          </span>
        )}
        <span className="truncate">{meta}</span>
      </span>
      <span className="-mx-4 mt-2.5 block">
        <Sparkline values={series} color={color} />
      </span>
    </Root>
  );
}
