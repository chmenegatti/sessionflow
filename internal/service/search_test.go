package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer"
	"sessionflow/internal/provider/claude"
	"sessionflow/internal/repository/sqlite"
)

func TestBuildMatch(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"migra", `"migra"*`},
		{"retomar  sessão", `"retomar"* "sessão"*`},
		{`"retomar   sessão" go`, `"retomar sessão" "go"`},
		{"a ab abc", `"a" "ab" "abc"*`},
		// Operadores e sintaxe do FTS5 viram texto.
		{"foo OR bar", `"foo"* "OR" "bar"*`},
		{"NOT x", `"NOT"* "x"`},
		{"col:val", `"col:val"*`},
		{"(a) b*", `"(a)" "b*"`},
		{"ação… sim", `"ação…"* "sim"*`},
		{"foo-ba", `"foo-ba"`},
		{"migrac\u0327a\u0303o", "\"migrac\u0327a\u0303o\"*"}, // NFD: marcas fazem parte do token
		{"NEAR(a b)", `"NEAR(a" "b)"`},
		// Aspa sem par: o resto vira palavras.
		{`abc "def ghi`, `"abc"* "def"* "ghi"*`},
		// Aspas dentro de palavra são escapadas.
		{`a"b`, `"a" "b"`},
		// Só pontuação: não há termo.
		{`*** "..." --`, ""},
		{`"" x`, `"x"`},
	} {
		if got := BuildMatch(c.in); got != c.want {
			t.Errorf("BuildMatch(%q) = %s; quer %s", c.in, got, c.want)
		}
	}
	many := strings.Repeat("palavra ", 40)
	if n := strings.Count(BuildMatch(many), "*"); n != maxQueryTerms {
		t.Errorf("termos = %d", n)
	}
}

type searchEnv struct {
	svc   *SearchService
	store *sqlite.IndexStore
	db    *sql.DB
}

func newSearchEnv(t *testing.T) *searchEnv {
	t.Helper()
	db, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := sqlite.NewIndexStore(db)
	return &searchEnv{svc: NewSearchService(store, discard), store: store, db: db}
}

type fakeSession struct {
	key       domain.SessionKey
	project   string
	parent    *domain.SessionKey
	automated bool
	updated   time.Time
	msgs      []string
}

// add grava a sessão com uma mensagem por texto (seq = índice); mensagens
// de índice ímpar são do agente.
func (e *searchEnv) add(t *testing.T, s fakeSession) {
	t.Helper()
	ctx := context.Background()
	tx, err := e.store.BeginSource(ctx, s.key.Provider, "/src/"+s.key.NativeID, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range s.msgs {
		role := domain.RoleUser
		if i%2 == 1 {
			role = domain.RoleAssistant
		}
		ts := s.updated.Add(time.Duration(i) * time.Second)
		if err := tx.AddMessage(ctx, s.key, sqlite.IndexedMessage{Seq: i, Role: role, Timestamp: &ts, SearchText: text}); err != nil {
			t.Fatal(err)
		}
	}
	sess := domain.Session{Key: s.key, ProjectPath: s.project, Parent: s.parent, Automated: s.automated, UpdatedAt: s.updated, Title: s.key.NativeID}
	if err := tx.PutSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx, sqlite.SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
}

func (e *searchEnv) search(t *testing.T, q SearchQuery) SearchResult {
	t.Helper()
	res, err := e.svc.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("Search(%+v): %v", q, err)
	}
	return res
}

// hitIDs: "sessão#seq" de cada resultado, na ordem.
func hitIDs(r SearchResult) string {
	var ids []string
	for _, h := range r.Hits {
		ids = append(ids, fmt.Sprintf("%s#%d", strings.TrimPrefix(h.SessionID, "claude:"), h.Seq))
	}
	return strings.Join(ids, ",")
}

