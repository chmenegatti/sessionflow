import type { ReactNode } from "react";
import { useNavigate } from "react-router";
import {
  AlertTriangle,
  Copy,
  FolderGit2,
  GitBranch,
  MessagesSquare,
  MousePointerClick,
  Settings2,
  TerminalSquare,
  X,
} from "lucide-react";
import { motion } from "motion/react";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useAsync } from "@/hooks/useAsync";
import { Clipboard } from "@wailsio/runtime";
import {
  ResumeService,
  SessionService,
  list,
  type SessionDetail,
  type TagItem,
} from "@/lib/api";
import { dateTime, plural } from "@/lib/format";
import { useAppStore } from "@/lib/store";
import { projectPath, sessionPath } from "@/lib/nav";
import { FavoriteButton } from "@/features/sessions/FavoriteButton";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { SessionTitle } from "@/features/sessions/SessionRow";
import { TagEditor } from "@/features/sessions/TagEditor";
import { EmptyState } from "./EmptyState";
import { useResume } from "@/features/resume/useResume";

// Painel lateral com os detalhes da sessão selecionada.
export function DetailPanel() {
  const selectedId = useAppStore((s) => s.selectedId);
  const select = useAppStore((s) => s.select);
  return (
    <aside
      aria-label="Detalhes"
      className="glass flex w-80 shrink-0 flex-col border-l"
    >
      <div className="flex h-14 shrink-0 items-center border-b px-4 text-[13px] font-semibold">
        <span className="flex-1">Detalhes</span>
        {selectedId && (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="Limpar seleção"
            onClick={() => select(null)}
          >
            <X />
          </Button>
        )}
      </div>
      {selectedId ? (
        <SessionDetails id={selectedId} />
      ) : (
        <EmptyState
          icon={MousePointerClick}
          title="Nada selecionado"
          description="Selecione uma sessão para ver os detalhes."
        />
      )}
    </aside>
  );
}

