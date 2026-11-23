import { useEffect, useState, type ReactNode } from "react";
import { useNavigate } from "react-router";
import { AlertTriangle, FolderGit2, MessagesSquare, Play, Search, Star, TerminalSquare } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusGate } from "@/components/layout/StatusGate";
import { EmptyState } from "@/components/layout/EmptyState";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { BarList } from "@/components/charts/BarList";
import { BlurFade } from "@/components/ui/blur-fade";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAsync } from "@/hooks/useAsync";
import { StatsService, list, providersOf, type AppStatus, type Dashboard, type ProviderID } from "@/lib/api";
import { projectPath } from "@/lib/nav";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { useResume } from "@/features/resume/useResume";
import { ProviderPill } from "@/features/sessions/ProviderTag";
import { ActivityCard, agentKeys, type ChartMetric } from "./ActivityCard";
import { AgentsCard } from "./AgentsCard";
import { KpiCard } from "./KpiCard";
import { RecentTable } from "./RecentTable";
import { periodPhrase, summary } from "./summary";

type Period = "7d" | "30d" | "90d" | "all";

const names = Object.fromEntries(agentKeys.map((k) => [k.key, k.label]));
const fmt = (n: number) => n.toLocaleString("pt-BR");

// Pílulas dos agentes: liga e desliga cada um; o último ligado não desliga.
function AgentPills({ status, on, setOn }: { status: AppStatus; on: string[]; setOn: (v: string[]) => void }) {
  const providers = providersOf(status);
  return (
    <div role="group" aria-label="Agentes" className="flex flex-wrap gap-1.5">
      {agentKeys.map((k) => {
        const active = on.includes(k.key);
        const last = active && on.length === 1;
        return (
          <ProviderPill
            key={k.key}
            provider={k.key}
            active={active}
            disabled={last}
            title={last ? "Pelo menos um agente fica ligado" : undefined}
            count={providers.find((p) => p.id === k.key)?.sessions ?? 0}
            onClick={() => setOn(active ? on.filter((x) => x !== k.key) : [...on, k.key])}
          />
        );
      })}
    </div>
  );
}

function RankCard({ title, hint, children }: { title: string; hint: string; children: ReactNode }) {
  return (
    <section className="surface rounded-[18px] px-5 py-4" aria-label={title}>
      <h3 className="text-[15px] font-semibold tracking-tight">{title}</h3>
      <p className="mt-0.5 mb-4 text-[11.5px] text-faint">{hint}</p>
      {children}
    </section>
  );
}

function Loading() {
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-36 rounded-[18px]" />
        ))}
      </div>
      <Skeleton className="h-80 rounded-[18px]" />
    </div>
  );
}

function Body({ d, status, providers }: { d: Dashboard; status: AppStatus; providers: string[] }) {
  const navigate = useNavigate();
  const [metric, setMetric] = useState<ChartMetric>("messages");
  const totals = (s: { total: number }[]) => s.map((p) => p.total);
  const ranking = (xs: { count: number }[] | null | undefined) => (xs ?? []).length > 0;
  return (
    <div className="flex flex-col gap-4">
      <section aria-label="Indicadores" className="grid grid-cols-2 gap-4 xl:grid-cols-4">
        <KpiCard
          icon={MessagesSquare}
          label="Sessões"
          value={d.sessions.value}
          metric={d.sessions}
          meta="com atividade no período"
          series={totals(d.sessionsSeries ?? [])}
          color="var(--accent2)"
          selected={metric === "sessions"}
          onSelect={() => setMetric("sessions")}
        />
        <KpiCard
          icon={TerminalSquare}
          label="Mensagens"
          value={d.messages.value}
          metric={d.messages}
          meta="suas e dos agentes"
          series={totals(d.messagesSeries ?? [])}
          color="var(--primary)"
          selected={metric === "messages"}
          onSelect={() => setMetric("messages")}
        />
        <KpiCard
          icon={FolderGit2}
          label="Projetos ativos"
          value={d.activeProjects.value}
          metric={d.activeProjects}
          meta={`de ${fmt(d.totalProjects)} no total`}
          series={d.projectsSeries ?? []}
          color="var(--positive)"
        />
        <KpiCard
          icon={Star}
          label="Favoritas"
          value={d.favorites}
          badge={d.favoritesAdded ? `↑ ${fmt(d.favoritesAdded)}` : undefined}
          meta={`${fmt(d.openNow)} ${d.openNow === 1 ? "aberta" : "abertas"} agora`}
          series={d.favoritesSeries ?? []}
          color="#fbbf24"
        />
      </section>

      <ActivityCard d={d} metric={metric} providers={providers} />

      <div className="grid gap-4 lg:grid-cols-3">
        <AgentsCard d={d} status={status} />
        <RankCard title="Projetos mais ativos" hint="Mensagens no período">
          {ranking(d.topProjects) ? (
            <BarList
              color="accent"
              items={list(d.topProjects).map((p) => ({
                label: p.name,
                value: p.count,
                title: p.path,
                onClick: () => navigate(projectPath(p.path)),
              }))}
            />
          ) : (
            <p className="text-[13px] text-muted-foreground">Nenhum projeto com atividade.</p>
          )}
        </RankCard>
        <RankCard title="Ferramentas mais usadas" hint="Chamadas no período">
          {ranking(d.topTools) ? (
            <BarList color="accent2" items={list(d.topTools).map((t) => ({ label: t.name, value: t.count }))} />
          ) : (
            <p className="text-[13px] text-muted-foreground">Nenhuma chamada de ferramenta.</p>
          )}
        </RankCard>
      </div>

      <RecentTable d={d} />
    </div>
  );
}

