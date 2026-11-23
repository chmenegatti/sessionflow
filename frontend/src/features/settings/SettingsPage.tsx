import { useState, type ReactNode } from "react";
import { AlertTriangle, Check, Loader2, RotateCcw, TerminalSquare } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusGate } from "@/components/layout/StatusGate";
import { useAsync } from "@/hooks/useAsync";
import {
  SettingsService,
  errorMessage,
  list,
  providersOf,
  type AppStatus,
  type ProviderSettings,
  type SettingsView,
  type TerminalSettings,
} from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { ProviderBadge } from "./ProviderBadge";
import { providerColor } from "@/features/sessions/ProviderTag";

function PathRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[10rem_1fr] items-baseline gap-4 px-4 py-2.5">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="truncate font-mono text-xs text-muted-foreground" title={value}>
        {value || "—"}
      </dd>
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <label className="grid gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {children}
      {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
    </label>
  );
}

// Salvar com estado: ocupado, erro do backend (validação) e confirmação.
function useSave(onSaved: () => void) {
  const notify = useAppStore((s) => s.notify);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      notify("Configuração salva.");
      onSaved();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };
  return { busy, error, run };
}

function ProviderForm({ p, status, disabled, onSaved }: { p: ProviderSettings; status?: AppStatus; disabled: boolean; onSaved: () => void }) {
  const [enabled, setEnabled] = useState(p.enabled);
  const [root, setRoot] = useState(p.rootDir);
  const [cli, setCli] = useState(p.cliPath);
  const save = useSave(onSaved);
  const st = providersOf(status ?? ({} as AppStatus)).find((x) => x.id === p.id);
  const dirty = enabled !== p.enabled || root !== p.rootDir || cli !== p.cliPath;

  return (
    <div className="space-y-3 px-4 py-4">
      <div className="flex items-center gap-3">
        <span className="flex flex-1 items-center gap-2 text-sm font-medium">
          <span className="size-2 rounded-full" style={{ background: providerColor(p.id), boxShadow: `0 0 8px ${providerColor(p.id)}` }} />
          {p.name}
        </span>
        {st && <ProviderBadge provider={st} />}
        <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <input type="checkbox" className="accent-primary" checked={enabled} disabled={disabled} onChange={(e) => setEnabled(e.target.checked)} />
          Habilitado
        </label>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Diretório de dados" hint="Vale ao reiniciar o SessionFlow.">
          <Input
            value={root}
            disabled={disabled}
            placeholder={p.defaultRoot}
            className="font-mono text-xs"
            spellCheck={false}
            onChange={(e) => setRoot(e.target.value)}
          />
        </Field>
        <Field
          label="Executável da CLI"
          hint={p.detectedCli ? <>Em uso: <code className="font-mono">{p.detectedCli}</code></> : p.enabled ? "Não encontrado: informe o caminho." : "Provider desabilitado."}
        >
          <Input
            value={cli}
            disabled={disabled}
            placeholder="detectar automaticamente"
            className="font-mono text-xs"
            spellCheck={false}
            onChange={(e) => setCli(e.target.value)}
          />
        </Field>
      </div>
      <div className="flex items-center gap-2">
        {save.error && (
          <p className="flex-1 text-xs text-destructive" role="alert">
            {save.error}
          </p>
        )}
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          disabled={disabled || !dirty || save.busy}
          onClick={() =>
            save.run(async () => {
              if (cli !== p.cliPath) await SettingsService.SetCLIPath(p.id, cli);
              if (enabled !== p.enabled || root !== p.rootDir) await SettingsService.SetProvider(p.id, enabled, root);
            })
          }
        >
          {save.busy ? <Loader2 className="animate-spin" /> : <Check />} Salvar
        </Button>
      </div>
    </div>
  );
}

