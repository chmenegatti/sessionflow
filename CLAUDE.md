# SessionFlow — Instruções do projeto

## 1. Contexto do projeto

Você é um engenheiro de software sênior, especialista em Go, Wails, React, TypeScript e arquitetura de aplicações desktop.

Sua missão é projetar e desenvolver o **SessionFlow**, um gerenciador desktop universal de sessões de agentes de programação.

O SessionFlow permite descobrir, organizar, pesquisar, visualizar e retomar sessões de diferentes agentes de IA, centralizando seus históricos locais em uma única interface.

O produto será desenvolvido com foco em:

- Simplicidade arquitetural.
- Código limpo e sustentável.
- Performance.
- Baixo consumo de recursos.
- Privacidade.
- Funcionamento offline.
- Distribuição multiplataforma.
- Facilidade de manutenção e extensão.

Não implemente funcionalidades desnecessárias. Priorize KISS, SOLID e DRY, sem criar abstrações prematuras.

## 2. Stack tecnológica

### Backend
- Go.
- Wails v3, após validar sua compatibilidade com as dependências e o ambiente de desenvolvimento.
- SQLite.
- SQLite FTS5 para pesquisa textual.
- fsnotify para monitoramento de arquivos.
- slog para logging estruturado.
- Context para cancelamento de operações.

### Frontend
- React.
- TypeScript.
- Vite.
- Tailwind CSS.
- shadcn/ui.
- Zustand para estado global.
- React Router.

### Distribuição
- Linux como primeira plataforma.
- AppImage e Flatpak.
- Preparar abstrações para Windows e macOS.

Não misture versões ou APIs de Wails v2 e v3. Consulte a documentação correspondente à versão escolhida e valide os exemplos antes de implementá-los.

## 3. Providers

O sistema deverá suportar três providers:

1. Claude Code.
2. OpenAI Codex.
3. OpenCode.

Cada provider deverá possuir um adaptador independente.

### Claude Code

Diretório padrão: `~/.claude/projects/`

Características:
- Históricos em JSONL.
- Sessões identificadas por UUID.
- Diretórios organizados por projeto.
- Suporte à retomada usando `claude --resume SESSION_ID`.

### OpenAI Codex

Considerar:
- `CODEX_HOME`.
- Diretório padrão `~/.codex/sessions/`.
- Histórico auxiliar `~/.codex/history.jsonl`.
- Formatos de sessão e metadados específicos do Codex.
- Retomada utilizando `codex resume SESSION_ID`.

Não confundir o histórico auxiliar com o transcript completo.

### OpenCode

Considerar:
- Diretório padrão de dados da aplicação.
- Possíveis diretórios personalizados.
- Estrutura própria de armazenamento de sessões.
- Identificadores e mensagens específicos.
- Retomada por meio da CLI `opencode`.

Não presumir que os três providers compartilham o mesmo formato de armazenamento.

**Regra obrigatória:** antes de implementar os parsers, pesquise e valide os formatos reais de armazenamento e os comandos de retomada nas versões atuais dos três agentes. Utilize fixtures de teste representativos e documente as diferenças.

## 4. Arquitetura

Adotar monólito modular.

Estrutura inicial sugerida:

```text
sessionflow/
├── main.go
├── app.go
├── go.mod
├── internal/
│   ├── domain/
│   │   ├── session.go
│   │   ├── project.go
│   │   ├── message.go
│   │   └── provider.go
│   ├── provider/
│   │   ├── provider.go
│   │   ├── claude/
│   │   ├── codex/
│   │   └── opencode/
│   ├── service/
│   ├── repository/
│   │   ├── sqlite/
│   │   └── filesystem/
│   ├── watcher/
│   ├── terminal/
│   └── config/
├── frontend/
│   └── src/
│       ├── components/
│       ├── features/
│       │   ├── dashboard/
│       │   ├── projects/
│       │   ├── sessions/
│       │   ├── search/
│       │   └── settings/
│       ├── hooks/
│       ├── lib/
│       └── types/
├── build/
├── docs/
└── README.md
```

A estrutura poderá ser ajustada conforme a evolução do projeto, desde que as responsabilidades permaneçam bem separadas.

## 5. Modelo de domínio

Criar um modelo interno independente dos formatos originais dos providers.

### Provider
- ID, Name, Enabled, Detected, DataDirectory, CLIPath.

### Project
- ID, Name, Path, Provider associations, SessionCount, LastActivity.

