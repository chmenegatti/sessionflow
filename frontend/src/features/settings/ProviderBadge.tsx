import type { ProviderStatus } from "@/lib/api";
import { cn } from "@/lib/utils";

// Rótulos dos estados devolvidos pelo backend (service.ProviderStatus.status).
const labels: Record<string, { label: string; tone: string; dot: string }> = {
  ok: { label: "Detectado", tone: "border-positive/30 bg-positive/10 text-positive", dot: "bg-positive shadow-[0_0_6px_rgba(94,234,212,0.9)]" },
  disabled: { label: "Desabilitado", tone: "border-border text-faint", dot: "bg-faint" },
  pending: { label: "Verificando…", tone: "border-primary/30 bg-primary/10 text-primary", dot: "bg-primary animate-pulse" },
  not_found: { label: "Não encontrado", tone: "border-border bg-white/[0.04] text-muted-foreground", dot: "bg-faint" },
  unsupported: { label: "Não suportado", tone: "border-negative/30 bg-negative/10 text-negative", dot: "bg-negative" },
  error: { label: "Erro", tone: "border-negative/30 bg-negative/10 text-negative", dot: "bg-negative shadow-[0_0_6px_rgba(251,113,133,0.9)]" },
};

export function ProviderBadge({ provider }: { provider: ProviderStatus }) {
  const s = labels[provider.status] ?? labels.ok;
  return (
    <span className={cn("inline-flex h-5 items-center gap-1.5 rounded-full border px-2 text-[11px] font-medium whitespace-nowrap", s.tone)}>
      <span className={cn("size-1.5 rounded-full", s.dot)} aria-hidden />
      {s.label}
    </span>
  );
}

// Detalhe curto para exibir abaixo do provider: erro, aviso ou falhas.
export function providerNote(p: ProviderStatus): string {
  if (p.error) return p.error;
  const parts = [p.detail];
  if (p.failed) parts.push(`${p.failed} fonte(s) com erro`);
  if (p.unsupported) parts.push(`${p.unsupported} fonte(s) em formato não suportado`);
  return parts.filter(Boolean).join(" · ");
}
