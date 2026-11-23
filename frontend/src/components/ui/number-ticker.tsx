// Adaptado de Magic UI "number-ticker" (MIT, magicui.design; publicado no
// 21st.dev por @dillionverma). Mudanças: formato pt-BR, cor do tema e
// reanimação quando o valor muda (ex.: após a indexação); sem contagem
// com prefers-reduced-motion.
import { useEffect, useRef, type ComponentPropsWithoutRef } from "react"
import { useInView, useMotionValue, useReducedMotion, useSpring } from "motion/react"
import { cn } from "cn"

interface NumberTickerProps extends ComponentPropsWithoutRef<"span"> {
  value: number
  delay?: number
  decimals?: number // casas decimais exibidas (padrão 0)
}

export function NumberTicker({ value, delay = 0, decimals = 0, className, ...props }: NumberTickerProps) {
  const fmtRef = useRef(new Intl.NumberFormat("pt-BR", { maximumFractionDigits: decimals, minimumFractionDigits: decimals }))
  const ref = useRef<HTMLSpanElement>(null)
  const motionValue = useMotionValue(0)
  const spring = useSpring(motionValue, { damping: 60, stiffness: 100 })
  const inView = useInView(ref, { once: true })
  const reduce = useReducedMotion()

  // Antes do efeito que muda o valor: jump() dispara "change" na hora.
  useEffect(
    () =>
      spring.on("change", (latest) => {
        if (ref.current) ref.current.textContent = fmtRef.current.format(decimals ? latest : Math.round(latest))
      }),
    [spring]
  )

  useEffect(() => {
    if (reduce) {
      // Sem contagem: valor final direto.
      spring.jump(value)
      return
    }
    if (!inView) return
    const t = setTimeout(() => motionValue.set(value), delay * 1000)
    return () => clearTimeout(t)
  }, [motionValue, spring, inView, delay, value, reduce])

  return (
    <span ref={ref} className={cn("inline-block tabular-nums", className)} {...props}>
      0
    </span>
  )
}
