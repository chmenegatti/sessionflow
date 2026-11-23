package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/repository/sqlite"
)

const bigID = "33333333-3333-4333-8333-333333333333"

type viewerEnv struct {
	svc    *TranscriptService
	store  *sqlite.IndexStore
	path   string
	claude provider.Provider
}

// newViewer indexa um transcript Claude com n prompts (+ respostas) e
// devolve o serviço.
func newViewer(t *testing.T, n int, line func(i int) string) *viewerEnv {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "projects", "-p", bigID+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0o755)
	var sb strings.Builder
	for i := range n {
		sb.WriteString(line(i))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	store := sqlite.NewIndexStore(idx)
	cl := claude.New(claude.Options{RootDir: root})
	indexer.New(store, discard, cl).Run(context.Background(), indexer.RunOptions{})
	svc := NewTranscriptService(store, []provider.Provider{cl}, discard)
	t.Cleanup(func() { svc.ServiceShutdown() })
	return &viewerEnv{svc: svc, store: store, path: path, claude: cl}
}

func userLine(i int) string {
	return fmt.Sprintf(`{"type":"user","timestamp":"2026-01-01T00:00:%02dZ","cwd":"/p","message":{"content":"prompt %d"}}`, i%60, i)
}

func readAll(t *testing.T, svc *TranscriptService, id string) ([]TranscriptPage, []MessageView) {
	t.Helper()
	page, err := svc.Open(context.Background(), "claude:"+id)
	if err != nil {
		t.Fatal(err)
	}
	pages := []TranscriptPage{page}
	for !page.Done {
		if page, err = svc.Next(context.Background(), page.Cursor); err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
	}
	var all []MessageView
	for _, p := range pages {
		all = append(all, p.Messages...)
	}
	return pages, all
}

func TestTranscriptPagesInOrder(t *testing.T) {
	v := newViewer(t, 250, userLine)
	pages, all := readAll(t, v.svc, bigID)
	// 100 + 100 + 50 + página final vazia (Done).
	if len(pages) != 4 || len(pages[0].Messages) != 100 || len(pages[2].Messages) != 50 || !pages[3].Done {
		t.Fatalf("páginas: %d (%d, %d)", len(pages), len(pages[0].Messages), len(pages[2].Messages))
	}
	for i, m := range all {
		if m.Seq != i || m.Role != domain.RoleUser || m.Blocks[0].Text != fmt.Sprintf("prompt %d", i) || m.Timestamp == 0 {
			t.Fatalf("mensagem %d: %+v", i, m)
		}
	}
	if len(v.svc.cursors) != 0 {
		t.Errorf("cursor não liberado ao terminar: %d", len(v.svc.cursors))
	}
}

