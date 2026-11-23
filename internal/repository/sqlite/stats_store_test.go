package sqlite

import (
	"context"
	"slices"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

// msg é uma mensagem de teste: horário (nil = sem ts), conta como mensagem,
// ferramentas.
type msg struct {
	at    *time.Time
	text  bool
	tools []string
}

type fakeSess struct {
	key       domain.SessionKey
	project   string
	parent    *domain.SessionKey
	automated bool
	msgs      []msg
}

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func writeStats(t *testing.T, s *IndexStore, sessions ...fakeSess) {
	t.Helper()
	ctx := context.Background()
	for _, fs := range sessions {
		tx, err := s.BeginSource(ctx, fs.key.Provider, "/src/"+fs.key.NativeID, domain.SourceJSONL)
		if err != nil {
			t.Fatal(err)
		}
		var last time.Time
		for i, m := range fs.msgs {
			if err := tx.AddMessage(ctx, fs.key, IndexedMessage{
				Seq: i, Role: domain.RoleUser, Timestamp: m.at, SearchText: "x", HasText: m.text, Tools: m.tools,
			}); err != nil {
				t.Fatal(err)
			}
			if m.at != nil && m.at.After(last) {
				last = *m.at
			}
		}
		sess := domain.Session{Key: fs.key, ProjectPath: fs.project, Parent: fs.parent, Automated: fs.automated, UpdatedAt: last}
		if err := tx.PutSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx, SyncState{Status: "ok"}); err != nil {
			t.Fatal(err)
		}
	}
}

func ck(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderClaude, NativeID: id}
}

func cx(id string) domain.SessionKey {
	return domain.SessionKey{Provider: domain.ProviderCodex, NativeID: id}
}

// Novembro de 2026, UTC (Offset 0) salvo onde o teste diz outra coisa.
var nov = StatsFilter{From: *at("2026-11-01T00:00:00Z"), To: *at("2026-12-01T00:00:00Z"), Bucket: BucketDay}

func TestActivityByDay(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s,
		fakeSess{key: ck("a"), project: "/p/app", msgs: []msg{
			{at("2026-11-10T10:00:00Z"), true, nil}, {at("2026-11-10T11:00:00Z"), true, nil},
			{at("2026-11-10T11:01:00Z"), false, []string{"Bash"}}, {at("2026-11-11T09:00:00Z"), true, nil},
		}},
		fakeSess{key: ck("b"), project: "/p/app", msgs: []msg{{at("2026-11-10T15:00:00Z"), true, nil}}},
		fakeSess{key: cx("c"), project: "/p/lib", msgs: []msg{{at("2026-11-12T08:00:00Z"), true, nil}}},
	)
	rows, err := s.Activity(context.Background(), nov)
	if err != nil {
		t.Fatal(err)
	}
	want := []ActivityRow{
		{Bucket: "2026-11-10", Provider: domain.ProviderClaude, Sessions: 2, Messages: 3},
		{Bucket: "2026-11-11", Provider: domain.ProviderClaude, Sessions: 1, Messages: 1},
		{Bucket: "2026-11-12", Provider: domain.ProviderCodex, Sessions: 1, Messages: 1},
	}
	if !slices.Equal(rows, want) {
		t.Errorf("\n got %+v\nwant %+v", rows, want)
	}
	// Fora do período: nada.
	f := nov
	f.From, f.To = *at("2026-10-01T00:00:00Z"), *at("2026-11-01T00:00:00Z")
	if rows, _ := s.Activity(context.Background(), f); len(rows) != 0 {
		t.Errorf("outubro: %+v", rows)
	}
}

func TestActivityUsesLocalDay(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s, fakeSess{key: ck("a"), msgs: []msg{{at("2026-11-10T02:30:00Z"), true, nil}}})
	f := nov
	f.Offset = -3 * 3600
	rows, err := s.Activity(context.Background(), f)
	if err != nil || len(rows) != 1 || rows[0].Bucket != "2026-11-09" {
		t.Errorf("%+v %v", rows, err)
	}
}

func TestActivityIgnoresMissingTimestamp(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s, fakeSess{key: ck("a"), msgs: []msg{{nil, true, nil}, {at("2026-11-10T10:00:00Z"), true, nil}}})
	f := nov
	f.From = time.Time{} // desde o início
	rows, err := s.Activity(context.Background(), f)
	if err != nil || len(rows) != 1 || rows[0].Messages != 1 {
		t.Errorf("%+v %v", rows, err)
	}
}

