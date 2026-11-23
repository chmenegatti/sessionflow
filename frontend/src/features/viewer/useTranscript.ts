import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { TranscriptService, errorMessage, list, type MessageView } from "@/lib/api";

// Páginas com conteúdo em memória. Ao passar disso, o conteúdo das mais
// antigas é liberado, mas as mensagens ficam como esqueleto (seq, papel,
// horário, sem blocos): a lista não muda de tamanho nem de chaves, então a
// rolagem não pula. Para rever o conteúdo, a conversa é reaberta.
const MAX_PAGES = 12;

// Mensagem cujo conteúdo foi liberado da memória.
export type Message = MessageView & { evicted?: boolean };

interface Window {
  pages: Message[][];
  live: number; // índice da primeira página com conteúdo
  dropped: number; // registros com conteúdo liberado
}

const empty: Window = { pages: [], live: 0, dropped: 0 };

function evict(m: MessageView): Message {
  return { seq: m.seq, nativeId: "", role: m.role, timestamp: m.timestamp, blocks: [], evicted: true };
}

export interface Transcript {
  messages: Message[];
  dropped: number;
  done: boolean;
  loading: boolean;
  openError: string | null; // falha ao abrir: a conversa não aparece
  pageError: string | null; // falha numa página seguinte: o que já veio fica
  loadMore: () => void;
  reopen: () => void;
}

// Abre a conversa no backend e lê página por página sob demanda. O cursor
// é fechado ao sair do viewer ou trocar de sessão. from > 0 (resultado de
// busca) começa naquela mensagem; as anteriores não vêm.
export function useTranscript(id: string, from = 0): Transcript {
  const [win, setWin] = useState<Window>(empty);
  const [done, setDone] = useState(false);
  const [loading, setLoading] = useState(true);
  const [openError, setOpenError] = useState<string | null>(null);
  const [pageError, setPageError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const cursor = useRef<string | null>(null);
  const busy = useRef(false);
  const gen = useRef(0);

  useEffect(() => {
    const g = ++gen.current;
    setWin(empty);
    setDone(false);
    setLoading(true);
    setOpenError(null);
    setPageError(null);
    busy.current = true;
    (from > 0 ? TranscriptService.OpenAt(id, from) : TranscriptService.Open(id)).then(
      (page) => {
        if (g !== gen.current) {
          void TranscriptService.Close(page.cursor);
          return;
        }
        cursor.current = page.done ? null : page.cursor;
        setWin({ pages: [list(page.messages)], live: 0, dropped: 0 });
        setDone(page.done);
        setLoading(false);
        busy.current = false;
      },
      (err) => {
        if (g !== gen.current) return;
        setOpenError(errorMessage(err));
        setLoading(false);
        busy.current = false;
      },
    );
    return () => {
      gen.current++;
      if (cursor.current) void TranscriptService.Close(cursor.current);
      cursor.current = null;
    };
  }, [id, from, attempt]);

  const loadMore = useCallback(() => {
    const c = cursor.current;
    if (!c || busy.current) return;
    const g = gen.current;
    busy.current = true;
    setLoading(true);
    setPageError(null);
    TranscriptService.Next(c).then(
      (page) => {
        if (g !== gen.current) return;
        if (page.done) cursor.current = null;
        setWin((w) => {
          const pages = [...w.pages, list(page.messages)];
          let { live, dropped } = w;
          while (pages.length - live > MAX_PAGES) {
            pages[live] = pages[live].map(evict);
            dropped += pages[live].length;
            live++;
          }
          return { pages, live, dropped };
        });
        setDone(page.done);
        setLoading(false);
        busy.current = false;
      },
      (err) => {
        if (g !== gen.current) return;
        setPageError(errorMessage(err));
        setLoading(false);
        busy.current = false;
      },
    );
  }, []);

  const reopen = useCallback(() => setAttempt((a) => a + 1), []);
  const messages = useMemo(() => win.pages.flat(), [win.pages]);
  return { messages, dropped: win.dropped, done, loading, openError, pageError, loadMore, reopen };
}
