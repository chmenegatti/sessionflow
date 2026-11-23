// Adaptado de Magic UI "blur-fade" (MIT, magicui.design; 21st.dev
// @dillionverma). Entrada com desfoque + deslocamento. Mudança: sem
// AnimatePresence (não há saída animada aqui).
import { useRef } from "react"
import { motion, useInView, type MotionProps, type Variants } from "motion/react"

interface BlurFadeProps extends MotionProps {
  children: React.ReactNode
  className?: string
  duration?: number
  delay?: number
  offset?: number
  direction?: "up" | "down" | "left" | "right"
  inView?: boolean
  blur?: string
}

export function BlurFade({
  children,
  className,
  duration = 0.35,
  delay = 0,
  offset = 6,
  direction = "down",
  inView = false,
  blur = "6px",
  ...props
}: BlurFadeProps) {
  const ref = useRef(null)
  const seen = useInView(ref, { once: true, margin: "-40px" })
  const visible = !inView || seen
  const axis = direction === "left" || direction === "right" ? "x" : "y"
  const variants: Variants = {
    hidden: { [axis]: direction === "right" || direction === "down" ? -offset : offset, opacity: 0, filter: `blur(${blur})` },
    visible: { [axis]: 0, opacity: 1, filter: "blur(0px)" },
  }
  return (
    <motion.div
      ref={ref}
      initial="hidden"
      animate={visible ? "visible" : "hidden"}
      variants={variants}
      transition={{ delay: 0.04 + delay, duration, ease: "easeOut" }}
      className={className}
      {...props}
    >
      {children}
    </motion.div>
  )
}
