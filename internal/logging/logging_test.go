package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileIsJSONAndRespectsLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "app.log")
	log, closer, err := New(Options{Level: slog.LevelInfo, FilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("invisível")
	log.Info("scan concluído", "provider", "claude", "sessions", 3)
	closer.Close()

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("esperava 1 linha (debug filtrado), veio %d: %s", len(lines), data)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("não é JSON: %v", err)
	}
	if rec["msg"] != "scan concluído" || rec["provider"] != "claude" || rec["sessions"] != float64(3) {
		t.Errorf("registro: %v", rec)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissão %v", info.Mode().Perm())
	}
}

func TestRedactsConversationContent(t *testing.T) {
	var buf bytes.Buffer
	log, _, _ := New(Options{Level: slog.LevelDebug, Console: &buf})
	log.Error("falha ao parsear linha",
		"path", "/x/s.jsonl",
		"content", "SEGREDO-1",
		slog.Group("line", "text", "SEGREDO-2", "n", 7),
	)
	out := buf.String()
	if strings.Contains(out, "SEGREDO") {
		t.Fatalf("conteúdo vazou: %s", out)
	}
	if !strings.Contains(out, "/x/s.jsonl") || !strings.Contains(out, "line.n=7") {
		t.Errorf("metadado legítimo sumiu: %s", out)
	}
}

func TestRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	os.WriteFile(path, bytes.Repeat([]byte("x"), MaxFileSize), 0o600)
	_, closer, err := New(Options{FilePath: path})
	if err != nil {
		t.Fatal(err)
	}
	closer.Close()
	if info, _ := os.Stat(path + ".1"); info == nil || info.Size() != MaxFileSize {
		t.Fatal("arquivo antigo não rotacionado")
	}
	if info, _ := os.Stat(path); info.Size() != 0 {
		t.Fatal("arquivo novo deveria estar vazio")
	}
}

func TestNoOutputsDiscards(t *testing.T) {
	log, closer, err := New(Options{})
	if err != nil || log == nil || closer.Close() != nil {
		t.Fatal("logger sem saída deveria funcionar")
	}
	log.Info("nada")
}

func TestWithMinLevel(t *testing.T) {
	var buf bytes.Buffer
	base, _, _ := New(Options{Level: slog.LevelDebug, Console: &buf})
	wails := WithMinLevel(base, slog.LevelWarn).With("component", "wails")
	wails.Debug("Binding call complete:", "result", "SEGREDO")
	wails.Info("asset")
	wails.Warn("aviso real")
	base.Debug("app debug continua")
	out := buf.String()
	if strings.Contains(out, "SEGREDO") || strings.Contains(out, "asset") {
		t.Fatalf("logger do Wails deveria filtrar abaixo de WARN: %s", out)
	}
	if !strings.Contains(out, "aviso real") || !strings.Contains(out, "component=wails") || !strings.Contains(out, "app debug continua") {
		t.Fatalf("saída inesperada: %s", out)
	}
}

func TestRedactsBindingPayloads(t *testing.T) {
	var buf bytes.Buffer
	log, _, _ := New(Options{Level: slog.LevelDebug, Console: &buf})
	log.Debug("Binding call complete:", "method", "X.Status", "args", "[SEGREDO]", "result", "{SEGREDO}")
	if strings.Contains(buf.String(), "SEGREDO") || !strings.Contains(buf.String(), "X.Status") {
		t.Fatalf("got %s", buf.String())
	}
}
