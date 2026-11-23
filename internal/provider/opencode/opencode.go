// Package opencode lê as sessões do OpenCode a partir do banco SQLite
// (opencode.db, esquema drizzle v1, OpenCode >= 1.2).
//
// Formato e decisões: docs/discovery/04-opencode.md. Resumo:
//   - só as tabelas session, message e part são lidas; nunca account*;
//   - o banco é aberto por internal/provider/sqliteread (sem criar arquivos
//     no diretório do OpenCode);
//   - o esquema v2 (session_message/session_part) e o legado JSON (< 1.2)
//     não são suportados: a fonte fica "unsupported", sem erro fatal.
package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/sqliteread"
)

// Options configura o adaptador.
type Options struct {
	RootDir     string // $XDG_DATA_HOME/opencode ou ~/.local/share/opencode
	DBOverride  string // valor de $OPENCODE_DB ("" = não definido)
	CLIPath     string
	Home        string
	LookPath    provider.LookPath
	SnapshotDir string // diretório privado para cópias do banco
}

// Adapter implementa provider.Provider para o OpenCode.
type Adapter struct{ o Options }

func New(o Options) *Adapter { return &Adapter{o: o} }

func (a *Adapter) ID() domain.ProviderID { return domain.ProviderOpenCode }

// dbPaths devolve os bancos candidatos. OPENCODE_DB absoluto ou relativo ao
// diretório de dados vence; ":memory:" não tem arquivo. Sem override:
// opencode.db e opencode-<canal>.db.
func (a *Adapter) dbPaths() []string {
	switch v := a.o.DBOverride; {
	case v == ":memory:":
		return nil
	case filepath.IsAbs(v):
		return []string{filepath.Clean(v)}
	case v != "":
		return []string{filepath.Join(a.o.RootDir, v)}
	}
	matches, _ := filepath.Glob(filepath.Join(a.o.RootDir, "opencode*.db"))
	return matches
}

// Detect verifica a existência do banco. O esquema é validado no Parse.
func (a *Adapter) Detect(context.Context) provider.Detection {
	d := provider.Detection{
		Provider: a.ID(),
		RootDir:  a.o.RootDir,
		CLIPath:  a.CLI(a.o.CLIPath),
		Status:   provider.StatusOK,
	}
	for _, p := range a.dbPaths() {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return d
		}
	}
	d.Status = provider.StatusNotFound
	d.Detail = "opencode.db não encontrado"
	if provider.IsDir(filepath.Join(a.o.RootDir, "storage", "session")) {
		d.Status = provider.StatusUnsupported
		d.Detail = "armazenamento JSON legado (OpenCode < 1.2) não suportado"
	}
	return d
}

// Sources devolve um Source por banco. O Fingerprint inclui o -wal, porque
// escritas em WAL não mudam o tamanho nem a data do .db.
func (a *Adapter) Sources(context.Context) ([]provider.Source, error) {
	var out []provider.Source
	for _, p := range a.dbPaths() {
		src, err := provider.StatSource(p, domain.SourceSQLite)
		if err != nil {
			continue
		}
		if info, err := os.Stat(p + "-wal"); err == nil {
			src.Fingerprint = fmt.Sprintf("wal:%d:%d", info.Size(), info.ModTime().UnixNano())
		}
		out = append(out, src)
	}
	return out, nil
}

// Parse lê todas as sessões do banco.
func (a *Adapter) Parse(ctx context.Context, src provider.Source, sink provider.Sink) (provider.ParseStats, error) {
	return a.parse(ctx, src, "", sink)
}

// ParseSession lê só a sessão key (viewer).
func (a *Adapter) ParseSession(ctx context.Context, src provider.Source, key domain.SessionKey, sink provider.Sink) (provider.ParseStats, error) {
	return a.parse(ctx, src, key.NativeID, sink)
}

