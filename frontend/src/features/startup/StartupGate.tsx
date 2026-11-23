import { useEffect, useState, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { StartupService, type StartupError } from "@/lib/api";

// Se a inicialização falhou (índice ou dados do usuário inacessíveis), o
// backend sobe só com o StartupService e a interface mostra o erro, em vez
// de o app fechar em silêncio quando aberto pelo menu.
export function StartupGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<StartupError | null>(null);
  useEffect(() => {
    StartupService.Error().then(setState, () => setState({ message: "", logFile: "" }));
  }, []);
  if (!state) return null;
  if (!state.message) return <>{children}</>;
  return (
    <div className="flex h-screen items-center justify-center p-8 text-foreground">
      <div className="aurora-bg" aria-hidden />
      <div className="glass max-w-lg space-y-4 rounded-[18px] border p-6 shadow-[0_30px_80px_-20px_rgba(0,0,0,0.9)]">
        <div className="flex items-center gap-2.5">
          <AlertTriangle className="size-5 text-destructive" />
          <h1 className="text-base font-medium">O SessionFlow não conseguiu iniciar</h1>
        </div>
        <p className="rounded-md border bg-muted/40 p-3 font-mono text-xs break-words">{state.message}</p>
        <p className="text-sm text-muted-foreground">
          Os históricos dos agentes não foram alterados. Verifique permissões e espaço em disco e abra o app de novo. Detalhes no log:
        </p>
        {state.logFile && <code className="block font-mono text-xs break-all select-all">{state.logFile}</code>}
      </div>
    </div>
  );
}
