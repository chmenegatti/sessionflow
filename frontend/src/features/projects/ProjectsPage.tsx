import { useNavigate } from "react-router";
import { AlertTriangle, ChevronRight, FolderGit2, RotateCw } from "lucide-react";
import { BlurFade } from "@/components/ui/blur-fade";
import { SkeletonRows } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/layout/PageHeader";
import { EmptyState } from "@/components/layout/EmptyState";
import { useAsync } from "@/hooks/useAsync";
import { SessionService, list } from "@/lib/api";
import { plural, relativeTime } from "@/lib/format";
import { projectPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { ProviderTag } from "@/features/sessions/ProviderTag";

export function ProjectsPage() {
  const version = useAppStore((s) => s.dataVersion);
  const projects = useAsync(() => SessionService.Projects(), [version]);
  const navigate = useNavigate();
  const items = list(projects.data);

  let body;
  if (projects.error) {
    body = (
      <EmptyState
        icon={AlertTriangle}
        title="Não foi possível carregar os projetos"
        description={projects.error}
        action={
          <Button variant="outline" size="sm" onClick={projects.reload}>
            <RotateCw /> Tentar novamente
          </Button>
        }
      />
    );
  } else if (projects.loading && !projects.data) {
    body = (
      <div className="px-6 py-6">
        <div className="surface overflow-hidden rounded-[18px]">
          <SkeletonRows rows={8} />
        </div>
      </div>
    );
  } else if (items.length === 0) {
    body = (
      <EmptyState
        icon={FolderGit2}
        title="Nenhum projeto"
        description="Projetos aparecem quando há sessões indexadas em um diretório."
      />
    );
  } else {
    body = (
      <ul className="grid min-h-0 flex-1 auto-rows-min grid-cols-1 gap-4 overflow-y-auto px-6 py-6 md:grid-cols-2 xl:grid-cols-3">
        {items.map((p, i) => (
          <li key={p.path}>
            <BlurFade delay={i < 12 ? i * 0.03 : 0} offset={4} blur="4px" className="h-full">
              <button
                type="button"
                onClick={() => navigate(projectPath(p.path))}
                title={p.path}
                className="surface group flex h-full w-full flex-col gap-3 rounded-[18px] px-4 py-4 text-left transition-[border-color,transform,box-shadow] duration-200 outline-none hover:-translate-y-0.5 hover:border-[var(--border-strong)] focus-visible:ring-2 focus-visible:ring-ring/50"
              >
                <span className="flex items-center gap-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-[10px] border border-primary/20 bg-linear-to-br from-primary/20 to-accent2/15 text-primary shadow-[0_0_18px_-6px_rgba(56,225,245,0.6)]">
                    <FolderGit2 className="size-4" />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[14px] font-semibold tracking-tight">{p.name}</span>
                    <span className="block truncate font-mono text-[11px] text-faint">{p.path}</span>
                  </span>
                  <ChevronRight className="size-4 shrink-0 text-faint transition-transform group-hover:translate-x-0.5 group-hover:text-primary" />
                </span>
                <span className="mt-auto flex items-center gap-1.5">
                  {list(p.providers).map((id) => (
                    <ProviderTag key={id} provider={id} />
                  ))}
                  <span className="ml-auto text-right text-[11.5px] text-faint">
                    <span className="text-muted-foreground">{plural(p.sessions, "sessão", "sessões")}</span> · {relativeTime(p.lastActivity)}
                  </span>
                </span>
              </button>
            </BlurFade>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <>
      <PageHeader title={projects.data ? `Projetos (${items.length})` : "Projetos"} />
      {body}
    </>
  );
}
