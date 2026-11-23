import { useAsync } from "@/hooks/useAsync";
import { useAppStore } from "@/lib/store";
import type { AnalyticsQuery } from "@/lib/api";

export interface DataView<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  stale: boolean;
}

// dataView decide o que a tela mostra. Dados de outro filtro só aparecem
// (esmaecidos) enquanto o novo carrega; se o novo falhar, o erro aparece e
// os números antigos somem — nunca ficam na tela como se fossem do filtro atual.
export function dataView<T>(res: { data: { key: string; data: T } | null; error: string | null; loading: boolean }, key: string): DataView<T> {
  const current = res.data?.key === key;
  const stale = !current && res.loading;
  return {
    data: res.data && (current || stale) ? res.data.data : null,
    error: res.error,
    loading: !res.data && res.loading,
    stale,
  };
}

// Carrega uma tela com a consulta; recarrega no index:updated sem esmaecer
// (só trocar filtro esmaece, como no Dashboard).
export function useAnalyticsData<T>(query: AnalyticsQuery, fetch: (q: AnalyticsQuery) => Promise<T>) {
  const version = useAppStore((s) => s.dataVersion);
  const key = JSON.stringify(query);
  const res = useAsync(() => fetch(query).then((data) => ({ key, data })), [key, version]);
  return { ...dataView(res, key), reload: res.reload };
}
