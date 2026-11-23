import { Star } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { SessionList } from "./SessionList";

export function FavoritesPage() {
  return (
    <>
      <PageHeader title="Favoritas" />
      <SessionList
        base={{ favoritesOnly: true }}
        allowHidden={false}
        empty={{
          icon: Star,
          title: "Nenhuma favorita",
          description: "Marque a estrela de uma sessão para encontrá-la aqui.",
        }}
      />
    </>
  );
}
