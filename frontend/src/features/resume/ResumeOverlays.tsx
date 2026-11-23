import { useEffect, useRef } from "react";
import { AnimatePresence, motion } from "motion/react";
import { AlertTriangle, CheckCircle2, TerminalSquare, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useAppStore } from "@/lib/store";
import { useResume } from "./useResume";

// Confirmação de retomada de sessão alterada há menos de um minuto (pode
// estar aberta em outro terminal; dois processos na mesma sessão podem
// perder mensagens). Diálogo da própria interface, não do navegador.
export function ResumeConfirm() {
  const plan = useAppStore((s) => s.confirmResume);
  const close = useAppStore((s) => s.setConfirmResume);
  const { launch } = useResume();
  // Foco inicial em Cancelar: um Enter distraído não retoma sessão ativa.
  const cancelRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!plan) return;
    cancelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close(null);
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [plan, close]);

  return (
    <AnimatePresence>
      {plan && (
        <motion.div
          className="fixed inset-0 z-50 flex items-center justify-center bg-[#03050d]/60 p-6 backdrop-blur-[2px]"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          onClick={() => close(null)}
        >
          <motion.div
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="resume-confirm-title"
            className="glass w-full max-w-md space-y-4 rounded-[18px] border p-5 shadow-[0_30px_80px_-20px_rgba(0,0,0,0.9),inset_0_1px_0_rgba(255,255,255,0.08)]"
            initial={{ scale: 0.96, y: 8 }}
            animate={{ scale: 1, y: 0 }}
            exit={{ scale: 0.96, y: 8 }}
            transition={{ type: "spring", bounce: 0, duration: 0.25 }}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-start gap-3">
              <AlertTriangle className="mt-0.5 size-5 shrink-0 text-amber-400" />
              <div className="space-y-1.5">
                <h2 id="resume-confirm-title" className="text-sm font-medium">
                  A sessão pode estar aberta
                </h2>
                <p className="text-sm text-muted-foreground">
                  Ela foi alterada há menos de um minuto. Se estiver aberta em outro terminal, as duas instâncias gravam no mesmo histórico e mensagens
                  podem se perder.
                </p>
              </div>
            </div>
            <code className="block truncate rounded-md bg-muted/60 px-2.5 py-1.5 font-mono text-[11px]" title={plan.command}>
              {plan.command}
            </code>
            <div className="flex justify-end gap-2">
              <Button ref={cancelRef} variant="ghost" size="sm" onClick={() => close(null)}>
                Cancelar
              </Button>
              <Button
                size="sm"
                onClick={() => {
                  close(null);
                  void launch(plan, true);
                }}
              >
                <TerminalSquare /> Retomar mesmo assim
              </Button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}

// Aviso curto no canto inferior; some sozinho.
export function NoticeToast() {
  const notice = useAppStore((s) => s.notice);
  const dismiss = useAppStore((s) => s.dismiss);
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(dismiss, notice.tone === "error" ? 8000 : 4000);
    return () => clearTimeout(t);
  }, [notice, dismiss]);
  const Icon = notice?.tone === "error" ? AlertTriangle : CheckCircle2;
  return (
    <div className="pointer-events-none fixed right-4 bottom-4 z-40" aria-live="polite">
      <AnimatePresence>
        {notice && (
          <motion.div
            key={notice.id}
            role={notice.tone === "error" ? "alert" : "status"}
            className="glass pointer-events-auto flex max-w-sm items-start gap-2.5 rounded-xl border px-3.5 py-2.5 text-sm shadow-[0_20px_50px_-15px_rgba(0,0,0,0.9),inset_0_1px_0_rgba(255,255,255,0.08)]"
            initial={{ opacity: 0, y: 12, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 8 }}
            transition={{ type: "spring", bounce: 0, duration: 0.3 }}
          >
            <Icon className={notice.tone === "error" ? "mt-0.5 size-4 shrink-0 text-destructive" : "mt-0.5 size-4 shrink-0 text-emerald-400"} />
            <p className="flex-1 break-words">{notice.text}</p>
            <button type="button" aria-label="Fechar aviso" className="text-muted-foreground hover:text-foreground" onClick={dismiss}>
              <X className="size-3.5" />
            </button>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
