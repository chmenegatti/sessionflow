import { useEffect } from "react";
import { useLocation, useNavigate } from "react-router";
import { SEARCH_INPUT_ID } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { useResume } from "@/features/resume/useResume";

// Atalhos globais (CLAUDE.md §9): navegação e Ctrl+Enter (retomar a sessão
// selecionada). Ctrl+F (buscar na conversa) ainda não existe.
const routes: Record<string, string> = {
  k: "/search",
  p: "/projects",
  ",": "/settings",
};

export function useShortcuts() {
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const { resume } = useResume();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!e.ctrlKey || e.altKey || e.metaKey || e.shiftKey) return;
      if (e.key === "Enter") {
        // Em campo de texto multilinha, Ctrl+Enter é do campo.
        if (e.target instanceof HTMLTextAreaElement) return;
        const id = useAppStore.getState().selectedId;
        if (id && !useAppStore.getState().confirmResume) {
          e.preventDefault();
          void resume(id);
        }
        return;
      }
      const to = routes[e.key.toLowerCase()];
      if (!to) return;
      e.preventDefault();
      if (to === pathname) {
        // Já na página: Ctrl+K volta ao campo de busca sem perder a consulta.
        const input = document.getElementById(SEARCH_INPUT_ID) as HTMLInputElement | null;
        input?.focus();
        input?.select();
        return;
      }
      navigate(to);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [navigate, pathname, resume]);
}