function DashboardView({ status, onResumeId }: { status: AppStatus; onResumeId: (id: string) => void }) {
  const version = useAppStore((s) => s.dataVersion);
  const [period, setPeriod] = useState<Period>("30d");
  const [on, setOn] = useState<string[]>(agentKeys.map((k) => k.key));
  const all = on.length === agentKeys.length;
  const key = `${period}|${on.join(",")}`;
  // A chave vai junto do resultado: recarregar pelo dataVersion não esmaece a
  // tela, só trocar o filtro.
  const res = useAsync(
    () => StatsService.Dashboard({ period, providers: all ? [] : (on as ProviderID[]) }).then((d) => ({ key, d })),
    [key, version],
  );
  const d = res.data?.d ?? null;
  const resumeId = d?.resumeId ?? "";
  useEffect(() => onResumeId(resumeId), [resumeId, onResumeId]);
  const stale = res.loading && res.data?.key !== key;
  const detected = providersOf(status).filter((p) => p.status === "ok").length;
  const today = new Date().toLocaleDateString("pt-BR", { weekday: "long", day: "numeric", month: "long" });

  return (
    <div className="mx-auto flex w-full max-w-[1280px] flex-col gap-5 px-6 pt-6 pb-10">
      <header className="flex flex-col gap-1.5">
        <p className="text-[12px] text-faint first-letter:uppercase">
          {today} · {detected} {detected === 1 ? "agente detectado" : "agentes detectados"}
        </p>
        <h2 className="title-gradient text-[30px] leading-tight font-semibold tracking-tight">Suas sessões</h2>
        <p className="max-w-3xl text-[14px] leading-relaxed text-muted-foreground">
          {d
            ? summary(d, names).map((s, i) =>
                s.bold ? (
                  <strong key={i} className="font-semibold text-foreground">
                    {s.text}
                  </strong>
                ) : (
                  <span key={i}>{s.text}</span>
                ),
              )
            : `Carregando ${periodPhrase(period)}…`}
        </p>
      </header>

      <div className="flex flex-wrap items-center gap-3">
        <Segmented
          label="Período"
          value={period}
          onChange={setPeriod}
          options={[
            { value: "7d", label: "7 dias" },
            { value: "30d", label: "30 dias" },
            { value: "90d", label: "90 dias" },
            { value: "all", label: "Tudo" },
          ]}
        />
        <AgentPills status={status} on={on} setOn={setOn} />
        <span className="ml-auto flex items-center gap-2 text-[11.5px] text-faint">
          <span className="size-1.5 rounded-full bg-positive shadow-[0_0_8px_rgba(94,234,212,0.7)]" />
          Atualiza sozinho
        </span>
      </div>

      {res.error && !d ? (
        <EmptyState icon={AlertTriangle} title="Falha ao carregar" description={res.error} />
      ) : !d ? (
        <Loading />
      ) : (
        <div className={cn("transition-opacity duration-200", stale && "opacity-50")}>
          <BlurFade>
            <Body d={d} status={status} providers={on} />
          </BlurFade>
        </div>
      )}
    </div>
  );
}

// Ações do cabeçalho: busca e "Retomar última".
function HeaderActions({ id }: { id: string }) {
  const navigate = useNavigate();
  const { resume } = useResume();
  return (
    <>
      <button
        type="button"
        onClick={() => navigate("/search")}
        className="hidden h-8 w-64 items-center gap-2 whitespace-nowrap rounded-[10px] border bg-white/[0.03] px-3 text-[12.5px] text-faint transition-colors outline-none hover:border-[var(--border-strong)] hover:text-muted-foreground focus-visible:ring-2 focus-visible:ring-ring/50 md:flex"
      >
        <Search className="size-3.5" />
        <span className="truncate">Buscar nas conversas…</span>
        <kbd className="ml-auto rounded border bg-white/[0.04] px-1.5 font-mono text-[10px]">Ctrl K</kbd>
      </button>
      <Tooltip>
        <TooltipTrigger asChild>
          <span>
            <Button size="sm" disabled={!id} onClick={() => resume(id)}>
              <Play /> Retomar última
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>{id ? "Abre no terminal a sessão mais recente" : "Nenhuma sessão para retomar"}</TooltipContent>
      </Tooltip>
    </>
  );
}

export function DashboardPage() {
  const [resumeId, setResumeId] = useState("");
  return (
    <>
      <PageHeader title="Dashboard" actions={<HeaderActions id={resumeId} />} />
      <ScrollArea className="min-h-0 flex-1">
        <StatusGate>{(status) => <DashboardView status={status} onResumeId={setResumeId} />}</StatusGate>
      </ScrollArea>
    </>
  );
}
