// Package codex lê os rollouts JSONL do OpenAI Codex CLI.
//
// Formato e decisões: docs/discovery/03-codex.md. Resumo:
//   - fonte primária: $CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl
//     (e archived_sessions/); state_*.sqlite é só espelho e não é lido;
//   - prompt do usuário vem de event_msg/user_message (texto limpo);
//     resposta, tools e reasoning vêm de response_item; demais event_msg
//     duplicam conteúdo e são ignorados;
//   - *.jsonl.zst (compressão, feature desligada hoje) não é suportado e é
//     apenas contado na detecção.
package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/jsonl"
)

// Options configura o adaptador.
type Options struct {
	RootDir  string // $CODEX_HOME ou ~/.codex
	CLIPath  string
	Home     string
	LookPath provider.LookPath
}

// Adapter implementa provider.Provider para o Codex.
type Adapter struct{ o Options }

func New(o Options) *Adapter { return &Adapter{o: o} }

func (a *Adapter) ID() domain.ProviderID { return domain.ProviderCodex }

func (a *Adapter) archivedDir() string { return filepath.Join(a.o.RootDir, "archived_sessions") }

// dirs são os diretórios de rollouts: ativos e arquivados.
func (a *Adapter) dirs() []string {
	return []string{filepath.Join(a.o.RootDir, "sessions"), a.archivedDir()}
}

// Detect verifica sessions/ e conta rollouts compactados (.zst).
func (a *Adapter) Detect(context.Context) provider.Detection {
	d := provider.Detection{
		Provider: a.ID(),
		RootDir:  a.o.RootDir,
		CLIPath:  a.CLI(a.o.CLIPath),
		Status:   provider.StatusOK,
	}
	if dirs := a.dirs(); !provider.IsDir(dirs[0]) && !provider.IsDir(dirs[1]) {
		d.Status = provider.StatusNotFound
		d.Detail = "diretório sessions/ não encontrado"
		return d
	}
	if n := a.countCompressed(); n > 0 {
		d.Detail = fmt.Sprintf("%d rollout(s) compactado(s) .zst não suportado(s)", n)
	}
	return d
}

func (a *Adapter) countCompressed() int {
	n := 0
	for _, dir := range a.dirs() {
		filepath.WalkDir(dir, func(_ string, e fs.DirEntry, err error) error {
			if err == nil && e.Type().IsRegular() && isRollout(e.Name(), ".jsonl.zst") {
				n++
			}
			return nil
		})
	}
	return n
}

func isRollout(name, ext string) bool {
	return strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ext)
}

