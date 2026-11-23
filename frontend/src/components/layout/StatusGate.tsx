import type { ReactNode } from "react";
import { AlertTriangle, RotateCw } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/lib/store";
import type { AppStatus } from "@/lib/api";
import { EmptyState } from "./EmptyState";

// Renderiza loading/erro do status da aplicação e entrega o status pronto.
export function StatusGate({ children }: { children: (status: AppStatus) => ReactNode }) {
  const { status, state, error, loadStatus } = useAppStore();

  if (state === "error") {
    return (
      <EmptyState
        icon={AlertTriangle}
        title="Não foi possível carregar o estado da aplicação"
        description={error ?? undefined}
        action={
          <Button variant="outline" size="sm" onClick={() => void loadStatus()}>
            <RotateCw /> Tentar novamente
          </Button>
        }
      />
    );
  }
  if (!status) {
    return (
      <div className="mx-auto w-full max-w-5xl space-y-6 p-6" role="status" aria-label="Carregando">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} className="h-[74px] rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  return <>{children(status)}</>;
}
