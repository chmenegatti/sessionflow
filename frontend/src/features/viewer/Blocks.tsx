import { useState, type ReactNode } from "react";
import { motion } from "motion/react";
import { AlertCircle, Brain, ChevronRight, FileText, Image, Info, Wrench, HelpCircle } from "lucide-react";
import { BlockKind, type BlockView } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Code, Markdown } from "./Markdown";

// Bloco recolhível (tool calls, resultados, raciocínio, originais). O
// conteúdo só é montado quando aberto: Markdown e destaque de sintaxe de
// blocos fechados não custam CPU nem DOM.
function Collapsible({
  icon,
  summary,
  children,
  tone,
  open,
}: {
  icon: ReactNode;
  summary: ReactNode;
  children: ReactNode;
  tone?: "error" | "subtle";
  open?: boolean;
}) {
  const [shown, setShown] = useState(!!open);
  return (
    <details
      open={open}
      onToggle={(e) => setShown(e.currentTarget.open)}
      className={cn(
        "group rounded-xl",
        tone !== "subtle" && "border bg-white/[0.025] shadow-[inset_0_1px_0_rgba(255,255,255,0.04)] transition-colors open:border-[var(--border-strong)] open:bg-white/[0.035]",
        tone === "error" && "border-negative/35 bg-negative/[0.06] open:border-negative/50 open:bg-negative/[0.08]",
      )}
    >
      <summary className={cn("flex cursor-default list-none items-center gap-2 px-2.5 text-xs", tone === "subtle" ? "py-0.5" : "py-1.5", " text-muted-foreground select-none hover:text-foreground [&::-webkit-details-marker]:hidden", tone !== "subtle" && "py-2")}>
        <ChevronRight className="size-3.5 shrink-0 transition-transform group-open:rotate-90 group-open:text-primary" />
        {icon}
        <span className="min-w-0 flex-1 truncate">{summary}</span>
      </summary>
      {shown && (
        <motion.div
          className="overflow-hidden border-t"
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: "auto", opacity: 1 }}
          transition={{ type: "spring", bounce: 0, duration: 0.28 }}
        >
          <div className="space-y-2 px-3 py-2.5">{children}</div>
        </motion.div>
      )}
    </details>
  );
}

function Truncated({ bytes }: { bytes: number }) {
  if (!bytes) return null;
  const kb = Math.max(1, Math.round(bytes / 1024));
  return <p className="text-[11px] text-muted-foreground">… {kb} KB omitidos na visualização (o original continua no histórico).</p>;
}

function Plain({ text }: { text: string }) {
  return <pre className="max-h-96 overflow-auto font-mono text-xs leading-normal whitespace-pre-wrap break-words">{text}</pre>;
}

function Chip({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <span className="inline-flex max-w-full items-center gap-1.5 rounded-lg border bg-white/[0.03] px-2 py-1 text-xs text-muted-foreground">
      {icon}
      <span className="truncate">{children}</span>
    </span>
  );
}

// Primeira linha não vazia, para o resumo de blocos recolhidos.
function preview(s: string, max = 90) {
  const line = s.split("\n").find((l) => l.trim()) ?? "";
  return line.length > max ? `${line.slice(0, max)}…` : line;
}

interface BlockProps {
  block: BlockView;
  plainText?: boolean; // texto do usuário: sem Markdown
  toolNames: Map<string, string>;
  expand?: boolean; // abre os recolhíveis (mensagem alvo da busca)
}

// Representação de um bloco, igual para todos os providers.
export function Block({ block: b, plainText, toolNames, expand }: BlockProps) {
  switch (b.kind) {
    case BlockKind.BlockText:
      return (
        <>
          {plainText ? <p className="text-sm whitespace-pre-wrap break-words">{b.text}</p> : <Markdown text={b.text} />}
          <Truncated bytes={b.truncated} />
        </>
      );
    case BlockKind.BlockCode:
      return <Code code={b.text} language={b.language} />;
    case BlockKind.BlockReasoning:
      return (
        <Collapsible open={expand} icon={<Brain className="size-3.5 text-accent2" />} summary="Raciocínio">
          <Markdown text={b.text} />
          <Truncated bytes={b.truncated} />
        </Collapsible>
      );
    case BlockKind.BlockToolCall:
      return (
        <Collapsible
          open={expand}
          icon={<Wrench className="size-3.5 text-accent2" />}
          summary={
            <>
              <span className="font-mono text-foreground">{b.toolName || "ferramenta"}</span>
              {b.toolInput && <span className="ml-2 font-mono">{preview(b.toolInput.replace(/[{}"]/g, " "))}</span>}
            </>
          }
        >
          {b.toolInput ? <Code code={b.toolInput} language="json" /> : <p className="text-xs text-muted-foreground">Sem parâmetros.</p>}
          <Truncated bytes={b.truncated} />
        </Collapsible>
      );
    case BlockKind.BlockToolResult: {
      const name = toolNames.get(b.toolCallId);
      return (
        <Collapsible
          open={expand}
          tone={b.isError ? "error" : undefined}
          icon={b.isError ? <AlertCircle className="size-3.5 text-negative" /> : <Wrench className="size-3.5 text-positive" />}
          summary={
            <>
              {b.isError ? "Erro" : "Resultado"}
              {name && <span className="ml-1 font-mono">· {name}</span>}
              {b.text && <span className="ml-2 font-mono">{preview(b.text)}</span>}
            </>
          }
        >
          {b.text ? <Plain text={b.text} /> : <p className="text-xs text-muted-foreground">Sem saída.</p>}
          <Truncated bytes={b.truncated} />
        </Collapsible>
      );
    }
    case BlockKind.BlockImageRef:
      return (
        <Chip icon={<Image className="size-3.5" />}>
          Imagem{b.text ? `: ${b.text}` : ""} {b.language && `(${b.language})`} — não exibida
        </Chip>
      );
    case BlockKind.BlockFile:
      return (
        <Chip icon={<FileText className="size-3.5" />}>
          Anexo: {b.text || "arquivo"} {b.language && `(${b.language})`}
        </Chip>
      );
    case BlockKind.BlockMetadata:
      // Linha discreta: metadados não competem com o conteúdo da conversa.
      return b.raw ? (
        <Collapsible tone="subtle" icon={<Info className="size-3.5" />} summary={<span className="font-mono">{b.text || "metadado"}</span>}>
          <Code code={b.raw} language="json" />
          <Truncated bytes={b.truncated} />
        </Collapsible>
      ) : (
        <p className="flex items-center gap-2 px-2.5 text-xs text-muted-foreground">
          <Info className="size-3.5" />
          <span className="font-mono">{b.text || "metadado"}</span>
        </p>
      );
    default:
      // Desconhecido (ou tipo novo): mostra o original preservado.
      return (
        <Collapsible icon={<HelpCircle className="size-3.5" />} summary={<>Evento não reconhecido: <span className="font-mono">{b.text || b.kind}</span></>}>
          {b.raw ? <Code code={b.raw} language="json" /> : <p className="text-xs text-muted-foreground">Original grande demais para exibir.</p>}
        </Collapsible>
      );
  }
}