// parse lê as sessões do banco; only != "" restringe a uma sessão.
func (a *Adapter) parse(ctx context.Context, src provider.Source, only string, sink provider.Sink) (provider.ParseStats, error) {
	var stats provider.ParseStats
	db, err := sqliteread.Open(ctx, src.Path, a.o.SnapshotDir)
	if err != nil {
		return stats, err
	}
	defer db.Close()

	sel, err := sessionQuery(ctx, db.DB)
	if err != nil {
		stats.Unsupported = errors.Is(err, provider.ErrUnsupported)
		return stats, err
	}
	var args []any
	if only != "" {
		sel += " WHERE id = ?"
		args = append(args, only)
	}
	sessions, err := loadSessions(ctx, db.DB, sel+" ORDER BY time_created, id", args...)
	if err != nil {
		return stats, err
	}
	for _, row := range sessions {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if err := a.parseSession(ctx, db.DB, row, src, sink, &stats); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// sessionQuery valida o esquema v1 e monta o SELECT com as colunas
// opcionais que existirem nesta versão.
func sessionQuery(ctx context.Context, db *sql.DB) (string, error) {
	tables, err := sqliteread.Tables(ctx, db)
	if err != nil {
		return "", err
	}
	if !tables["session"] || !tables["message"] || !tables["part"] {
		if tables["session_message"] {
			return "", fmt.Errorf("%w: esquema OpenCode v2", provider.ErrUnsupported)
		}
		return "", fmt.Errorf("%w: tabelas session/message/part ausentes", provider.ErrUnsupported)
	}
	cols, err := sqliteread.Columns(ctx, db, "session")
	if err != nil {
		return "", err
	}
	for _, c := range []string{"id", "directory", "title", "time_created", "time_updated"} {
		if !cols[c] {
			return "", fmt.Errorf("%w: coluna session.%s ausente", provider.ErrUnsupported, c)
		}
	}
	opt := func(c string) string {
		if cols[c] {
			return c
		}
		return "NULL"
	}
	return "SELECT id, " + opt("parent_id") + ", directory, title, time_created, time_updated, " +
		opt("time_archived") + " FROM session", nil
}

type sessionRow struct {
	id, directory, title string
	parentID             sql.NullString
	created, updated     int64
	archived             sql.NullInt64
}

func loadSessions(ctx context.Context, db *sql.DB, q string, args ...any) ([]sessionRow, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.parentID, &r.directory, &r.title, &r.created, &r.updated, &r.archived); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rawMessage struct {
	Role     string `json:"role"`
	ParentID string `json:"parentID"`
	ModelID  string `json:"modelID"`
	Model    struct {
		ModelID string `json:"modelID"`
	} `json:"model"`
	// Lido à parte (decodeTokens): consumo ilegível não derruba a mensagem.
	Tokens json.RawMessage `json:"tokens"`
}

type rawPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Mime     string `json:"mime"`
	Filename string `json:"filename"`
	Tool     string `json:"tool"`
	CallID   string `json:"callID"`
	State    struct {
		Status string          `json:"status"`
		Input  json.RawMessage `json:"input"`
		Output string          `json:"output"`
		Error  string          `json:"error"`
	} `json:"state"`
}

// metadataParts são parts conhecidos sem texto de conversa.
var metadataParts = map[string]bool{
	"step-start": true, "step-finish": true, "snapshot": true, "patch": true,
	"agent": true, "retry": true, "compaction": true, "subtask": true,
}

func (a *Adapter) parseSession(ctx context.Context, db *sql.DB, row sessionRow, src provider.Source, sink provider.Sink, stats *provider.ParseStats) error {
	key, err := domain.NewSessionKey(domain.ProviderOpenCode, row.id)
	if err != nil {
		stats.SkippedItems++
		return nil
	}
	parts, err := loadParts(ctx, db, row.id, src, stats)
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx,
		"SELECT id, time_created, data FROM message WHERE session_id = ? ORDER BY time_created, id", row.id)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		seq, count         int
		model, firstPrompt string
		span               provider.Span
	)
	span.Add(provider.MillisTime(row.created))
	span.Add(provider.MillisTime(row.updated))
	for rows.Next() {
		var id string
		var created int64
		var data []byte
		if err := rows.Scan(&id, &created, &data); err != nil {
			return err
		}
		stats.Lines++
		var rm rawMessage
		if json.Unmarshal(data, &rm) != nil {
			stats.BadLines++
			continue
		}
		m := domain.Message{Seq: seq, NativeID: id, ParentID: rm.ParentID, Timestamp: provider.MillisTime(created)}
		switch rm.Role {
		case "user":
			m.Role = domain.RoleUser
		case "assistant":
			m.Role = domain.RoleAssistant
			if rm.ModelID != "" {
				model = rm.ModelID
			}
		default:
			m.Role = domain.RoleSystem
		}
		if model == "" && rm.Model.ModelID != "" {
			model = rm.Model.ModelID
		}
		m.Blocks = parts[id]
		if domain.CountsAsMessage(m) {
			count++
			if m.Role == domain.RoleUser && firstPrompt == "" {
				firstPrompt = provider.FirstLine(firstText(m.Blocks))
			}
		}
		seq++
		if err := sink.Message(key, m); err != nil {
			return err
		}
		if us, ok := sink.(provider.UsageSink); ok && m.Role == domain.RoleAssistant {
			if t, ok := decodeTokens(rm.Tokens); ok {
				if err := us.Usage(key, usageOf(id, m.Seq, m.Timestamp, rm.ModelID, t)); err != nil {
					return err
				}
			} else if len(rm.Tokens) > 0 && string(rm.Tokens) != "null" {
				stats.SkippedItems++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s := domain.Session{
		Key: key, Title: row.title, FirstPrompt: firstPrompt, Model: model,
		CreatedAt: span.First, UpdatedAt: span.Last, MessageCount: count,
		SourceKind: domain.SourceSQLite, SourcePath: src.Path,
		Archived:  row.archived.Valid && row.archived.Int64 > 0,
		Automated: firstPrompt == "",
	}
	if s.Title == "" {
		s.Title = firstPrompt
	}
	if p, err := domain.NormalizeProjectPath(row.directory); err == nil {
		s.ProjectPath = p
	}
	if row.parentID.Valid && row.parentID.String != "" {
		if pk, err := domain.NewSessionKey(domain.ProviderOpenCode, row.parentID.String); err == nil {
			s.Parent = &pk
		}
	}
	return sink.Session(s)
}

// loadParts lê os parts da sessão agrupados por mensagem, na ordem
// (time_created, id).
func loadParts(ctx context.Context, db *sql.DB, sessionID string, src provider.Source, stats *provider.ParseStats) (map[string][]domain.Block, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT message_id, data FROM part WHERE session_id = ? ORDER BY time_created, id", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]domain.Block{}
	for rows.Next() {
		var msgID string
		var data []byte
		if err := rows.Scan(&msgID, &data); err != nil {
			return nil, err
		}
		var p rawPart
		if json.Unmarshal(data, &p) != nil {
			stats.BadLines++
			continue
		}
		out[msgID] = append(out[msgID], partBlocks(p, data, src)...)
	}
	return out, rows.Err()
}

func partBlocks(p rawPart, data []byte, src provider.Source) []domain.Block {
	switch {
	case p.Type == "text":
		return []domain.Block{{Kind: domain.BlockText, Text: p.Text}}
	case p.Type == "reasoning":
		if p.Text == "" {
			return nil
		}
		return []domain.Block{{Kind: domain.BlockReasoning, Text: p.Text}}
	case p.Type == "tool":
		out := []domain.Block{{Kind: domain.BlockToolCall, ToolName: p.Tool, ToolCallID: p.CallID, ToolInput: validJSON(p.State.Input)}}
		switch p.State.Status {
		case "completed":
			out = append(out, domain.Block{Kind: domain.BlockToolResult, ToolCallID: p.CallID, Text: p.State.Output})
		case "error":
			out = append(out, domain.Block{Kind: domain.BlockToolResult, ToolCallID: p.CallID, Text: p.State.Error, IsError: true})
		}
		return out
	case p.Type == "file":
		// Campos mime/filename vistos no código-fonte (message-v2.ts), não
		// em dados locais: lidos de forma tolerante. A URL (pode ser data:
		// com base64) não é copiada.
		kind := domain.BlockFile
		if strings.HasPrefix(p.Mime, "image/") {
			kind = domain.BlockImageRef
		}
		return []domain.Block{{Kind: kind, Text: p.Filename, Language: p.Mime}}
	case metadataParts[p.Type]:
		return []domain.Block{provider.Metadata(p.Type, data, src)}
	default:
		return []domain.Block{provider.Unknown(p.Type, data)}
	}
}

func validJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return raw
}

func firstText(bs []domain.Block) string {
	for _, b := range bs {
		if b.Kind == domain.BlockText && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// CLI procura o executável opencode (configurado, PATH, instalador
// oficial em ~/.opencode/bin, ~/.local/bin).
func (a *Adapter) CLI(configured string) string {
	return provider.FindCLI(configured, "opencode", a.o.LookPath,
		filepath.Join(a.o.Home, ".opencode", "bin", "opencode"),
		filepath.Join(a.o.Home, ".local", "bin", "opencode"))
}

var sessionIDRe = regexp.MustCompile(`^ses_[0-9A-Za-z]{8,64}$`)

// ResumeArgs: opencode <projeto> --session <id>. A TUI abre o projeto do
// argumento posicional; o caminho é absoluto (começa com "/"), então não
// é lido como opção.
func (a *Adapter) ResumeArgs(s domain.Session) ([]string, error) {
	if !sessionIDRe.MatchString(s.Key.NativeID) {
		return nil, provider.ErrInvalidSessionID
	}
	if !filepath.IsAbs(s.ProjectPath) {
		return nil, errors.New("sessão sem diretório de projeto")
	}
	return []string{s.ProjectPath, "--session", s.Key.NativeID}, nil
}

// WatchDirs: o diretório de dados (banco novo) e o de cada banco, sem
// subdiretórios. O watcher ignora o -shm, que a própria leitura toca.
func (a *Adapter) WatchDirs() []provider.WatchDir {
	seen := map[string]bool{a.o.RootDir: true}
	out := []provider.WatchDir{{Path: a.o.RootDir}}
	for _, p := range a.dbPaths() {
		if d := filepath.Dir(p); !seen[d] {
			seen[d] = true
			out = append(out, provider.WatchDir{Path: d})
		}
	}
	return out
}
