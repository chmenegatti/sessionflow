// Inspirado em motion-primitives "animated-background" (MIT,
// motion-primitives.com; 21st.dev @ibelick): um fundo que desliza até o
// item sob o mouse. Mudança: o original guarda o item ativo no estado do
// pai e clona todos os filhos, então cada hover re-renderiza a lista
// inteira. Aqui um único realce é posicionado por delegação de eventos e
// só ele re-renderiza (os filhos chegam como a mesma referência).
import { useRef, useState, type ReactNode } from "react"
import { AnimatePresence, motion, type Transition } from "motion/react"
import { cn } from "cn"

// Atributo que marca cada item da lista.
export const hoverItem = { "data-hover-item": "" } as const

interface Box {
  top: number
  height: number
}

export function AnimatedBackground({
  as = "div",
  children,
  className,
  highlightClassName,
  transition = { type: "spring", bounce: 0, duration: 0.25 },
}: {
  as?: "div" | "ul"
  children: ReactNode
  className?: string
  highlightClassName?: string
  transition?: Transition
}) {
  const ref = useRef<HTMLElement>(null)
  const [box, setBox] = useState<Box | null>(null)

  const onMouseOver = (e: React.MouseEvent) => {
    const item = (e.target as HTMLElement).closest<HTMLElement>("[data-hover-item]")
    if (!item || !ref.current?.contains(item)) return
    const top = item.offsetTop
    const height = item.offsetHeight
    setBox((b) => (b && b.top === top && b.height === height ? b : { top, height }))
  }

  // <li> dentro de <ul>: HTML válido. border-0 anula o divide-y no realce.
  const Highlight = as === "ul" ? motion.li : motion.div
  const Tag = as as "div"
  return (
    <Tag
      ref={ref as React.RefObject<HTMLDivElement>}
      className={cn("relative", className)}
      onMouseOver={onMouseOver}
      onMouseLeave={() => setBox(null)}
    >
      <AnimatePresence>
        {box && (
          <Highlight
            aria-hidden
            role="presentation"
            className={cn("pointer-events-none absolute inset-x-0 top-0 border-0", highlightClassName)}
            initial={{ opacity: 0, y: box.top, height: box.height }}
            animate={{ opacity: 1, y: box.top, height: box.height }}
            exit={{ opacity: 0 }}
            transition={transition}
          />
        )}
      </AnimatePresence>
      {children}
    </Tag>
  )
}
