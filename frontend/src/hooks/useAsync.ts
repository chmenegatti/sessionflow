import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/api";

export interface AsyncState<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  reload: () => void;
}

// Executa fn quando deps mudam. Respostas de chamadas antigas são
// descartadas (evita mostrar dados de um filtro anterior).
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const seq = useRef(0);

  useEffect(() => {
    const id = ++seq.current;
    setLoading(true);
    setError(null);
    fn().then(
      (v) => {
        if (id !== seq.current) return;
        setData(v);
        setLoading(false);
      },
      (err) => {
        if (id !== seq.current) return;
        setError(errorMessage(err));
        setLoading(false);
      },
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  return { data, error, loading, reload };
}
