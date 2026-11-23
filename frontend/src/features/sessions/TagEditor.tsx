import { useState } from "react";
import { Plus, X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SessionService, errorMessage, list, type TagItem } from "@/lib/api";
import { useAppStore } from "@/lib/store";

// Adiciona e remove tags de uma sessão. Validação e normalização ficam no
// backend; aqui só se exibe o erro devolvido.
export function TagEditor({ id, tags, known }: { id: string; tags: string[]; known: TagItem[] }) {
  const touch = useAppStore((s) => s.touch);
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (op: () => Promise<string[] | null>) => {
    setBusy(true);
    setError(null);
    try {
      await op();
      touch();
      return true;
    } catch (err) {
      setError(errorMessage(err));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const add = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!value.trim()) return;
    if (await run(() => SessionService.AddTag(id, value))) setValue("");
  };

  const listId = `tags-${id}`;
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-1.5">
        {tags.length === 0 && <span className="text-xs text-muted-foreground">Sem tags</span>}
        <AnimatePresence initial={false} mode="popLayout">
        {tags.map((t) => (
          <motion.span
            layout
            key={t}
            initial={{ opacity: 0, scale: 0.8 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.8 }}
            transition={{ type: "spring", stiffness: 500, damping: 30 }}
            className="inline-flex items-center gap-0.5 rounded-md border border-accent2/25 bg-accent2/10 py-0.5 pr-0.5 pl-1.5 text-xs text-accent2"
          >
            #{t}
            <Button
              variant="ghost"
              size="icon-xs"
              className="size-4"
              aria-label={`Remover tag ${t}`}
              disabled={busy}
              onClick={() => run(() => SessionService.RemoveTag(id, t))}
            >
              <X />
            </Button>
          </motion.span>
        ))}
        </AnimatePresence>
      </div>
      <form onSubmit={add} className="flex gap-1.5">
        <Input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="Nova tag"
          aria-label="Nova tag"
          maxLength={40}
          list={listId}
          className="h-7 text-xs"
          aria-invalid={error ? true : undefined}
        />
        <datalist id={listId}>
          {list(known)
            .filter((k) => !tags.some((t) => t.toLowerCase() === k.name.toLowerCase()))
            .map((k) => (
              <option key={k.name} value={k.name} />
            ))}
        </datalist>
        <Button type="submit" variant="outline" size="icon-sm" aria-label="Adicionar tag" disabled={busy || !value.trim()}>
          <Plus />
        </Button>
      </form>
      {error && <p className="text-xs text-destructive" role="alert">{error}</p>}
    </div>
  );
}
