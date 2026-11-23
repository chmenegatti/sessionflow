import { Link, useSearchParams } from "react-router";
import { ArrowLeft, FolderGit2, MessagesSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/layout/PageHeader";
import { EmptyState } from "@/components/layout/EmptyState";
import { SessionList } from "@/features/sessions/SessionList";

// Sessões de um projeto, de todos os providers.
export function ProjectSessionsPage() {
  const [params] = useSearchParams();
  const path = params.get("path") ?? "";
  const name = path.split("/").filter(Boolean).pop() ?? path;

  return (
    <>
      <PageHeader
        title={name || "Projeto"}
        crumbs={
          <>
            <Link to="/projects" className="transition-colors hover:text-foreground">
              Projetos
            </Link>
            <span className="mx-2 text-faint">/</span>
            <span className="text-foreground">{name || "Projeto"}</span>
          </>
        }
        actions={
          <Button variant="ghost" size="sm" asChild>
            <Link to="/projects">
              <ArrowLeft /> Projetos
            </Link>
          </Button>
        }
      />
      {path ? (
        <>
          <div className="flex items-center gap-3 px-6 pt-6">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-xl border border-primary/20 bg-linear-to-br from-primary/20 to-accent2/15 text-primary shadow-[0_0_20px_-6px_rgba(56,225,245,0.6)]">
              <FolderGit2 className="size-5" />
            </span>
            <div className="min-w-0">
              <h2 className="title-gradient truncate text-[22px] font-semibold tracking-tight">{name}</h2>
              <p className="truncate font-mono text-[11.5px] text-faint" title={path}>
                {path}
              </p>
            </div>
          </div>
          <SessionList
            key={path}
            base={{ project: path }}
            showProject={false}
            empty={{
              icon: MessagesSquare,
              title: "Nenhuma sessão neste projeto",
              description: "Ajuste os filtros ou ative a exibição de subagentes e automáticas.",
            }}
          />
        </>
      ) : (
        <EmptyState icon={FolderGit2} title="Projeto não informado" description="Escolha um projeto na lista." />
      )}
    </>
  );
}