func TestActivityWeekAndMonth(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s, fakeSess{key: ck("a"), msgs: []msg{
		{at("2026-11-10T12:00:00Z"), true, nil}, // terça
		{at("2026-11-15T12:00:00Z"), true, nil}, // domingo da mesma semana
		{at("2026-11-16T12:00:00Z"), true, nil}, // segunda seguinte
	}})
	f := nov
	f.Bucket = BucketWeek
	rows, _ := s.Activity(context.Background(), f)
	if len(rows) != 2 || rows[0].Bucket != "2026-11-09" || rows[0].Messages != 2 || rows[0].Sessions != 1 || rows[1].Bucket != "2026-11-16" {
		t.Errorf("semanas: %+v", rows)
	}
	f.Bucket = BucketMonth
	rows, _ = s.Activity(context.Background(), f)
	if len(rows) != 1 || rows[0].Bucket != "2026-11-01" || rows[0].Messages != 3 {
		t.Errorf("mês: %+v", rows)
	}
}

func TestStatsExcludesHidden(t *testing.T) {
	s, _ := newStore(t)
	parent := ck("main")
	writeStats(t, s,
		fakeSess{key: parent, project: "/p/app", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, []string{"Bash"}}}},
		fakeSess{key: ck("main/agent-a"), parent: &parent, project: "/p/app", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, []string{"Bash"}}}},
		fakeSess{key: ck("auto"), automated: true, project: "/p/obs", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, []string{"Bash"}}}},
	)
	tot, byProv, err := s.StatsTotals(context.Background(), nov)
	if err != nil || tot != (Totals{Sessions: 1, Messages: 1, Projects: 1}) || byProv[domain.ProviderClaude] != 1 {
		t.Errorf("%+v %v %v", tot, byProv, err)
	}
	tools, _ := s.TopTools(context.Background(), nov, 5)
	if len(tools) != 1 || tools[0].Count != 1 {
		t.Errorf("ferramentas: %+v", tools)
	}
}

func TestTopToolsAndProjects(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s,
		fakeSess{key: ck("a"), project: "/p/app", msgs: []msg{
			{at("2026-11-10T10:00:00Z"), true, nil}, {at("2026-11-10T10:01:00Z"), false, []string{"Bash", "Bash"}},
			{at("2026-11-10T10:02:00Z"), false, []string{"Bash", "Edit"}}, {at("2026-11-10T10:03:00Z"), true, nil},
		}},
		fakeSess{key: cx("b"), project: "/p/lib", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, nil}}},
	)
	tools, err := s.TopTools(context.Background(), nov, 5)
	if err != nil || !slices.Equal(tools, []NamedCount{{Name: "Bash", Count: 3}, {Name: "Edit", Count: 1}}) {
		t.Errorf("ferramentas: %+v %v", tools, err)
	}
	projects, err := s.TopProjects(context.Background(), nov, 5)
	if err != nil || !slices.Equal(projects, []NamedCount{{Name: "app", Path: "/p/app", Count: 2}, {Name: "lib", Path: "/p/lib", Count: 1}}) {
		t.Errorf("projetos: %+v %v", projects, err)
	}
	if top1, _ := s.TopProjects(context.Background(), nov, 1); len(top1) != 1 {
		t.Errorf("limite: %+v", top1)
	}
	per, err := s.ProjectsPerBucket(context.Background(), nov)
	if err != nil || per["2026-11-10"] != 2 {
		t.Errorf("projetos por dia: %v %v", per, err)
	}
}

