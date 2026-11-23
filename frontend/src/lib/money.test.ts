import { describe, expect, it } from "vitest";
import { formatMoney, moneyStatusLabel, roundDecimal } from "./money";

describe("roundDecimal", () => {
  it("arredonda half-even sobre a string", () => {
    expect(roundDecimal("0.125", 2)).toBe("0.12");
    expect(roundDecimal("0.135", 2)).toBe("0.14");
    expect(roundDecimal("0.000000005", 6)).toBe("0.000000");
    expect(roundDecimal("0.1251", 2)).toBe("0.13");
    expect(roundDecimal("9.995", 2)).toBe("10.00");
    expect(roundDecimal("12345678901234567.890000000", 2)).toBe(
      "12345678901234567.89",
    );
  });
});

describe("formatMoney", () => {
  it("2 casas por padrão", () => {
    expect(formatMoney("0.108000000", "BRL")).toBe("R$ 0,11");
    expect(formatMoney("12345678.900000000", "BRL")).toBe("R$ 12.345.678,90");
  });
  it("até 6 casas abaixo de 0,01", () => {
    expect(formatMoney("0.004000000", "USD")).toBe("US$ 0,004");
    expect(formatMoney("0.000001234", "USD")).toBe("US$ 0,000001");
  });
  it("zero é zero", () => {
    expect(formatMoney("0.000000000", "EUR")).toBe("€ 0,00");
  });
});

describe("moneyStatusLabel", () => {
  it("rótulos fixos", () => {
    expect(moneyStatusLabel("complete")).toBe("completa");
    expect(moneyStatusLabel("partial")).toBe("parcial");
    expect(moneyStatusLabel("no_price")).toBe("sem preço");
    expect(moneyStatusLabel("insufficient")).toBe("sem dados suficientes");
  });
});