function TerminalForm({ t, disabled, onSaved }: { t: TerminalSettings; disabled: boolean; onSaved: () => void }) {
  const [path, setPath] = useState(t.path);
  const [args, setArgs] = useState(list(t.args).join("\n"));
  const save = useSave(onSaved);
  const dirty = path !== t.path || args !== list(t.args).join("\n");
  return (
    <div className="space-y-3 px-4 py-4">
      <p className="flex items-center gap-2 text-xs text-muted-foreground">
        <TerminalSquare className="size-3.5" />
        {t.resolved ? (
          <>
            Em uso: <code className="font-mono text-foreground">{t.resolved}</code>
          </>
        ) : (
          <span className="text-destructive">{t.error}</span>
        )}
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label="Emulador" hint="Vazio = detectar (ptyxis, gnome-terminal, konsole, kitty…).">
          <Input
            value={path}
            disabled={disabled}
            placeholder="/usr/bin/…"
            className="font-mono text-xs"
            spellCheck={false}
            onChange={(e) => setPath(e.target.value)}
          />
        </Field>
        <Field
          label="Argumentos (um por linha, opcional)"
          hint={
            <>
              <code className="font-mono">{"{dir}"}</code> = diretório, <code className="font-mono">{"{cmd}"}</code> = comando. Nada passa por shell.
            </>
          }
        >
          <textarea
            value={args}
            disabled={disabled}
            rows={3}
            placeholder={"--working-directory={dir}\n-e\n{cmd}"}
            spellCheck={false}
            className="rounded-[10px] border bg-white/[0.03] px-2.5 py-1.5 font-mono text-xs outline-none transition-[border-color,box-shadow] focus-visible:border-primary/50 focus-visible:shadow-[0_0_0_3px_rgba(56,225,245,0.12)] disabled:opacity-50"
            onChange={(e) => setArgs(e.target.value)}
          />
        </Field>
      </div>
      <div className="flex items-center gap-2">
        {save.error && (
          <p className="flex-1 text-xs text-destructive" role="alert">
            {save.error}
          </p>
        )}
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          disabled={disabled || !dirty || save.busy}
          onClick={() => save.run(() => SettingsService.SetTerminal(path, args.split("\n")))}
        >
          {save.busy ? <Loader2 className="animate-spin" /> : <Check />} Salvar
        </Button>
      </div>
    </div>
  );
}

function Settings({ status }: { status: AppStatus }) {
  // Recarrega só depois de salvar: eventos de indexação (watcher) não podem
  // apagar o que o usuário está digitando. Cada formulário é recriado
  // quando o valor salvo dele muda (key = valor).
  const [formKey, setFormKey] = useState(0);
  const view = useAsync(() => SettingsService.Get(), [formKey]);
  const reload = () => setFormKey((n) => n + 1);
  const v: SettingsView | null = view.data;

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-5 px-6 pt-6 pb-10">
      <header>
        <h2 className="title-gradient text-[26px] font-semibold tracking-tight">Configurações</h2>
        <p className="mt-1 text-[13px] text-muted-foreground">Agentes, terminal e arquivos locais. Nada sai desta máquina.</p>
      </header>
      {v && !v.writable && (
        <p className="flex items-start gap-2 rounded-xl border border-negative/40 bg-negative/[0.07] p-3 text-sm shadow-[0_0_24px_-10px_rgba(251,113,133,0.6)]">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-destructive" />
          <span>
            <code className="font-mono">config.json</code> está inválido; o SessionFlow usa os padrões e não grava por cima. Corrija o arquivo à mão.
          </span>
        </p>
      )}
      {v?.restartRequired && (
        <p className="flex items-center gap-2 rounded-xl border border-amber-400/40 bg-amber-400/[0.07] p-3 text-sm shadow-[0_0_24px_-10px_rgba(251,191,36,0.6)]">
          <RotateCcw className="size-4 shrink-0 text-amber-400" /> Reinicie o SessionFlow para aplicar diretórios e providers alterados.
        </p>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Providers</CardTitle>
          <CardDescription>Diretório dos históricos e executável usado para retomar sessões.</CardDescription>
        </CardHeader>
        <CardContent className="divide-y p-0">
          {view.error ? (
            <p className="px-4 py-3 text-sm text-destructive">{view.error}</p>
          ) : !v ? (
            <div className="space-y-3 p-4">
              <Skeleton className="h-20" />
              <Skeleton className="h-20" />
            </div>
          ) : (
            list(v.providers).map((p) => <ProviderForm key={JSON.stringify(p)} p={p} status={status} disabled={!v.writable} onSaved={reload} />)
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Terminal</CardTitle>
          <CardDescription>Onde as sessões são retomadas (Ctrl+Enter).</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {v ? <TerminalForm key={JSON.stringify(v.terminal)} t={v.terminal} disabled={!v.writable} onSaved={reload} /> : <Skeleton className="m-4 h-20" />}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Arquivos locais</CardTitle>
          <CardDescription>Tudo fica nesta máquina. O índice pode ser apagado e é reconstruído a partir dos históricos.</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <dl className="divide-y">
            <PathRow label="Configuração" value={status.configFile} />
            <PathRow label="Índice" value={status.indexDb} />
            <PathRow label="Dados do usuário" value={status.userDb} />
            <PathRow label="Log" value={status.logFile} />
          </dl>
        </CardContent>
      </Card>

      <p className="text-xs text-faint">SessionFlow v{status.version}</p>
    </div>
  );
}

export function SettingsPage() {
  return (
    <>
      <PageHeader title="Configurações" />
      <ScrollArea className="min-h-0 flex-1">
        <StatusGate>{(status) => <Settings status={status} />}</StatusGate>
      </ScrollArea>
    </>
  );
}