// Sources percorre sessions/ e archived_sessions/ sem seguir links.
func (a *Adapter) Sources(ctx context.Context) ([]provider.Source, error) {
	var out []provider.Source
	unreadable := 0
	for _, dir := range a.dirs() {
		err := filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				if path == dir && errors.Is(err, fs.ErrNotExist) {
					return fs.SkipDir
				}
				unreadable++ // subdiretório ilegível: segue
				return nil
			}
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			if e.Type().IsRegular() && isRollout(e.Name(), ".jsonl") {
				if src, err := provider.StatSource(path, domain.SourceJSONL); err == nil {
					out = append(out, src)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if unreadable > 0 {
		return out, fmt.Errorf("%w: %d diretório(s) ilegível(is)", provider.ErrIncomplete, unreadable)
	}
	return out, nil
}

// idFromName extrai o UUID do fim de rollout-<timestamp>-<uuid>.jsonl.
// Em threads revertidas (<thread>_<rollout>) usa o ID da thread.
func idFromName(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.IndexByte(stem, '_'); i >= 0 {
		stem = stem[:i]
	}
	const uuidLen = 36
	if len(stem) < uuidLen {
		return ""
	}
	id := stem[len(stem)-uuidLen:]
	if strings.Count(id, "-") != 4 {
		return ""
	}
	return id
}

// Parse lê o rollout em streaming.
func (a *Adapter) Parse(ctx context.Context, src provider.Source, sink provider.Sink) (provider.ParseStats, error) {
	var stats provider.ParseStats
	f, err := os.Open(src.Path)
	if err != nil {
		return stats, err
	}
	defer f.Close()

	us, usageOn := sink.(provider.UsageSink)
	p := &parser{sink: sink, stats: &stats, src: src, usageOn: usageOn}
	if id := idFromName(src.Path); id != "" {
		p.key, _ = domain.NewSessionKey(domain.ProviderCodex, id)
	}
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
	stats.Lines, stats.LongLines, stats.Partial = r.Lines, r.LongLines, r.Partial
	if p.key.NativeID == "" {
		return stats, errors.New("rollout sem ID de sessão")
	}
	if usageOn {
		if err := p.usage.emit(p.key, us, &stats); err != nil {
			return stats, err
		}
	}

	s := p.session()
	s.SourceKind = domain.SourceJSONL
	s.SourcePath = src.Path
	s.Archived = strings.HasPrefix(src.Path, a.archivedDir()+string(filepath.Separator))
	if s.UpdatedAt.IsZero() {
		s.UpdatedAt = src.ModTime.UTC()
	}
	return stats, sink.Session(s)
}

type rawLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// rawPayload tem todos os campos como JSON cru: o mesmo nome muda de tipo
// entre variantes (ex.: "summary" é string em turn_context e lista em
// reasoning). Cada campo é interpretado só onde é usado.
type rawPayload struct {
	Type      json.RawMessage `json:"type"`
	ID        json.RawMessage `json:"id"`
	CWD       json.RawMessage `json:"cwd"`
	Model     json.RawMessage `json:"model"`
	Role      json.RawMessage `json:"role"`
	Message   json.RawMessage `json:"message"`
	Info      json.RawMessage `json:"info"`
	Name      json.RawMessage `json:"name"`
	CallID    json.RawMessage `json:"call_id"`
	Arguments json.RawMessage `json:"arguments"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Action    json.RawMessage `json:"action"`
	Content   json.RawMessage `json:"content"`
	Summary   json.RawMessage `json:"summary"`
}

// str devolve o valor se for string JSON; senão "".
func str(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// ignored são variantes de RolloutItem sem valor para listagem ou busca
// (docs/discovery/03 §3).
var ignored = map[string]bool{
	"dynamic_tools_updated": true, "token_usage_record": true, "world_state": true,
	"retained_context": true, "security_risk_score": true, "realtime_item": true,
	"inter_agent_communication": true, "inter_agent_communication_metadata": true,
}

type parser struct {
	key   domain.SessionKey
	sink  provider.Sink
	stats *provider.ParseStats
	src   provider.Source

	seq         int
	usageOn     bool // o sink grava consumo (indexador)
	usage       usageTracker
	metaSeen    bool
	span        provider.Span
	cwd         string
	model       string
	firstPrompt string
	count       int
}

func (p *parser) handle(line []byte) error {
	var l rawLine
	if err := json.Unmarshal(line, &l); err != nil {
		p.stats.BadLines++
		return nil
	}
	ts := provider.ParseTime(l.Timestamp)
	p.span.Add(ts)
	var pl rawPayload
	if len(l.Payload) > 0 && json.Unmarshal(l.Payload, &pl) != nil {
		p.stats.BadLines++
		return nil
	}

	switch l.Type {
	case "session_meta":
		if !p.metaSeen {
			p.metaSeen = true
			p.cwd = str(pl.CWD)
			// O ID do session_meta vale se o nome do arquivo não trouxe um
			// e nenhuma mensagem foi emitida ainda.
			if id := str(pl.ID); id != "" && p.seq == 0 {
				if k, err := domain.NewSessionKey(domain.ProviderCodex, id); err == nil {
					p.key = k
				}
			}
		}
		return nil
	case "turn_context":
		if m := str(pl.Model); m != "" {
			p.model = m
		}
		if p.cwd == "" {
			p.cwd = str(pl.CWD)
		}
		return nil
	case "event_msg":
		if p.usageOn && str(pl.Type) == "token_count" {
			p.usage.add(pl.Info, p.seq, ts, p.model)
		}
		if str(pl.Type) == "user_message" {
			if p.firstPrompt == "" {
				p.firstPrompt = provider.FirstLine(str(pl.Message))
			}
			return p.emit(domain.Message{Role: domain.RoleUser, Timestamp: ts,
				Blocks: []domain.Block{{Kind: domain.BlockText, Text: str(pl.Message)}}})
		}
		// Demais eventos duplicam response_item ou são telemetria: metadado.
		return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts,
			Blocks: []domain.Block{provider.Metadata("event_msg/"+str(pl.Type), line, p.src)}})
	case "response_item":
		return p.responseItem(pl, ts, l.Payload)
	case "compacted":
		return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts,
			Blocks: []domain.Block{provider.Metadata("compacted", line, p.src)}})
	default:
		block := provider.Unknown(l.Type, line)
		if ignored[l.Type] {
			block = provider.Metadata(l.Type, line, p.src)
		}
		return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts, Blocks: []domain.Block{block}})
	}
}

func (p *parser) responseItem(pl rawPayload, ts *time.Time, raw json.RawMessage) error {
	typ := str(pl.Type)
	switch typ {
	case "message":
		if role := str(pl.Role); role != "assistant" {
			// developer/user/system: instruções e contexto injetado. O prompt
			// limpo do usuário vem de event_msg/user_message.
			return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts,
				Blocks: []domain.Block{provider.Metadata("contexto injetado ("+role+")", raw, p.src)}})
		}
		text := contentText(pl.Content)
		if text == "" {
			return nil
		}
		return p.emit(domain.Message{Role: domain.RoleAssistant, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockText, Text: text}}})
	case "reasoning":
		text := contentText(pl.Summary)
		if text == "" {
			// Só encrypted_content: registra o passo, sem o conteúdo cifrado.
			return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts,
				Blocks: []domain.Block{{Kind: domain.BlockMetadata, Text: "reasoning (cifrado)"}}})
		}
		return p.emit(domain.Message{Role: domain.RoleAssistant, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockReasoning, Text: text}}})
	case "function_call":
		return p.emit(domain.Message{Role: domain.RoleAssistant, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockToolCall, ToolName: str(pl.Name), ToolCallID: str(pl.CallID), ToolInput: jsonOrString(str(pl.Arguments))}}})
	case "custom_tool_call":
		return p.emit(domain.Message{Role: domain.RoleAssistant, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockToolCall, ToolName: str(pl.Name), ToolCallID: str(pl.CallID), ToolInput: jsonOrString(str(pl.Input))}}})
	case "web_search_call":
		return p.emit(domain.Message{Role: domain.RoleAssistant, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockToolCall, ToolName: "web_search", ToolInput: validJSON(pl.Action)}}})
	case "function_call_output", "custom_tool_call_output":
		return p.emit(domain.Message{Role: domain.RoleTool, Timestamp: ts,
			Blocks: []domain.Block{{Kind: domain.BlockToolResult, ToolCallID: str(pl.CallID), Text: outputText(pl.Output)}}})
	default:
		return p.emit(domain.Message{Role: domain.RoleSystem, Timestamp: ts,
			Blocks: []domain.Block{provider.Unknown("response_item/"+typ, raw)}})
	}
}

// contentText junta os campos "text" dos itens de content
// (output_text/input_text).
func contentText(raw json.RawMessage) string {
	var items []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var parts []string
	for _, it := range items {
		if it.Text != "" {
			parts = append(parts, it.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// outputText aceita output como string ou objeto com "content" string.
func outputText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Content != "" {
		return obj.Content
	}
	return ""
}

// jsonOrString devolve s como JSON se já for JSON válido; senão como string
// JSON (ex.: patch do apply_patch).
func jsonOrString(s string) json.RawMessage {
	if s == "" {
		return nil
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, _ := json.Marshal(s)
	return b
}

func validJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return raw
}

func (p *parser) emit(m domain.Message) error {
	if p.key.NativeID == "" {
		p.stats.SkippedItems++ // sem ID ainda (nome fora do padrão e sem session_meta)
		return nil
	}
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
		Title:        p.firstPrompt,
		FirstPrompt:  p.firstPrompt,
		Model:        p.model,
		CreatedAt:    p.span.First,
		UpdatedAt:    p.span.Last,
		MessageCount: p.count,
		Automated:    p.firstPrompt == "",
	}
	if path, err := domain.NormalizeProjectPath(p.cwd); err == nil {
		s.ProjectPath = path
	}
	return s
}

// CLI procura o executável codex (configurado, PATH, ~/.local/bin).
func (a *Adapter) CLI(configured string) string {
	return provider.FindCLI(configured, "codex", a.o.LookPath, provider.CLICandidates(runtime.GOOS, domain.ProviderCodex, a.o.Home)...)
}

// ResumeArgs: codex resume <uuid>. O UUID tem precedência sobre nomes de
// sessão na CLI. Rodar fora do cwd original faz a TUI perguntar qual
// diretório usar; o serviço roda no cwd da sessão para evitar a pergunta.
func (a *Adapter) ResumeArgs(s domain.Session) ([]string, error) {
	if !provider.IsUUID(s.Key.NativeID) {
		return nil, provider.ErrInvalidSessionID
	}
	return []string{"resume", s.Key.NativeID}, nil
}

// WatchDirs: sessions/AAAA/MM/DD e archived_sessions/.
func (a *Adapter) WatchDirs() []provider.WatchDir {
	var out []provider.WatchDir
	for _, d := range a.dirs() {
		out = append(out, provider.WatchDir{Path: d, Recursive: true})
	}
	return out
}
