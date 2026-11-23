import { useCallback } from "react";
import { ResumeService, errorMessage, type ResumePlan } from "@/lib/api";
import { useAppStore } from "@/lib/store";

// Retoma uma sessão no terminal. O backend valida tudo (sessão, CLI,
// diretório, terminal) e monta o comando; aqui só há o fluxo de interface:
// sessão possivelmente aberta em outro lugar pede confirmação antes.
export function useResume() {
  const notify = useAppStore((s) => s.notify);
  const setConfirm = useAppStore((s) => s.setConfirmResume);

  const launch = useCallback(
    async (plan: ResumePlan, allowActive: boolean) => {
      try {
        const done = await ResumeService.Resume(plan.sessionId, allowActive);
        notify(`Sessão aberta no ${done.terminal}.`);
      } catch (err) {
        notify(errorMessage(err), "error");
      }
    },
    [notify],
  );

  const resume = useCallback(
    async (id: string) => {
      let plan: ResumePlan;
      try {
        plan = await ResumeService.Plan(id);
      } catch (err) {
        notify(errorMessage(err), "error");
        return;
      }
      if (plan.active) setConfirm(plan);
      else await launch(plan, false);
    },
    [launch, notify, setConfirm],
  );

  return { resume, launch };
}