func claudeKey(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderClaude, NativeID: id}
}

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestSearchRelevanceAndMatching(t *testing.T) {
	e := newSearchEnv(t)
	filler := strings.Repeat("texto de preenchimento sem relação ", 30)
	e.add(t, fakeSession{key: claudeKey("a"), project: "/p/app", updated: base, msgs: []string{
		"kubernetes " + filler,                    // a#0: termo uma vez num texto longo
		"Migração do repositório para o monorepo", // a#1
		"como retomar sessão antiga",              // a#2
		"sessão para retomar depois",              // a#3: mesmas palavras, outra ordem
	}})
	e.add(t, fakeSession{key: claudeKey("b"), project: "/p/app", updated: base.Add(time.Hour), msgs: []string{
		"kubernetes kubernetes no cluster kubernetes", // b#0: termo frequente, texto curto
		"ReadSession lê o arquivo",                    // b#1
	}})

	// bm25: texto curto com o termo repetido vem antes do longo.
	if got := hitIDs(e.search(t, SearchQuery{Text: "kubernetes"})); got != "b#0,a#0" {
		t.Errorf("relevância: %s", got)
	}
	// Sem acento encontra com acento; prefixo encontra a palavra inteira.
	for _, q := range []string{"migracao", "MIGRA", "repositório monorepo", "migrac\u0327a\u0303o"} {
		if got := hitIDs(e.search(t, SearchQuery{Text: q})); got != "a#1" {
			t.Errorf("%q: %s", q, got)
		}
	}
	// Palavras soltas: todas precisam aparecer, em qualquer ordem.
	if got := hitIDs(e.search(t, SearchQuery{Text: "retomar sessão"})); got != "a#2,a#3" && got != "a#3,a#2" {
		t.Errorf("palavras: %s", got)
	}
	// Frase: só a sequência exata.
	if got := hitIDs(e.search(t, SearchQuery{Text: `"retomar sessão"`})); got != "a#2" {
		t.Errorf("frase: %s", got)
	}
	if got := hitIDs(e.search(t, SearchQuery{Text: "kubernetes inexistente"})); got != "" {
		t.Errorf("AND: %s", got)
	}
	// Sintaxe do FTS digitada não quebra a busca.
	for _, q := range []string{`"`, "NOT", "a AND (b", "col:x", "*", `kubernetes"`} {
		if _, err := e.svc.Search(context.Background(), SearchQuery{Text: q}); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
}

func TestSearchSnippetIsPlainText(t *testing.T) {
	e := newSearchEnv(t)
	e.add(t, fakeSession{key: claudeKey("a"), updated: base, msgs: []string{
		"antes\n\n<script>alert(1)</script> o termo alvo aparece aqui\tdepois",
	}})
	h := e.search(t, SearchQuery{Text: "alvo"}).Hits[0]
	var text strings.Builder
	var matches []string
	for _, p := range h.Snippet {
		text.WriteString(p.Text)
		if p.Match {
			matches = append(matches, p.Text)
		}
	}
	if strings.Join(matches, "|") != "alvo" {
		t.Errorf("destaque: %+v", h.Snippet)
	}
	if got := text.String(); got != "antes <script>alert(1)</script> o termo alvo aparece aqui depois" {
		t.Errorf("trecho: %q", got)
	}
	if strings.ContainsAny(text.String(), sqlite.MarkOpen+sqlite.MarkClose) {
		t.Error("marcador vazou para o texto")
	}
	if h.Role != domain.RoleUser || h.Timestamp != base.UnixMilli() || h.Title != "a" {
		t.Errorf("metadados: %+v", h)
	}
}

func TestSearchFilters(t *testing.T) {
	e := newSearchEnv(t)
	parent := claudeKey("main")
	e.add(t, fakeSession{key: parent, project: "/p/app", updated: base, msgs: []string{"termo comum"}})
	e.add(t, fakeSession{key: claudeKey("sub"), parent: &parent, project: "/p/app", updated: base, msgs: []string{"termo comum"}})
	e.add(t, fakeSession{key: claudeKey("auto"), automated: true, project: "/p/app", updated: base, msgs: []string{"termo comum"}})
	e.add(t, fakeSession{key: claudeKey("lib"), project: "/p/lib", updated: base, msgs: []string{"termo comum"}})
	e.add(t, fakeSession{key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "cx"}, project: "/p/app", updated: base, msgs: []string{"termo comum"}})

	count := func(q SearchQuery) int {
		q.Text = "comum"
		r := e.search(t, q)
		if r.Total != len(r.Hits) {
			t.Errorf("%+v: total %d, hits %d", q, r.Total, len(r.Hits))
		}
		return r.Total
	}
	if n := count(SearchQuery{}); n != 3 {
		t.Errorf("visíveis: %d", n)
	}
	if n := count(SearchQuery{IncludeHidden: true}); n != 5 {
		t.Errorf("com ocultas: %d", n)
	}
	if n := count(SearchQuery{Project: "/p/app"}); n != 2 {
		t.Errorf("projeto: %d", n)
	}
	if n := count(SearchQuery{Providers: []domain.ProviderID{domain.ProviderCodex}}); n != 1 {
		t.Errorf("provider: %d", n)
	}
	if n := count(SearchQuery{Providers: []domain.ProviderID{domain.ProviderCodex}, Project: "/p/lib"}); n != 0 {
		t.Errorf("provider+projeto: %d", n)
	}
	r := e.search(t, SearchQuery{Text: "comum", IncludeHidden: true, Project: "/p/app"})
	for _, h := range r.Hits {
		if h.ProjectName != "app" {
			t.Errorf("projeto do resultado: %+v", h)
		}
		if strings.HasSuffix(h.SessionID, ":sub") != h.Subagent || strings.HasSuffix(h.SessionID, ":auto") != h.Automated {
			t.Errorf("marcas: %+v", h)
		}
	}
}

