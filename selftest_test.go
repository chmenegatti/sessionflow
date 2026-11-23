package main

import (
	"bytes"
	"strings"
	"testing"
	"testing/fstest"
)

// --selftest: abre os bancos num diretório temporário, indexa os fixtures
// embutidos e confere uma sessão de cada agente, sem janela e sem tocar no
// home real.
func TestSelftestOK(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // nada pode ir para o home real
	var out bytes.Buffer
	if code := selftest(&out, selftestFixtures); code != 0 {
		t.Fatalf("código %d:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "SELFTEST OK") {
		t.Errorf("saída:\n%s", out.String())
	}
}

func TestSelftestReportsFailure(t *testing.T) {
	var out bytes.Buffer
	if code := selftest(&out, fstest.MapFS{}); code != 1 {
		t.Fatalf("código %d com fixtures vazios:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "SELFTEST FALHOU") || !strings.Contains(out.String(), "fixtures") {
		t.Errorf("saída sem a etapa:\n%s", out.String())
	}
}
