import { useState } from "react";
import { Star } from "lucide-react";
import { motion } from "motion/react";
import { Button } from "@/components/ui/button";
import { SessionService, errorMessage } from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";

// Estrela de favorito com atualização otimista; reverte se o backend falhar.
export function FavoriteButton({ id, favorite, size = "icon-xs" }: { id: string; favorite: boolean; size?: "icon-xs" | "icon-sm" }) {
  const touch = useAppStore((s) => s.touch);
  const [on, setOn] = useState(favorite);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [synced, setSynced] = useState(favorite);
  if (favorite !== synced) {
    // Dado novo do backend (recarga da lista): sincroniza.
    setSynced(favorite);
    setOn(favorite);
  }

  const toggle = async () => {
    const next = !on;
    setOn(next);
    setBusy(true);
    setError(null);
    try {
      await SessionService.SetFavorite(id, next);
      touch();
    } catch (err) {
      setOn(!next);
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const label = on ? "Remover das favoritas" : "Adicionar às favoritas";
  return (
    <Button
      variant="ghost"
      size={size}
      aria-label={label}
      aria-pressed={on}
      title={error ?? label}
      disabled={busy}
      onClick={toggle}
      className={cn(error && "text-destructive")}
    >
      {/* "Salta" ao marcar: mola na escala, com brilho dourado. */}
      <motion.span
        key={on ? "on" : "off"}
        className="inline-flex"
        initial={on ? { scale: 0.4, rotate: -30 } : false}
        animate={{ scale: 1, rotate: 0 }}
        transition={{ type: "spring", stiffness: 500, damping: 14 }}
      >
        <Star
          className={cn(
            "transition-colors",
            on ? "fill-amber-400 text-amber-400 drop-shadow-[0_0_6px_rgba(251,191,36,0.55)]" : "text-muted-foreground",
          )}
        />
      </motion.span>
    </Button>
  );
}
