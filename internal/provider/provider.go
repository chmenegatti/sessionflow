// Package provider define o contrato dos adaptadores de cada agente
// (Claude Code, Codex, OpenCode) e utilitários comuns a eles.
//
// Um adaptador só LÊ os históricos originais. Ele descobre as fontes
// (arquivos JSONL ou bancos SQLite), interpreta o formato e entrega sessões
// e mensagens normalizadas (internal/domain) a um Sink. Persistir é
// responsabilidade de quem consome (internal/indexer).
package provider

import (
	"context"
	"errors"
	"time"

	"sessionflow/internal/domain"
)

// Status resume o resultado da detecção de um provider.
type Status string

const (
	StatusOK          Status = "ok"          // dados encontrados e formato suportado
	StatusNotFound    Status = "not_found"   // diretório de dados ausente
	StatusUnsupported Status = "unsupported" // dados presentes em formato que não sabemos ler
	StatusError       Status = "error"       // falha ao inspecionar
	StatusDisabled    Status = "disabled"    // desabilitado na configuração
)

// Detection é o que um adaptador sabe sobre a instalação local.
type Detection struct {
	Provider domain.ProviderID `json:"provider"`
	RootDir  string            `json:"rootDir"`
	Status   Status            `json:"status"`
	Detail   string            `json:"detail,omitempty"` // curto, sem conteúdo de conversa
	CLIPath  string            `json:"cliPath,omitempty"`
}

// Source é uma unidade de leitura: um arquivo JSONL ou um banco SQLite.
// Size, ModTime e Fingerprint permitem pular fontes que não mudaram.
type Source struct {
	Path        string
	Kind        domain.SourceKind
	Size        int64
	ModTime     time.Time
	Fingerprint string // opaco; ex.: estado do -wal de um banco SQLite
	// Raw pede o JSON original em blocos de metadado (viewer). A indexação
	// não precisa dele e evita a cópia.
	Raw bool
}

// SessionReader é implementado por adaptadores cuja fonte guarda várias
// sessões (OpenCode): lê só a sessão pedida.
type SessionReader interface {
	ParseSession(ctx context.Context, src Source, key domain.SessionKey, sink Sink) (ParseStats, error)
}

// Sink recebe o resultado do parse em streaming. Para cada sessão, as
// mensagens chegam em ordem (Seq crescente) e Session é chamado uma vez,
// depois das mensagens dela, com os metadados finais.
type Sink interface {
	Message(key domain.SessionKey, m domain.Message) error
	Session(s domain.Session) error
}

// UsageSink é implementado por sinks que gravam consumo de tokens (o
// indexador). O viewer não implementa: os adaptadores verificam por type
// assertion e só então extraem consumo. Cada registro chega uma vez por
// resposta/pedido, já deduplicado, antes de Session da sessão.
type UsageSink interface {
	Usage(key domain.SessionKey, u domain.Usage) error
}

// ParseStats conta problemas tolerados durante o parse. Nunca carrega
// conteúdo.
type ParseStats struct {
	Lines        int  // registros lidos
	BadLines     int  // JSON inválido, ignorados
	LongLines    int  // acima do limite, ignorados
	Partial      bool // última linha incompleta (escrita em andamento)
	Unsupported  bool // fonte reconhecida, mas em formato não suportado
	SkippedItems int  // registros válidos sem utilidade (ex.: sem ID)

	UsageConflicts int  // linhas da mesma resposta com consumo diferente (Claude)
	UsageMismatch  bool // soma dos pedidos ≠ acumulado da sessão (Codex)
}

// Provider é o contrato de um adaptador.
type Provider interface {
	ID() domain.ProviderID
	// Detect inspeciona diretórios e executável. Não falha: problemas
	// aparecem em Detection.Status.
	Detect(ctx context.Context) Detection
	// Sources lista as fontes atuais. Pode devolver fontes junto com um
	// erro que envolve ErrIncomplete.
	Sources(ctx context.Context) ([]Source, error)
	// Parse lê uma fonte inteira e a entrega ao sink.
	Parse(ctx context.Context, src Source, sink Sink) (ParseStats, error)
}

// ErrUnsupported indica uma fonte em formato que o adaptador não lê.
var ErrUnsupported = errors.New("formato não suportado")

// ErrIncomplete acompanha uma listagem de Sources parcial (ex.: um
// subdiretório ilegível). As fontes devolvidas são válidas, mas a ausência
// de uma fonte não prova que ela foi apagada.
var ErrIncomplete = errors.New("listagem incompleta")

// Resumer monta a retomada de uma sessão pela CLI original. Não executa
// nada: quem executa é o terminal.Launcher, a partir do serviço.
type Resumer interface {
	// CLI devolve o executável: o configurado (se for executável), o do
	// PATH ou um caminho de instalação conhecido; "" se não houver.
	CLI(configured string) string
	// ResumeArgs devolve os argumentos após o executável para retomar s.
	// ID fora do formato do provider é erro: nunca vira argumento solto
	// (um ID como "--help" seria lido como opção).
	ResumeArgs(s domain.Session) ([]string, error)
}

// ErrInvalidSessionID indica um ID nativo fora do formato do provider.
var ErrInvalidSessionID = errors.New("identificador de sessão fora do formato do provider")

// WatchDir é um diretório que o watcher monitora.
type WatchDir struct {
	Path      string
	Recursive bool // inclui subdiretórios (criados depois também)
}

// Watchable informa onde as fontes do provider mudam.
type Watchable interface {
	WatchDirs() []WatchDir
}
