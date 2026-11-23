import { Bot, Archive, GitBranch } from "lucide-react";
import { useNavigate } from "react-router";
import type { SessionItem } from "@/lib/api";
import { sessionPath } from "@/lib/nav";
import { list } from "@/lib/api";
import { dateTime, plural, relativeTime } from "@/lib/format";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { FavoriteButton } from "./FavoriteButton";
import { ProviderTag } from "./ProviderTag";

export function SessionTitle({ title, className }: { title: string; className?: string }) {
  return title ? (
    <span className={cn("truncate", className)}>{title}</span>
  ) : (
    <span className={cn("truncate italic text-muted-foreground", className)}>Sem título</span>
  );
}

// Linha da listagem. Clicar seleciona a sessão e abre o painel de detalhes.
export function SessionRow({ session, showProject = true }: { session: SessionItem; showProject?: boolean }) {
  const selected = useAppStore((s) => s.selectedId === session.id);
  const select = useAppStore((s) => s.select);
  const navigate = useNavigate();
  const tags = list(session.tags);

  // Estrela e área da sessão são controles irmãos (sem botão dentro de botão).
  return (
    <div
      className={cn(
        "flex items-start gap-2 px-4 py-3 transition-colors duration-200",
        selected && "bg-primary/[0.07] shadow-[inset_2px_0_0_var(--primary),inset_0_0_0_1px_rgba(56,225,245,0.18)]",
      )}
    >
      <FavoriteButton id={session.id} favorite={session.favorite} />
      <button
        type="button"
        aria-current={selected || undefined}
        onClick={() => select(session.id)}
        onDoubleClick={() => navigate(sessionPath(session.id))}
        title="Clique para detalhes, duplo clique para abrir a conversa"
        className="flex min-w-0 flex-1 cursor-default flex-col gap-1 rounded-sm text-left outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <span className="flex items-center gap-2">
          <SessionTitle title={session.title} className="text-sm font-medium" />
          {session.subagent && <GitBranch className="size-3.5 shrink-0 text-muted-foreground" aria-label="Subagente" />}
          {session.automated && <Bot className="size-3.5 shrink-0 text-muted-foreground" aria-label="Automática" />}
          {session.archived && <Archive className="size-3.5 shrink-0 text-muted-foreground" aria-label="Arquivada" />}
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-faint">
          <ProviderTag provider={session.provider} />
          {showProject && session.projectName && (
            <span className="truncate font-mono text-muted-foreground" title={session.projectPath}>
              {session.projectName}
            </span>
          )}
          <span title={session.updatedAt ? dateTime(session.updatedAt) : undefined}>{relativeTime(session.updatedAt)}</span>
          <span>{plural(session.messageCount, "mensagem", "mensagens")}</span>
          {tags.map((t) => (
            <span key={t} className="rounded-md border border-accent2/25 bg-accent2/10 px-1.5 py-px text-[11px] text-accent2">
              #{t}
            </span>
          ))}
        </span>
      </button>
    </div>
  );
}
