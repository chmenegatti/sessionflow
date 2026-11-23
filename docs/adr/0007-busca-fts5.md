# ADR 0007 — Busca global sobre FTS5

**Status:** aceito · 2026-11-22 (Fase 5)

## Contexto
O índice já guardava o texto pesquisável das mensagens em FTS5 (ADR 0003, discovery 05). Faltava expor a busca sem deixar a sintaxe do FTS5 vazar para o usuário (consulta inválida = erro; operadores = comportamento surpreendente), sem enviar HTML do histórico para a interface e sem ler a conversa inteira para chegar a um resultado.

## Decisão
- Consulta construída no backend (`BuildMatch`): cada palavra e cada frase entre aspas vira string FTS5 citada. Palavras com 3+ letras no último token casam como prefixo; frases, só exatas. Sem operadores expostos.
- Ordem por `bm25`, desempate por data e id. Total contado até 1000.
- Trecho com marcadores de uso privado convertidos em segmentos `{text, match}`.
- Navegação por `seq` com `OpenAt`, que pula as mensagens anteriores no backend.
- Sem trigram: dobraria o índice; prefixo cobre o caso comum.

## Consequências
- Nenhuma entrada do usuário produz erro de sintaxe; não há `OR`/`NOT` para usuários avançados.
- Palavras de 1–2 letras não casam como prefixo (`go` não acha `golang`).
- Abrir um resultado ainda lê a fonte até a mensagem, mas não transfere nem renderiza as anteriores.
