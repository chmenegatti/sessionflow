import { memo, useState } from "react";
import { Bot, ChevronRight, Cog, User, Wrench } from "lucide-react";
import { Role, type MessageView } from "@/lib/api";
import type { Message as Msg } from "./useTranscript";
import { dateTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { providerColor, providerLabel } from "@/features/sessions/ProviderTag";
import { Block } from "./Blocks";

// Linha da conversa: uma mensagem, ou um grupo de eventos de sistema
// consecutivos (metadados, contexto injetado, desconhecidos) recolhido.
export type Row = { kind: "message"; message: Msg } | { kind: "system"; messages: Msg[] };

export function buildRows(messages: Msg[]): Row[] {
  const rows: Row[] = [];
  for (const m of messages) {
    if (m.role === Role.RoleSystem) {
      const last = rows[rows.length - 1];
      if (last?.kind === "system") last.messages.push(m);
      else rows.push({ kind: "system", messages: [m] });
    } else {
      rows.push({ kind: "message", message: m });
    }
  }
  return rows;
}

// Nome da ferramenta de cada chamada, para rotular os resultados.
export function toolNames(messages: MessageView[]): Map<string, string> {
  const m = new Map<string, string>();
  for (const msg of messages) {
    for (const b of msg.blocks ?? []) if (b.toolCallId && b.toolName) m.set(b.toolCallId, b.toolName);
  }
  return m;
}

function time(ms: number) {
  return ms ? new Date(ms).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" }) : "";
}

function Header({ icon, label, ts, className }: { icon: React.ReactNode; label: string; ts: number; className?: string }) {
  return (
    <div className={cn("flex items-center gap-2 text-xs", className)}>
      {icon}
      <span className="font-medium">{label}</span>
      {ts > 0 && (
        <time className="text-faint" title={dateTime(ts)} dateTime={new Date(ts).toISOString()}>
          {time(ts)}
        </time>
      )}
    </div>
  );
}

interface RowProps {
  row: Row;
  highlight?: boolean; // mensagem aberta a partir da busca
  provider: string;
  names: Map<string, string>;
}

export const RowView = memo(function RowView({ row, provider, names, highlight }: RowProps) {
  const evicted = row.kind === "system" ? row.messages[0].evicted : row.message.evicted;
  if (evicted) return <Evicted />;
  return row.kind === "system" ? (
    <SystemGroup messages={row.messages} names={names} />
  ) : (
    <Message message={row.message} provider={provider} names={names} highlight={highlight} />
  );
});

// Linha cujo conteúdo saiu da memória (conversa muito longa).
function Evicted() {
  return <p className="px-6 py-2 text-[11px] text-muted-foreground italic">Conteúdo liberado da memória — reabra a conversa para vê-lo.</p>;
}

// Eventos de sistema consecutivos, recolhidos; detalhes montados ao abrir.
function SystemGroup({ messages: ms, names }: { messages: MessageView[]; names: Map<string, string> }) {
  const [open, setOpen] = useState(false);
  const labels = [...new Set(ms.flatMap((m) => (m.blocks ?? []).map((b) => b.text || b.kind)))].slice(0, 4).join(", ");
  return (
    <details className="group px-6 py-1.5" onToggle={(e) => setOpen(e.currentTarget.open)}>
      <summary className="flex cursor-default list-none items-center gap-2 text-xs text-muted-foreground select-none hover:text-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 transition-transform group-open:rotate-90" />
        <Cog className="size-3.5" />
        {ms.length === 1 ? "1 evento de sistema" : `${ms.length} eventos de sistema`}
        <span className="truncate font-mono opacity-70">{labels}</span>
      </summary>
      {open && (
        <div className="mt-1.5 space-y-1.5 border-l border-accent2/25 pl-3">
          {ms.map((m) => (
            <div key={m.seq} className="space-y-1">
              {m.timestamp > 0 && <p className="text-[11px] text-muted-foreground">{dateTime(m.timestamp)}</p>}
              {(m.blocks ?? []).map((b, i) => (
                <Block key={i} block={b} toolNames={names} />
              ))}
            </div>
          ))}
        </div>
      )}
    </details>
  );
}

function Message({ message: m, provider, names, highlight }: { message: MessageView; provider: string; names: Map<string, string>; highlight?: boolean }) {
  const blocks = m.blocks ?? [];
  const user = m.role === Role.RoleUser;
  const tool = m.role === Role.RoleTool;
  return (
    <div className="px-6 py-2">
    <article
      aria-label={user ? "Mensagem do usuário" : tool ? "Resultado de ferramenta" : "Resposta do agente"}
      className={cn(
        "space-y-2",
        user && "surface rounded-2xl px-4 py-3",
        highlight &&
          "rounded-2xl border border-primary/50 px-4 py-3 shadow-[0_0_0_3px_rgba(56,225,245,0.10),0_0_36px_-8px_rgba(56,225,245,0.55)]",
      )}
    >
      {tool ? (
        <Header icon={<Wrench className="size-3.5 text-faint" />} label="Ferramenta" ts={m.timestamp} className="text-muted-foreground" />
      ) : (
        <Header
          icon={
            user ? (
              <span className="flex size-5 items-center justify-center rounded-md bg-white/[0.07]">
                <User className="size-3" />
              </span>
            ) : (
              <span
                className="flex size-5 items-center justify-center rounded-md"
                style={{ background: `color-mix(in oklab, ${providerColor(provider)} 18%, transparent)`, color: providerColor(provider) }}
              >
                <Bot className="size-3" />
              </span>
            )
          }
          label={user ? "Você" : providerLabel(provider)}
          ts={m.timestamp}
        />
      )}
      <div className="space-y-2">
        {blocks.map((b, i) => (
          <Block key={i} block={b} plainText={user} toolNames={names} expand={highlight} />
        ))}
        {blocks.length === 0 && <p className="text-xs text-muted-foreground italic">Mensagem sem conteúdo.</p>}
      </div>
    </article>
    </div>
  );
}

// Chave estável da linha (seq da primeira mensagem): preserva as medidas
// da virtualização quando páginas antigas saem da memória.
export function rowKey(row: Row): number {
  return row.kind === "system" ? row.messages[0].seq : row.message.seq;
}
