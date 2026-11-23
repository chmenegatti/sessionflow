import { useSearchParams } from "react-router";
import { ProviderID, type AnalyticsQuery } from "@/lib/api";

export const allProviders = [ProviderID.ProviderClaude, ProviderID.ProviderCodex, ProviderID.ProviderOpenCode];
export const periods = ["today", "7d", "30d", "90d", "month", "prev_month", "all", "custom"] as const;
export type Period = (typeof periods)[number];

const today = () => new Date().toLocaleDateString("sv-SE"); // AAAA-MM-DD local

// Filtros do Analytics na URL (?period=&from=&to=&p=&project=&model=):
// trocar de aba e voltar mantêm o recorte.
export function useAnalyticsQuery() {
  const [params, setParams] = useSearchParams();
  const raw = params.get("period") ?? "30d";
  const period: Period = (periods as readonly string[]).includes(raw) ? (raw as Period) : "30d";
  const providers = (params.get("p") ?? "").split(",").filter((p): p is ProviderID => allProviders.includes(p as ProviderID));
  const query: AnalyticsQuery = {
    period,
    from: period === "custom" ? (params.get("from") ?? today()) : "",
    to: period === "custom" ? (params.get("to") ?? today()) : "",
    providers,
    project: params.get("project") ?? "",
    model: params.get("model") ?? "",
  };
  const update = (patch: Partial<AnalyticsQuery>) =>
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        const set = (k: string, v: string) => (v ? next.set(k, v) : next.delete(k));
        if (patch.period !== undefined) set("period", patch.period === "30d" ? "" : patch.period);
        if (patch.from !== undefined) set("from", patch.from);
        if (patch.to !== undefined) set("to", patch.to);
        if (patch.providers !== undefined) set("p", (patch.providers ?? []).join(","));
        if (patch.project !== undefined) set("project", patch.project);
        if (patch.model !== undefined) set("model", patch.model);
        return next;
      },
      { replace: true },
    );
  // Mesma consulta sem o modelo (Overview e Activity não filtram por modelo).
  const withoutModel: AnalyticsQuery = { ...query, model: "" };
  return { query, withoutModel, update, search: params.toString() };
}
