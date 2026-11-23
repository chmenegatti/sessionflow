import { MessagesSquare } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { SessionList } from "./SessionList";

export function SessionsPage() {
  return (
    <>
      <PageHeader title="Sessões" />
      <SessionList
        empty={{
          icon: MessagesSquare,
          title: "Nenhuma sessão encontrada",
          description: "Ajuste os filtros ou aguarde a indexação dos históricos do Claude Code, Codex e OpenCode.",
        }}
      />
    </>
  );
}
