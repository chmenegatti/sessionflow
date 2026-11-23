import { Fragment, useState } from "react";
import { useNavigate } from "react-router";
import {
  ArrowLeft,
  CalendarPlus,
  CalendarX2,
  ChevronDown,
  Pencil,
  Plus,
  Power,
  PowerOff,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { useAsync } from "@/hooks/useAsync";
import {
  CostService,
  list,
  type PriceItem,
  type PriceRevision,
} from "@/lib/api";
import { useAppStore } from "@/lib/store";
import { cn } from "@/lib/utils";
import { ProviderTag } from "@/features/sessions/ProviderTag";
import { AnalyticsCard } from "./AnalyticsCard";
import { inclusiveEnd, localDate, shortDate, unitLabel } from "./costFormat";
import { PriceForm, type PriceFormMode } from "./PriceForm";
import { useAnalyticsQuery } from "./useAnalyticsQuery";

const currencies = ["BRL", "USD", "EUR", "GBP"];

const priceText = (v: string | null) =>
  v == null ? <span className="text-faint">sem preço</span> : v;

function validity(r: PriceRevision) {
  const end = inclusiveEnd(r.validTo);
  return `${shortDate(r.validFrom)} – ${end ? shortDate(new Date(end + "T12:00:00").toISOString()) : "sem fim"}`;
}

function stateOf(it: PriceItem): { label: string; tone: string } {
  const c = it.current;
  if (c.disabledAt)
    return { label: "desativado", tone: "bg-white/[0.06] text-faint" };
  if (c.validTo && new Date(c.validTo) <= new Date())
    return {
      label: "encerrado",
      tone: "bg-white/[0.06] text-muted-foreground",
    };
  return { label: "ativo", tone: "bg-positive/12 text-positive" };
}

// Tabela de preços: adicionar, editar (nova revisão), novo preço a partir de
// uma data, desativar/reativar e histórico de revisões.
export function PricesPage() {
  const navigate = useNavigate();
  const { search } = useAnalyticsQuery();
  const version = useAppStore((s) => s.dataVersion);
  const notify = useAppStore((s) => s.notify);
  const items = useAsync(() => CostService.Prices().then(list), [version]);
  const currency = useAsync(() => CostService.DefaultCurrency(), []);
  const [form, setForm] = useState<PriceFormMode | null>(null);
  const [closing, setClosing] = useState<PriceRevision | null>(null);
  const done = (text: string) => {
    setForm(null);
    setClosing(null);
    notify(text);
    items.reload();
  };
  const act = async (fn: () => Promise<unknown>, text: string) => {
    try {
      await fn();
      done(text);
    } catch (e) {
      notify(String(e instanceof Error ? e.message : e), "error");
    }
  };
  const setCurrency = async (c: string) => {
    try {
      await CostService.SetDefaultCurrency(c);
      currency.reload();
    } catch (e) {
      notify(String(e instanceof Error ? e.message : e), "error");
    }
  };
  const def = currency.data ?? "BRL";
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate({ pathname: "/analytics/costs", search })}
        >
          <ArrowLeft /> Custos
        </Button>
        <p className="text-[12px] text-muted-foreground">
          Preços cadastrados por você. Nada é consultado na internet;
          estimativas já calculadas guardam o preço que usaram.
        </p>
        <label className="ml-auto flex items-center gap-2 text-[12px] text-muted-foreground">
          Moeda padrão
          <NativeSelect
            value={def}
            onChange={(e) => setCurrency(e.target.value)}
          >
            {[...new Set([def, ...currencies])].map((c) => (
              <option key={c}>{c}</option>
            ))}
          </NativeSelect>
        </label>
        <Button size="sm" onClick={() => setForm({ kind: "add" })}>
          <Plus /> Novo preço
        </Button>
      </div>
      {closing && (
        <CloseForm
          key={closing.id}
          rev={closing}
          onCancel={() => setClosing(null)}
          onDone={done}
        />
      )}
      {form && (
        <PriceForm
          key={JSON.stringify(form)}
          mode={form}
          defaultCurrency={def}
          onCancel={() => setForm(null)}
          onDone={done}
        />
      )}
      <AnalyticsCard
        title="Preços"
        hint="Valores por unidade de cobrança; categoria sem preço deixa a estimativa parcial"
        loading={items.loading && !items.data}
        error={items.data ? null : items.error}
        onRetry={items.reload}
        empty={!!items.data && items.data.length === 0}
        emptyText="Nenhum preço cadastrado."
      >
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-[12.5px]">
            <thead>
              <tr className="border-b text-left text-[11px] text-faint">
                <th className="w-7" />
                <th className="px-2 py-2 font-medium">Agente</th>
                <th className="px-2 py-2 font-medium">Modelo</th>
                <th className="px-2 py-2 font-medium">Unidade</th>
                <th className="px-2 py-2 text-right font-medium">Entrada</th>
                <th className="px-2 py-2 text-right font-medium">Saída</th>
                <th className="px-2 py-2 text-right font-medium">Cache lido</th>
                <th className="px-2 py-2 text-right font-medium">
                  Cache criado
                </th>
                <th className="px-2 py-2 font-medium">Vigência</th>
                <th className="px-2 py-2 font-medium">Estado</th>
                <th className="py-2 pl-2 text-right font-medium">Ações</th>
              </tr>
            </thead>
            <tbody>
              {list(items.data).map((it) => (
                <PriceRow
                  key={it.current.priceKey}
                  it={it}
                  onEdit={() => setForm({ kind: "edit", rev: it.current })}
                  onFrom={() => setForm({ kind: "from", rev: it.current })}
                  onClose={() => {
                    setForm(null);
                    setClosing(it.current);
                  }}
                  onDisable={() =>
                    act(
                      () => CostService.DisablePrice(it.current.id),
                      "Preço desativado",
                    )
                  }
                  onReactivate={() =>
                    act(
                      () => CostService.ReactivatePrice(it.current.id),
                      "Preço reativado",
                    )
                  }
                />
              ))}
            </tbody>
          </table>
        </div>
      </AnalyticsCard>
    </div>
  );
}

