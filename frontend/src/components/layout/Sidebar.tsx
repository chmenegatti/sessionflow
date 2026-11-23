import { useEffect, useState } from "react";
import { NavLink, useNavigate } from "react-router";
import { motion } from "motion/react";
import { Hash } from "lucide-react";
import { TextShimmer } from "@/components/ui/text-shimmer";
import { cn } from "@/lib/utils";
import { mainNav, projectPath, settingsNav, type NavItem } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { useAsync } from "@/hooks/useAsync";
import { StatsService, list, providersOf } from "@/lib/api";
import { AppLogo } from "./AppLogo";

const spring = { type: "spring", bounce: 0.15, duration: 0.35 } as const;

function SidebarLink({ item }: { item: NavItem }) {
  const Icon = item.icon;
  return (
    <NavLink
      to={item.to}
      end={item.to === "/"}
      className={({ isActive }) =>
        cn(
          "group relative flex h-9 items-center gap-2.5 rounded-[10px] px-2.5 text-[13px] font-medium text-sidebar-foreground/60 outline-none transition-colors",
          "hover:text-sidebar-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
          isActive && "text-sidebar-foreground",
        )
      }
    >
      {({ isActive }) => (
        <>
          {/* Fundo do item ativo desliza entre os itens; marcador em gradiente com brilho. */}
          {isActive && (
            <motion.span
              layoutId="nav-active"
              transition={spring}
              className="absolute inset-0 rounded-[10px] bg-white/[0.07] shadow-[inset_0_1px_0_rgba(255,255,255,0.08),0_8px_24px_-12px_rgba(0,0,0,0.8)]"
            >
              <span className="absolute top-2 bottom-2 -left-2 w-[3px] rounded-full bg-linear-to-b from-primary to-accent2 shadow-[0_0_12px_rgba(56,225,245,0.7)]" />
            </motion.span>
          )}
          <Icon className={cn("relative size-4 shrink-0 transition-transform group-hover:scale-110", isActive && "text-primary")} />
          <span className="relative flex-1 truncate">{item.label}</span>
          {item.shortcut && (
            <kbd className="relative font-mono text-[10px] text-faint opacity-0 transition-opacity group-hover:opacity-100">{item.shortcut}</kbd>
          )}
        </>
      )}
    </NavLink>
  );
}

// "Sincronizado · há N s", atualizado a cada 5 s.
function useSince(iso: string | null | undefined) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(t);
  }, []);
  if (!iso) return null;
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return `há ${s} s`;
  if (s < 3600) return `há ${Math.round(s / 60)} min`;
  return `há ${Math.round(s / 3600)} h`;
}

export function Sidebar() {
  const navigate = useNavigate();
  const status = useAppStore((s) => s.status);
  const version = useAppStore((s) => s.dataVersion);
  const scanning = status?.scan.running ?? false;
  const pinned = useAsync(() => StatsService.PinnedProjects(), [version]);
  const since = useSince(status?.scan.finishedAt);
  const sources = providersOf(status ?? ({} as never)).reduce((n, p) => n + p.sources, 0);

  return (
    <aside className="glass flex w-60 shrink-0 flex-col border-r">
      <div className="flex h-14 items-center gap-2.5 px-4">
        <AppLogo className="size-7 drop-shadow-[0_4px_12px_rgba(56,225,245,0.35)]" />
        <span className="text-[15px] font-semibold tracking-tight">SessionFlow</span>
      </div>
      <nav aria-label="Principal" className="flex flex-1 flex-col gap-0.5 overflow-y-auto px-3 py-2">
        {mainNav.map((item) => (
          <SidebarLink key={item.to} item={item} />
        ))}
        {list(pinned.data).length > 0 && (
          <>
            <p className="px-2.5 pt-5 pb-1.5 text-[10.5px] font-medium tracking-[0.08em] text-faint uppercase">Projetos fixados</p>
            {list(pinned.data).map((p) => (
              <button
                key={p.path}
                type="button"
                title={p.path}
                onClick={() => navigate(projectPath(p.path))}
                className="group flex h-8 items-center gap-2.5 rounded-[10px] px-2.5 text-left text-[13px] text-sidebar-foreground/60 outline-none transition-colors hover:bg-white/[0.05] hover:text-sidebar-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
              >
                <Hash className="size-3.5 shrink-0 text-faint group-hover:text-primary" />
                <span className="flex-1 truncate">{p.name}</span>
                <span className="font-mono text-[10px] text-faint">{p.count.toLocaleString("pt-BR")}</span>
              </button>
            ))}
          </>
        )}
      </nav>
      <div className="flex flex-col gap-2 border-t border-sidebar-border px-3 py-3">
        <SidebarLink item={settingsNav} />
        <div className="surface rounded-xl px-3 py-2.5 text-[11.5px] text-muted-foreground">
          {scanning ? (
            <div className="flex items-center gap-2">
              <span className="relative flex size-2">
                <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary/70" />
                <span className="relative inline-flex size-2 rounded-full bg-primary" />
              </span>
              <TextShimmer className="text-[11.5px]">Indexando históricos…</TextShimmer>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <span className="size-2 rounded-full bg-positive shadow-[0_0_0_3px_rgba(94,234,212,0.18),0_0_12px_rgba(94,234,212,0.6)]" />
              <span>Sincronizado{since ? ` · ${since}` : ""}</span>
            </div>
          )}
          <p className="mt-1 text-[10.5px] text-faint">
            {sources > 0 ? `${sources.toLocaleString("pt-BR")} fontes · ` : ""}v{status?.version ?? ""}
          </p>
          <div className="mt-2 h-1 overflow-hidden rounded-full bg-white/[0.06]">
            <div className={cn("h-full rounded-full bg-linear-to-r from-primary to-accent2", scanning ? "w-2/3 animate-pulse" : "w-full")} />
          </div>
        </div>
      </div>
    </aside>
  );
}
