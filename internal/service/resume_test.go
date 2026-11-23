package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/provider/codex"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/repository/sqlite"
	"sessionflow/internal/terminal"
)

// fakeLauncher registra o que seria aberto.
type fakeLauncher struct {
	calls []terminal.Command
	terms []terminal.Terminal
	err   error
}

func (f *fakeLauncher) Launch(_ context.Context, t terminal.Terminal, c terminal.Command) error {
	f.calls = append(f.calls, c)
	f.terms = append(f.terms, t)
	return f.err
}

type resumeEnv struct {
	svc      *ResumeService
	store    *sqlite.IndexStore
	cfg      *ConfigStore
	launcher *fakeLauncher
	project  string
	src      string // arquivo da sessão Claude principal
	clis     map[domain.ProviderID]string
	now      time.Time
	deps     ResumeDeps
}

const (
	claudeID = "123e4567-e89b-12d3-a456-426614174000"
	codexID  = "223e4567-e89b-12d3-a456-426614174000"
	ocID     = "ses_3a1f9c2b7e0dAbCdEf"
)

func newResumeEnv(t *testing.T) *resumeEnv {
	t.Helper()
	dir := t.TempDir()
	e := &resumeEnv{launcher: &fakeLauncher{}, project: filepath.Join(dir, "projeto"), clis: map[domain.ProviderID]string{}}
	os.Mkdir(e.project, 0o755)
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	for _, id := range domain.AllProviders() {
		p := filepath.Join(bin, string(id))
		os.WriteFile(p, nil, 0o755)
		e.clis[id] = p
	}
	term := filepath.Join(bin, "ptyxis")
	os.WriteFile(term, nil, 0o755)
	look := func(name string) (string, error) {
		if name == "ptyxis" {
			return term, nil
		}
		return "", errors.New("não")
	}

	db, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(dir, "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.store = sqlite.NewIndexStore(db)
	e.src = filepath.Join(dir, claudeID+".jsonl")
	os.WriteFile(e.src, []byte("{}\n"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(e.src, old, old)
	e.now = time.Now()

	parent := domain.SessionKey{Provider: domain.ProviderClaude, NativeID: claudeID}
	for _, s := range []domain.Session{
		{Key: parent, ProjectPath: e.project, SourceKind: domain.SourceJSONL, SourcePath: e.src, UpdatedAt: old},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: claudeID + "/agent-a"}, Parent: &parent, ProjectPath: e.project, UpdatedAt: old},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "323e4567-e89b-12d3-a456-426614174000"}, ProjectPath: filepath.Join(dir, "apagado"), UpdatedAt: old},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "423e4567-e89b-12d3-a456-426614174000"}, UpdatedAt: old},
		{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: codexID}, ProjectPath: e.project, UpdatedAt: old},
		{Key: domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: ocID}, ProjectPath: e.project, SourceKind: domain.SourceSQLite, UpdatedAt: old},
	} {
		path := s.SourcePath
		if path == "" {
			path = "/src/" + s.Key.NativeID
		}
		tx, err := e.store.BeginSource(context.Background(), s.Key.Provider, path, domain.SourceJSONL)
		if err != nil {
			t.Fatal(err)
		}
		tx.PutSession(context.Background(), s)
		tx.Commit(context.Background(), sqlite.SyncState{Status: "ok"})
	}

	cfg := config.Default()
	for id, p := range e.clis {
		pc := cfg.Providers[id]
		pc.CLIPath = p
		cfg.Providers[id] = pc
	}
	e.cfg = NewConfigStore(filepath.Join(dir, "config.json"), cfg, nil)
	providers := []provider.Provider{
		claude.New(claude.Options{Home: dir}), codex.New(codex.Options{Home: dir}), opencode.New(opencode.Options{Home: dir}),
	}
	e.deps = ResumeDeps{
		Index: e.store, Providers: providers, Config: e.cfg, Launcher: e.launcher, LookPath: look,
		Now: func() time.Time { return e.now }, Logger: discard, GOOS: "linux",
	}
	e.svc = NewResumeService(e.deps)
	return e
}

// Cada provider: executável, argumentos e diretório certos, via terminal.
func TestResumePerProvider(t *testing.T) {
	e := newResumeEnv(t)
	for _, c := range []struct {
		id   string
		args []string
	}{
		{"claude:" + claudeID, []string{"--resume", claudeID}},
		{"codex:" + codexID, []string{"resume", codexID}},
		{"opencode:" + ocID, []string{e.project, "--session", ocID}},
	} {
		plan, err := e.svc.Resume(context.Background(), c.id, false)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		last := e.launcher.calls[len(e.launcher.calls)-1]
		prov := domain.ProviderID(strings.SplitN(c.id, ":", 2)[0])
		if last.Path != e.clis[prov] || !slices.Equal(last.Args, c.args) || last.Dir != e.project {
			t.Errorf("%s: comando %+v", c.id, last)
		}
		if plan.Terminal != "ptyxis" || plan.Active || plan.Dir != e.project || !strings.HasPrefix(plan.Command, e.clis[prov]+" ") {
			t.Errorf("%s: plano %+v", c.id, plan)
		}
	}
}