function PriceRow({
  it,
  onEdit,
  onFrom,
  onClose,
  onDisable,
  onReactivate,
}: {
  it: PriceItem;
  onEdit: () => void;
  onFrom: () => void;
  onClose: () => void;
  onDisable: () => void;
  onReactivate: () => void;
}) {
  const [open, setOpen] = useState(false);
  const c = it.current;
  const st = stateOf(it);
  const uses = (id: number) =>
    (it.uses as Record<string, number> | null)?.[String(id)] ?? 0;
  const history = list(it.history);
  return (
    <Fragment>
      <tr className="border-b hover:bg-white/[0.03]">
        <td className="py-2">
          <Button
            variant="ghost"
            size="icon-xs"
            aria-expanded={open}
            aria-label={open ? "Recolher histórico" : "Ver histórico"}
            onClick={() => setOpen(!open)}
          >
            <ChevronDown
              className={cn("transition-transform", !open && "-rotate-90")}
            />
          </Button>
        </td>
        <td className="px-2 py-2">
          <ProviderTag provider={c.provider} />
        </td>
        <td
          className="px-2 py-2 font-mono text-[11.5px]"
          title={c.source ? `Origem: ${c.source}` : undefined}
        >
          {c.model}
        </td>
        <td className="px-2 py-2 text-muted-foreground">
          {c.currency} {unitLabel(c.unitTokens)}
        </td>
        <td className="px-2 py-2 text-right tabular-nums">
          {priceText(c.input)}
        </td>
        <td className="px-2 py-2 text-right tabular-nums">
          {priceText(c.output)}
        </td>
        <td className="px-2 py-2 text-right tabular-nums">
          {priceText(c.cacheRead)}
        </td>
        <td className="px-2 py-2 text-right tabular-nums">
          {priceText(c.cacheCreation)}
        </td>
        <td className="px-2 py-2 whitespace-nowrap text-muted-foreground">
          {validity(c)}
        </td>
        <td className="px-2 py-2">
          <span className={cn("rounded-md px-1.5 py-px text-[11px]", st.tone)}>
            {st.label}
          </span>
        </td>
        <td className="py-2 pl-2">
          <div className="flex justify-end gap-1">
            {c.disabledAt ? (
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Reativar"
                title="Reativar"
                onClick={onReactivate}
              >
                <Power />
              </Button>
            ) : (
              <>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Editar"
                  title="Corrigir o preço (nova revisão)"
                  onClick={onEdit}
                >
                  <Pencil />
                </Button>
                {!c.validTo && (
                  <>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Novo preço a partir de"
                      title="Novo preço a partir de uma data"
                      onClick={onFrom}
                    >
                      <CalendarPlus />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label="Encerrar vigência"
                      title="Encerrar a vigência sem cadastrar outro preço"
                      onClick={onClose}
                    >
                      <CalendarX2 />
                    </Button>
                  </>
                )}
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Desativar"
                  title="Desativar (estimativas antigas mantêm o preço)"
                  onClick={onDisable}
                >
                  <PowerOff />
                </Button>
              </>
            )}
          </div>
        </td>
      </tr>
      {open && (
        <tr className="border-b bg-white/[0.02]">
          <td />
          <td colSpan={10} className="py-2 pr-2">
            <ol className="flex flex-col gap-1.5 text-[12px]">
              {history.map((h, i) => (
                <li
                  key={h.id}
                  className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-muted-foreground"
                >
                  <span className="font-medium text-foreground">
                    Revisão {i + 1}
                  </span>
                  <span>criada em {shortDate(h.createdAt)}</span>
                  <span className="tabular-nums">
                    {h.currency} {h.input ?? "—"} / {h.output ?? "—"} /{" "}
                    {h.cacheRead ?? "—"} / {h.cacheCreation ?? "—"}{" "}
                    {unitLabel(h.unitTokens)}
                  </span>
                  <span>{validity(h)}</span>
                  {h.closedAt && (
                    <span>vigência encerrada em {shortDate(h.closedAt)}</span>
                  )}
                  {h.replacedBy != null && (
                    <span className="text-faint">substituída</span>
                  )}
                  {h.disabledAt && (
                    <span className="text-faint">
                      desativada em {shortDate(h.disabledAt)}
                    </span>
                  )}
                  <span className="text-faint">
                    {uses(h.id).toLocaleString("pt-BR")}{" "}
                    {uses(h.id) === 1 ? "estimativa usa" : "estimativas usam"}
                  </span>
                  {h.source && (
                    <span className="text-faint">origem: {h.source}</span>
                  )}
                </li>
              ))}
            </ol>
          </td>
        </tr>
      )}
    </Fragment>
  );
}