func TestStatsTotalsProviderFilter(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s,
		fakeSess{key: ck("a"), project: "/p/app", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, nil}}},
		fakeSess{key: cx("b"), project: "/p/lib", msgs: []msg{{at("2026-11-10T10:00:00Z"), true, nil}, {at("2026-11-11T10:00:00Z"), true, nil}}},
	)
	f := nov
	f.Providers = []domain.ProviderID{domain.ProviderCodex}
	tot, byProv, err := s.StatsTotals(context.Background(), f)
	if err != nil || tot != (Totals{Sessions: 1, Messages: 2, Projects: 1}) || byProv[domain.ProviderClaude] != 0 {
		t.Errorf("%+v %v %v", tot, byProv, err)
	}
	if n, _ := s.TotalProjects(context.Background(), f.Providers); n != 1 {
		t.Errorf("total de projetos: %d", n)
	}
	if n, _ := s.TotalProjects(context.Background(), nil); n != 2 {
		t.Errorf("total geral: %d", n)
	}
	first, err := s.FirstActivity(context.Background(), nil)
	if err != nil || !first.Equal(*at("2026-11-10T10:00:00Z")) {
		t.Errorf("primeira atividade: %v %v", first, err)
	}
}

func TestActivityProjectFilter(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s,
		fakeSess{key: ck("a"), project: "/p/um", msgs: []msg{{at: at("2026-11-20T12:00:00Z"), text: true}, {at: at("2026-11-20T13:00:00Z"), text: true}}},
		fakeSess{key: ck("b"), project: "/p/dois", msgs: []msg{{at: at("2026-11-20T12:00:00Z"), text: true}}},
	)
	f := StatsFilter{From: *at("2026-11-01T00:00:00Z"), To: *at("2026-12-01T00:00:00Z"), Bucket: BucketDay, Project: "/p/um"}
	tot, _, err := s.StatsTotals(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if tot.Sessions != 1 || tot.Messages != 2 {
		t.Errorf("totais com projeto: %+v", tot)
	}
	rows, _ := s.Activity(context.Background(), f)
	if len(rows) != 1 || rows[0].Messages != 2 {
		t.Errorf("atividade com projeto: %+v", rows)
	}
	f.Project = "/p/inexistente"
	if tot, _, _ := s.StatsTotals(context.Background(), f); tot.Sessions != 0 {
		t.Errorf("projeto inexistente: %+v", tot)
	}
}

func TestActivityHourBucket(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s, fakeSess{key: ck("a"), msgs: []msg{{at: at("2026-11-20T02:30:00Z"), text: true}}}) // 23:30 em -03:00
	f := StatsFilter{From: *at("2026-11-19T00:00:00Z"), To: *at("2026-11-21T00:00:00Z"), Bucket: BucketHour, Offset: -3 * 3600}
	rows, err := s.Activity(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Bucket != "2026-11-19T23" {
		t.Errorf("%+v", rows)
	}
}

func TestActivityHeatmapAndActiveDays(t *testing.T) {
	s, _ := newStore(t)
	writeStats(t, s,
		// segunda 23/11/2026 23:30 em -03:00 = terça 02:30 UTC
		fakeSess{key: ck("a"), project: "/p/um", msgs: []msg{{at: at("2026-11-24T02:30:00Z"), text: true}, {at: at("2026-11-24T02:40:00Z"), text: true}}},
		fakeSess{key: ck("b"), project: "/p/um", msgs: []msg{{at: at("2026-11-20T15:00:00Z"), text: true}}},
		fakeSess{key: ck("c"), project: "/p/dois", msgs: []msg{{at: at("2026-11-20T16:00:00Z"), text: true}}},
		fakeSess{key: ck("sub"), parent: ptr(ck("a")), msgs: []msg{{at: at("2026-11-21T15:00:00Z"), text: true}}}, // fora (subagente)
	)
	f := StatsFilter{From: *at("2026-11-01T03:00:00Z"), To: *at("2026-12-01T03:00:00Z"), Offset: -3 * 3600}
	h, err := s.ActivityHeatmap(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if h[0][23] != 2 || h[4][12] != 1 || h[4][13] != 1 { // segunda 23h; sexta 12h e 13h
		t.Errorf("mapa: seg23=%d sex12=%d sex13=%d", h[0][23], h[4][12], h[4][13])
	}
	days, err := s.ActiveDays(context.Background(), f)
	if err != nil || days != 2 {
		t.Errorf("dias ativos %d (quer 2: 20 e 23), %v", days, err)
	}
	ps, err := s.TopProjectsBySessions(context.Background(), f, 10)
	if err != nil || len(ps) != 2 || ps[0].Path != "/p/um" || ps[0].Count != 2 {
		t.Errorf("sessões por projeto: %+v %v", ps, err)
	}
}
