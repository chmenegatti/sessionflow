import { Component, type ReactNode } from "react";
import { AlertTriangle, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "./EmptyState";

// Erro de renderização numa página vira um aviso no lugar dela, em vez de
// desmontar o app inteiro (janela em branco). Trocar de página recomeça
// (key = rota). A mensagem do erro não inclui conteúdo das conversas.
export class PageErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error("erro ao renderizar a página:", error.message);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <EmptyState
        icon={AlertTriangle}
        title="Algo deu errado nesta tela"
        description="Os dados não foram afetados. Tente de novo ou abra outra página."
        action={
          <Button variant="outline" size="sm" onClick={() => this.setState({ error: null })}>
            <RotateCw /> Tentar de novo
          </Button>
        }
      />
    );
  }
}
