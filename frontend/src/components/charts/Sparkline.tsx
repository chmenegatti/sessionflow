import { useId } from "react";
import { areaPath, linePath, linear } from "./scale";

// Mini-gráfico de área (indicadores do Dashboard).
export function Sparkline({ values, color, height = 46 }: { values: number[]; color: string; height?: number }) {
  const id = useId();
  const w = 300;
  // Pelo menos 2 pontos: uma série de 1 intervalo (ex.: "Tudo" com histórico
  // desta semana) vira uma linha reta.
  const vs = values.length >= 2 ? values : values.length === 1 ? [values[0], values[0]] : [0, 0];
  const n = vs.length;
  const max = Math.max(0, ...vs);
  const y = linear(max, height - 10);
  const xs = Array.from({ length: n }, (_, i) => (i * w) / (n - 1));
  const ys = vs.map((v) => height - 3 - y(v));
  return (
    <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" className="block w-full" style={{ height }} aria-hidden>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor={color} stopOpacity=".35" />
          <stop offset="1" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={areaPath(xs, ys, xs.map(() => height))} fill={`url(#${id})`} />
      <path d={linePath(xs, ys)} fill="none" stroke={color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
