package claude

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sessionflow/internal/provider/providertest"
)

// Histórico do Windows: CRLF no fim das linhas e cwd com espaço e acento
// (docs/discovery/fixtures/windows).
func TestParseWindowsFixture(t *testing.T) {
	path := filepath.Join(providertest.Fixture("windows", "claude"), "C--Users-Jos--Silva-proj", "66666666-6666-4666-8666-666666666666.jsonl")
	sink, stats := parse(t, New(Options{}), path)
	if stats.BadLines != 0 {
		t.Fatalf("linhas CRLF rejeitadas: %+v", stats)
	}
	s := sink.Sessions[0]
	for _, m := range sink.Messages[s.Key] {
		for _, b := range m.Blocks {
			if strings.HasSuffix(b.Text, "\r") {
				t.Errorf("texto com \\r: %q", b.Text)
			}
		}
	}
	if len(sink.Messages[s.Key]) != 2 {
		t.Errorf("%d mensagens, esperado 2", len(sink.Messages[s.Key]))
	}
	// O caminho só é absoluto no próprio Windows.
	if runtime.GOOS == "windows" && s.ProjectPath != `C:\Users\José Silva\proj` {
		t.Errorf("projeto %q", s.ProjectPath)
	}
}
