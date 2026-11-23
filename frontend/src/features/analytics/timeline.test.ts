import { describe, expect, it } from "vitest";
import { timelineView } from "./timeline";

const f = (sum: number, count: number) => ({ sum, count });
const tok = (
  records: number,
  o: Partial<
    Record<
      | "input"
      | "output"
      | "cacheRead"
      | "cacheCreation"
      | "reasoning"
      | "total",
      [number, number]
    >
  >,
) => {
  const z = f(0, 0);
  const g = (k: keyof typeof o) => (o[k] ? f(o[k]![0], o[k]![1]) : z);
  return {
    records,
    input: g("input"),
    output: g("output"),
    cacheRead: g("cacheRead"),
    cacheCreation: g("cacheCreation"),
    reasoning: g("reasoning"),
    total: g("total"),
    derived: 0,
  };
};

describe("timelineView", () => {
  const tl = {
    stepMs: 60_000,
    resumes: 1,
    points: [
      {
        start: 0,
        tokens: tok(2, {
          input: [10, 2],
          output: [5, 2],
          cacheRead: [100, 2],
          total: [115, 2],
        }),
        cumulative: 115,
        resumed: false,
      },
      {
        start: 3_600_000,
        tokens: tok(2, {
          input: [1, 1],
          output: [8, 2],
          cacheRead: [50, 2],
          reasoning: [3, 1],
          total: [59, 1],
        }),
        cumulative: 174,
        resumed: true,
      },
    ],
    untimed: tok(1, { input: [7, 1], output: [1, 1], total: [8, 1] }),
    total: tok(5, {
      input: [18, 4],
      output: [14, 5],
      cacheRead: [150, 4],
      reasoning: [3, 1],
      total: [182, 4],
    }),
  };
  const v = timelineView(tl as never);
  it("só os componentes informados; reasoning fora da pilha", () => {
    expect(v.keys).toEqual(["input", "output", "cacheRead"]);
    expect(v.points[1].reasoning).toBe(3);
    expect(v.points[0].stack.reduce((a, b) => a + b.value, 0)).toBe(115);
  });
  it("marca parcial e retomada por intervalo", () => {
    expect(v.points.map((p) => p.partial)).toEqual([false, true]);
    expect(v.points.map((p) => p.resumed)).toEqual([false, true]);
  });
  it("acumulado e registros sem horário", () => {
    expect(v.points[1].cumulative).toBe(174);
    expect(v.untimedRecords).toBe(1);
    expect(v.total).toBe(182);
    expect(v.max).toBe(115);
  });
  it("sessão sem consumo não tem gráfico", () => {
    const e = timelineView({
      stepMs: 60_000,
      resumes: 0,
      points: [],
      untimed: tok(0, {}),
      total: tok(0, {}),
    } as never);
    expect(e.points).toEqual([]);
    expect(e.keys).toEqual([]);
  });
});
