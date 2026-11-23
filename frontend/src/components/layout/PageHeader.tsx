import type { ReactNode } from "react";
import { PanelRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAppStore } from "@/lib/store";

export function PageHeader({ title, actions, crumbs }: { title: string; actions?: ReactNode; crumbs?: ReactNode }) {
  const toggleDetail = useAppStore((s) => s.toggleDetail);
  const detailOpen = useAppStore((s) => s.detailOpen);
  return (
    <header className="glass relative z-10 flex h-14 shrink-0 items-center gap-3 border-b px-6">
      <h1 className="flex-1 truncate text-[13px] font-medium text-muted-foreground">
        {crumbs ?? <span className="text-foreground">{title}</span>}
      </h1>
      {actions}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="Painel de detalhes"
            aria-pressed={detailOpen}
            onClick={toggleDetail}
          >
            <PanelRight />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Painel de detalhes</TooltipContent>
      </Tooltip>
    </header>
  );
}