function SessionDetails({ id }: { id: string }) {
  const version = useAppStore((s) => s.dataVersion);
  const detail = useAsync(() => SessionService.Get(id), [id, version]);
  const tags = useAsync(() => SessionService.Tags(), [version]);

  if (detail.error) {
    return (
      <EmptyState
        icon={AlertTriangle}
        title="Não foi possível carregar a sessão"
        description={detail.error}
        action={
          <Button variant="outline" size="sm" onClick={detail.reload}>
            Tentar novamente
          </Button>
        }
      />
    );
  }
  if (!detail.data || detail.data.session.id !== id) {
    return (
      <div className="space-y-4 p-4" role="status" aria-label="Carregando">
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-5 w-16 rounded-full" />
        <Skeleton className="h-7 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }
  // Troca de sessão: o conteúdo entra com fade (key = sessão).
  return (
    <motion.div
      key={id}
      className="flex min-h-0 flex-1 flex-col"
      initial={{ opacity: 0, x: 8 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.2, ease: "easeOut" }}
    >
      <Details d={detail.data} known={list(tags.data)} />
    </motion.div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[6.5rem_1fr] gap-2 py-1">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

function Details({ d, known }: { d: SessionDetail; known: TagItem[] }) {
  const s = d.session;
  const navigate = useNavigate();
  const select = useAppStore((st) => st.select);
  const kind = s.subagent
    ? "Subagente"
    : s.automated
      ? "Automática (sem prompt humano)"
      : "Sessão principal";

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="space-y-5 p-4">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1 space-y-1.5">
            <h2 className="text-sm leading-snug font-medium break-words">
              <SessionTitle title={s.title} className="whitespace-normal" />
            </h2>
            <div className="flex flex-wrap items-center gap-1.5">
              <ProviderTag provider={s.provider} />
              {s.archived && (
                <span className="text-xs text-muted-foreground">arquivada</span>
              )}
            </div>
          </div>
          <FavoriteButton id={s.id} favorite={s.favorite} size="icon-sm" />
        </div>

        <Button
          size="sm"
          className="w-full"
          onClick={() => navigate(sessionPath(s.id))}
        >
          <MessagesSquare /> Abrir conversa
        </Button>

        <ResumeSection id={s.id} />

        {s.projectPath && (
          <Button
            variant="outline"
            size="sm"
            className="w-full justify-start"
            title={s.projectPath}
            onClick={() => navigate(projectPath(s.projectPath))}
          >
            <FolderGit2 /> <span className="truncate">{s.projectName}</span>
          </Button>
        )}

        <section aria-label="Tags" className="space-y-2">
          <h3 className="text-xs font-medium text-muted-foreground">Tags</h3>
          <TagEditor id={s.id} tags={list(s.tags)} known={known} />
        </section>

        <dl className="text-xs">
          <Field label="Tipo">{kind}</Field>
          {d.parentId && (
            <Field label="Sessão pai">
              <Button
                variant="link"
                size="xs"
                className="h-auto p-0"
                onClick={() => select(d.parentId)}
              >
                <GitBranch /> abrir sessão pai
              </Button>
            </Field>
          )}
          {d.children > 0 && <Field label="Subagentes">{d.children}</Field>}
          <Field label="Atualizada">{dateTime(s.updatedAt)}</Field>
          <Field label="Criada">{dateTime(s.createdAt)}</Field>
          <Field label="Mensagens">
            {plural(s.messageCount, "mensagem", "mensagens")}
          </Field>
          <Field label="Modelo">{s.model || "—"}</Field>
          <Field label="ID">
            <code className="font-mono text-[11px] select-all">
              {d.nativeId}
            </code>
          </Field>
          <Field label="Projeto">
            <span className="font-mono text-[11px]">
              {s.projectPath || "—"}
            </span>
          </Field>
          <Field label="Origem">
            <span className="font-mono text-[11px]" title={d.sourceKind}>
              {d.sourcePath}
            </span>
          </Field>
        </dl>

        {d.firstPrompt && (
          <section aria-label="Primeiro prompt" className="space-y-1.5">
            <h3 className="text-xs font-medium text-muted-foreground">
              Primeiro prompt
            </h3>
            <p className="rounded-md bg-muted/50 p-2 text-xs break-words whitespace-pre-wrap">
              {d.firstPrompt}
            </p>
          </section>
        )}
      </div>
    </ScrollArea>
  );
}

// copyCommand copia o comando da retomada (o diretório vai no aviso: o
// comando precisa rodar nele).
async function copyCommand(command: string, dir: string) {
  const notify = useAppStore.getState().notify;
  try {
    await Clipboard.SetText(command);
    notify(`Comando copiado. Rode em ${dir}.`);
  } catch {
    notify("Não foi possível copiar o comando.", "error");
  }
}

// Retomada pela CLI: mostra o comando exato (ou por que não dá) antes de
// abrir o terminal.
function ResumeSection({ id }: { id: string }) {
  const version = useAppStore((s) => s.dataVersion);
  const navigate = useNavigate();
  const plan = useAsync(() => ResumeService.Plan(id), [id, version]);
  const { resume } = useResume();

  if (plan.loading && !plan.data) return <Skeleton className="h-8 w-full" />;
  if (plan.error) {
    return (
      <div className="space-y-1.5 rounded-md border border-dashed p-2.5 text-xs text-muted-foreground">
        <p className="flex items-start gap-1.5">
          <TerminalSquare className="mt-0.5 size-3.5 shrink-0" /> {plan.error}
        </p>
        {/executável|terminal/i.test(plan.error) && (
          <Button
            variant="link"
            size="xs"
            className="h-auto p-0"
            onClick={() => navigate("/settings")}
          >
            <Settings2 /> Configurações
          </Button>
        )}
      </div>
    );
  }
  if (!plan.data) return null;
  const p = plan.data;
  return (
    <div className="space-y-1.5">
      {p.copyOnly ? (
        <>
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            onClick={() => void copyCommand(p.command, p.dir)}
          >
            <Copy /> Copiar comando
          </Button>
          <p className="text-[11px] text-muted-foreground">{p.copyReason}</p>
        </>
      ) : (
        <Button
          variant="outline"
          size="sm"
          className="w-full"
          onClick={() => void resume(id)}
        >
          <TerminalSquare /> Retomar no terminal
          <kbd className="ml-auto font-mono text-[10px] text-muted-foreground">
            Ctrl ↵
          </kbd>
        </Button>
      )}
      <code
        className="block truncate rounded bg-muted/50 px-2 py-1 font-mono text-[10.5px] text-muted-foreground"
        title={`${plan.data.command}\nem ${plan.data.dir}\nterminal: ${plan.data.terminal}`}
      >
        {plan.data.command}
      </code>
    </div>
  );
}
