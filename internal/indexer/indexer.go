// Package indexer lê as fontes de cada provider e grava os metadados das
// sessões e o texto pesquisável das mensagens no índice (index.db).
//
// Cada provider roda isolado: erro, fonte inválida ou panic num adaptador
// fica registrado no relatório dele e os demais continuam. Cada fonte é
// gravada numa transação própria; se o parse falhar, a leitura anterior da
// fonte permanece no índice.
package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/repository/sqlite"
)

// ProviderReport resume a última indexação de um provider.
type ProviderReport struct {
	Detection   provider.Detection `json:"detection"`
	Sources     int                `json:"sources"`     // fontes encontradas
	Indexed     int                `json:"indexed"`     // fontes lidas nesta execução
	Unchanged   int                `json:"unchanged"`   // puladas por não terem mudado
	Failed      int                `json:"failed"`      // erro ao ler
	Unsupported int                `json:"unsupported"` // formato não suportado
	Partial     int                `json:"partial"`     // terminavam numa linha incompleta
	BadRecords  int                `json:"badRecords"`  // registros inválidos ignorados
	Removed     int                `json:"removed"`     // sessões cujas fontes sumiram
	Duplicates  int                `json:"duplicates"`  // mesma sessão em mais de uma fonte
	Usage       int                `json:"usage"`       // registros de consumo gravados nesta execução
	Deferred    int                `json:"deferred"`    // alteradas, adiadas pelo limite de releitura
	Sessions    int                `json:"sessions"`    // total no índice após a execução
	Error       string             `json:"error,omitempty"`
	DurationMs  int64              `json:"durationMs"`
}

// Report é o resultado de uma execução.
type Report struct {
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt time.Time        `json:"finishedAt"`
	Providers  []ProviderReport `json:"providers"`
}

// Changed informa se a execução alterou o índice.
func (r Report) Changed() bool {
	for _, p := range r.Providers {
		if p.Indexed > 0 || p.Removed > 0 {
			return true
		}
	}
	return false
}

// Deferred devolve os providers com fontes adiadas.
func (r Report) Deferred() []domain.ProviderID {
	var out []domain.ProviderID
	for _, p := range r.Providers {
		if p.Deferred > 0 {
			out = append(out, p.Detection.Provider)
		}
	}
	return out
}

// RunOptions restringe uma execução.
type RunOptions struct {
	// Providers limita a execução (vazio = todos, e as sessões de
	// providers desabilitados são removidas).
	Providers []domain.ProviderID
	// Throttle adia fontes grandes relidas há menos de ThrottleEvery
	// (alterações vindas do watcher numa sessão em andamento).
	Throttle bool
}

// Limite de releitura: uma sessão ativa grava a cada poucos segundos e a
// fonte inteira é relida a cada mudança. Acima de ThrottleSize, no máximo
// uma releitura por ThrottleEvery vinda do watcher.
const (
	ThrottleSize  = 8 << 20
	ThrottleEvery = 30 * time.Second
)

// Indexer coordena providers e store.
type Indexer struct {
	store     *sqlite.IndexStore
	log       *slog.Logger
	providers []provider.Provider
	disabled  []domain.ProviderID
	now       func() time.Time

	mu     sync.Mutex
	parsed map[string]time.Time // provider+caminho → última releitura
}

// New cria o indexador. providers contém só os habilitados.
func New(store *sqlite.IndexStore, log *slog.Logger, providers ...provider.Provider) *Indexer {
	return &Indexer{store: store, log: log, providers: providers, now: time.Now, parsed: map[string]time.Time{}}
}

// WithDisabled informa providers desabilitados na configuração: as sessões
// deles saem do índice a cada execução.
func (ix *Indexer) WithDisabled(ids ...domain.ProviderID) *Indexer {
	ix.disabled = ids
	return ix
}

// Run indexa os providers pedidos (todos, sem opções), um de cada vez.
func (ix *Indexer) Run(ctx context.Context, o RunOptions) Report {
	rep := Report{StartedAt: time.Now()}
	full := len(o.Providers) == 0
	for _, id := range ix.disabled {
		if !full {
			break
		}
		if n, err := ix.store.RemoveSourcesExcept(ctx, id, nil); err != nil {
			ix.log.Warn("remover provider desabilitado", "provider", id, "err", err)
		} else if n > 0 {
			ix.log.Info("sessões de provider desabilitado removidas", "provider", id, "sessions", n)
		}
	}
	for _, p := range ix.providers {
		if ctx.Err() != nil {
			break
		}
		if !full && !slices.Contains(o.Providers, p.ID()) {
			continue
		}
		rep.Providers = append(rep.Providers, ix.runProvider(ctx, p, o.Throttle))
	}
	rep.FinishedAt = time.Now()
	return rep
}

