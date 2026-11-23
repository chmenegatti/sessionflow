# Hardening (pós-Fase 9) — plano resumido

Pedido: oito correções (consistência, concorrência, exportação, analytics). Sem migration; sem dependência nova.

## Dependências entre itens

- 5 (testes de concorrência) usa o fingerprint do item 1 e o encerramento de vigência do item 2 → item 2 sobe para antes do 5.
- 3 (ordenação por moeda) é usado pela UI de custos; 4 (legenda) toca o mesmo `CostsPage`.

## Ordem

1. **Item 1 — Recalcular com fingerprint.** `PreviewRecalculate` devolve `token` (SHA-256 do plano: chave, estimativa nova e a gravada de cada mudança). `Recalculate(q, token)` refaz o plano sob o mutex; token diferente → `ErrStalePreview` e nada é gravado. `SaveEstimates` já é transacional (rollback testado com trigger que falha no meio).
2. **Item 2 — Encerrar vigência.** `CostService.ClosePrice(id, validTo)` usa `PriceStore.CloseValidity` (UPDATE condicional `valid_to IS NULL`). Sem sucessor; cálculos depois do fim → `no_price`. Devolve quantas estimativas já gravadas ficaram depois do fim (Recalcular). UI: botão "Encerrar vigência" com data.
3. **Item 3 — Ordenação por moeda.** Comparador único em `sqlite` (`CompareCostSums`): linha classificada pela moeda primária (menor código ISO com custo), ordem alfabética das moedas, total da moeda desc, respostas desc, chave asc; sem custo no fim. Usado por `groupCosts` e `CostStore.Sessions`. UI separa rankings por moeda.
4. **Item 5 — Testes de concorrência.** Gancho de teste `stage` no `CostService` (nil em produção) para pausar o Recalcular com o mutex preso; cenários determinísticos com canais.
5. **Item 7 — CSV.** BOM no `writeCSV`; `csvText` neutraliza `= + - @ \t \r` no início e depois de espaços/controles; quoting do `encoding/csv`.
6. **Item 4 — Legenda.** Participação calculada em BigInt sobre os nanos (string decimal), maior resto para somar 100%; float só na largura da barra.
7. **Item 6 — Dias seguidos.** Função pura `streaks`; dia corrente sem atividade não zera a sequência atual.
8. **Item 8 — Linha do tempo da sessão.** `IndexStore.UsageSessionRecords` (mesma CTE `d`, dedup) + `AnalyticsService.SessionTimeline`: intervalos só com dados, passo escolhido pela duração, registros sem horário à parte, retomadas (pausa ≥ 30 min) marcadas; total reconcilia com a linha da tabela de consumo.
