import type { ReactNode } from "react"
import { cn } from "cn"

// Controle segmentado (Aurora): pílulas num trilho translúcido; a ativa fica
// em acento a 16% com contorno. Um grupo de rádio para leitores de tela.
export function Segmented<T extends string>({
  value,
  options,
  onChange,
  size = "md",
  label,
  className,
}: {
  value: T
  options: { value: T; label: ReactNode }[]
  onChange: (v: T) => void
  size?: "sm" | "md"
  label?: string
  className?: string
}) {
  return (
    <div
      role="radiogroup"
      aria-label={label}
      className={cn("inline-flex gap-0.5 rounded-[10px] border bg-white/[0.04] p-[3px]", className)}
    >
      {options.map((o) => {
        const on = o.value === value
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={on}
            onClick={() => onChange(o.value)}
            className={cn(
              "rounded-[7px] font-medium whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
              size === "sm" ? "px-2 py-0.5 text-xs" : "px-2.5 py-1 text-[13px]",
              on && "bg-primary/15 text-primary shadow-[inset_0_0_0_1px_rgba(56,225,245,0.4)] hover:text-primary"
            )}
          >
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
