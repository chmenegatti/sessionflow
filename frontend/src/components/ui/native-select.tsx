import * as React from "react"
import { cn } from "cn"

// <select> nativo com o visual dos demais controles.
function NativeSelect({ className, ...props }: React.ComponentProps<"select">) {
  return (
    <select
      data-slot="native-select"
      className={cn(
        "h-7 rounded-[10px] border border-input bg-white/[0.04] px-2 text-[0.8rem] outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 [&>option]:bg-[#0c1126]",
        className
      )}
      {...props}
    />
  )
}

export { NativeSelect }
