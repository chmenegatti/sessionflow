import { useState } from "react";
import { useNavigate } from "react-router";
import { MessageSquareOff } from "lucide-react";
import { Segmented } from "@/components/ui/segmented";
import type { Dashboard, SessionItem } from "@/lib/api";
import { relativeTime, dateTime } from "@/lib/format";
import { sessionPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { FavoriteButton } from "@/features/sessions/FavoriteButton";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { SessionTitle } from "@/features/sessions/SessionRow";

type Tab = "all" | "open" | "favorites";

// Sessões recentes em tabela: clique seleciona, duplo clique abre.
export function RecentTable({ d }: { d: Dashboard }) {
  const [tab, setTab] = useState<Tab>("all");
  const navigate = useNavigate();
  const select = useAppStore((s) => s.select);
  const selectedId = useAppStore((s) => s.selectedId);
  const rows: SessionItem[] = (tab === "all" ? d.recent.all : tab === "open" ? d.recent.open : d.recent.favorites) ?? [];
  const open = new Set((d.recent.open ?? []).map((s) => s.id));
  const max = Math.max(1, ...rows.map((r) => r.messageCount));
  return (
    <section className="surface overflow-hidden rounded-[18px]" aria-label="Sessões recentes">
      <div className="flex items-end gap-4 px-5 pt-4 pb-3">
        <div>
          <h3 className="text-[15px] font-semibold tracking-tight">Sessões recentes</h3>
          <p className="mt-0.5 text-[11.5px] text-faint">Ordenadas pela última atividade</p>
        </div>
        <Segmented
          className="ml-auto"
          label="Filtro das sessões recentes"
          value={tab}
          onChange={setTab}
          options={[
            { value: "all", label: "Todas" },
            { value: "open", label: `Abertas${d.openNow ? ` · ${d.openNow}` : ""}` },
            { value: "favorites", label: "Favoritas" },
          ]}
        />
      </div>
      {rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 border-t px-5 py-10 text-[13px] text-muted-foreground">
          <MessageSquareOff className="size-5 text-faint" />
          {tab === "open" ? "Nenhuma sessão aberta agora." : tab === "favorites" ? "Nenhuma favorita com atividade no período." : "Nenhuma sessão no período."}
        </div>
      ) : (
        <table className="w-full table-fixed border-collapse text-[13px]">
          <thead>
            <tr className="border-y text-left text-[11.5px] text-faint">
              <th className="w-[34%] px-5 py-2.5 font-medium">Sessão</th>
              <th className="w-28 px-3 py-2.5 font-medium">Agente</th>
              <th className="px-3 py-2.5 font-medium">Projeto</th>
              <th className="px-3 py-2.5 font-medium">Mensagens</th>
              <th className="w-32 px-3 py-2.5 font-medium">Atividade</th>
              <th className="w-32 px-3 py-2.5 font-medium">Estado</th>
              <th className="w-10 px-3 py-2.5" />
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => {
              const isOpen = open.has(s.id);
              return (
                <tr
                  key={s.id}
                  tabIndex={0}
                  aria-label={`${s.title || "Sem título"}: Enter abre a conversa, Espaço mostra os detalhes`}
                  onClick={() => select(s.id)}
                  onDoubleClick={() => navigate(sessionPath(s.id))}
                  onKeyDown={(e) => {
                    if (e.target !== e.currentTarget) return;
                    if (e.key === "Enter") navigate(sessionPath(s.id));
                    else if (e.key === " ") {
                      e.preventDefault();
                      select(s.id);
                    }
                  }}
                  title="Clique para detalhes, duplo clique para abrir a conversa"
                  className={cn(
                    "cursor-default border-b transition-colors outline-none last:border-b-0 hover:bg-primary/[0.05] focus-visible:bg-primary/[0.07] focus-visible:shadow-[inset_2px_0_0_var(--primary)]",
                    selectedId === s.id && "bg-primary/[0.08]",
                  )}
                >
                  <td className="px-5 py-2.5">
                    <SessionTitle title={s.title} className="block font-medium" />
                    <span className="font-mono text-[10.5px] text-faint">{s.id.split(":")[1]?.slice(0, 8)}…</span>
                  </td>
                  <td className="px-3 py-2.5">
                    <ProviderTag provider={s.provider} />
                  </td>
                  <td className="truncate px-3 py-2.5 font-mono text-[11.5px] text-muted-foreground" title={s.projectPath}>
                    {s.projectName || "—"}
                  </td>
                  <td className="px-3 py-2.5">
                    <span className="flex items-center gap-2">
                      <span className="h-1.5 w-24 overflow-hidden rounded-full bg-white/[0.07]">
                        <span className="block h-full rounded-full bg-primary shadow-[0_0_10px_rgba(56,225,245,0.6)]" style={{ width: `${(s.messageCount / max) * 100}%` }} />
                      </span>
                      <span className="text-muted-foreground tabular-nums">{s.messageCount.toLocaleString("pt-BR")}</span>
                    </span>
                  </td>
                  <td className="px-3 py-2.5 text-muted-foreground" title={s.updatedAt ? dateTime(s.updatedAt) : undefined}>
                    {relativeTime(s.updatedAt)}
                  </td>
                  <td className="px-3 py-2.5">
                    <span className="inline-flex items-center gap-2 text-muted-foreground">
                      <span
                        className={cn(
                          "size-1.5 rounded-full",
                          isOpen ? "bg-positive shadow-[0_0_0_3px_rgba(94,234,212,0.2),0_0_10px_rgba(94,234,212,0.7)]" : "bg-faint",
                        )}
                      />
                      {isOpen ? "Aberta" : "Concluída"}
                    </span>
                  </td>
                  <td className="px-3 py-2.5" onClick={(e) => e.stopPropagation()} onDoubleClick={(e) => e.stopPropagation()}>
                    <FavoriteButton id={s.id} favorite={s.favorite} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
    </section>
  );
}
