package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/repository/sqlite"
)

// Erros do viewer exibidos ao usuário.
var (
	ErrProviderDisabled = errors.New("o provider desta sessão está desabilitado na configuração")
	ErrSourceMissing    = errors.New("o histórico original desta sessão não existe mais")
	ErrCursorExpired    = errors.New("a leitura da conversa expirou; abra-a novamente")
	ErrReadTimeout      = errors.New("tempo esgotado ao ler o histórico")
	errReadFailed       = errors.New("falha ao ler o histórico; veja o log")
)

// Limites do viewer. O parse é em streaming: em memória fica no máximo uma
// página por conversa aberta.
const (
	transcriptPage = 100       // mensagens por página
	pageBytes      = 512 << 10 // a página fecha antes se passar disso
	maxBlockText   = 64 << 10  // texto de um bloco enviado à interface
	maxRawText     = 8 << 10   // JSON original de metadados/desconhecidos
	maxCursors     = 8         // conversas abertas ao mesmo tempo
	cursorIdle     = 10 * time.Minute
	readTimeout    = 30 * time.Second
	shutdownWait   = 3 * time.Second
)

// BlockView é um bloco pronto para exibição. Textos longos são cortados
// (Truncated = bytes omitidos); o original continua na fonte.
type BlockView struct {
	Kind       domain.BlockKind `json:"kind"`
	Text       string           `json:"text"`
	Language   string           `json:"language"` // linguagem do código ou tipo MIME de anexo
	ToolName   string           `json:"toolName"`
	ToolCallID string           `json:"toolCallId"`
	ToolInput  string           `json:"toolInput"` // JSON indentado
	IsError    bool             `json:"isError"`
	Raw        string           `json:"raw"` // JSON original indentado (metadados, desconhecidos)
	Truncated  int              `json:"truncated"`
}

// MessageView é uma mensagem pronta para exibição. Timestamp em ms (0 =
// desconhecido).
type MessageView struct {
	Seq       int         `json:"seq"`
	NativeID  string      `json:"nativeId"`
	Role      domain.Role `json:"role"`
	Timestamp int64       `json:"timestamp"`
	Blocks    []BlockView `json:"blocks"`
}

// TranscriptPage é a próxima página de uma conversa aberta.
type TranscriptPage struct {
	Cursor   string        `json:"cursor"`
	Messages []MessageView `json:"messages"`
	Done     bool          `json:"done"` // não há mais páginas; o cursor foi liberado
}

// TranscriptService lê conversas sob demanda a partir da fonte original.
type TranscriptService struct {
	index     *sqlite.IndexStore
	providers map[domain.ProviderID]provider.Provider
	log       *slog.Logger

	mu      sync.Mutex
	cursors map[string]*cursor
}

// NewTranscriptService cria o serviço com os adaptadores habilitados.
func NewTranscriptService(index *sqlite.IndexStore, providers []provider.Provider, log *slog.Logger) *TranscriptService {
	m := make(map[domain.ProviderID]provider.Provider, len(providers))
	for _, p := range providers {
		m[p.ID()] = p
	}
	return &TranscriptService{index: index, providers: m, log: log, cursors: map[string]*cursor{}}
}

// Open abre a conversa e devolve a primeira página.
func (t *TranscriptService) Open(ctx context.Context, id string) (TranscriptPage, error) {
	return t.OpenAt(ctx, id, 0)
}