func TestSearchPagination(t *testing.T) {
	e := newSearchEnv(t)
	msgs := make([]string, 75)
	for i := range msgs {
		msgs[i] = "alfa beta" // empate no bm25: desempate por data, depois id
	}
	e.add(t, fakeSession{key: claudeKey("a"), updated: base, msgs: msgs})

	seen := map[string]bool{}
	var order []string
	for offset := 0; ; {
		r := e.search(t, SearchQuery{Text: "alfa", Limit: 30, Offset: offset})
		if r.Total != 75 || r.TotalCapped {
			t.Fatalf("total %d capped %v", r.Total, r.TotalCapped)
		}
		for _, h := range r.Hits {
			id := fmt.Sprint(h.Seq)
			if seen[id] {
				t.Fatalf("repetido entre páginas: %s", id)
			}
			seen[id] = true
			order = append(order, id)
		}
		offset += len(r.Hits)
		if !r.HasMore {
			break
		}
		if len(r.Hits) != 30 {
			t.Fatalf("página incompleta antes do fim: %d", len(r.Hits))
		}
	}
	if len(seen) != 75 {
		t.Errorf("união das páginas: %d", len(seen))
	}
	// Mesma ordem a cada chamada; empate resolvido pela mais recente.
	if order[0] != "74" || order[74] != "0" {
		t.Errorf("ordem: %s … %s", order[0], order[74])
	}
	if r := e.search(t, SearchQuery{Text: "alfa", Offset: 500}); len(r.Hits) != 0 || r.HasMore {
		t.Errorf("além do fim: %+v", r)
	}
}

func TestSearchTotalIsCapped(t *testing.T) {
	e := newSearchEnv(t)
	msgs := make([]string, searchCountCap+5)
	for i := range msgs {
		msgs[i] = "gama"
	}
	e.add(t, fakeSession{key: claudeKey("a"), updated: base, msgs: msgs})
	r := e.search(t, SearchQuery{Text: "gama"})
	if r.Total != searchCountCap || !r.TotalCapped || !r.HasMore || len(r.Hits) != searchPageSize {
		t.Errorf("total %d capped %v more %v hits %d", r.Total, r.TotalCapped, r.HasMore, len(r.Hits))
	}
	if r := e.search(t, SearchQuery{Text: "gama", Limit: 10_000}); len(r.Hits) != maxSearchPage {
		t.Errorf("limite máximo: %d", len(r.Hits))
	}
}

