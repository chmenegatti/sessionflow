package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

func newSessionService(t *testing.T) *SessionService {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	idx, _, err := sqlite.OpenIndex(ctx, filepath.Join(dir, "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	usr, err := sqlite.OpenUser(ctx, filepath.Join(dir, "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close(); usr.Close() })

	store := sqlite.NewIndexStore(idx)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parent := domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1"}
	for i, s := range []domain.Session{
		{Key: parent, ProjectPath: "/p/app", Title: "<command-name>/init</command-name>", FirstPrompt: "x", Model: "m"},
		{Key: domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "c1/agent-a"}, Parent: &parent, ProjectPath: "/p/app", Title: "sub"},
		{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "x1"}, ProjectPath: "/p/app", Title: "Codex", FirstPrompt: "Primeiro prompt"},
		{Key: domain.SessionKey{Provider: domain.ProviderOpenCode, NativeID: "o1"}, ProjectPath: "/p/lib", Title: ""},
	} {
		s.UpdatedAt = base.Add(time.Duration(i) * time.Hour)
		tx, err := store.BeginSource(ctx, s.Key.Provider, "/src/"+s.Key.NativeID, domain.SourceJSONL)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.PutSession(ctx, s); err != nil {
			t.Fatal(err)
		}
		tx.Commit(ctx, sqlite.SyncState{Status: "ok"})
	}
	return NewSessionService(store, sqlite.NewUserStore(usr), discard)
}

func pageIDs(p SessionPage) string {
	var ids []string
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	return strings.Join(ids, ",")
}

func TestSessionListFiltersAndTitles(t *testing.T) {
	svc := newSessionService(t)
	ctx := context.Background()

	page, err := svc.List(ctx, SessionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if pageIDs(page) != "opencode:o1,codex:x1,claude:c1" || page.Total != 3 || page.HasMore {
		t.Fatalf("padrão: %s total=%d", pageIDs(page), page.Total)
	}
	byID := map[string]SessionItem{}
	for _, it := range page.Items {
		byID[it.ID] = it
	}
	// Marcação removida; título vazio cai no primeiro prompt ou fica "".
	if byID["claude:c1"].Title != "/init" || byID["opencode:o1"].Title != "" || byID["codex:x1"].Title != "Codex" {
		t.Errorf("títulos: %+v", byID)
	}
	if it := byID["claude:c1"]; it.ProjectName != "app" || it.Tags == nil || it.UpdatedAt == 0 || it.CreatedAt != 0 {
		t.Errorf("item: %+v", it)
	}

	page, _ = svc.List(ctx, SessionQuery{Providers: []domain.ProviderID{domain.ProviderClaude}, IncludeHidden: true, Sort: "oldest"})
	if pageIDs(page) != "claude:c1,claude:c1/agent-a" || !page.Items[1].Subagent {
		t.Errorf("claude com ocultas: %s", pageIDs(page))
	}
	page, _ = svc.List(ctx, SessionQuery{Project: "/p/app", Limit: 1})
	if page.Total != 2 || !page.HasMore || len(page.Items) != 1 {
		t.Errorf("paginação: %+v", page)
	}
	if _, err := svc.List(ctx, SessionQuery{Providers: []domain.ProviderID{"gemini"}}); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("provider inválido: %v", err)
	}
}

