import * as React from "react"
import { cn } from "cn"

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <input
      type={type}
      data-slot="input"
      className={cn(
        "h-8 w-full min-w-0 rounded-[10px] border border-input bg-white/[0.04] px-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.05)] py-1 text-sm transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30 focus-visible:shadow-[0_0_20px_rgba(56,225,245,0.25)] disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive dark:bg-input/30",
        className
      )}
      {...props}
    />
  )
}

export { Input }