func TestSearchErrors(t *testing.T) {
	e := newSearchEnv(t)
	ctx := context.Background()
	if r, err := e.svc.Search(ctx, SearchQuery{Text: "  "}); err != nil || r.Hits == nil || len(r.Hits) != 0 {
		t.Errorf("vazia: %+v %v", r, err)
	}
	if _, err := e.svc.Search(ctx, SearchQuery{Text: strings.Repeat("a", maxQueryRunes+1)}); !errors.Is(err, ErrQueryTooLong) {
		t.Errorf("longa: %v", err)
	}
	if _, err := e.svc.Search(ctx, SearchQuery{Text: "x", Providers: []domain.ProviderID{"nope"}}); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("provider: %v", err)
	}
	// Busca substituída (frontend cancelou): cancelamento, não falha.
	e.add(t, fakeSession{key: claudeKey("a"), updated: base, msgs: []string{"x"}})
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.svc.Search(cctx, SearchQuery{Text: "x"}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelada: %v", err)
	}
	e.db.Close()
	if _, err := e.svc.Search(ctx, SearchQuery{Text: "x"}); !errors.Is(err, ErrSearchFailed) {
		t.Errorf("banco fechado: %v", err)
	}
}

// Consistência do índice com as fontes: alteração, acréscimo e exclusão de
// arquivos, passando pelo indexador real.
func TestSearchIndexFollowsSourceChanges(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "-p")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, bigID+".jsonl")
	line := func(text string) string {
		return `{"type":"user","timestamp":"2026-01-01T00:00:00Z","cwd":"/p","message":{"content":"` + text + `"}}` + "\n"
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	e := newSearchEnv(t)
	ix := indexer.New(e.store, discard, claude.New(claude.Options{RootDir: root}))
	run := func() {
		t.Helper()
		ix.Run(context.Background(), indexer.RunOptions{})
		// O FTS confere com a tabela de conteúdo (rank 1 = checa o conteúdo).
		if _, err := e.db.Exec("INSERT INTO messages_fts(messages_fts, rank) VALUES('integrity-check', 1)"); err != nil {
			t.Fatalf("integridade do FTS: %v", err)
		}
	}
	hits := func(q string) int { return len(e.search(t, SearchQuery{Text: q}).Hits) }

	write(line("banana madura") + line("outra mensagem"))
	run()
	if hits("banana") != 1 || hits("outra") != 1 {
		t.Fatalf("indexação inicial: banana=%d outra=%d", hits("banana"), hits("outra"))
	}

	// Arquivo reescrito: o texto antigo sai do índice.
	write(line("laranja lima") + line("outra mensagem alterada"))
	run()
	if hits("banana") != 0 || hits("laranja") != 1 || hits("alterada") != 1 {
		t.Errorf("reescrita: banana=%d laranja=%d", hits("banana"), hits("laranja"))
	}

	// Acréscimo no fim (sessão continuada): antigo e novo aparecem.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(line("abacaxi novo"))
	f.Close()
	run()
	if hits("laranja") != 1 || hits("abacaxi") != 1 {
		t.Errorf("acréscimo: laranja=%d abacaxi=%d", hits("laranja"), hits("abacaxi"))
	}
	r := e.search(t, SearchQuery{Text: "abacaxi"})
	if r.Hits[0].Seq != 2 || r.Hits[0].SessionID != "claude:"+bigID {
		t.Errorf("navegação: %+v", r.Hits[0])
	}

	// Arquivo apagado: nenhum resultado da sessão.
	os.Remove(path)
	run()
	if hits("laranja") != 0 || hits("abacaxi") != 0 || hits("outra") != 0 {
		t.Error("sessão apagada ainda aparece na busca")
	}
	var n int
	e.db.QueryRow("SELECT count(*) FROM messages").Scan(&n)
	if n != 0 {
		t.Errorf("mensagens órfãs: %d", n)
	}
}