func TestFavoritesAndTagsFlow(t *testing.T) {
	svc := newSessionService(t)
	ctx := context.Background()

	if err := svc.SetFavorite(ctx, "claude:c1/agent-a", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetFavorite(ctx, "codex:x1", true); err != nil {
		t.Fatal(err)
	}
	// Favoritas incluem ocultas favoritadas.
	page, _ := svc.List(ctx, SessionQuery{FavoritesOnly: true})
	if pageIDs(page) != "codex:x1,claude:c1/agent-a" || !page.Items[0].Favorite {
		t.Fatalf("favoritas: %s", pageIDs(page))
	}

	tags, err := svc.AddTag(ctx, "codex:x1", "  revisar   depois ")
	if err != nil || strings.Join(tags, "|") != "revisar depois" {
		t.Fatalf("AddTag: %v %v", tags, err)
	}
	svc.AddTag(ctx, "opencode:o1", "REVISAR depois")
	page, _ = svc.List(ctx, SessionQuery{Tag: "revisar depois"})
	if pageIDs(page) != "opencode:o1,codex:x1" {
		t.Errorf("filtro por tag: %s", pageIDs(page))
	}
	// Tag + favoritas: interseção.
	page, _ = svc.List(ctx, SessionQuery{Tag: "revisar depois", FavoritesOnly: true})
	if pageIDs(page) != "codex:x1" {
		t.Errorf("tag ∩ favoritas: %s", pageIDs(page))
	}
	all, _ := svc.Tags(ctx)
	if len(all) != 1 || all[0].Count != 2 {
		t.Errorf("Tags: %+v", all)
	}
	// Tag de sessão que saiu do índice não conta.
	svc.user.AddTag(ctx, domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "apagada"}, "revisar depois")
	svc.user.AddTag(ctx, domain.SessionKey{Provider: domain.ProviderClaude, NativeID: "apagada"}, "órfã")
	all, _ = svc.Tags(ctx)
	if len(all) != 1 || all[0].Count != 2 {
		t.Errorf("Tags com sessão apagada: %+v", all)
	}
	tags, _ = svc.RemoveTag(ctx, "codex:x1", "Revisar Depois")
	if len(tags) != 0 || tags == nil {
		t.Errorf("RemoveTag: %#v", tags)
	}

	d, err := svc.Get(ctx, "codex:x1")
	if err != nil || !d.Session.Favorite || d.FirstPrompt != "Primeiro prompt" || d.SourcePath != "/src/x1" || d.NativeID != "x1" {
		t.Errorf("Get: %+v %v", d, err)
	}
	parent, _ := svc.Get(ctx, "claude:c1")
	if parent.Children != 1 || parent.ParentID != "" {
		t.Errorf("pai: %+v", parent)
	}
	sub, _ := svc.Get(ctx, "claude:c1/agent-a")
	if sub.ParentID != "claude:c1" {
		t.Errorf("subagente: %+v", sub)
	}
}

func TestSessionErrors(t *testing.T) {
	svc := newSessionService(t)
	ctx := context.Background()
	if _, err := svc.Get(ctx, "sem-separador"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("chave inválida: %v", err)
	}
	if _, err := svc.Get(ctx, "claude:nao-existe"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("ausente: %v", err)
	}
	if err := svc.SetFavorite(ctx, "claude:nao-existe", true); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("favoritar ausente: %v", err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 41), "a\x00b"} {
		if _, err := svc.AddTag(ctx, "codex:x1", bad); !errors.Is(err, ErrInvalidTag) {
			t.Errorf("tag %q: %v", bad, err)
		}
	}
}

func TestDisplayTitle(t *testing.T) {
	cases := map[[2]string]string{
		{"Título", "x"}: "Título",
		{"<command-message>init is analyzing</command-message>\n<command-name>/init</command-name>", ""}: "init is analyzing /init",
		{"<local-command-caveat>x</local-command-caveat>", ""}:                                           "x",
		{"a < b e c > d", ""}:                  "a < b e c > d",
		{"", "  prompt  "}:                     "prompt",
		{"<br/>", ""}:                          "<br/>",
		{"Parser de List<String> e <div>", ""}: "Parser de List<String> e <div>",
		{"<pasted_content>colado</pasted_content> resto", ""}: "colado resto",
		{"<fork-boilerplate/>", "prompt"}:                     "prompt",
	}
	for in, want := range cases {
		if got := DisplayTitle(in[0], in[1]); got != want {
			t.Errorf("DisplayTitle(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
