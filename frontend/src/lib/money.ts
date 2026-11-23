// Valores monetários chegam do backend como string decimal com 9 casas
// ("0.108000000"): o arredondamento é feito na string (half-even), sem
// passar por float, e só a exibição final usa Intl.

// roundDecimal arredonda uma string decimal não negativa para `places` casas
// (half-even).
export function roundDecimal(s: string, places: number): string {
  const [int, frac = ""] = s.split(".");
  const head = frac.slice(0, places).padEnd(places, "0");
  const rest = frac.slice(places);
  let n = BigInt(int + head);
  if (rest && /[1-9]/.test(rest)) {
    const first = rest[0];
    const tail = rest.slice(1);
    const up =
      first > "5" || (first === "5" && (/[1-9]/.test(tail) || n % 2n === 1n));
    if (up) n += 1n;
  }
  const digits = n.toString().padStart(places + 1, "0");
  if (places === 0) return digits;
  return `${digits.slice(0, -places)}.${digits.slice(-places)}`;
}

const isZero = (s: string) => !/[1-9]/.test(s);

const fmt = (currency: string, places: number) =>
  new Intl.NumberFormat("pt-BR", {
    style: "currency",
    currency,
    minimumFractionDigits: places,
    maximumFractionDigits: places,
  });

// formatMoney: 2 casas; valor menor que 0,01 (e não zero) mostra até 6 casas
// para não virar "0,00". Moeda inválida cai no código ao lado do número.
export function formatMoney(s: string, currency: string): string {
  try {
    const two = roundDecimal(s, 2);
    if (!isZero(two) || isZero(s)) return fmt(currency, 2).format(Number(two));
    const six = roundDecimal(s, 6);
    if (isZero(six)) return `< ${fmt(currency, 6).format(0.000001)}`;
    const places = Math.max(2, six.replace(/0+$/, "").split(".")[1].length);
    return fmt(currency, places).format(Number(six));
  } catch {
    return `${s} ${currency}`;
  }
}

export type EstimateStatus =
  "complete" | "partial" | "no_price" | "insufficient";

const statusLabels: Record<EstimateStatus, string> = {
  complete: "completa",
  partial: "parcial",
  no_price: "sem preço",
  insufficient: "sem dados suficientes",
};

export const moneyStatusLabel = (s: EstimateStatus) => statusLabels[s];

// nanos converte a string decimal (até 9 casas) em inteiro de 10⁻⁹.
function nanos(s: string): bigint {
  return BigInt(roundDecimal(s, 9).replace(".", ""));
}

// decimalShares: participação de cada valor no total, em %, calculada em
// BigInt sobre os nanos (sem float). Inteiros pelo maior resto, então as
// partes somam 100%; empate de resto vai para a primeira. Parte > 0 que
// arredonda para 0 mostra "<1%" e parte que não é o total inteiro nunca
// aparece como 100% (">99%").
export function decimalShares(values: string[]): string[] {
  const v = values.map(nanos);
  const total = v.reduce((a, b) => a + b, 0n);
  if (total <= 0n) return values.map(() => "0%");
  const floor = v.map((x) => (x * 100n) / total);
  const rest = v.map((x) => (x * 100n) % total);
  let left = 100n - floor.reduce((a, b) => a + b, 0n);
  const order = v
    .map((_, i) => i)
    .sort((a, b) => (rest[b] > rest[a] ? 1 : rest[b] < rest[a] ? -1 : a - b));
  for (const i of order) {
    if (left <= 0n) break;
    if (rest[i] > 0n) {
      floor[i] += 1n;
      left -= 1n;
    }
  }
  return floor.map((p, i) => {
    if (v[i] === 0n) return "0%";
    if (p === 0n) return "<1%";
    if (p === 100n && v[i] < total) return ">99%";
    return `${p}%`;
  });
}
