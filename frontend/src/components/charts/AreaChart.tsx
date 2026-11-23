import { useEffect, useId, useRef, useState } from "react";
import { areaPath, linePath, linear, niceMax } from "./scale";

export interface AreaPoint {
  start: number; // ms
  byKey: Record<string, number>;
  // Aviso no balão (ex.: "parcial · 40%"); unavailable: o valor do intervalo
  // não é conhecido, então o balão não mostra número nem entra na média.
  note?: { text: string; unavailable: boolean };
}

export interface AreaKey {
  key: string;
  color: string;
  label: string;
}

const H = 240;
const BASE_PAD = { left: 40, right: 12, top: 12, bottom: 26 };

// Largura real do container: o SVG é desenhado em pixels (texto não estica).
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.round(e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

// Áreas empilhadas por série (ordem de keys), com brilho na cor de cada uma,
// linha tracejada do período anterior, grade e balão de vidro no hover.
export function AreaChart({
  points,
  keys,
  previous,
  formatTick,
  formatTooltip,
  onSelect,
  formatValue = (n: number) => n.toLocaleString("pt-BR"),
  unit,
  padLeft = 40,
}: {
  points: AreaPoint[];
  keys: AreaKey[];
  previous?: number[];
  formatTick: (start: number) => string;
  formatTooltip: (start: number) => string;
  onSelect?: (start: number) => void;
  formatValue?: (n: number) => string; // eixo e balão
  unit?: string; // ex.: "tokens", no balão
  padLeft?: number; // margem dos rótulos do eixo y
}) {
  const PAD = { ...BASE_PAD, left: padLeft };
  const id = useId();
  const [ref, width] = useWidth<HTMLDivElement>();
  const [rawHover, setHover] = useState<number | null>(null);
  const n = points.length;
  // A série pode encolher com o mouse parado (troca de período pelo
  // teclado): índice fora dela é ignorado.
  const hover = rawHover !== null && rawHover < n ? rawHover : null;
  const totals = points.map((p) => keys.reduce((s, k) => s + (p.byKey[k.key] ?? 0), 0));
  const max = niceMax(Math.max(0, ...totals, ...(previous ?? [])));
  const innerW = Math.max(0, width - PAD.left - PAD.right);
  const innerH = H - PAD.top - PAD.bottom;
  const x = (i: number) => PAD.left + (n > 1 ? (i * innerW) / (n - 1) : innerW / 2);
  const yScale = linear(max, innerH);
  const y = (v: number) => H - PAD.bottom - yScale(v);
  const xs = points.map((_, i) => x(i));

  // Camadas empilhadas.
  let base = points.map(() => 0);
  const layers = keys.map((k) => {
    const top = points.map((p, i) => base[i] + (p.byKey[k.key] ?? 0));
    const layer = { k, area: areaPath(xs, top.map(y), base.map(y)), line: linePath(xs, top.map(y)) };
    base = top;
    return layer;
  });

  // Rótulos do eixo: no máximo ~7, espaçados.
  const step = Math.max(1, Math.ceil(n / 7));
  const ticks = points.map((p, i) => ({ i, p })).filter(({ i }) => i % step === 0 || i === n - 1);
  const known = totals.filter((_, i) => !points[i].note?.unavailable);
  const avg = known.length ? known.reduce((a, b) => a + b, 0) / known.length : 0;
  const hoverNote = hover !== null ? points[hover].note : undefined;

  const pick = (clientX: number, el: Element) => {
    if (!n || !innerW) return null;
    const rx = clientX - el.getBoundingClientRect().left - PAD.left;
    return Math.min(n - 1, Math.max(0, Math.round((rx / innerW) * (n - 1))));
  };

  return (
    <div ref={ref} className="relative w-full select-none" style={{ height: H }}>
      {width > 0 && (
        <svg
          width={width}
          height={H}
          className={onSelect ? "cursor-pointer" : undefined}
          onMouseMove={(e) => setHover(pick(e.clientX, e.currentTarget))}
          onMouseLeave={() => setHover(null)}
          onClick={(e) => {
            const i = pick(e.clientX, e.currentTarget);
            if (i !== null && onSelect) onSelect(points[i].start);
          }}
          role="img"
          aria-label="Gráfico de atividade"
        >
          <defs>
            {keys.map((k) => (
              <linearGradient key={k.key} id={`${id}-${k.key}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor={k.color} stopOpacity=".42" />
                <stop offset="1" stopColor={k.color} stopOpacity=".04" />
              </linearGradient>
            ))}
          </defs>
          {[0, 1, 2, 3, 4].map((g) => {
            const v = (max / 4) * g;
            return (
              <g key={g}>
                <line x1={PAD.left} x2={width - PAD.right} y1={y(v)} y2={y(v)} stroke="rgba(255,255,255,0.08)" strokeDasharray={g ? "2 5" : undefined} />
                <text x={PAD.left - 8} y={y(v) + 3.5} textAnchor="end" fontSize="10.5" fill="var(--faint)">
                  {formatValue(Math.round(v))}
                </text>
              </g>
            );
          })}
          {layers.map(({ k, area, line }) => (
            <g key={k.key}>
              <path d={area} fill={`url(#${id}-${k.key})`} />
              <path d={line} fill="none" stroke={k.color} strokeWidth="1.8" strokeLinejoin="round" style={{ filter: `drop-shadow(0 0 5px ${k.color})` }} />
            </g>
          ))}
          {previous && previous.length === n && (
            <path d={linePath(xs, previous.map(y))} fill="none" stroke="var(--faint)" strokeWidth="1.2" strokeDasharray="3 4" />
          )}
          {ticks.map(({ i, p }) => (
            <text key={i} x={x(i)} y={H - 7} textAnchor={i === 0 ? "start" : i === n - 1 ? "end" : "middle"} fontSize="10.5" fill="var(--faint)">
              {formatTick(p.start)}
            </text>
          ))}
          {hover !== null && (
            <g pointerEvents="none">
              <line x1={x(hover)} x2={x(hover)} y1={PAD.top} y2={H - PAD.bottom} stroke="var(--primary)" strokeOpacity=".6" />
              <circle cx={x(hover)} cy={y(totals[hover])} r="4.5" fill="var(--background)" stroke="var(--primary)" strokeWidth="2" />
            </g>
          )}
        </svg>
      )}
      {hover !== null && width > 0 && (
        <div
          className="glass pointer-events-none absolute z-10 min-w-40 rounded-xl border px-3 py-2 shadow-[0_16px_40px_-12px_rgba(0,0,0,0.9)]"
          style={{
            left: Math.min(x(hover) + 12, width - 190),
            top: Math.max(0, Math.min(y(totals[hover]) - 20, H - 120)),
          }}
        >
          <p className="text-[11px] text-muted-foreground">{formatTooltip(points[hover].start)}</p>
          {hoverNote?.unavailable ? (
            <p className="text-sm text-faint">indisponível</p>
          ) : (
            <p className="text-lg font-semibold tabular-nums">{formatValue(totals[hover])}
              {unit && <span className="ml-1 text-[11px] font-normal text-muted-foreground">{unit}</span>}
            </p>
          )}
          {hoverNote && <p className="text-[11px] text-amber-300">{hoverNote.text}</p>}
          {avg > 0 && !hoverNote?.unavailable && (
            <p className={totals[hover] >= avg ? "text-[11px] text-positive" : "text-[11px] text-negative"}>
              {totals[hover] >= avg ? "↑" : "↓"} {(totals[hover] / avg).toLocaleString("pt-BR", { maximumFractionDigits: 1 })}× a média
            </p>
          )}
          {!hoverNote?.unavailable && <ul className="mt-1.5 space-y-0.5">
            {keys.map((k) => (
              <li key={k.key} className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <span className="size-2 rounded-full" style={{ background: k.color }} />
                {k.label}
                <span className="ml-auto text-foreground tabular-nums">{formatValue(points[hover].byKey[k.key] ?? 0)}</span>
              </li>
            ))}
          </ul>}
        </div>
      )}
    </div>
  );
}
