import { cn } from "cn"

// Placeholder de carregamento com brilho deslizante (respeita
// prefers-reduced-motion via CSS).
export function Skeleton({ className }: { className?: string }) {
  return <div aria-hidden className={cn("skeleton rounded-md bg-muted/60", className)} />
}

// Linhas de lista em carregamento.
export function SkeletonRows({ rows = 6 }: { rows?: number }) {
  return (
    <div role="status" aria-label="Carregando" className="divide-y">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-start gap-3 px-4 py-3" style={{ opacity: 1 - i * 0.12 }}>
          <Skeleton className="mt-0.5 size-4 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-2/5" />
            <Skeleton className="h-3 w-3/5" />
          </div>
        </div>
      ))}
    </div>
  )
}