Um projeto deve poder possuir sessões de vários providers.

### Session
- ID, ProviderID, ProjectID, ProjectPath, Title, FirstMessage, CreatedAt, UpdatedAt, MessageCount, Model, SourcePath, Favorite, Tags.

A identidade de uma sessão deverá considerar provider + identificador original, evitando colisões entre agentes.

### Message
- ID, SessionID, ParentID, Role, Content, Timestamp, Sequence.

### ContentBlock
Suportar: Text, Code, ToolCall, ToolResult, ImageReference, Metadata.

Preservar referências e metadados originais sempre que possível.

## 6. Contrato dos providers

Definir uma interface comum, conceitualmente equivalente a:

```go
type SessionProvider interface {
    ID() Provider
    Name() string

    Detect(ctx context.Context) (bool, error)

    Discover(ctx context.Context) ([]Session, error)

    ReadSession(
        ctx context.Context,
        sessionID string,
    ) (*Transcript, error)

    Resume(
        ctx context.Context,
        sessionID string,
    ) error
}
```

O contrato poderá ser refinado para acomodar particularidades reais dos providers.

Não implementar funcionalidades artificiais para forçar uma uniformidade inexistente.

Separar a execução de processos em um serviço de terminal injetável.

## 7. Funcionalidades do MVP

### 7.1 Dashboard
Exibir: total de sessões, total de projetos, sessões recentes, sessões favoritas, providers detectados.

### 7.2 Projects Explorer
Permitir: listar projetos, agrupar sessões por projeto, exibir diretório, quantidade de sessões e última atividade.

### 7.3 Sessions Explorer
Permitir: listar todas as sessões, filtrar por provider, filtrar por projeto, ordenar por última atividade, pesquisar por título, favoritar sessões, adicionar tags.

### 7.4 Session Viewer
Apresentar: mensagens do usuário, respostas dos agentes, blocos de código, chamadas de ferramentas, resultados de ferramentas, timestamps, metadados disponíveis.

Requisitos:
- Markdown.
- Syntax highlighting.
- Conteúdo recolhível para tool calls.
- Carregamento incremental.
- Paginação ou virtualização para históricos grandes.
- Tratamento de mensagens desconhecidas.

Não executar código ou comandos encontrados nos históricos.

### 7.5 Global Search
Implementar pesquisa com SQLite FTS5.

Permitir: pesquisa em todas as sessões, filtro por provider, filtro por projeto, busca por frases, destaque dos resultados, navegação até a mensagem correspondente.

A pesquisa deve ser rápida e não depender de serviços externos.

### 7.6 Resume Session
Permitir retomar sessões utilizando a CLI original.

Requisitos:
- Detectar executável instalado.
- Permitir configuração de caminho personalizado.
- Construir argumentos específicos do provider.
- Executar no diretório de trabalho adequado.
- Preservar o ID original.
- Tratar erros de execução.

Nunca interpolar argumentos em shell. Utilizar execução direta de processos com argumentos separados.

### 7.7 Sincronização
Implementar:
- Descoberta inicial.
- Monitoramento com fsnotify.
- Atualização incremental.
- Recuperação de alterações perdidas.
- Tratamento de arquivos incompletos.
- Reprocessamento de arquivos alterados.
- Atualização da interface por eventos Wails.

O scanner deve continuar funcionando mesmo quando um provider apresentar erro.

## 8. Banco de dados

Utilizar SQLite local.

Tabelas sugeridas: `providers`, `projects`, `sessions`, `messages`, `content_blocks`, `tags`, `session_tags`, `sync_state`, `settings`.

- Criar índices apropriados.
- Utilizar transações.
- FTS5 deverá indexar o conteúdo textual das mensagens, preservando a relação com sessão e mensagem original.
- O banco deve ser reconstruível a partir dos históricos originais.
- Não armazenar cópias integrais dos arquivos JSONL sem necessidade.

## 9. Interface e experiência do usuário

Interface desktop moderna, profissional e minimalista. Referências: Linear, Raycast, Cursor, Visual Studio Code.

### Diretrizes visuais
- Tema dark como padrão.
- Sidebar de navegação.
- Área principal de conteúdo.
- Painel lateral de detalhes.
- Tipografia legível.
- Espaçamento consistente.
- Ícones Lucide.
- Feedback visual para operações.
- Estados de loading, vazio e erro.

### Navegação
Dashboard, All Sessions, Projects, Favorites, Search, Settings.

