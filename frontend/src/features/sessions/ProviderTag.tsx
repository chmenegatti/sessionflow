import { cn } from "@/lib/utils";

// Rótulo curto e cor de cada provider (apresentação apenas). Classes
// literais: o Tailwind só gera o que aparece escrito no código.
const meta: Record<string, { label: string; name: string; color: string; tag: string }> = {
  claude: { label: "Claude", name: "Claude Code", color: "var(--claude)", tag: "border-claude/30 bg-claude/15 text-claude" },
  codex: { label: "Codex", name: "Codex", color: "var(--codex)", tag: "border-codex/30 bg-codex/15 text-codex" },
  opencode: { label: "OpenCode", name: "OpenCode", color: "var(--opencode)", tag: "border-opencode/30 bg-opencode/15 text-opencode" },
};

export const providerLabel = (id: string) => meta[id]?.label ?? id;
export const providerName = (id: string) => meta[id]?.name ?? id;
export const providerColor = (id: string) => meta[id]?.color ?? "var(--muted-foreground)";

export function ProviderTag({ provider, className }: { provider: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center rounded-md border px-1.5 text-[11px] font-medium whitespace-nowrap",
        meta[provider]?.tag ?? "border-border bg-secondary text-secondary-foreground",
        className,
      )}
    >
      {providerLabel(provider)}
    </span>
  );
}

// Pílula de filtro por agente: ponto na cor do agente, brilha quando ligada.
export function ProviderPill({
  provider,
  active,
  onClick,
  count,
  disabled,
  title,
}: {
  provider: string;
  active: boolean;
  onClick: () => void;
  count?: number;
  disabled?: boolean;
  title?: string;
}) {
  const color = providerColor(provider);
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={cn(
        "flex h-8 items-center gap-2 rounded-full border px-3 text-[12.5px] transition-all outline-none focus-visible:ring-2 focus-visible:ring-ring/50 disabled:cursor-default",
        active
          ? "border-[var(--border-strong)] bg-white/[0.06] text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.06)]"
          : "text-faint hover:text-muted-foreground",
      )}
    >
      <span
        className="size-2 rounded-full transition-opacity"
        style={{ background: color, boxShadow: active ? `0 0 8px ${color}` : undefined, opacity: active ? 1 : 0.35 }}
      />
      {providerName(provider)}
      {count !== undefined && <span className="font-mono text-[10.5px] text-faint">{count.toLocaleString("pt-BR")}</span>}
    </button>
  );
}

// Filtro por agentes em que vazio = todos. Liga/desliga p; nunca desliga o
// último; todos ligados volta a vazio.
export function toggleProviders<T extends string>(selected: T[], p: T, all: T[]): T[] {
  const current = selected.length ? selected : all;
  const next = current.includes(p) ? current.filter((x) => x !== p) : [...current, p];
  if (next.length === 0) return selected;
  return next.length === all.length ? [] : next;
}

export const providerActive = <T extends string>(selected: T[], p: T) => selected.length === 0 || selected.includes(p);