// Blocos grandes são cortados e a página fecha por tamanho.
func TestTranscriptTruncatesAndSplitsBySize(t *testing.T) {
	big := strings.Repeat("x", 100<<10)
	v := newViewer(t, 12, func(i int) string {
		return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"` + big + `"}]}}`
	})
	pages, all := readAll(t, v.svc, bigID)
	b := all[0].Blocks[0]
	if len(b.Text) != maxBlockText || b.Truncated != len(big)-maxBlockText || b.Kind != domain.BlockToolResult {
		t.Errorf("corte: len=%d truncated=%d", len(b.Text), b.Truncated)
	}
	if len(all) != 12 || len(pages[0].Messages) >= 12 {
		t.Errorf("página deveria fechar por tamanho: %d mensagens na 1ª", len(pages[0].Messages))
	}
}

func TestTranscriptMetadataKeepsOriginal(t *testing.T) {
	v := newViewer(t, 2, func(i int) string {
		if i == 0 {
			return `{"type":"attachment","attachment":{"type":"environment","os":"linux"}}`
		}
		return `{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`
	})
	_, all := readAll(t, v.svc, bigID)
	meta, call := all[0].Blocks[0], all[1].Blocks[0]
	if meta.Kind != domain.BlockMetadata || !strings.Contains(meta.Raw, `"os": "linux"`) {
		t.Errorf("metadado sem original: %+v", meta)
	}
	if call.ToolInput != "{\n  \"command\": \"ls\"\n}" {
		t.Errorf("entrada da ferramenta: %q", call.ToolInput)
	}
}

func TestTranscriptErrors(t *testing.T) {
	v := newViewer(t, 1, userLine)
	ctx := context.Background()
	if _, err := v.svc.Open(ctx, "claude:nao-existe"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("ausente: %v", err)
	}
	if _, err := v.svc.Open(ctx, "x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("chave: %v", err)
	}
	if _, err := v.svc.Next(ctx, "cursor-inventado"); !errors.Is(err, ErrCursorExpired) {
		t.Errorf("cursor: %v", err)
	}
	off := NewTranscriptService(v.store, nil, discard)
	if _, err := off.Open(ctx, "claude:"+bigID); !errors.Is(err, ErrProviderDisabled) {
		t.Errorf("desabilitado: %v", err)
	}
	os.Remove(v.path)
	if _, err := v.svc.Open(ctx, "claude:"+bigID); !errors.Is(err, ErrSourceMissing) {
		t.Errorf("fonte apagada: %v", err)
	}
}

func TestTranscriptCloseAndEviction(t *testing.T) {
	v := newViewer(t, 250, userLine)
	ctx := context.Background()
	first, _ := v.svc.Open(ctx, "claude:"+bigID)
	v.svc.Close(first.Cursor)
	if _, err := v.svc.Next(ctx, first.Cursor); !errors.Is(err, ErrCursorExpired) {
		t.Errorf("após Close: %v", err)
	}

	var cursors []string
	for range maxCursors + 1 {
		p, err := v.svc.Open(ctx, "claude:"+bigID)
		if err != nil {
			t.Fatal(err)
		}
		cursors = append(cursors, p.Cursor)
	}
	if len(v.svc.cursors) != maxCursors {
		t.Errorf("cursores abertos = %d", len(v.svc.cursors))
	}
	// O mais antigo foi despejado: a conversa não pode parecer concluída.
	if _, err := v.svc.Next(ctx, cursors[0]); !errors.Is(err, ErrCursorExpired) {
		t.Errorf("despejado: %v", err)
	}
	if _, err := v.svc.Next(ctx, cursors[maxCursors]); err != nil {
		t.Errorf("mais novo: %v", err)
	}

	done := make(chan struct{})
	go func() { v.svc.ServiceShutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown não encerrou as leituras")
	}
}

type panicProvider struct{ provider.Provider }

func (panicProvider) Parse(context.Context, provider.Source, provider.Sink) (provider.ParseStats, error) {
	panic("conteúdo secreto")
}

func TestTranscriptProviderPanic(t *testing.T) {
	v := newViewer(t, 1, userLine)
	svc := NewTranscriptService(v.store, []provider.Provider{panicProvider{v.claude}}, discard)
	_, err := svc.Open(context.Background(), "claude:"+bigID)
	if !errors.Is(err, errReadFailed) || strings.Contains(err.Error(), "secreto") {
		t.Errorf("panic: %v", err)
	}
}

// Open que falha depois de criar o cursor (aqui: tempo da chamada esgota
// antes da 1ª página) não pode deixar cursor órfão: o frontend não recebe o
// ID para fechá-lo.
func TestTranscriptOpenFailureReleasesCursor(t *testing.T) {
	v := newViewer(t, 1, userLine)
	stuck := stuckProvider{Provider: v.claude, release: make(chan struct{})}
	defer close(stuck.release)
	svc := NewTranscriptService(v.store, []provider.Provider{stuck}, discard)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.Open(ctx, "claude:"+bigID); err == nil {
		t.Fatal("esperava erro")
	}
	svc.mu.Lock()
	n := len(svc.cursors)
	svc.mu.Unlock()
	if n != 0 {
		t.Errorf("cursores órfãos: %d", n)
	}
}

// stuckProvider ignora o cancelamento (I/O preso).
type stuckProvider struct {
	provider.Provider
	release chan struct{}
}

func (s stuckProvider) Parse(_ context.Context, _ provider.Source, sink provider.Sink) (provider.ParseStats, error) {
	<-s.release
	return provider.ParseStats{}, nil
}

func TestTranscriptShutdownIsBounded(t *testing.T) {
	v := newViewer(t, 1, userLine)
	stuck := stuckProvider{Provider: v.claude, release: make(chan struct{})}
	defer close(stuck.release)
	svc := NewTranscriptService(v.store, []provider.Provider{stuck}, discard)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	svc.Open(ctx, "claude:"+bigID) // fica sem página: o adaptador não responde
	start := time.Now()
	svc.mu.Lock()
	svc.cursors["preso"] = &cursor{id: "preso", cancel: func() {}, finished: make(chan struct{})}
	svc.mu.Unlock()
	svc.ServiceShutdown()
	if d := time.Since(start); d > shutdownWait+time.Second {
		t.Errorf("shutdown levou %v", d)
	}
}

// OpenAt (resultado de busca) começa na mensagem pedida, sem enviar as
// anteriores.
func TestTranscriptOpenAt(t *testing.T) {
	v := newViewer(t, 250, userLine)
	ctx := context.Background()
	page, err := v.svc.OpenAt(ctx, "claude:"+bigID, 180)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 70 || page.Messages[0].Seq != 180 || page.Messages[69].Seq != 249 {
		t.Fatalf("página: %d mensagens a partir de %d", len(page.Messages), page.Messages[0].Seq)
	}
	if last, _ := v.svc.Next(ctx, page.Cursor); !last.Done {
		t.Error("esperava fim")
	}
	// Alvo além do fim (fonte encolheu): conversa vazia, sem erro.
	if page, err := v.svc.OpenAt(ctx, "claude:"+bigID, 999); err != nil || len(page.Messages) != 0 || !page.Done {
		t.Errorf("além do fim: %+v %v", page, err)
	}
}
