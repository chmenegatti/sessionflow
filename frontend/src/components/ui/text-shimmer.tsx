// Adaptado de motion-primitives "text-shimmer" (MIT, motion-primitives.com;
// 21st.dev @ibelick). Mudança: sempre <span> — o original cria um
// componente motion novo a cada render (remonta o elemento).
import { memo, type CSSProperties } from "react"
import { motion, useReducedMotion } from "motion/react"
import { cn } from "cn"

function Shimmer({ children, className, duration = 1.8, spread = 2 }: { children: string; className?: string; duration?: number; spread?: number }) {
  // Animação contínua em JS: MotionConfig não a desliga, então checa aqui.
  if (useReducedMotion()) return <span className={cn("text-muted-foreground", className)}>{children}</span>
  return (
    <motion.span
      className={cn(
        "relative inline-block bg-[length:250%_100%,auto] bg-clip-text text-transparent",
        "[--base-color:#7b84ad] [--base-gradient-color:#e0fbff] [background-repeat:no-repeat,padding-box]",
        "[--bg:linear-gradient(90deg,#0000_calc(50%-var(--spread)),var(--base-gradient-color),#0000_calc(50%+var(--spread)))]",
        className
      )}
      initial={{ backgroundPosition: "100% center" }}
      animate={{ backgroundPosition: "0% center" }}
      transition={{ repeat: Infinity, duration, ease: "linear" }}
      style={
        {
          "--spread": `${children.length * spread}px`,
          backgroundImage: "var(--bg), linear-gradient(var(--base-color), var(--base-color))",
        } as CSSProperties
      }
    >
      {children}
    </motion.span>
  )
}

export const TextShimmer = memo(Shimmer)