// OpenAt abre a conversa a partir da mensagem fromSeq (resultado de busca):
// as anteriores são lidas mas não enviadas, então a primeira página já
// contém o alvo, mesmo em históricos longos.
func (t *TranscriptService) OpenAt(ctx context.Context, id string, fromSeq int) (TranscriptPage, error) {
	key, err := parseKey(id)
	if err != nil {
		return TranscriptPage{}, err
	}
	row, err := t.index.GetSession(ctx, key)
	if errors.Is(err, sqlite.ErrNotFound) {
		return TranscriptPage{}, ErrSessionNotFound
	}
	if err != nil {
		t.log.Error("viewer", "op", "ler sessão", "err", err)
		return TranscriptPage{}, errInternal
	}
	p, ok := t.providers[key.Provider]
	if !ok {
		return TranscriptPage{}, ErrProviderDisabled
	}
	src, err := provider.StatSource(row.SourcePath, row.SourceKind)
	if err != nil {
		return TranscriptPage{}, ErrSourceMissing
	}
	src.Raw = true
	c := t.start(p, src, key, max(fromSeq, 0))
	page, err := t.next(ctx, c)
	if err != nil {
		// O frontend não recebe o cursor quando Open falha: libera aqui.
		t.Close(c.id)
	}
	return page, err
}

// Next devolve a página seguinte de uma conversa aberta.
func (t *TranscriptService) Next(ctx context.Context, cursorID string) (TranscriptPage, error) {
	t.mu.Lock()
	c := t.cursors[cursorID]
	t.mu.Unlock()
	if c == nil {
		return TranscriptPage{}, ErrCursorExpired
	}
	return t.next(ctx, c)
}

// Close libera uma conversa aberta (o viewer foi fechado).
func (t *TranscriptService) Close(cursorID string) {
	t.mu.Lock()
	c := t.cursors[cursorID]
	delete(t.cursors, cursorID)
	t.mu.Unlock()
	if c != nil {
		c.cancel()
	}
}

// ServiceShutdown (ciclo de vida Wails) cancela as leituras abertas.
func (t *TranscriptService) ServiceShutdown() error {
	t.mu.Lock()
	cs := t.cursors
	t.cursors = map[string]*cursor{}
	t.mu.Unlock()
	for _, c := range cs {
		c.cancel()
	}
	// Um adaptador preso em I/O não pode impedir o app de fechar.
	deadline := time.After(shutdownWait)
	for _, c := range cs {
		select {
		case <-c.finished:
		case <-deadline:
			t.log.Warn("viewer: leituras não terminaram no encerramento")
			return nil
		}
	}
	return nil
}

// cursor é uma leitura em andamento: uma goroutine faz o parse e entrega
// páginas por um canal sem buffer, parando enquanto ninguém pede a próxima.
type cursor struct {
	id       string
	pages    chan []MessageView
	ctx      context.Context // cancelado por Close, despejo ou shutdown
	cancel   context.CancelFunc
	finished chan struct{} // fechado quando a goroutine termina
	err      error         // escrito antes de fechar pages

	read     sync.Mutex // um Next por vez
	lastUsed time.Time  // protegido por TranscriptService.mu
}

