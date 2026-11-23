import type { ReactNode } from "react";
import { AlertTriangle, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

// Card de gráfico do Analytics com os três estados obrigatórios: carregando,
// erro e vazio.
export function AnalyticsCard({
  title,
  hint,
  loading,
  error,
  empty,
  emptyText = "Sem dados no período.",
  onRetry,
  actions,
  className,
  children,
}: {
  title: string;
  hint?: ReactNode;
  loading?: boolean;
  error?: string | null;
  empty?: boolean;
  emptyText?: string;
  onRetry?: () => void;
  actions?: ReactNode;
  className?: string;
  children?: ReactNode;
}) {
  let body = children;
  if (error) {
    body = (
      <div className="flex flex-col items-center gap-2 py-8 text-center text-[13px] text-muted-foreground">
        <AlertTriangle className="size-5 text-negative" />
        <p>Não foi possível carregar: {error}</p>
        {onRetry && (
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RotateCw /> Tentar de novo
          </Button>
        )}
      </div>
    );
  } else if (loading) {
    body = <Skeleton className="h-40 rounded-xl" />;
  } else if (empty) {
    body = (
      <p className="py-8 text-center text-[13px] text-muted-foreground">
        {emptyText}
      </p>
    );
  }
  return (
    <section
      className={cn("surface rounded-[18px] px-5 py-4", className)}
      aria-label={title}
    >
      <div className="mb-3 flex items-start gap-3">
        <div className="min-w-0">
          <h3 className="text-[15px] font-semibold tracking-tight">{title}</h3>
          {hint && <p className="mt-0.5 text-[11.5px] text-faint">{hint}</p>}
        </div>
        {actions && <div className="ml-auto">{actions}</div>}
      </div>
      {body}
    </section>
  );
}
