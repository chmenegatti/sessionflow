package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/repository/sqlite"
	"sessionflow/internal/terminal"
)

// Erros da retomada exibidos ao usuário.
var (
	ErrNotResumable   = errors.New("subagentes e subsessões não são retomados sozinhos; retome a sessão principal")
	ErrCLINotFound    = errors.New("executável do provider não encontrado; informe o caminho em Configurações")
	ErrNoProjectDir   = errors.New("a sessão não registra o diretório de trabalho; não é possível retomá-la com segurança")
	ErrSessionActive  = errors.New("a sessão foi alterada há menos de um minuto e pode estar aberta em outro terminal")
	errResumeInternal = errors.New("falha ao preparar a retomada; veja o log")
)

// activeWindow: fonte alterada há menos que isso = sessão provavelmente
// aberta. Dois processos na mesma sessão podem perder mensagens (Claude).
const activeWindow = time.Minute

// ResumePlan descreve a retomada antes de executá-la. Command é só para
// exibição; a execução usa Path/Args/Dir separados.
type ResumePlan struct {
	SessionID string            `json:"sessionId"`
	Provider  domain.ProviderID `json:"provider"`
	CLIPath   string            `json:"cliPath"`
	Args      []string          `json:"args"`
	Dir       string            `json:"dir"`
	Command   string            `json:"command"`  // linha legível (não é executada)
	Terminal  string            `json:"terminal"` // nome do emulador
	Active    bool              `json:"active"`   // alterada há menos de um minuto
}

// ResumeService retoma sessões pela CLI original, num terminal.
type ResumeService struct {
	index     *sqlite.IndexStore
	providers map[domain.ProviderID]provider.Provider
	config    *ConfigStore
	launcher  terminal.Launcher
	look      terminal.LookPath
	now       func() time.Time
	log       *slog.Logger
}

// ResumeDeps são as dependências do ResumeService.
type ResumeDeps struct {
	Index     *sqlite.IndexStore
	Providers []provider.Provider
	Config    *ConfigStore
	Launcher  terminal.Launcher
	LookPath  terminal.LookPath
	Now       func() time.Time // nil = time.Now
	Logger    *slog.Logger
}

// NewResumeService cria o serviço.
func NewResumeService(d ResumeDeps) *ResumeService {
	m := make(map[domain.ProviderID]provider.Provider, len(d.Providers))
	for _, p := range d.Providers {
		m[p.ID()] = p
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &ResumeService{index: d.Index, providers: m, config: d.Config, launcher: d.Launcher, look: d.LookPath, now: now, log: d.Logger}
}

// Plan valida a sessão e monta o comando, sem executar.
func (r *ResumeService) Plan(ctx context.Context, id string) (ResumePlan, error) {
	plan, _, _, err := r.plan(ctx, id)
	return plan, err
}

// Resume abre a sessão no terminal. Com a sessão possivelmente aberta em
// outro lugar, só prossegue com allowActive (confirmação do usuário).
func (r *ResumeService) Resume(ctx context.Context, id string, allowActive bool) (ResumePlan, error) {
	plan, term, cmd, err := r.plan(ctx, id)
	if err != nil {
		return plan, err
	}
	if plan.Active && !allowActive {
		return plan, ErrSessionActive
	}
	if err := r.launcher.Launch(ctx, term, cmd); err != nil {
		r.log.Warn("retomada", "provider", plan.Provider, "terminal", plan.Terminal, "err", err)
		return plan, fmt.Errorf("não foi possível abrir o terminal: %w", err)
	}
	r.log.Info("sessão retomada", "provider", plan.Provider, "terminal", plan.Terminal)
	return plan, nil
}

func (r *ResumeService) plan(ctx context.Context, id string) (ResumePlan, terminal.Terminal, terminal.Command, error) {
	var (
		plan ResumePlan
		term terminal.Terminal
		cmd  terminal.Command
	)
	key, err := parseKey(id)
	if err != nil {
		return plan, term, cmd, err
	}
	s, err := r.index.GetSession(ctx, key)
	if errors.Is(err, sqlite.ErrNotFound) {
		return plan, term, cmd, ErrSessionNotFound
	}
	if err != nil {
		r.log.Error("retomada", "op", "ler sessão", "err", err)
		return plan, term, cmd, errResumeInternal
	}
	plan.SessionID, plan.Provider = id, key.Provider
	p, ok := r.providers[key.Provider]
	if !ok {
		return plan, term, cmd, ErrProviderDisabled
	}
	res, ok := p.(provider.Resumer)
	if !ok || s.Parent != nil {
		return plan, term, cmd, ErrNotResumable
	}

	cfg := r.config.Get()
	cli := res.CLI(cfg.Providers[key.Provider].CLIPath)
	if cli == "" {
		return plan, term, cmd, ErrCLINotFound
	}
	dir, err := WorkDir(s.ProjectPath)
	if err != nil {
		return plan, term, cmd, err
	}
	s.ProjectPath = dir
	args, err := res.ResumeArgs(s)
	if err != nil {
		return plan, term, cmd, err
	}
	term, err = terminal.Resolve(cfg.Terminal, r.look)
	if err != nil {
		return plan, term, cmd, err
	}
	cmd = terminal.Command{Path: cli, Args: args, Dir: dir}
	if err := term.Check(cmd); err != nil {
		return plan, term, cmd, err
	}
	plan.CLIPath, plan.Args, plan.Dir = cli, args, dir
	plan.Command = displayCommand(cmd.Argv())
	plan.Terminal = term.Name()
	plan.Active = r.active(s)
	return plan, term, cmd, nil
}

// WorkDir valida o diretório de trabalho da sessão: absoluto, existente e
// diretório. Rodar o agente em outro lugar faria as ferramentas dele
// atuarem nos arquivos errados, então não há diretório substituto.
func WorkDir(p string) (string, error) {
	if p == "" {
		return "", ErrNoProjectDir
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("diretório de trabalho inválido (não é absoluto): %s", p)
	}
	clean := filepath.Clean(p)
	if info, err := os.Stat(clean); err != nil || !info.IsDir() {
		return "", fmt.Errorf("o diretório de trabalho da sessão não existe mais: %s", clean)
	}
	return clean, nil
}

// active: última mensagem ou última escrita no arquivo da sessão há menos
// de activeWindow. Bancos (OpenCode) guardam várias sessões: a data do
// arquivo não diz nada sobre esta, então vale só a da mensagem.
func (r *ResumeService) active(s domain.Session) bool { return sessionActive(s, r.now()) }

// sessionActive: escrita (mensagem ou arquivo JSONL) há menos de
// activeWindow. Regra única da retomada e do Dashboard ("Abertas").
func sessionActive(s domain.Session, now time.Time) bool {
	last := s.UpdatedAt
	if s.SourceKind == domain.SourceJSONL {
		if info, err := os.Stat(s.SourcePath); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return !last.IsZero() && now.Sub(last) < activeWindow
}

// displayCommand monta uma linha legível do argv, com aspas simples onde
// preciso. Só para exibição: nada é interpretado por shell.
func displayCommand(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && !strings.ContainsFunc(a, func(r rune) bool {
			return !(r == '/' || r == '-' || r == '_' || r == '.' || r == ':' || r == '=' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
		}) {
			parts[i] = a
			continue
		}
		parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}
