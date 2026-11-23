import type { ReactNode } from "react";

// Rosca com o total no centro.
export function Donut({ parts, center, size = 132 }: { parts: { value: number; color: string }[]; center: ReactNode; size?: number }) {
  const r = size / 2 - 14;
  const c = 2 * Math.PI * r;
  const total = parts.reduce((n, p) => n + p.value, 0);
  let off = 0;
  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="rgba(255,255,255,0.08)" strokeWidth="14" />
        {total > 0 &&
          parts.map((p, i) => {
            const len = (p.value / total) * c;
            const gap = parts.filter((x) => x.value > 0).length > 1 ? 2 : 0;
            const el = (
              <circle
                key={i}
                cx={size / 2}
                cy={size / 2}
                r={r}
                fill="none"
                stroke={p.color}
                strokeWidth="14"
                strokeDasharray={`${Math.max(0, len - gap)} ${c - len + gap}`}
                strokeDashoffset={-off}
                transform={`rotate(-90 ${size / 2} ${size / 2})`}
                style={{ filter: `drop-shadow(0 0 6px ${p.color})` }}
              />
            );
            off += len;
            return el;
          })}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">{center}</div>
    </div>
  );
}
