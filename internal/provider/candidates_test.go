package provider

import (
	"slices"
	"testing"

	"sessionflow/internal/domain"
)

// Caminhos conhecidos das CLIs por sistema (docs/PROVIDERS.md, 0.2.0).
func TestCLICandidatesByOS(t *testing.T) {
	for _, c := range []struct {
		goos, home string
		p          domain.ProviderID
		want       []string
	}{
		{"linux", "/home/u", domain.ProviderClaude, []string{"/home/u/.local/bin/claude", "/home/u/.claude/local/claude"}},
		{"linux", "/home/u", domain.ProviderCodex, []string{"/home/u/.local/bin/codex"}},
		{"linux", "/home/u", domain.ProviderOpenCode, []string{"/home/u/.opencode/bin/opencode", "/home/u/.local/bin/opencode"}},
		{"darwin", "/Users/u", domain.ProviderClaude, []string{"/Users/u/.local/bin/claude", "/Users/u/.claude/local/claude"}},
		{"darwin", "/Users/u", domain.ProviderCodex, []string{"/Users/u/.local/bin/codex", "/opt/homebrew/bin/codex", "/usr/local/bin/codex"}},
		{"darwin", "/Users/u", domain.ProviderOpenCode, []string{"/Users/u/.opencode/bin/opencode", "/Users/u/.local/bin/opencode", "/opt/homebrew/bin/opencode"}},
		{"windows", `C:\Users\José Silva`, domain.ProviderClaude, []string{`C:\Users\José Silva\.local\bin\claude.exe`}},
		{"windows", `C:\Users\u`, domain.ProviderCodex, nil},
		{"windows", `C:\Users\u`, domain.ProviderOpenCode, []string{`C:\Users\u\.opencode\bin\opencode.exe`}},
	} {
		if got := CLICandidates(c.goos, c.p, c.home); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: got %q quer %q", c.goos, c.p, got, c.want)
		}
	}
}

// No Windows a CLI pode ser .exe ou script do npm (.cmd/.bat/.ps1): é
// encontrada, e a retomada decide se abre (só .exe) ou oferece copiar.
func TestCLINameWindows(t *testing.T) {
	for name, want := range map[string]bool{
		`C:\x\claude.exe`: true, `C:\x\CLAUDE.EXE`: true, `C:\x\codex.cmd`: true, `C:\x\codex.ps1`: true, `C:\x\codex.bat`: true,
		`C:\x\codex`: false, `C:\x\codex.txt`: false,
	} {
		if got := cliName("windows", name); got != want {
			t.Errorf("%s: %v, quer %v", name, got, want)
		}
	}
	if !cliName("linux", "/usr/bin/codex") {
		t.Error("linux: nome sem extensão recusado")
	}
}
