import { describe, expect, it } from "vitest";
import { centerTotal, coverageNote } from "./format";
import { sharePct } from "@/components/charts/scale";
import { toggleProviders } from "@/features/sessions/ProviderTag";

describe("centerTotal", () => {
  it("nunca mostra 0 para indisponível", () => {
    expect(centerTotal({ sum: 0, count: 0 }, 0)).toEqual({ text: "—" });
    expect(centerTotal({ sum: 0, count: 0 }, 5)).toEqual({ text: "indisponível" });
    expect(centerTotal({ sum: 1500, count: 3 }, 4)).toEqual({ text: "1,5 mil", note: "parcial · 75%" });
    expect(centerTotal({ sum: 1500, count: 4 }, 4)).toEqual({ text: "1,5 mil" });
  });
});

describe("coverageNote", () => {
  it("só avisa quando falta total", () => {
    expect(coverageNote(0, 0)).toBeUndefined();
    expect(coverageNote(3, 3)).toBeUndefined();
    expect(coverageNote(3, 0)).toEqual({ unavailable: true, text: "total indisponível" });
    expect(coverageNote(200, 199)).toEqual({ unavailable: false, text: "parcial · 99%" });
  });
});

describe("sharePct", () => {
  it("fatia pequena e quase total", () => {
    expect(sharePct(0, 100)).toBe("0%");
    expect(sharePct(1, 1000)).toBe("<1%");
    expect(sharePct(999, 1000)).toBe("99%");
    expect(sharePct(5, 5)).toBe("100%");
  });
});

describe("toggleProviders", () => {
  const all = ["claude", "codex", "opencode"];
  it("vazio = todos; nunca desliga o último", () => {
    expect(toggleProviders([], "codex", all)).toEqual(["claude", "opencode"]);
    expect(toggleProviders(["claude"], "claude", all)).toEqual(["claude"]);
    expect(toggleProviders(["claude", "opencode"], "codex", all)).toEqual([]);
  });
});
