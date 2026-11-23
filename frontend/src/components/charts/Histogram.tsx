// Barras verticais por faixa (distribuições: duração, mensagens, intervalo).
export function Histogram({ buckets, unit }: { buckets: { label: string; count: number }[]; unit: string }) {
  const max = Math.max(0, ...buckets.map((b) => b.count));
  return (
    <div className="flex h-32 items-end gap-3" role="img" aria-label={buckets.map((b) => `${b.label}: ${b.count} ${unit}`).join(", ")}>
      {buckets.map((b) => (
        <div key={b.label} className="flex h-full min-w-0 flex-1 flex-col items-center justify-end gap-1.5" title={`${b.label}: ${b.count.toLocaleString("pt-BR")} ${unit}`}>
          <span className="text-[11.5px] font-medium tabular-nums">{b.count.toLocaleString("pt-BR")}</span>
          <div className="flex min-h-0 w-full flex-1 items-end">
            <span
              className="w-full rounded-t-[6px] bg-linear-to-t from-accent2/50 to-accent2 shadow-[0_0_14px_rgba(167,139,250,0.35)]"
              style={{ height: `${max ? Math.max(3, (b.count / max) * 100) : 3}%`, opacity: b.count ? 1 : 0.25 }}
            />
          </div>
          <span className="truncate text-[10.5px] text-faint">{b.label}</span>
        </div>
      ))}
    </div>
  );
}
