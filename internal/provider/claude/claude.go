// Package claude lê os transcripts JSONL do Claude Code.
//
// Formato e decisões: docs/discovery/02-claude-code.md. O formato é interno
// ao Claude Code e muda entre versões; por isso todo campo é opcional e
// tipos desconhecidos viram BlockUnknown.
package claude

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/jsonl"
)

// Options configura o adaptador.
type Options struct {
	RootDir  string // $CLAUDE_CONFIG_DIR ou ~/.claude
	CLIPath  string // executável configurado ("" = procurar)
	Home     string
	LookPath provider.LookPath
}

// Adapter implementa provider.Provider para o Claude Code.
type Adapter struct{ o Options }

// New cria o adaptador.
func New(o Options) *Adapter { return &Adapter{o: o} }

func (a *Adapter) ID() domain.ProviderID { return domain.ProviderClaude }

func (a *Adapter) projectsDir() string { return filepath.Join(a.o.RootDir, "projects") }

// Detect verifica o diretório projects/ e procura o executável.
func (a *Adapter) Detect(context.Context) provider.Detection {
	d := provider.Detection{
		Provider: a.ID(),
		RootDir:  a.o.RootDir,
		CLIPath:  a.CLI(a.o.CLIPath),
		Status:   provider.StatusOK,
	}
	if !provider.IsDir(a.projectsDir()) {
		d.Status = provider.StatusNotFound
		d.Detail = "diretório projects/ não encontrado"
	}
	return d
}

// Sources lista projects/<dir>/<uuid>.jsonl e os transcripts de subagentes
// em projects/<dir>/<uuid>/subagents/agent-*.jsonl. Links simbólicos não são
// seguidos.
func (a *Adapter) Sources(ctx context.Context) ([]provider.Source, error) {
	projects, err := os.ReadDir(a.projectsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []provider.Source
	unreadable := 0
	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !p.IsDir() {
			continue
		}
		dir := filepath.Join(a.projectsDir(), p.Name())
		entries, err := os.ReadDir(dir)
		if err != nil {
			unreadable++ // diretório ilegível não derruba os demais
			continue
		}
		for _, e := range entries {
			name := e.Name()
			switch {
			case e.Type().IsRegular() && isTranscript(name):
				out = appendSource(out, filepath.Join(dir, name))
			case e.IsDir():
				subs, _ := filepath.Glob(filepath.Join(dir, name, "subagents", "agent-*.jsonl"))
				for _, s := range subs {
					out = appendSource(out, s)
				}
			}
		}
	}
	if unreadable > 0 {
		return out, fmt.Errorf("%w: %d diretório(s) de projeto ilegível(is)", provider.ErrIncomplete, unreadable)
	}
	return out, nil
}

// isTranscript aceita <id>.jsonl e ignora cópias que o Claude Code marca
// como órfãs (*.orphaned-<ts>-<sufixo>.jsonl).
func isTranscript(name string) bool {
	return strings.HasSuffix(name, ".jsonl") && !strings.Contains(name, ".orphaned-")
}

func appendSource(out []provider.Source, path string) []provider.Source {
	if src, err := provider.StatSource(path, domain.SourceJSONL); err == nil {
		return append(out, src)
	}
	return out
}

// identify deriva a chave da sessão do caminho. O nome do arquivo é o
// sessionId (100% dos casos observados). Subagentes compartilham o
// sessionId do pai, então a chave inclui o nome do arquivo do agente.
func identify(path string) (key domain.SessionKey, parent *domain.SessionKey, err error) {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "subagents" {
		parentID := filepath.Base(filepath.Dir(dir))
		pk, err := domain.NewSessionKey(domain.ProviderClaude, parentID)
		if err != nil {
			return key, nil, err
		}
		key, err = domain.NewSessionKey(domain.ProviderClaude, parentID+"/"+stem)
		return key, &pk, err
	}
	key, err = domain.NewSessionKey(domain.ProviderClaude, stem)
	return key, nil, err
}