func (ix *Indexer) runProvider(ctx context.Context, p provider.Provider, throttle bool) (rep ProviderReport) {
	start := time.Now()
	id := p.ID()
	log := ix.log.With("provider", id)
	defer func() {
		if r := recover(); r != nil {
			// Bug num adaptador não derruba o app nem os outros providers.
			// O valor do panic pode conter dados lidos; registra só o tipo.
			rep.Error = "falha interna no adaptador"
			log.Error("panic no adaptador", "type", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
		}
		if n, err := ix.store.CountSessions(context.WithoutCancel(ctx)); err == nil {
			rep.Sessions = n[id]
		}
		rep.DurationMs = time.Since(start).Milliseconds()
		level := slog.LevelInfo
		if rep.Indexed+rep.Removed+rep.Failed+rep.Unsupported == 0 && rep.Error == "" {
			// Execução sem efeito (watcher, reconciliação): só em debug, ou o
			// log cresceria uma linha a cada poucos segundos de sessão ativa.
			level = slog.LevelDebug
		}
		log.Log(context.WithoutCancel(ctx), level, "indexação", "sources", rep.Sources, "indexed", rep.Indexed, "unchanged", rep.Unchanged,
			"failed", rep.Failed, "unsupported", rep.Unsupported, "removed", rep.Removed,
			"sessions", rep.Sessions, "usage", rep.Usage, "ms", rep.DurationMs)
	}()

	rep.Detection = p.Detect(ctx)
	if rep.Detection.Status == provider.StatusNotFound || rep.Detection.Status == provider.StatusUnsupported {
		// Dados ausentes: o índice espelha a fonte.
		rep.Removed, _ = ix.store.RemoveSourcesExcept(ctx, id, nil)
		return rep
	}

	sources, err := p.Sources(ctx)
	incomplete := errors.Is(err, provider.ErrIncomplete)
	if err != nil && !incomplete {
		rep.Error = "listar fontes: " + shortErr(err)
		log.Warn("listar fontes", "err", err)
		return rep
	}
	if incomplete {
		// Indexa o que foi listado, mas não remove nada: a fonte ausente
		// pode estar só ilegível agora.
		rep.Detection.Detail = strings.TrimSpace(rep.Detection.Detail + " " + shortErr(err))
		log.Warn("listagem incompleta", "err", err)
	}
	rep.Sources = len(sources)

	// Fontes que sumiram saem antes da leitura: assim uma duplicata da
	// sessão removida assume já nesta execução. Listagem incompleta não
	// remove nada.
	if !incomplete {
		keep := make([]string, 0, len(sources))
		for _, src := range sources {
			keep = append(keep, src.Path)
		}
		if rep.Removed, err = ix.store.RemoveSourcesExcept(ctx, id, keep); err != nil {
			rep.Error = "remover fontes antigas: " + shortErr(err)
			return rep
		}
	}
	states, err := ix.store.SyncStates(ctx, id)
	if err != nil {
		rep.Error = "ler estado: " + shortErr(err)
		return rep
	}
	for _, src := range sources {
		if ctx.Err() != nil {
			return rep
		}
		if st, ok := states[src.Path]; ok && unchanged(st, src) && !retry(st.Status) {
			rep.Unchanged++
			continue
		}
		if throttle && ix.recentlyParsed(id, src) {
			rep.Deferred++
			continue
		}
		ix.indexSource(ctx, p, src, &rep, log)
		ix.markParsed(id, src)
	}
	if rep.Unsupported > 0 && rep.Unsupported == rep.Sources {
		rep.Detection.Status = provider.StatusUnsupported
	}
	return rep
}

// recentlyParsed: fonte grande relida há menos de ThrottleEvery.
func (ix *Indexer) recentlyParsed(id domain.ProviderID, src provider.Source) bool {
	if src.Size < ThrottleSize {
		return false
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	last, ok := ix.parsed[string(id)+"\x00"+src.Path]
	return ok && ix.now().Sub(last) < ThrottleEvery
}

func (ix *Indexer) markParsed(id domain.ProviderID, src provider.Source) {
	if src.Size < ThrottleSize {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.parsed[string(id)+"\x00"+src.Path] = ix.now()
}

// Estados de sync_state.status.
const (
	stateOK          = "ok"
	statePartial     = "partial"
	stateError       = "error"
	stateUnsupported = "unsupported"
	stateDuplicate   = "duplicate" // sessão já veio de outra fonte
)

// retry: erro pode ser transitório (permissão, disco cheio, banco ocupado)
// e duplicata depende de outra fonte; ambos são relidos mesmo sem mudança.
// unsupported é determinístico e só é relido quando a fonte muda.
func retry(status string) bool { return status == stateError || status == stateDuplicate }

func unchanged(st sqlite.SyncState, src provider.Source) bool {
	return st.Size == src.Size && st.MTimeNS == src.ModTime.UnixNano() && st.Cursor == src.Fingerprint
}

func (ix *Indexer) indexSource(ctx context.Context, p provider.Provider, src provider.Source, rep *ProviderReport, log *slog.Logger) {
	state := sqlite.SyncState{Size: src.Size, MTimeNS: src.ModTime.UnixNano(), Cursor: src.Fingerprint, Status: stateOK}
	tx, err := ix.store.BeginSource(ctx, p.ID(), src.Path, src.Kind)
	if err != nil {
		rep.Failed++
		log.Warn("abrir transação", "path", src.Path, "err", err)
		return
	}
	sk := &sink{ctx: ctx, tx: tx}
	stats, err := parseSafely(ctx, p, src, sk, log)
	rep.BadRecords += stats.BadLines + stats.LongLines
	if err != nil {
		tx.Rollback()
		if ctx.Err() != nil {
			return // cancelado: tenta de novo na próxima execução
		}
		state.Status, state.Error = stateError, shortErr(err)
		if stats.Unsupported || errors.Is(err, provider.ErrUnsupported) {
			state.Status = stateUnsupported
			rep.Unsupported++
		} else {
			rep.Failed++
		}
		log.Warn("fonte ignorada", "path", src.Path, "status", state.Status, "err", err)
		if err := ix.store.SetSyncState(ctx, p.ID(), src.Path, state); err != nil {
			log.Warn("gravar estado", "path", src.Path, "err", err)
		}
		return
	}
	if stats.Partial {
		state.Status = statePartial
		rep.Partial++
	}
	if tx.Duplicates > 0 {
		state.Status = stateDuplicate
	}
	if err := tx.Commit(ctx, state); err != nil {
		rep.Failed++
		log.Warn("gravar fonte", "path", src.Path, "err", err)
		return
	}
	rep.Indexed++
	rep.Duplicates += tx.Duplicates
	rep.Usage += sk.usage
	if stats.BadLines+stats.LongLines > 0 {
		log.Info("registros ignorados", "path", src.Path, "bad", stats.BadLines, "long", stats.LongLines)
	}
	// Avisos de consumo, sem conteúdo: só contagens e o caminho da fonte.
	if stats.UsageConflicts > 0 {
		log.Warn("consumo divergente entre linhas da mesma resposta", "path", src.Path, "responses", stats.UsageConflicts)
	}
	if stats.UsageMismatch {
		log.Warn("soma dos pedidos difere do acumulado da sessão", "path", src.Path)
	}
}

// errPanic marca uma fonte cujo parse entrou em panic.
var errPanic = errors.New("falha interna no adaptador")

// parseSafely converte panic do adaptador em erro da fonte. Sem isso a
// transação ficaria aberta, segurando o lock de escrita do índice.
func parseSafely(ctx context.Context, p provider.Provider, src provider.Source, s provider.Sink, log *slog.Logger) (stats provider.ParseStats, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("panic no parse", "path", src.Path, "type", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
			err = errPanic
		}
	}()
	return p.Parse(ctx, src, s)
}

// shortErr limita a mensagem gravada no banco e mostrada na interface.
func shortErr(err error) string {
	s := err.Error()
	if len(s) > 300 {
		s = provider.Truncate(s, 300) + "…"
	}
	return s
}

// sink converte mensagens do domínio em linhas do índice.
type sink struct {
	ctx   context.Context
	tx    *sqlite.SourceTx
	usage int // registros de consumo recebidos (log)
}

func (s *sink) Message(key domain.SessionKey, m domain.Message) error {
	text := SearchText(m)
	if text == "" {
		return nil // metadado, desconhecido ou sem texto: nada a buscar
	}
	var tools []string
	for _, b := range m.Blocks {
		if b.Kind == domain.BlockToolCall && b.ToolName != "" {
			tools = append(tools, b.ToolName)
		}
	}
	return s.tx.AddMessage(s.ctx, key, sqlite.IndexedMessage{
		Seq: m.Seq, NativeID: m.NativeID, Role: m.Role, Timestamp: m.Timestamp, SearchText: text,
		HasText: domain.CountsAsMessage(m), Tools: tools,
	})
}

func (s *sink) Session(x domain.Session) error { return s.tx.PutSession(s.ctx, x) }

// Usage grava o consumo de uma resposta/pedido (provider.UsageSink).
func (s *sink) Usage(key domain.SessionKey, u domain.Usage) error {
	s.usage++
	return s.tx.AddUsage(s.ctx, key, u)
}

// Limites do texto indexado (docs/discovery/05 §5): o índice não é cópia
// integral das conversas.
const (
	maxToolInput  = 1 << 10
	maxToolResult = 4 << 10
	maxMessage    = 16 << 10
)

// SearchText monta o texto pesquisável de uma mensagem: textos, nome e
// entrada resumida das ferramentas e resultado truncado. Ficam de fora
// reasoning, metadados, desconhecidos e mensagens de sistema.
func SearchText(m domain.Message) string {
	if m.Role == domain.RoleSystem {
		return ""
	}
	var parts []string
	for _, b := range m.Blocks {
		switch b.Kind {
		case domain.BlockText, domain.BlockCode:
			parts = append(parts, b.Text)
		case domain.BlockToolCall:
			parts = append(parts, strings.TrimSpace(b.ToolName+" "+provider.Truncate(string(b.ToolInput), maxToolInput)))
		case domain.BlockToolResult:
			parts = append(parts, provider.Truncate(b.Text, maxToolResult))
		}
	}
	return provider.Truncate(strings.TrimSpace(strings.Join(parts, "\n")), maxMessage)
}
