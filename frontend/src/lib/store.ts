import { create } from "zustand";
import {
  AppService,
  errorMessage,
  type AppStatus,
  type ResumePlan,
} from "@/lib/api";

type LoadState = "idle" | "loading" | "ready" | "error";

interface AppState {
  status: AppStatus | null;
  state: LoadState;
  error: string | null;
  detailOpen: boolean;
  // Sessão exibida no painel de detalhes.
  selectedId: string | null;
  // Incrementado quando os dados mudam (indexação, favorito, tag): listas
  // e detalhes recarregam.
  dataVersion: number;
  // Aviso curto no canto da tela (resultado de uma ação).
  notice: Notice | null;
  // Retomada aguardando confirmação (sessão possivelmente aberta).
  confirmResume: ResumePlan | null;
  loadStatus: () => Promise<void>;
  toggleDetail: () => void;
  select: (id: string | null) => void;
  touch: () => void;
  notify: (text: string, tone?: Notice["tone"]) => void;
  dismiss: () => void;
  setConfirmResume: (plan: ResumePlan | null) => void;
}

export interface Notice {
  id: number;
  text: string;
  tone: "ok" | "error";
}

export const useAppStore = create<AppState>((set) => ({
  status: null,
  state: "idle",
  error: null,
  detailOpen: false,
  selectedId: null,
  dataVersion: 0,
  notice: null,
  confirmResume: null,
  loadStatus: async () => {
    set({ state: "loading", error: null });
    try {
      const status = await AppService.Status();
      set({ status, state: "ready" });
    } catch (err) {
      set({ state: "error", error: errorMessage(err) });
    }
  },
  toggleDetail: () => set((s) => ({ detailOpen: !s.detailOpen })),
  select: (id) =>
    set((s) => ({ selectedId: id, detailOpen: id !== null || s.detailOpen })),
  touch: () => set((s) => ({ dataVersion: s.dataVersion + 1 })),
  notify: (text, tone = "ok") =>
    set((s) => ({ notice: { id: (s.notice?.id ?? 0) + 1, text, tone } })),
  dismiss: () => set({ notice: null }),
  setConfirmResume: (plan) => set({ confirmResume: plan }),
}));
