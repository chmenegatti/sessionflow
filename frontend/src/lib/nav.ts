import {
  LayoutDashboard,
  MessagesSquare,
  FolderGit2,
  Star,
  Search,
  Settings,
  ChartColumn,
  type LucideIcon,
} from "lucide-react";

export interface NavItem {
  to: string;
  label: string;
  icon: LucideIcon;
  shortcut?: string; // exibição; o atalho real está em hooks/useShortcuts
}

export const mainNav: NavItem[] = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/sessions", label: "Sessões", icon: MessagesSquare },
  { to: "/projects", label: "Projetos", icon: FolderGit2, shortcut: "Ctrl P" },
  { to: "/favorites", label: "Favoritas", icon: Star },
  { to: "/search", label: "Busca", icon: Search, shortcut: "Ctrl K" },
  { to: "/analytics", label: "Analytics", icon: ChartColumn },
];

export const settingsNav: NavItem = {
  to: "/settings",
  label: "Configurações",
  icon: Settings,
  shortcut: "Ctrl ,",
};

// Rota da página de um projeto (o caminho vai na query string).
export function projectPath(path: string) {
  return `/projects/view?path=${encodeURIComponent(path)}`;
}

// Rota do viewer de uma sessão.
// seq (resultado de busca) abre a conversa já na mensagem.
export function sessionPath(id: string, seq?: number) {
  const at = seq === undefined ? "" : `&seq=${seq}`;
  return `/session?id=${encodeURIComponent(id)}${at}`;
}

// Campo da busca global (Ctrl+K foca nele).
export const SEARCH_INPUT_ID = "global-search";
