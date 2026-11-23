package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Relógio dos testes: sábado, 22/11/2026, 15:00 em -03:00.
var (
	brt     = time.FixedZone("BRT", -3*3600)
	statNow = time.Date(2026, 11, 22, 15, 0, 0, 0, brt)
)

type statsEnv struct {
	svc   *StatsService
	store *sqlite.IndexStore
	user  *sqlite.UserStore
	dir   string
	now   time.Time
}

func newStatsEnv(t *testing.T) *statsEnv {
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
	e := &statsEnv{store: sqlite.NewIndexStore(idx), user: sqlite.NewUserStore(usr), dir: dir, now: statNow}
	e.svc = NewStatsService(e.store, NewSessionService(e.store, e.user, discard), e.user, discard)
	e.svc.now = func() time.Time { return e.now }
	return e
}

// sess grava uma sessão principal com mensagens de texto (uma por horário)
// e chamadas de ferramenta; o arquivo da fonte fica com mtime antigo.
func (e *statsEnv) sess(t *testing.T, key domain.SessionKey, project string, times []time.Time, tools ...string) {
	t.Helper()
	ctx := context.Background()
	src := filepath.Join(e.dir, key.NativeID+".jsonl")
	os.WriteFile(src, []byte("{}\n"), 0o644)
	old := statNow.Add(-24 * time.Hour)
	os.Chtimes(src, old, old)
	tx, err := e.store.BeginSource(ctx, key.Provider, src, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	var last time.Time
	for i, ts := range times {
		ts := ts
		tx.AddMessage(ctx, key, sqlite.IndexedMessage{Seq: i, Role: domain.RoleUser, Timestamp: &ts, SearchText: "x", HasText: true})
		if ts.After(last) {
			last = ts
		}
	}
	if len(tools) > 0 && len(times) > 0 {
		ts := times[0]
		tx.AddMessage(ctx, key, sqlite.IndexedMessage{Seq: len(times), Role: domain.RoleAssistant, Timestamp: &ts, SearchText: "t", Tools: tools})
	}
	tx.PutSession(ctx, domain.Session{Key: key, ProjectPath: project, UpdatedAt: last, SourceKind: domain.SourceJSONL, SourcePath: src, Title: key.NativeID})
	if err := tx.Commit(ctx, sqlite.SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
}

func daysAgo(n int, hour int) time.Time {
	return time.Date(2026, 11, 22-n, hour, 0, 0, 0, brt)
}

func (e *statsEnv) dash(t *testing.T, q StatsQuery) Dashboard {
	t.Helper()
	d, err := e.svc.Dashboard(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDashboardEmpty(t *testing.T) {
	e := newStatsEnv(t)
	d := e.dash(t, StatsQuery{Period: "30d"})
	if d.Sessions.Value != 0 || d.Messages.Value != 0 || len(d.MessagesSeries) != 30 || d.ResumeID != "" || d.Peak != 0 {
		t.Errorf("%+v", d)
	}
	for _, p := range d.MessagesSeries {
		if p.Total != 0 || p.ByProvider == nil {
			t.Fatalf("ponto %+v", p)
		}
	}
	if d.Recent.All == nil || d.Recent.Open == nil || d.Recent.Favorites == nil || d.TopTools == nil || d.ByProvider == nil {
		t.Error("listas nulas (o frontend espera arrays)")
	}
	if all := e.dash(t, StatsQuery{Period: "all"}); all.Bucket != "day" || len(all.MessagesSeries) == 0 {
		t.Errorf("all vazio: %+v", all)
	}
}

func TestDashboardPeriods(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p/app", []time.Time{daysAgo(0, 10), daysAgo(1, 10), daysAgo(1, 11)})
	e.sess(t, ck2("old"), "/p/app", []time.Time{daysAgo(9, 10), daysAgo(10, 10)})
	d := e.dash(t, StatsQuery{Period: "7d"})
	if len(d.MessagesSeries) != 7 || d.Bucket != "day" || !d.Messages.HasPrev {
		t.Fatalf("série: %d %s %v", len(d.MessagesSeries), d.Bucket, d.Messages.HasPrev)
	}
	if d.Messages.Value != 3 || d.Messages.Prev != 2 || d.Sessions.Value != 1 || d.Sessions.Prev != 1 {
		t.Errorf("métricas: %+v %+v", d.Messages, d.Sessions)
	}
	last := d.MessagesSeries[6]
	if last.Total != 1 || last.Start != time.Date(2026, 11, 22, 0, 0, 0, 0, brt).UnixMilli() {
		t.Errorf("hoje: %+v", last)
	}
	if len(d.PrevMessages) != 7 || d.PrevMessages[4] != 1 || d.PrevMessages[3] != 1 {
		t.Errorf("anterior: %v", d.PrevMessages)
	}
	if d.Peak != time.Date(2026, 11, 21, 0, 0, 0, 0, brt).UnixMilli() {
		t.Errorf("pico: %d", d.Peak)
	}
	if _, err := e.svc.Dashboard(context.Background(), StatsQuery{Period: "1y"}); !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("período inválido: %v", err)
	}
}

func TestDashboardAllUsesWeeksOrMonths(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p/app", []time.Time{daysAgo(400, 10), daysAgo(0, 10)})
	d := e.dash(t, StatsQuery{Period: "all"})
	if d.Bucket != "week" || d.Messages.HasPrev || len(d.PrevMessages) != 0 || d.Messages.Value != 2 {
		t.Errorf("semanas: %s %+v", d.Bucket, d.Messages)
	}
	if n := len(d.MessagesSeries); n < 57 || n > 59 {
		t.Errorf("pontos: %d", n)
	}
	e2 := newStatsEnv(t)
	e2.sess(t, ck2("b"), "/p/app", []time.Time{time.Date(2023, 6, 1, 10, 0, 0, 0, brt), daysAgo(0, 10)})
	if d := e2.dash(t, StatsQuery{Period: "all"}); d.Bucket != "month" || d.MessagesSeries[0].Start != time.Date(2023, 6, 1, 0, 0, 0, 0, brt).UnixMilli() {
		t.Errorf("meses: %s %+v", d.Bucket, d.MessagesSeries[0])
	}
}

func TestDashboardEmptyProvidersMeansAll(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p/app", []time.Time{daysAgo(0, 10)})
	e.sess(t, cx2("b"), "/p/lib", []time.Time{daysAgo(0, 11)})
	if d := e.dash(t, StatsQuery{Period: "7d", Providers: []domain.ProviderID{}}); d.Sessions.Value != 2 || len(d.ByProvider) != 2 {
		t.Errorf("%+v %+v", d.Sessions, d.ByProvider)
	}
}

func TestDashboardProviderFilterAppliesToTotals(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p/app", []time.Time{daysAgo(0, 10)}, "Bash")
	e.sess(t, cx2("b"), "/p/lib", []time.Time{daysAgo(0, 11)}, "exec_command")
	e.user.SetFavorite(context.Background(), ck2("a"), true)
	all := e.dash(t, StatsQuery{Period: "7d"})
	if all.Favorites != 1 || all.TotalProjects != 2 || len(all.TopTools) != 2 {
		t.Errorf("todos: fav %d proj %d tools %+v", all.Favorites, all.TotalProjects, all.TopTools)
	}
	d := e.dash(t, StatsQuery{Period: "7d", Providers: []domain.ProviderID{domain.ProviderCodex}})
	if d.Favorites != 0 || d.TotalProjects != 1 || d.Sessions.Value != 1 || len(d.TopTools) != 1 || d.TopTools[0].Name != "exec_command" {
		t.Errorf("codex: fav %d proj %d sess %d tools %+v", d.Favorites, d.TotalProjects, d.Sessions.Value, d.TopTools)
	}
	if len(d.Recent.All) != 1 || d.Recent.All[0].ID != "codex:b" || len(all.Recent.Favorites) != 1 {
		t.Errorf("recentes: %+v / fav %+v", d.Recent.All, all.Recent.Favorites)
	}
	if _, err := e.svc.Dashboard(context.Background(), StatsQuery{Period: "7d", Providers: []domain.ProviderID{"nope"}}); !errors.Is(err, ErrInvalidFilter) {
		t.Errorf("provider inválido: %v", err)
	}
}

func TestDashboardFavoritesAdded(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("a"), "/p/app", []time.Time{daysAgo(0, 10)})
	e.user.SetFavorite(context.Background(), ck2("a"), true)
	d := e.dash(t, StatsQuery{Period: "all"})
	if d.Favorites != 1 || len(d.FavoritesSeries) != len(d.MessagesSeries) {
		t.Errorf("favoritas: %d série %d/%d", d.Favorites, len(d.FavoritesSeries), len(d.MessagesSeries))
	}
}

// A sessão aberta (arquivo alterado agora) é pulada por "Retomar última" e
// aparece em "Abertas".
func TestDashboardResumeSkipsOpen(t *testing.T) {
	e := newStatsEnv(t)
	e.sess(t, ck2("old"), "/p/app", []time.Time{daysAgo(0, 9)})
	e.sess(t, ck2("open"), "/p/app", []time.Time{daysAgo(0, 14)})
	src := filepath.Join(e.dir, "open.jsonl")
	os.Chtimes(src, statNow.Add(-10*time.Second), statNow.Add(-10*time.Second))
	d := e.dash(t, StatsQuery{Period: "7d"})
	if d.ResumeID != "claude:old" || d.OpenNow != 1 || len(d.Recent.Open) != 1 || d.Recent.Open[0].ID != "claude:open" {
		t.Errorf("resume %q abertas %d %+v", d.ResumeID, d.OpenNow, d.Recent.Open)
	}
	// Filtro de agente também vale para "Retomar última".
	if d := e.dash(t, StatsQuery{Period: "7d", Providers: []domain.ProviderID{domain.ProviderCodex}}); d.ResumeID != "" {
		t.Errorf("codex: %q", d.ResumeID)
	}
}

func TestPinnedProjects(t *testing.T) {
	e := newStatsEnv(t)
	for i, p := range []string{"a", "b", "c", "d"} {
		times := make([]time.Time, i+1)
		for j := range times {
			times[j] = daysAgo(j, 10)
		}
		e.sess(t, ck2(p), "/p/"+p, times)
	}
	e.sess(t, ck2("velho"), "/p/velho", []time.Time{daysAgo(40, 10), daysAgo(41, 10), daysAgo(42, 10), daysAgo(43, 10), daysAgo(44, 10)})
	got, err := e.svc.PinnedProjects(context.Background())
	if err != nil || len(got) != 3 || got[0].Name != "d" || got[2].Name != "b" {
		t.Errorf("%+v %v", got, err)
	}
}

func TestDashboardQueryTime(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("medição de tempo: só sem -race e sem -short")
	}
	e := newStatsEnv(t)
	for s := range 100 {
		times := make([]time.Time, 500)
		for i := range times {
			times[i] = statNow.Add(-time.Duration((s*500+i)%(90*24*60)) * time.Minute)
		}
		e.sess(t, ck2(fmt.Sprintf("s%03d", s)), fmt.Sprintf("/p/proj%d", s%30), times, "Bash", "Edit")
	}
	for _, p := range []string{"30d", "90d", "all"} {
		start := time.Now()
		e.dash(t, StatsQuery{Period: p})
		if d := time.Since(start); d > 150*time.Millisecond {
			t.Errorf("%s: %v (meta 150 ms)", p, d)
		}
	}
}

func ck2(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderClaude, NativeID: id}
}

func cx2(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderCodex, NativeID: id}
}

// Variação contra o anterior: com atividade constante, de manhã (antes do
// horário de sempre), o período atual não pode parecer em queda. O anterior
// tem a mesma duração do atual até agora.
func TestDashboardPrevComparesSameElapsedTime(t *testing.T) {
	e := newStatsEnv(t)
	e.now = time.Date(2026, 11, 22, 9, 0, 0, 0, brt)
	var times []time.Time
	for n := 1; n < 20; n++ { // hoje às 10h ainda não aconteceu
		times = append(times, daysAgo(n, 10))
	}
	e.sess(t, ck2("a"), "/p", times)
	d := e.dash(t, StatsQuery{Period: "7d"})
	if d.Messages.Value != 6 || d.Messages.Prev != 6 {
		t.Errorf("mensagens = %d, anterior = %d; quer 6 e 6", d.Messages.Value, d.Messages.Prev)
	}
	if len(d.PrevMessages) != 7 {
		t.Errorf("série anterior com %d pontos; quer 7", len(d.PrevMessages))
	}
}