// Parse lê o transcript em streaming.
func (a *Adapter) Parse(ctx context.Context, src provider.Source, sink provider.Sink) (provider.ParseStats, error) {
	var stats provider.ParseStats
	key, parent, err := identify(src.Path)
	if err != nil {
		return stats, err
	}
	f, err := os.Open(src.Path)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	us, usageOn := sink.(provider.UsageSink)
	p := &parser{key: key, sink: sink, stats: &stats, src: src, usageOn: usageOn}
	r := jsonl.NewReader(f, 0)
	for {
		if r.Lines%512 == 0 {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
		}
		line, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, err
		}
		if err := p.handle(line); err != nil {
			return stats, err
		}
	}
	if err := p.flush(); err != nil {
		return stats, err
	}
	stats.Lines, stats.LongLines, stats.Partial = r.Lines, r.LongLines, r.Partial
	if usageOn {
		if err := p.usage.emit(key, us, &stats); err != nil {
			return stats, err
		}
	}

	s := p.session()
	s.Parent = parent
	s.SourceKind = domain.SourceJSONL
	s.SourcePath = src.Path
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = src.ModTime.UTC()
	}
	return stats, sink.Session(s)
}

// ignored são registros conhecidos sem valor para a listagem ou a busca
// (docs/discovery/02 §4).
var ignored = map[string]bool{
	"attachment": true, "last-prompt": true, "queue-operation": true, "mode": true,
	"permission-mode": true, "session-owner": true, "atis-latch": true, "cost-state": true,
	"pr-link": true, "bridge-session": true, "agent-name": true, "tools-used": true,
	"continued-in": true, "fork-context-ref": true, "summary": true,
}

// ignoredPrefixes cobre famílias de registros (api-request*, file-history-*).
var ignoredPrefixes = []string{"api-request", "file-history-"}

func isIgnored(t string) bool {
	if ignored[t] {
		return true
	}
	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// rawLine contém só os campos usados. Tudo é opcional.
type rawLine struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	IsMeta      bool            `json:"isMeta"`
	Timestamp   string          `json:"timestamp"`
	CWD         string          `json:"cwd"`
	Entrypoint  string          `json:"entrypoint"`
	AITitle     string          `json:"aiTitle"`
	CustomTitle string          `json:"customTitle"`
	Message     *rawMessage     `json:"message"`
	Content     json.RawMessage `json:"content"`
	Attachment  attachmentType  `json:"attachment"`
	RequestID   string          `json:"requestId"`
}

// attachmentType lê só attachment.type, sem copiar o resto do objeto (que
// pode ser grande: saída de hooks, ambiente). Valor que não seja objeto é
// ignorado sem invalidar a linha.
type attachmentType string

func (a *attachmentType) UnmarshalJSON(b []byte) error {
	var x struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(b, &x) == nil {
		*a = attachmentType(x.Type)
	}
	return nil
}

