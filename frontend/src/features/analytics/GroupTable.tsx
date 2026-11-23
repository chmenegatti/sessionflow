import type { TokenSums, UsageGroup } from "@/lib/api";
import { exactTokens, fieldState, formatTokens, pct } from "./format";

const cols: { key: keyof TokenSums; label: string }[] = [
  { key: "total", label: "Total" },
  { key: "input", label: "Entrada" },
  { key: "output", label: "Saída" },
  { key: "cacheRead", label: "Cache lido" },
  { key: "cacheCreation", label: "Cache criado" },
  { key: "reasoning", label: "Reasoning" },
];

// Célula de um campo: valor, "—" indisponível, valor com marca de parcial.
export function TokenCell({ t, k }: { t: TokenSums; k: keyof TokenSums }) {
  const f = t[k];
  if (typeof f === "number") return null;
  const st = fieldState(f, t.records);
  if (st.kind === "none" || st.kind === "unavailable") {
    return (
      <span
        className="text-faint"
        title="Indisponível: o provider não informa este campo"
      >
        —
      </span>
    );
  }
  return (
    <span
      title={
        st.kind === "partial"
          ? `${exactTokens(st.sum)} · informado em ${f.count} de ${t.records} registros`
          : exactTokens(st.sum)
      }
    >
      {formatTokens(st.sum)}
      {st.kind === "partial" && (
        <sup className="ml-0.5 text-[9.5px] text-amber-300">
          {pct(st.coverage)}
        </sup>
      )}
    </span>
  );
}

// Tabela de consumo por grupo (agente, modelo, projeto).
export function GroupTable({
  groups,
  label,
  name,
}: {
  groups: UsageGroup[];
  label: string;
  name: (g: UsageGroup) => string;
}) {
  return (
    <div className="max-h-96 overflow-auto">
      <table className="w-full border-collapse text-[12.5px]">
        <thead className="sticky top-0 bg-[#0b1022]">
          <tr className="border-b text-left text-[11px] text-faint">
            <th className="py-2 pr-3 font-medium whitespace-nowrap">{label}</th>
            {cols.map((c) => (
              <th
                key={c.key}
                className="px-2 py-2 text-right font-medium whitespace-nowrap"
              >
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <tr
              key={g.key}
              className="border-b last:border-b-0 hover:bg-white/[0.03]"
            >
              <td
                className="max-w-56 truncate py-2 pr-3 text-muted-foreground"
                title={g.key}
              >
                {name(g)}
              </td>
              {cols.map((c) => (
                <td
                  key={c.key}
                  className={
                    c.key === "total"
                      ? "px-2 py-2 text-right font-medium tabular-nums"
                      : "px-2 py-2 text-right text-muted-foreground tabular-nums"
                  }
                >
                  <TokenCell t={g.tokens} k={c.key} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
