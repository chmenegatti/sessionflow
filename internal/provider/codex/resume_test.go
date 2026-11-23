package codex

import (
	"errors"
	"os"
	"path/filepath"
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
	bin := filepath.Join(dir, "bin")
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

func TestCodexResumeArgs(t *testing.T) {
	a := New(Options{RootDir: "/r"})
	id := "123e4567-e89b-12d3-a456-426614174000"
	got, err := a.ResumeArgs(domain.Session{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: id}, ProjectPath: "/p"})
	if err != nil || !slices.Equal(got, []string{"resume", id}) {
		t.Errorf("%q %v", got, err)
	}
	if w := a.WatchDirs(); len(w) != 2 || w[0].Path != "/r/sessions" || !w[0].Recursive || !w[1].Recursive {
		t.Errorf("watch: %+v", w)
	}
}