type rawMessage struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	// Lido à parte (decodeUsage): um consumo ilegível não pode derrubar a
	// mensagem no viewer e na busca.
	Usage json.RawMessage `json:"usage"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    json.RawMessage `json:"source"`
	Title     string          `json:"title"`
}

type parser struct {
	key   domain.SessionKey
	sink  provider.Sink
	stats *provider.ParseStats
	src   provider.Source

	seq       int
	lines     int             // linhas lidas; identidade de reserva do consumo
	pending   *domain.Message // resposta do assistente sendo agrupada
	usageOn   bool            // o sink grava consumo (indexador)
	usage     usageTracker
	pendingID string // message.id da resposta pendente

	span        provider.Span
	cwd         string
	model       string
	aiTitle     string
	customTitle string
	firstPrompt string
	firstMeta   string // 1ª linha de contexto injetado: título de sessões automáticas
	entrypoint  string
	count       int
	human       bool
}

func (p *parser) handle(line []byte) error {
	p.lines++
	var l rawLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.stats.BadLines++
		return nil
	}
	ts := provider.ParseTime(l.Timestamp)
	if l.Type == "user" || l.Type == "assistant" || l.Type == "system" {
		p.span.Add(ts)
		if p.cwd == "" {
			p.cwd = l.CWD
		}
		if p.entrypoint == "" {
			p.entrypoint = l.Entrypoint
		}
	}

	switch l.Type {
	case "assistant":
		if l.Message == nil {
			p.stats.SkippedItems++
			return nil
		}
		if m := l.Message.Model; m != "" && m != "<synthetic>" {
			p.model = m
		}
		// Mensagens <synthetic> são geradas pelo próprio Claude Code, sem
		// chamada à API: não há consumo.
		if p.usageOn && l.Message.Model != "<synthetic>" {
			if u, ok := decodeUsage(l.Message.Usage); ok {
				// Sem nenhum identificador, a linha é a identidade: nunca
				// junta respostas diferentes num registro só.
				id := cmp.Or(l.Message.ID, l.RequestID, l.UUID, fmt.Sprintf("line:%d", p.lines))
				p.usage.add(id, p.seq, ts, l.Message.Model, u)
			} else if len(l.Message.Usage) > 0 && string(l.Message.Usage) != "null" {
				p.stats.SkippedItems++
			}
		}
		blocks := assistantBlocks(l.Message.Content)
		// Uma resposta da API vem em várias linhas com o mesmo message.id.
		if p.pending != nil && l.Message.ID != "" && l.Message.ID == p.pendingID {
			p.pending.Blocks = append(p.pending.Blocks, blocks...)
			return nil
		}
		if err := p.flush(); err != nil {
			return err
		}
		p.pending = &domain.Message{
			NativeID: l.UUID, ParentID: l.ParentUUID, Role: domain.RoleAssistant,
			Timestamp: ts, Blocks: blocks,
		}
		p.pendingID = l.Message.ID
		return nil

	case "user":
		if err := p.flush(); err != nil {
			return err
		}
		if l.Message == nil {
			p.stats.SkippedItems++
			return nil
		}
		return p.emit(p.userMessage(l, ts))

	case "system":
		if err := p.flush(); err != nil {
			return err
		}
		return p.emit(domain.Message{
			NativeID: l.UUID, ParentID: l.ParentUUID, Role: domain.RoleSystem, Timestamp: ts,
			Blocks: []domain.Block{provider.Metadata(cmp.Or(l.Subtype, "system"), line, p.src)},
		})

	case "ai-title":
		if l.AITitle != "" {
			p.aiTitle = l.AITitle // o último vence
		}
	case "custom-title":
		if l.CustomTitle != "" {
			p.customTitle = l.CustomTitle
		}
	default:
		if err := p.flush(); err != nil {
			return err
		}
		block := provider.Unknown(l.Type, line)
		if isIgnored(l.Type) {
			// Conhecido, sem representação própria: metadado com o original.
			block = provider.Metadata(label(l), line, p.src)
		}
		return p.emit(domain.Message{NativeID: l.UUID, Role: domain.RoleSystem, Timestamp: ts, Blocks: []domain.Block{block}})
	}
	return nil
}

func (p *parser) userMessage(l rawLine, ts *time.Time) domain.Message {
	m := domain.Message{NativeID: l.UUID, ParentID: l.ParentUUID, Role: domain.RoleUser, Timestamp: ts}
	content := l.Message.Content

	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		if text != "" {
			m.Blocks = []domain.Block{{Kind: domain.BlockText, Text: text}}
		}
	} else {
		m.Blocks = userBlocks(content)
	}

	switch {
	case l.IsMeta:
		// Contexto injetado pelo Claude Code ou prompt de SDK, não digitado
		// pelo usuário.
		m.Role = domain.RoleSystem
		if p.firstMeta == "" {
			p.firstMeta = provider.FirstLine(firstText(m.Blocks))
		}
		for i := range m.Blocks {
			if m.Blocks[i].Kind == domain.BlockText {
				m.Blocks[i].Kind = domain.BlockMetadata
			}
		}
	case onlyToolResults(m.Blocks):
		m.Role = domain.RoleTool
	default:
		if t := firstText(m.Blocks); t != "" {
			p.human = true
			if p.firstPrompt == "" {
				p.firstPrompt = provider.FirstLine(t)
			}
		}
	}
	return m
}

func assistantBlocks(raw json.RawMessage) []domain.Block {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		if text == "" {
			return nil
		}
		return []domain.Block{{Kind: domain.BlockText, Text: text}}
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]domain.Block, 0, len(items))
	for _, it := range items {
		var b rawBlock
		if json.Unmarshal(it, &b) != nil {
			continue
		}
		switch b.Type {
		case "text":
			out = append(out, domain.Block{Kind: domain.BlockText, Text: b.Text})
		case "thinking":
			// Frequentemente vazio (só assinatura): nada a mostrar.
			if b.Thinking != "" {
				out = append(out, domain.Block{Kind: domain.BlockReasoning, Text: b.Thinking})
			}
		case "redacted_thinking":
		case "tool_use":
			out = append(out, domain.Block{Kind: domain.BlockToolCall, ToolName: b.Name, ToolCallID: b.ID, ToolInput: b.Input})
		default:
			out = append(out, provider.Unknown(b.Type, it))
		}
	}
	return out
}

// label descreve um registro conhecido; "attachment" leva o subtipo
// (hook_success, environment…).
func label(l rawLine) string {
	if l.Type == "attachment" && l.Attachment != "" {
		return "attachment: " + string(l.Attachment)
	}
	return l.Type
}

// mediaType lê source.media_type de blocos image/document.
func mediaType(raw json.RawMessage) string {
	var s struct {
		MediaType string `json:"media_type"`
	}
	json.Unmarshal(raw, &s)
	return s.MediaType
}

// images devolve referências às imagens de um tool_result.content.
func images(raw json.RawMessage) []domain.Block {
	var items []rawBlock
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	var out []domain.Block
	for _, it := range items {
		if it.Type == "image" {
			out = append(out, domain.Block{Kind: domain.BlockImageRef, Language: mediaType(it.Source)})
		}
	}
	return out
}

func userBlocks(raw json.RawMessage) []domain.Block {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]domain.Block, 0, len(items))
	for _, it := range items {
		var b rawBlock
		if json.Unmarshal(it, &b) != nil {
			continue
		}
		switch b.Type {
		case "text":
			out = append(out, domain.Block{Kind: domain.BlockText, Text: b.Text})
		case "image":
			// O conteúdo (base64) não é copiado; só o tipo.
			out = append(out, domain.Block{Kind: domain.BlockImageRef, Language: mediaType(b.Source)})
		case "document":
			out = append(out, domain.Block{Kind: domain.BlockFile, Text: cmp.Or(b.Title, "documento"), Language: mediaType(b.Source)})
		case "tool_result":
			out = append(out, domain.Block{
				Kind: domain.BlockToolResult, ToolCallID: b.ToolUseID,
				Text: resultText(b.Content), IsError: b.IsError,
			})
			out = append(out, images(b.Content)...)
		default:
			out = append(out, provider.Unknown(b.Type, it))
		}
	}
	return out
}

// resultText extrai o texto de tool_result.content: string ou lista de
// blocos {type:"text"}. Outros blocos (imagens) são ignorados.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var items []rawBlock
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var sb strings.Builder
	for _, it := range items {
		if it.Type == "text" && it.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(it.Text)
		}
	}
	return sb.String()
}

func onlyToolResults(bs []domain.Block) bool {
	if len(bs) == 0 {
		return false
	}
	for _, b := range bs {
		if b.Kind != domain.BlockToolResult {
			return false
		}
	}
	return true
}

func firstText(bs []domain.Block) string {
	for _, b := range bs {
		if b.Kind == domain.BlockText && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

func (p *parser) flush() error {
	if p.pending == nil {
		return nil
	}
	m := *p.pending
	p.pending, p.pendingID = nil, ""
	return p.emit(m)
}

func (p *parser) emit(m domain.Message) error {
	m.Seq = p.seq
	p.seq++
	if domain.CountsAsMessage(m) {
		p.count++
	}
	return p.sink.Message(p.key, m)
}

func (p *parser) session() domain.Session {
	s := domain.Session{
		Key:          p.key,
		FirstPrompt:  p.firstPrompt,
		Model:        p.model,
		CreatedAt:    p.span.First,
		UpdatedAt:    p.span.Last,
		MessageCount: p.count,
		Automated:    !p.human || strings.HasPrefix(p.entrypoint, "sdk"),
	}
	if path, err := domain.NormalizeProjectPath(p.cwd); err == nil {
		s.ProjectPath = path
	}
	switch {
	case p.customTitle != "":
		s.Title = p.customTitle
	case p.aiTitle != "":
		s.Title = p.aiTitle
	case p.firstPrompt != "":
		s.Title = p.firstPrompt
	default:
		s.Title = p.firstMeta
	}
	return s
}

// CLI procura o executável claude (configurado, PATH, ~/.local/bin).
func (a *Adapter) CLI(configured string) string {
	return provider.FindCLI(configured, "claude", a.o.LookPath,
		filepath.Join(a.o.Home, ".local", "bin", "claude"),
		filepath.Join(a.o.Home, ".claude", "local", "claude"))
}

// ResumeArgs: claude --resume <uuid>. O Claude encontra a sessão de
// qualquer diretório, mas as ferramentas do agente atuam no diretório
// atual; por isso o serviço roda no diretório do projeto.
func (a *Adapter) ResumeArgs(s domain.Session) ([]string, error) {
	if !provider.IsUUID(s.Key.NativeID) {
		return nil, provider.ErrInvalidSessionID
	}
	return []string{"--resume", s.Key.NativeID}, nil
}

// WatchDirs: projects/ inteiro (um subdiretório por projeto e os de
// subagentes).
func (a *Adapter) WatchDirs() []provider.WatchDir {
	return []provider.WatchDir{{Path: a.projectsDir(), Recursive: true}}
}