// Encerrar a vigência sem sucessor: depois do último dia, sem outro preço
// vigente, as respostas ficam sem preço.
function CloseForm({
  rev,
  onCancel,
  onDone,
}: {
  rev: PriceRevision;
  onCancel: () => void;
  onDone: (text: string) => void;
}) {
  const [last, setLast] = useState(() =>
    new Date().toLocaleDateString("sv-SE"),
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await CostService.ClosePrice(rev.id, last);
      const stale = r.stale
        ? ` ${r.stale.toLocaleString("pt-BR")} ${r.stale === 1 ? "estimativa já calculada depois da data continua" : "estimativas já calculadas depois da data continuam"} com este preço até você usar Recalcular.`
        : "";
      onDone(
        `Vigência encerrada em ${shortDate(new Date(last + "T12:00:00").toISOString())}.${stale}`,
      );
    } catch (err) {
      setError(String(err instanceof Error ? err.message : err));
      setBusy(false);
    }
  };
  return (
    <form
      onSubmit={submit}
      aria-label="Encerrar vigência"
      className="surface flex flex-col gap-3 rounded-[18px] border border-primary/25 px-5 py-4"
    >
      <div>
        <h3 className="text-[15px] font-semibold tracking-tight">
          Encerrar vigência
        </h3>
        <p className="mt-0.5 text-[12px] text-muted-foreground">
          <span className="font-mono">{rev.model}</span> deixa de valer depois
          do último dia. Nenhum preço novo é criado: sem outro preço vigente, as
          respostas seguintes ficam sem preço.
        </p>
      </div>
      <label className="flex w-fit flex-col gap-1 text-[12px] text-muted-foreground">
        Último dia de vigência
        <Input
          type="date"
          required
          className="h-8 w-44"
          value={last}
          min={localDate(rev.validFrom)}
          onChange={(e) => setLast(e.target.value)}
        />
      </label>
      {error && <p className="text-[12.5px] text-negative">{error}</p>}
      <div className="flex gap-2">
        <Button size="sm" type="submit" disabled={busy || !last}>
          <CalendarX2 /> Encerrar
        </Button>
        <Button variant="ghost" size="sm" type="button" onClick={onCancel}>
          Cancelar
        </Button>
      </div>
    </form>
  );
}
