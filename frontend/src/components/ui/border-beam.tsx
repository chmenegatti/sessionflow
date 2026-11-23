// Adaptado de Magic UI "border-beam" (MIT, magicui.design; 21st.dev
// @dillionverma). Feixe que percorre a borda do elemento pai (que precisa
// de position: relative e border-radius). Mudança: cores do app.
import { motion, useReducedMotion, type MotionStyle } from "motion/react"
import { cn } from "cn"

export function BorderBeam({
  className,
  size = 80,
  duration = 6,
  colorFrom = "#38e1f5",
  colorTo = "#a78bfa",
  borderWidth = 1,
}: {
  className?: string
  size?: number
  duration?: number
  colorFrom?: string
  colorTo?: string
  borderWidth?: number
}) {
  // Laço infinito animado em JS: sem feixe quando o sistema pede menos movimento.
  if (useReducedMotion()) return null
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0 rounded-[inherit] border-(length:--border-beam-width) border-transparent mask-[linear-gradient(transparent,transparent),linear-gradient(#000,#000)] mask-intersect [mask-clip:padding-box,border-box]"
      style={{ "--border-beam-width": `${borderWidth}px` } as React.CSSProperties}
    >
      <motion.div
        className={cn("absolute aspect-square bg-linear-to-l from-(--color-from) via-(--color-to) to-transparent", className)}
        style={
          {
            width: size,
            offsetPath: `rect(0 auto auto 0 round ${size}px)`,
            "--color-from": colorFrom,
            "--color-to": colorTo,
          } as MotionStyle
        }
        initial={{ offsetDistance: "0%" }}
        animate={{ offsetDistance: ["0%", "100%"] }}
        transition={{ repeat: Infinity, ease: "linear", duration }}
      />
    </div>
  )
}