func (t *TranscriptService) start(p provider.Provider, src provider.Source, key domain.SessionKey, from int) *cursor {
	ctx, cancel := context.WithCancel(context.Background())
	c := &cursor{id: newCursorID(), pages: make(chan []MessageView), ctx: ctx, cancel: cancel, finished: make(chan struct{}), lastUsed: time.Now()}

	t.mu.Lock()
	t.evictLocked()
	t.cursors[c.id] = c
	t.mu.Unlock()

	go func() {
		defer close(c.finished)
		defer close(c.pages)
		defer func() {
			if r := recover(); r != nil {
				// O valor do panic pode conter dados lidos: registra só o tipo.
				t.log.Error("panic no viewer", "provider", key.Provider, "type", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
				c.err = errReadFailed
			}
		}()
		pg := &pager{ctx: ctx, key: key, from: from, out: c.pages}
		var err error
		if r, ok := p.(provider.SessionReader); ok {
			_, err = r.ParseSession(ctx, src, key, pg)
		} else {
			_, err = p.Parse(ctx, src, pg)
		}
		if err == nil {
			err = pg.flush()
		}
		if err != nil && ctx.Err() == nil {
			t.log.Warn("viewer: leitura falhou", "provider", key.Provider, "path", src.Path, "err", err)
			c.err = errReadFailed
		}
	}()
	return c
}

// evictLocked fecha cursores ociosos e, se preciso, o mais antigo.
func (t *TranscriptService) evictLocked() {
	now := time.Now()
	var oldest *cursor
	for id, c := range t.cursors {
		if now.Sub(c.lastUsed) > cursorIdle {
			c.cancel()
			delete(t.cursors, id)
			continue
		}
		if oldest == nil || c.lastUsed.Before(oldest.lastUsed) {
			oldest = c
		}
	}
	if len(t.cursors) >= maxCursors && oldest != nil {
		oldest.cancel()
		delete(t.cursors, oldest.id)
	}
}

func (t *TranscriptService) next(ctx context.Context, c *cursor) (TranscriptPage, error) {
	c.read.Lock()
	defer c.read.Unlock()
	t.mu.Lock()
	c.lastUsed = time.Now()
	t.mu.Unlock()

	timer := time.NewTimer(readTimeout)
	defer timer.Stop()
	select {
	case page, ok := <-c.pages:
		if ok {
			return TranscriptPage{Cursor: c.id, Messages: page}, nil
		}
		t.mu.Lock()
		delete(t.cursors, c.id)
		t.mu.Unlock()
		switch {
		case c.err != nil:
			return TranscriptPage{}, c.err
		case c.ctx.Err() != nil:
			// Cancelado (despejo, Close) antes do fim: não é o fim da conversa.
			return TranscriptPage{}, ErrCursorExpired
		}
		c.cancel()
		return TranscriptPage{Cursor: c.id, Messages: []MessageView{}, Done: true}, nil
	case <-timer.C:
		return TranscriptPage{}, ErrReadTimeout
	case <-ctx.Done():
		return TranscriptPage{}, ctx.Err()
	}
}

func newCursorID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// pager é o Sink do viewer: converte cada mensagem da sessão pedida e
// entrega páginas pelo canal.
type pager struct {
	ctx   context.Context
	key   domain.SessionKey
	from  int // mensagens com seq menor são puladas
	out   chan<- []MessageView
	buf   []MessageView
	bytes int
}

func (p *pager) Message(k domain.SessionKey, m domain.Message) error {
	if k != p.key || m.Seq < p.from {
		return nil
	}
	v, size := messageView(m)
	p.buf = append(p.buf, v)
	p.bytes += size
	if len(p.buf) >= transcriptPage || p.bytes >= pageBytes {
		return p.flush()
	}
	return nil
}

func (p *pager) Session(domain.Session) error { return nil }

func (p *pager) flush() error {
	if len(p.buf) == 0 {
		return nil
	}
	select {
	case p.out <- p.buf:
		p.buf, p.bytes = nil, 0
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// messageView converte a mensagem e devolve o tamanho aproximado em bytes.
func messageView(m domain.Message) (MessageView, int) {
	v := MessageView{Seq: m.Seq, NativeID: m.NativeID, Role: m.Role, Blocks: make([]BlockView, 0, len(m.Blocks))}
	if m.Timestamp != nil {
		v.Timestamp = m.Timestamp.UnixMilli()
	}
	size := 0
	for _, b := range m.Blocks {
		bv := BlockView{
			Kind: b.Kind, Language: b.Language, ToolName: b.ToolName, ToolCallID: b.ToolCallID, IsError: b.IsError,
		}
		var cut int
		bv.Text, cut = clip(b.Text)
		bv.Truncated += cut
		bv.ToolInput, cut = clip(pretty(b.ToolInput))
		bv.Truncated += cut
		bv.Raw, cut = clipN(pretty(b.Raw), maxRawText)
		bv.Truncated += cut
		size += len(bv.Text) + len(bv.ToolInput) + len(bv.Raw) + 64
		v.Blocks = append(v.Blocks, bv)
	}
	return v, size
}

func clip(s string) (string, int) { return clipN(s, maxBlockText) }

func clipN(s string, n int) (string, int) {
	if len(s) <= n {
		return s, 0
	}
	kept := provider.Truncate(s, n)
	return kept, len(s) - len(kept)
}

// pretty indenta JSON; se não for JSON válido devolve como veio.
func pretty(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") != nil {
		return string(raw)
	}
	return buf.String()
}
