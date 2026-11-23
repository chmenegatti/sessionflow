// Escalas e caminhos dos gráficos SVG. Domínio vazio (máximo 0) nunca vira
// NaN: tudo cai na base.

export function linear(domainMax: number, rangePx: number): (v: number) => number {
  if (!(domainMax > 0)) return () => 0;
  return (v) => (v / domainMax) * rangePx;
}

// Caminho de linha "M x y L x y …".
export function linePath(xs: number[], ys: number[]): string {
  return xs.map((x, i) => `${i ? "L" : "M"}${x.toFixed(1)} ${ys[i].toFixed(1)}`).join(" ");
}

// Área entre uma linha de topo e uma de base (mesmos xs).
export function areaPath(xs: number[], top: number[], base: number[]): string {
  const up = xs.map((x, i) => `${i ? "L" : "M"}${x.toFixed(1)} ${top[i].toFixed(1)}`).join(" ");
  const down = xs
    .map((_, i) => xs.length - 1 - i)
    .map((j) => `L${xs[j].toFixed(1)} ${base[j].toFixed(1)}`)
    .join(" ");
  return `${up} ${down}Z`;
}

// Máximo "redondo" para a grade.
export function niceMax(v: number): number {
  // Múltiplo de 4 com passo "redondo": a grade tem 4 faixas e os rótulos
  // saem inteiros (12,5 viraria "13").
  if (!(v > 0)) return 4;
  const step = v / 4;
  const p = 10 ** Math.floor(Math.log10(step));
  for (const m of [1, 1.5, 2, 2.5, 3, 4, 5, 6, 7.5, 8, 10]) {
    const s = m * p;
    if (s >= step && (s >= 1 ? Number.isInteger(s) : true)) return Math.max(4, s * 4);
  }
  return 40 * p;
}

// Fatia de um total em %: valor > 0 que arredondaria para 0 vira "<1%", e
// fatia que não é o total inteiro nunca aparece como 100%.
export function sharePct(value: number, total: number): string {
  if (total <= 0 || value <= 0) return "0%";
  const p = (value / total) * 100;
  if (p < 1) return "<1%";
  if (value < total) return `${Math.min(99, Math.round(p))}%`;
  return "100%";
}
