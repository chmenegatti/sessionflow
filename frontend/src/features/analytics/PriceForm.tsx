import { useState, type FormEvent, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import {
  CostService,
  ProviderID,
  type PriceInput,
  type PriceRevision,
} from "@/lib/api";
import { providerName } from "@/features/sessions/ProviderTag";
import { inclusiveEnd, localDate } from "./costFormat";

export type PriceFormMode =
  | { kind: "add" }
  | { kind: "edit"; rev: PriceRevision }
  | { kind: "from"; rev: PriceRevision };

const categories = [
  { key: "input", label: "Entrada", hint: "tokens de entrada sem cache" },
  { key: "output", label: "Saída", hint: "inclui reasoning" },
  { key: "cacheRead", label: "Cache lido", hint: "" },
  { key: "cacheCreation", label: "Cache criado", hint: "" },
] as const;

type Cat = (typeof categories)[number]["key"];

const today = () => new Date().toLocaleDateString("sv-SE");

function initial(mode: PriceFormMode, currency: string): PriceInput {
  if (mode.kind === "add") {
    return {
      provider: ProviderID.ProviderClaude,
      model: "",
      currency,
      unitTokens: 1_000_000,
      input: "",
      output: "",
      cacheRead: "",
      cacheCreation: "",
      validFrom: today(),
      validTo: "",
      source: "",
      note: "",
    };
  }
  const r = mode.rev;
  return {
    provider: r.provider,
    model: r.model,
    currency: r.currency,
    unitTokens: r.unitTokens,
    input: r.input ?? "",
    output: r.output ?? "",
    cacheRead: r.cacheRead ?? "",
    cacheCreation: r.cacheCreation ?? "",
    validFrom: mode.kind === "from" ? today() : localDate(r.validFrom),
    validTo: mode.kind === "from" ? "" : inclusiveEnd(r.validTo),
    source: mode.kind === "from" ? "" : r.source,
    note: mode.kind === "from" ? "" : r.note,
  };
}

const titles = {
  add: "Novo preço",
  edit: "Editar preço",
  from: "Novo preço a partir de uma data",
};

// Formulário de preço. Categoria vazia = sem preço (não vira zero: a
// estimativa dessa categoria fica parcial). Validação no backend.
export function PriceForm({
  mode,
  defaultCurrency,
  onDone,
  onCancel,
}: {
  mode: PriceFormMode;
  defaultCurrency: string;
  onDone: (message: string) => void;
  onCancel: () => void;
}) {
  const [f, setF] = useState<PriceInput>(() => initial(mode, defaultCurrency));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const set = (patch: Partial<PriceInput>) => setF((x) => ({ ...x, ...patch }));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const body: PriceInput = {
      ...f,
      input: f.input || null,
      output: f.output || null,
      cacheRead: f.cacheRead || null,
      cacheCreation: f.cacheCreation || null,
    };
    try {
      if (mode.kind === "add") {
        await CostService.AddPrice(body);
        onDone("Preço cadastrado");
      } else if (mode.kind === "edit") {
        await CostService.EditPrice(mode.rev.id, body);
        onDone(
          "Preço corrigido (nova revisão). Use Recalcular para aplicar às estimativas já calculadas.",
        );
      } else {
        const res = await CostService.NewPriceFrom(mode.rev.id, body);
        onDone(
          res.stale > 0
            ? `Preço novo cadastrado. ${res.stale.toLocaleString("pt-BR")} ${res.stale === 1 ? "estimativa" : "estimativas"} a partir dessa data ${res.stale === 1 ? "continua" : "continuam"} com o preço anterior: use Recalcular em Custos para aplicar.`
            : "Preço novo cadastrado",
        );
      }
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err));
      setBusy(false);
    }
  };
  const fixed = mode.kind !== "add"; // agente e modelo identificam o preço
  return (
    <form
      onSubmit={submit}
      className="surface flex flex-col gap-4 rounded-[18px] border border-primary/25 px-5 py-4"
      aria-label={titles[mode.kind]}
    >
      <h3 className="text-[15px] font-semibold tracking-tight">
        {titles[mode.kind]}
      </h3>
      {mode.kind === "edit" && (
        <p className="flex items-start gap-2 rounded-lg border border-amber-400/30 bg-amber-400/[0.06] px-3 py-2 text-[12px] text-amber-200">
          <AlertTriangle className="mt-px size-3.5 shrink-0" />
          Editar corrige o preço e cria uma revisão nova. Estimativas já
          calculadas não mudam; use Recalcular para aplicar. Para registrar uma
          mudança de preço no tempo, use "Novo preço a partir de".
        </p>
      )}
      {mode.kind === "from" && (
        <p className="text-[12px] text-muted-foreground">
          O preço atual passa a valer até o dia anterior ao início deste.
          Respostas a partir dessa data que já têm estimativa continuam com o
          preço anterior até você usar Recalcular em Custos; respostas novas
          usam este preço.
        </p>
      )}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Agente">
          <NativeSelect
            className="h-8 w-full"
            value={f.provider}
            disabled={fixed}
            onChange={(e) => set({ provider: e.target.value as ProviderID })}
          >
            {[
              ProviderID.ProviderClaude,
              ProviderID.ProviderCodex,
              ProviderID.ProviderOpenCode,
            ].map((p) => (
              <option key={p} value={p}>
                {providerName(p)}
              </option>
            ))}
          </NativeSelect>
        </Field>
        <Field
          label="Modelo"
          hint="id exato ou prefixo com * (ex.: claude-opus-*)"
        >
          <Input
            value={f.model}
            disabled={fixed}
            required
            onChange={(e) => set({ model: e.target.value })}
            placeholder="claude-opus-5-5"
          />
        </Field>
        <Field label="Moeda" hint="código de 3 letras">
          <Input
            value={f.currency}
            maxLength={3}
            required
            onChange={(e) => set({ currency: e.target.value.toUpperCase() })}
          />
        </Field>
        <Field label="Unidade">
          <NativeSelect
            className="h-8 w-full"
            value={f.unitTokens}
            onChange={(e) => set({ unitTokens: Number(e.target.value) })}
          >
            <option value={1_000_000}>por 1 milhão de tokens</option>
            <option value={1000}>por mil tokens</option>
            <option value={1}>por token</option>
          </NativeSelect>
        </Field>
        {categories.map((c) => (
          <Field
            key={c.key}
            label={c.label}
            hint={c.hint || "vazio = sem preço"}
          >
            <Input
              inputMode="decimal"
              value={(f[c.key as Cat] as string | null) ?? ""}
              placeholder="sem preço"
              onChange={(e) =>
                set({
                  [c.key]: e.target.value.replace(",", "."),
                } as Partial<PriceInput>)
              }
            />
          </Field>
        ))}
        <Field label="Vigente desde">
          <Input
            type="date"
            className="[color-scheme:dark]"
            required
            value={f.validFrom}
            onChange={(e) => set({ validFrom: e.target.value })}
          />
        </Field>
        <Field label="Até (inclusive)" hint="vazio = sem fim">
          <Input
            type="date"
            className="[color-scheme:dark]"
            value={f.validTo}
            onChange={(e) => set({ validTo: e.target.value })}
          />
        </Field>
        <Field label="Origem" hint="de onde veio o preço">
          <Input
            value={f.source}
            onChange={(e) => set({ source: e.target.value })}
            placeholder="página de preços, nov/2026"
          />
        </Field>
        <Field label="Observação">
          <Input
            value={f.note}
            onChange={(e) => set({ note: e.target.value })}
          />
        </Field>
      </div>
      {error && (
        <p className="rounded-lg border border-negative/30 bg-negative/[0.06] px-3 py-1.5 text-[12px] text-negative">
          {error}
        </p>
      )}
      <div className="flex gap-2">
        <Button type="submit" size="sm" disabled={busy}>
          Salvar
        </Button>
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1 text-[12px]">
      <span className="text-muted-foreground">{label}</span>
      {children}
      {hint && <span className="text-[11px] text-faint">{hint}</span>}
    </label>
  );
}
