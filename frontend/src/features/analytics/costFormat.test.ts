import { describe, expect, it } from "vitest";
import { formatMoney } from "@/lib/money";
import { costDelta, coverageText, currencySections, inclusiveEnd, localDate, unitLabel } from "./costFormat";

const sums = (over: Partial<Record<string, unknown>>) => ({
  currency: "USD", input: "0", output: "0", cacheRead: "0", cacheCreation: "0", total: "0",
  records: 0, complete: 0, partial: 0, noPrice: 0, insufficient: 0, unestimated: 0, ...over,
});

describe("datas de vigência", () => {
  it("fim exclusivo vira o último dia inclusivo", () => {
    // 2026-12-01 00:00 em -03:00 = 03:00 UTC
    expect(inclusiveEnd("2026-12-01T03:00:00Z")).toBe("2026-11-30");
    expect(inclusiveEnd(null)).toBe("");
  });
  it("data local", () => {
    expect(localDate("2026-11-01T03:00:00Z")).toBe("2026-11-01");
  });
});

describe("costDelta", () => {
  it("compara a mesma moeda; sem anterior não mostra", () => {
    expect(costDelta(sums({ total: "2.000000000" }), [sums({ total: "1.000000000" })])).toEqual({ text: "↑ 100%", up: true });
    expect(costDelta(sums({ total: "1.000000000" }), [sums({ currency: "BRL", total: "1.000000000" })])).toBeNull();
    expect(costDelta(sums({ total: "1.000000000" }), [])).toBeNull();
    expect(costDelta(sums({ total: "1.000000000" }), [sums({ total: "0.000000000" })])).toEqual({ text: "novo", up: true });
  });
});

describe("coverageText", () => {
  it("só aparece quando falta custo", () => {
    expect(coverageText(sums({ records: 10, complete: 10 }))).toBeUndefined();
    expect(coverageText(sums({ records: 10, complete: 6, partial: 1, noPrice: 3 }))).toBe("estimativa sobre 70% das respostas");
    expect(coverageText(sums({ records: 10, complete: 10, partial: 0 }))).toBeUndefined();
    expect(coverageText(sums({ records: 4, complete: 3, partial: 1 }))).toBe("1 resposta com custo parcial");
  });
});

describe("unitLabel", () => {
  it("rótulos", () => {
    expect(unitLabel(1_000_000)).toBe("por 1 milhão de tokens");
    expect(unitLabel(1000)).toBe("por mil tokens");
    expect(unitLabel(1)).toBe("por token");
  });
});

describe("composição por categoria", () => {
  it("legenda vem dos decimais e bate com o valor exibido", async () => {
    const { categoryParts } = await import("./CostsPage");
    const parts = categoryParts(sums({ currency: "USD", input: "0.000000001", output: "2.999999999", cacheRead: "1.000000000", cacheCreation: "0", total: "4.000000000" }) as never);
    expect(parts.map((p) => p.share)).toEqual(["<1%", "75%", "25%", "0%"]);
    expect(parts[1].text).toBe(formatMoney("2.999999999", "USD"));
  });
});

describe("seções por moeda nos rankings", () => {
  const row = (...cs: [string, string][]) => ({ sums: cs.map(([currency, total]) => sums({ currency, total, records: 1 })) });
  it("cabeçalho onde a moeda muda, só com mais de uma moeda", () => {
    const rows = [row(["BRL", "9"]), row(["USD", "1"], ["BRL", "2"]), row(["EUR", "5"]), row(["USD", "100"]), row(["", "0"])];
    expect(currencySections(rows as never)).toEqual(["BRL", null, "EUR", "USD", ""]);
    expect(currencySections([row(["USD", "1"]), row(["USD", "2"])] as never)).toEqual([null, null]);
    expect(currencySections([row(["USD", "1"]), row(["", "0"])] as never)).toEqual([null, ""]);
  });
});

describe("seções em página de lista paginada", () => {
  const row = (c: string) => ({ sums: [sums({ currency: c, total: "1", records: 1 })] });
  it("com outras páginas, a moeda da página sempre tem cabeçalho", () => {
    expect(currencySections([row("USD"), row("USD")] as never, true)).toEqual(["USD", null]);
    expect(currencySections([row("USD"), row("USD")] as never)).toEqual([null, null]);
  });
});
