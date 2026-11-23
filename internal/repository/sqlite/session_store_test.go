package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

// seed grava sessões variadas, uma fonte por sessão.
func seed(t *testing.T, s *IndexStore) {
	t.Helper()
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parent := domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1"}
	sessions := []domain.Session{
		{Key: parent, ProjectPath: "/p/app", Title: "Claude 1", UpdatedAt: base.Add(3 * time.Hour)},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1/agent-a"}, Parent: &parent, ProjectPath: "/p/app", UpdatedAt: base.Add(4 * time.Hour)},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "auto"}, Automated: true, ProjectPath: "/p/obs", UpdatedAt: base.Add(5 * time.Hour)},
		{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "x1"}, ProjectPath: "/p/app", Title: "Codex", UpdatedAt: base.Add(time.Hour), Archived: true},
		{Key: domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: "o1"}, ProjectPath: "/p/lib", Title: "OpenCode", UpdatedAt: base.Add(2 * time.Hour)},
		{Key: domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: "o2"}, ProjectPath: "/p/lib", Title: "Sem data"},
	}
	for _, sess := range sessions {
		tx, err := s.BeginSource(ctx, sess.Key.Provider, "/src/"+sess.Key.NativeID, domain.SourceJSONL)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx, SyncState{Status: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
}

func ids(ss []domain.Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Key.String())
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListSessionsFiltersAndOrder(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	cases := []struct {
		name  string
		f     SessionFilter
		want  []string
		total int
	}{
		{"visíveis por atividade; sem data por último", SessionFilter{Limit: 10},
			[]string{"claude:c1", "opencode:o1", "codex:x1", "opencode:o2"}, 4},
		{"mais antigas primeiro", SessionFilter{Limit: 10, Oldest: true},
			[]string{"codex:x1", "opencode:o1", "claude:c1", "opencode:o2"}, 4},
		{"provider", SessionFilter{Limit: 10, Providers: []domain.ProviderID{domain.ProviderOpenCode, domain.ProviderCodex}},
			[]string{"opencode:o1", "codex:x1", "opencode:o2"}, 3},
		{"projeto", SessionFilter{Limit: 10, ProjectPath: "/p/app"}, []string{"claude:c1", "codex:x1"}, 2},
		{"ocultas incluídas", SessionFilter{Limit: 2, IncludeHidden: true}, []string{"claude:auto", "claude:c1/agent-a"}, 6},
		{"paginação", SessionFilter{Limit: 2, Offset: 2}, []string{"codex:x1", "opencode:o2"}, 4},
		{"chaves", SessionFilter{Limit: 10, Keys: []string{"codex:x1", "claude:auto", "nada:x"}, IncludeHidden: true},
			[]string{"claude:auto", "codex:x1"}, 2},
		{"chaves vazias", SessionFilter{Limit: 10, Keys: []string{}}, nil, 0},
	}
	for _, c := range cases {
		got, total, err := s.ListSessions(ctx, c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !equal(ids(got), c.want) || total != c.total {
			t.Errorf("%s: got %v (%d), want %v (%d)", c.name, ids(got), total, c.want, c.total)
		}
	}
}

func TestGetSessionAndChildren(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	sub, err := s.GetSession(ctx, domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1/agent-a"})
	if err != nil || sub.Parent == nil || sub.Parent.NativeID != "c1" || sub.ProjectPath != "/p/app" || sub.SourcePath != "/src/c1/agent-a" {
		t.Fatalf("subagente: %+v %v", sub, err)
	}
	x, _ := s.GetSession(ctx, domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "x1"})
	if !x.Archived || x.UpdatedAt.Hour() != 1 || !x.CreatedAt.IsZero() {
		t.Errorf("codex: %+v", x)
	}
	ex, _ := s.ExistingKeys(ctx, []string{"claude:c1", "claude:sumiu"})
	if !ex["claude:c1"] || ex["claude:sumiu"] || len(ex) != 1 {
		t.Errorf("ExistingKeys = %v", ex)
	}
	if n, _ := s.CountChildren(ctx, domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1"}); n != 1 {
		t.Errorf("filhos = %d", n)
	}
	if _, err := s.GetSession(ctx, domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "?"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("ausente: %v", err)
	}
}

func TestListProjects(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ps, err := s.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// /p/obs só tem sessão automática: fica de fora.
	if len(ps) != 2 || ps[0].Path != "/p/app" || ps[1].Path != "/p/lib" {
		t.Fatalf("projetos: %+v", ps)
	}
	app := ps[0]
	if app.Name != "app" || app.SessionCount != 2 || len(app.Providers) != 2 ||
		app.Providers[0] != domain.ProviderClaude || app.Providers[1] != domain.ProviderCodex || app.LastActivity.Hour() != 3 {
		t.Errorf("app: %+v", app)
	}
}

func newUser(t *testing.T) *UserStore {
	t.Helper()
	db, err := OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewUserStore(db)
}

func TestFavorites(t *testing.T) {
	u := newUser(t)
	ctx := context.Background()
	k := domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1"}
	for range 2 { // idempotente
		if err := u.SetFavorite(ctx, k, true); err != nil {
			t.Fatal(err)
		}
	}
	keys, _ := u.FavoriteKeys(ctx)
	fav, _ := u.Favorites(ctx, []string{"claude:c1", "codex:x"})
	if len(keys) != 1 || !fav["claude:c1"] || fav["codex:x"] {
		t.Fatalf("keys=%v fav=%v", keys, fav)
	}
	u.SetFavorite(ctx, k, false)
	if keys, _ := u.FavoriteKeys(ctx); len(keys) != 0 {
		t.Errorf("após desmarcar: %v", keys)
	}
}

func TestTags(t *testing.T) {
	u := newUser(t)
	ctx := context.Background()
	a := domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "a"}
	b := domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "b"}
	u.AddTag(ctx, a, "Bug")
	u.AddTag(ctx, a, "bug") // mesma tag, outra capitalização
	u.AddTag(ctx, a, "infra")
	u.AddTag(ctx, b, "BUG")

	pairs, _ := u.TagKeys(ctx)
	if len(pairs) != 3 || pairs[0].Name != "Bug" || pairs[1].Name != "Bug" || pairs[2].Name != "infra" {
		t.Fatalf("TagKeys = %+v", pairs)
	}
	of, _ := u.TagsOf(ctx, []string{"claude:a", "codex:b"})
	if !equal(of["claude:a"], []string{"Bug", "infra"}) || !equal(of["codex:b"], []string{"Bug"}) {
		t.Errorf("TagsOf = %v", of)
	}
	if keys, _ := u.KeysWithTag(ctx, "bug"); len(keys) != 2 {
		t.Errorf("KeysWithTag = %v", keys)
	}
	u.RemoveTag(ctx, a, "infra")
	var n int
	u.db.QueryRow("SELECT count(*) FROM tags").Scan(&n)
	if n != 1 {
		t.Errorf("tag sem uso deveria sumir: %d tags", n)
	}
}
