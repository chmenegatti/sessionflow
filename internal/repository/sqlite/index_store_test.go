package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

func newStore(t *testing.T) (*IndexStore, func(q string, args ...any) int) {
	t.Helper()
	db, _, err := OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewIndexStore(db), func(q string, args ...any) int { return count(t, db, q, args...) }
}

func key(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderCodex, NativeID: id}
}

// writeSource grava uma sessão com uma mensagem a partir de path.
func writeSource(t *testing.T, s *IndexStore, path, id, project, text string) *SourceTx {
	t.Helper()
	ctx := context.Background()
	tx, err := s.BeginSource(ctx, domain.ProviderCodex, path, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := tx.AddMessage(ctx, key(id), IndexedMessage{Seq: 0, Role: domain.RoleUser, Timestamp: &now, SearchText: text}); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutSession(ctx, domain.Session{Key: key(id), ProjectPath: project, Title: "t", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx, SyncState{Size: 1, Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestSourceTxReplacesAndDeduplicates(t *testing.T) {
	s, n := newStore(t)
	writeSource(t, s, "/a.jsonl", "s1", "/proj/x", "primeira versão")
	writeSource(t, s, "/a.jsonl", "s1", "/proj/y", "segunda versão") // fonte relida
	if n("SELECT count(*) FROM sessions") != 1 || n("SELECT count(*) FROM messages") != 1 {
		t.Fatal("releitura duplicou dados")
	}
	if n("SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'primeira'") != 0 {
		t.Error("texto antigo ficou no FTS")
	}
	if n("SELECT count(*) FROM projects") != 1 || n("SELECT count(*) FROM projects WHERE path = '/proj/y'") != 1 {
		t.Error("projeto órfão não removido")
	}

	// Mesma sessão vinda de outra fonte: ignorada e contada.
	tx := writeSource(t, s, "/b.jsonl", "s1", "/proj/z", "cópia")
	if tx.Duplicates != 1 || n("SELECT count(*) FROM sessions") != 1 || n("SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'cópia'") != 0 {
		t.Errorf("duplicata: %d", tx.Duplicates)
	}
}

func TestRollbackKeepsPreviousRead(t *testing.T) {
	s, n := newStore(t)
	writeSource(t, s, "/a.jsonl", "s1", "/p", "boa")
	tx, err := s.BeginSource(context.Background(), domain.ProviderCodex, "/a.jsonl", domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if n("SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'boa'") != 1 {
		t.Error("rollback apagou a leitura anterior")
	}
	if err := s.SetSyncState(context.Background(), domain.ProviderCodex, "/a.jsonl", SyncState{Status: "error", Error: "x"}); err != nil {
		t.Fatal(err)
	}
	st, _ := s.SyncStates(context.Background(), domain.ProviderCodex)
	if st["/a.jsonl"].Status != "error" || n("SELECT count(*) FROM sessions") != 1 {
		t.Errorf("estado: %+v", st)
	}
}

func TestRemoveSourcesExcept(t *testing.T) {
	s, n := newStore(t)
	writeSource(t, s, "/a.jsonl", "s1", "/p1", "a")
	writeSource(t, s, "/b.jsonl", "s2", "/p2", "b")
	removed, err := s.RemoveSourcesExcept(context.Background(), domain.ProviderCodex, []string{"/b.jsonl"})
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	if n("SELECT count(*) FROM sessions") != 1 || n("SELECT count(*) FROM sync_state") != 1 || n("SELECT count(*) FROM projects") != 1 {
		t.Error("restos da fonte removida")
	}
	counts, _ := s.CountSessions(context.Background())
	if counts[domain.ProviderCodex] != 1 || counts[domain.ProviderClaude] != 0 {
		t.Errorf("counts = %v", counts)
	}
	// Outro provider não é afetado.
	if removed, _ := s.RemoveSourcesExcept(context.Background(), domain.ProviderClaude, nil); removed != 0 {
		t.Error("removeu sessões de outro provider")
	}
}

func TestSessionCountsSeparateHidden(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	tx, _ := s.BeginSource(ctx, domain.ProviderCodex, "/a", domain.SourceJSONL)
	parent := key("p")
	for _, sess := range []domain.Session{
		{Key: parent, ProjectPath: "/v"},
		{Key: key("auto"), ProjectPath: "/h", Automated: true},
		{Key: key("child"), ProjectPath: "/h", Parent: &parent},
	} {
		if err := tx.PutSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit(ctx, SyncState{Status: "ok"})
	counts, err := s.SessionCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c := counts[domain.ProviderCodex]; c.Visible != 1 || c.Hidden != 2 {
		t.Errorf("counts = %+v", c)
	}
	if n, _ := s.VisibleProjects(ctx); n != 1 {
		t.Errorf("projetos visíveis = %d", n)
	}
}

func TestAddUsageNullsAndIdempotent(t *testing.T) {
	s, q := newStore(t)
	ctx := context.Background()
	tx, err := s.BeginSource(ctx, domain.ProviderCodex, "/u.jsonl", domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	in, out := int64(10), int64(20)
	now := time.Now()
	u := domain.Usage{ResponseID: "r1", Timestamp: &now, Model: "m", Input: &in, Output: &out, Total: ptr(int64(30)),
		Derived: []string{"input", "total"}, Status: domain.UsageStatusComplete}
	if err := tx.AddUsage(ctx, key("s1"), u); err != nil {
		t.Fatal(err)
	}
	if err := tx.AddUsage(ctx, key("s1"), u); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutSession(ctx, domain.Session{Key: key("s1"), Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx, SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if n := q("SELECT count(*) FROM usage"); n != 1 {
		t.Fatalf("%d registros", n)
	}
	if n := q("SELECT count(*) FROM usage WHERE reasoning_tokens IS NULL AND cache_read_tokens IS NULL AND input_tokens = 10 AND derived = 'input,total' AND status = 'complete'"); n != 1 {
		t.Error("NULL/derived/status errados")
	}
	// Reler a fonte apaga os registros (cascata).
	tx3, _ := s.BeginSource(ctx, domain.ProviderCodex, "/u.jsonl", domain.SourceJSONL)
	if err := tx3.Commit(ctx, SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	if n := q("SELECT count(*) FROM usage"); n != 0 {
		t.Errorf("%d registros após reler", n)
	}
}

func ptr[T any](v T) *T { return &v }