func TestResumeRefusals(t *testing.T) {
	e := newResumeEnv(t)
	ctx := context.Background()
	for id, want := range map[string]error{
		"claude:" + claudeID + "/agent-a":             ErrNotResumable,
		"claude:423e4567-e89b-12d3-a456-426614174000": ErrNoProjectDir,
		"claude:nao-existe":                           ErrSessionNotFound,
		"lixo":                                        ErrInvalidKey,
	} {
		if _, err := e.svc.Resume(ctx, id, true); !errors.Is(err, want) {
			t.Errorf("%s: %v (quer %v)", id, err, want)
		}
	}
	if _, err := e.svc.Resume(ctx, "claude:323e4567-e89b-12d3-a456-426614174000", true); err == nil || !strings.Contains(err.Error(), "não existe mais") {
		t.Errorf("diretório apagado: %v", err)
	}
	// Executável configurado sumiu.
	os.Remove(e.clis[domain.ProviderCodex])
	if _, err := e.svc.Resume(ctx, "codex:"+codexID, true); !errors.Is(err, ErrCLINotFound) {
		t.Errorf("sem cli: %v", err)
	}
	// Terminal configurado inválido.
	e.cfg.Update(func(c *config.Config) error { c.Terminal = terminal.Config{Path: "/nao/existe"}; return nil })
	if _, err := e.svc.Resume(ctx, "claude:"+claudeID, true); !errors.Is(err, terminal.ErrInvalidTerminal) {
		t.Errorf("terminal: %v", err)
	}
	// Provider desabilitado (sem adaptador).
	off := NewResumeService(ResumeDeps{Index: e.store, Config: e.cfg, Launcher: e.launcher, Logger: discard})
	if _, err := off.Resume(ctx, "claude:"+claudeID, true); !errors.Is(err, ErrProviderDisabled) {
		t.Errorf("desabilitado: %v", err)
	}
	if len(e.launcher.calls) != 0 {
		t.Errorf("nada deveria ter sido aberto: %+v", e.launcher.calls)
	}
}

// Fonte alterada há pouco: pede confirmação antes de abrir.
func TestResumeActiveSessionNeedsConfirmation(t *testing.T) {
	e := newResumeEnv(t)
	ctx := context.Background()
	os.Chtimes(e.src, e.now.Add(-10*time.Second), e.now.Add(-10*time.Second))
	plan, err := e.svc.Resume(ctx, "claude:"+claudeID, false)
	if !errors.Is(err, ErrSessionActive) || !plan.Active || len(e.launcher.calls) != 0 {
		t.Fatalf("sem confirmação: %+v %v", plan, err)
	}
	if _, err := e.svc.Resume(ctx, "claude:"+claudeID, true); err != nil || len(e.launcher.calls) != 1 {
		t.Errorf("confirmada: %v", err)
	}
	// Fora da janela: não pergunta.
	e.now = e.now.Add(2 * time.Minute)
	if plan, err := e.svc.Plan(ctx, "claude:"+claudeID); err != nil || plan.Active {
		t.Errorf("antiga: %+v %v", plan, err)
	}
	// Falha ao abrir o terminal chega ao usuário.
	e.launcher.err = errors.New("exec: permission denied")
	if _, err := e.svc.Resume(ctx, "claude:"+claudeID, false); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("falha do terminal: %v", err)
	}
}

func TestDisplayCommand(t *testing.T) {
	got := displayCommand([]string{"/bin/claude", "--resume", "x y", "it's", "$(id)", ""})
	want := `/bin/claude --resume 'x y' 'it'\''s' '$(id)' ''`
	if got != want {
		t.Errorf("%s\n%s", got, want)
	}
}

func TestWorkDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)
	if got, err := WorkDir(dir + "/./"); err != nil || got != dir {
		t.Errorf("válido: %q %v", got, err)
	}
	for _, p := range []string{"", "rel/dir", file, filepath.Join(dir, "x")} {
		if _, err := WorkDir(p); err == nil {
			t.Errorf("%q aceito", p)
		}
	}
}

// Windows: CLI que é shim do npm (.cmd) só roda por um shell; a retomada
// vira "copiar o comando" e nada é executado.
func TestResumeWindowsCmdShimIsCopyOnly(t *testing.T) {
	e := newResumeEnv(t)
	shim := filepath.Join(filepath.Dir(e.clis[domain.ProviderClaude]), "claude.cmd")
	os.WriteFile(shim, nil, 0o755)
	e.cfg.Update(func(c *config.Config) error {
		pc := c.Providers[domain.ProviderClaude]
		pc.CLIPath = shim
		c.Providers[domain.ProviderClaude] = pc
		return nil
	})
	d := e.deps
	d.GOOS = "windows"
	svc := NewResumeService(d)
	id := "claude:" + claudeID
	plan, err := svc.Plan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CopyOnly || !strings.Contains(plan.CopyReason, "shell") || plan.Command == "" {
		t.Errorf("plano %+v", plan)
	}
	if _, err := svc.Resume(context.Background(), id, true); !errors.Is(err, ErrCopyOnly) {
		t.Errorf("Resume: %v", err)
	}
	if len(e.launcher.calls) != 0 {
		t.Errorf("launcher chamado: %+v", e.launcher.calls)
	}
}

// Windows com .exe: lança (console nova quando não há Windows Terminal).
func TestResumeWindowsExeLaunches(t *testing.T) {
	e := newResumeEnv(t)
	exe := filepath.Join(filepath.Dir(e.clis[domain.ProviderClaude]), "claude.exe")
	os.WriteFile(exe, nil, 0o755)
	e.cfg.Update(func(c *config.Config) error {
		pc := c.Providers[domain.ProviderClaude]
		pc.CLIPath = exe
		c.Providers[domain.ProviderClaude] = pc
		return nil
	})
	d := e.deps
	d.GOOS = "windows"
	plan, err := NewResumeService(d).Resume(context.Background(), "claude:"+claudeID, true)
	if err != nil || plan.CopyOnly || plan.Terminal != "console" {
		t.Fatalf("%+v %v", plan, err)
	}
	if len(e.launcher.terms) != 1 || !e.launcher.terms[0].Console {
		t.Errorf("terminal %+v", e.launcher.terms)
	}
}
