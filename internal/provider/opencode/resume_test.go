package opencode

import (
	"errors"
	"os"
	"path/filepath"
	"sessionflow/internal/provider/providertest"
	"slices"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider"
)

// IDs que virariam opção ou caminho na linha de comando são recusados.
var badIDs = []string{"", "--help", "-c", "../x", "a b", "123e4567-e89b-12d3-a456-42661417400", "ses_ab;rm", "ses_", "ses_abc"}

func TestResumeArgsRejectsBadIDs(t *testing.T) {
	a := New(Options{})
	for _, id := range badIDs {
		s := domain.Session{Key: domain.SessionKey{Provider: a.ID(), NativeID: id}, ProjectPath: "/p"}
		if _, err := a.ResumeArgs(s); !errors.Is(err, provider.ErrInvalidSessionID) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestCLIConfiguredMustBeExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := providertest.Exe(dir, "bin")
	plain := filepath.Join(dir, "plain")
	os.WriteFile(bin, nil, 0o755)
	os.WriteFile(plain, nil, 0o644)
	a := New(Options{Home: dir})
	if got := a.CLI(bin); got != bin {
		t.Errorf("configurado: %q", got)
	}
	if got := a.CLI(plain); got != "" {
		t.Errorf("não executável: %q", got)
	}
}

func TestOpenCodeResumeArgs(t *testing.T) {
	a := New(Options{RootDir: providertest.Abs("r"), DBOverride: providertest.Abs("outro", "oc.db")})
	key := domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: "ses_3a1f9c2b7e0d4AbCdEf123456"}
	app := providertest.Abs("p", "app")
	got, err := a.ResumeArgs(domain.Session{Key: key, ProjectPath: app})
	if err != nil || !slices.Equal(got, []string{app, "--session", key.NativeID}) {
		t.Errorf("%q %v", got, err)
	}
	// Sem diretório (ou relativo), o posicional poderia virar opção.
	for _, p := range []string{"", "-x", "rel"} {
		if _, err := a.ResumeArgs(domain.Session{Key: key, ProjectPath: p}); err == nil {
			t.Errorf("projeto %q aceito", p)
		}
	}
	w := a.WatchDirs()
	if len(w) != 2 || w[0].Path != providertest.Abs("r") || w[1].Path != providertest.Abs("outro") || w[0].Recursive || w[1].Recursive {
		t.Errorf("watch: %+v", w)
	}
}