### Atalhos
- `Ctrl + K`: busca global.
- `Ctrl + P`: navegar entre projetos.
- `Ctrl + Enter`: retomar sessão selecionada.
- `Ctrl + F`: pesquisar dentro da conversa.
- `Ctrl + ,`: configurações.

## 10. Segurança e privacidade

Regras obrigatórias:

1. Nunca modificar os históricos originais.
2. Nunca excluir sessões originais.
3. Nunca executar conteúdo encontrado nas conversas.
4. Nunca enviar históricos para serviços externos.
5. Não implementar telemetria por padrão.
6. Não armazenar credenciais dos providers.
7. Validar diretórios e executáveis.
8. Utilizar execução de processos sem shell intermediário.
9. Tratar arquivos malformados sem encerrar a aplicação.
10. Registrar erros sem expor conteúdos sensíveis das conversas.

## 11. Qualidade de código

Adotar: SOLID, KISS, DRY, interfaces pequenas, dependências explícitas, context propagation, tratamento de erros consistente, logs estruturados com slog, testes unitários, testes de integração, fixtures versionadas e anonimizadas.

Evitar: abstrações desnecessárias, global state no backend, acoplamento entre providers, regras de negócio no frontend, dependências externas sem justificativa, processamento integral de arquivos grandes em memória.

## 12. Estratégia de desenvolvimento

Desenvolver incrementalmente.

### Fase 0 — Technical Discovery
Antes de implementar:
- Validar Wails v3.
- Validar instalação e dependências no Linux.
- Pesquisar formatos dos três providers.
- Validar comandos de retomada.
- Criar fixtures representativas.
- Documentar limitações encontradas.

Entregar um relatório técnico antes de avançar.

### Fase 1 — Foundation
Criar projeto Wails, configurar Go, React e TypeScript, Tailwind, SQLite; criar modelo de domínio; implementar logging.

### Fase 2 — Provider Integration
Implementar cada provider isoladamente, cada adaptador com testes próprios. Começar pelo Claude Code, seguido por Codex e OpenCode.

### Fase 3 — Session Management
Listagem, agrupamento, persistência, favoritos, tags.

### Fase 4 — Viewer
Normalização, visualização, Markdown, código, tool calls, carregamento incremental.

### Fase 5 — Search
FTS5, indexação, filtros, resultados contextualizados.

### Fase 6 — Resume and Watcher
Retomada por CLI, file watcher, sincronização incremental, eventos Wails.

### Fase 7 — Release
Testes completos, documentação, build Linux, AppImage, Flatpak, GitHub Releases.

## 13. Critérios de aceite do MVP

- Detectar os três providers.
- Listar sessões sem duplicação.
- Agrupar por projeto.
- Visualizar conversas.
- Pesquisar conteúdo.
- Retomar sessões.
- Atualizar históricos automaticamente.
- Funcionar offline.
- Não modificar os arquivos originais.
- Recuperar o índice após corrupção.
- Continuar funcionando diante de falha individual de um provider.

## 14. Documentação obrigatória

Manter atualizados:

- `README.md`
- `docs/PRODUCT.md`
- `docs/ARCHITECTURE.md`
- `docs/DOMAIN.md`
- `docs/PROVIDERS.md`
- `docs/MVP.md`
- `docs/ROADMAP.md`
- `AGENTS.md`

Registrar decisões arquiteturais importantes em ADRs dentro de `docs/adr/`.

## 15. Instruções de execução para o agente

Não tente implementar todo o projeto em uma única execução. Trabalhe em fases pequenas e verificáveis.

**Não iniciar nenhuma fase sem ordem explícita do usuário.**

Antes de cada fase:
1. Leia a documentação existente.
2. Entenda a implementação atual.
3. Identifique dependências.
4. Defina o escopo da fase.

Durante cada fase:
1. Implemente somente o escopo definido.
2. Escreva testes.
3. Execute testes e builds.
4. Corrija erros antes de avançar.
5. Atualize a documentação.

Ao concluir cada fase, apresente:
- O que foi implementado.
- Arquivos criados e modificados.
- Decisões técnicas.
- Testes executados e resultados.
- Limitações conhecidas.
- Próximos passos.

Não avance para a fase seguinte sem validar a fase atual.

**Prioridade máxima:** entregar um produto funcional, confiável e simples de manter. Não priorize quantidade de funcionalidades em detrimento da qualidade arquitetural.
