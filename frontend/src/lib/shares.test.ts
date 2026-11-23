import { describe, expect, it } from "vitest";
import { decimalShares } from "./money";

const nums = (xs: string[]) =>
  xs.map((x) =>
    x.startsWith("<") || x.startsWith(">") ? NaN : Number(x.replace("%", "")),
  );

describe("decimalShares (legenda da composição)", () => {
  it("usa todas as casas decimais, sem float", () => {
    // 1 nano de diferença decide o arredondamento: float perderia.
    expect(
      decimalShares(["0.333333334", "0.333333333", "0.333333333"]),
    ).toEqual(["34%", "33%", "33%"]);
    expect(
      decimalShares([
        "12345678901234567.000000001",
        "12345678901234567.000000000",
      ]),
    ).toEqual(["50%", "50%"]);
  });
  it("soma 100% quando o arredondamento sobra ou falta (maior resto)", () => {
    const out = decimalShares(["1.000000000", "1.000000000", "1.000000000"]);
    expect(out).toEqual(["34%", "33%", "33%"]); // iguais: desempate pela ordem
    expect(nums(out).reduce((a, b) => a + b, 0)).toBe(100);
    const up = decimalShares(["0.125", "0.125", "0.75"]); // 12,5 / 12,5 / 75
    expect(nums(up).reduce((a, b) => a + b, 0)).toBe(100);
    expect(up[2]).toBe("75%");
  });
  it("arredonda para cima e para baixo pelo resto", () => {
    expect(decimalShares(["0.666", "0.334"])).toEqual(["67%", "33%"]); // 66,6 → 67; 33,4 → 33
    expect(decimalShares(["0.664", "0.336"])).toEqual(["66%", "34%"]);
  });
  it("participação muito pequena não some nem vira 0%", () => {
    expect(decimalShares(["1000.000000000", "0.000000001"])).toEqual([
      ">99%",
      "<1%",
    ]);
    expect(decimalShares(["0", "5.5"])).toEqual(["0%", "100%"]);
  });
  it("total zero", () => {
    expect(decimalShares(["0", "0.000000000"])).toEqual(["0%", "0%"]);
    expect(decimalShares([])).toEqual([]);
  });
});
