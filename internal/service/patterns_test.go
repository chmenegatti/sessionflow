package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// put grava uma sessão com mensagens de texto nos horários dados.
func (e *statsEnv) put(t *testing.T, s domain.Session, times ...time.Time) {
	t.Helper()
	ctx := context.Background()
	src := filepath.Join(e.dir, s.Key.NativeID+".jsonl")
	os.WriteFile(src, []byte("{}\n"), 0o644)
	tx, err := e.store.BeginSource(ctx, s.Key.Provider, src, domain.SourceJSONL)
	if err != nil {
		t.Fatal(err)
	}
	for i, ts := range times {
		ts := ts
		tx.AddMessage(ctx, s.Key, sqlite.IndexedMessage{Seq: i, Role: domain.RoleUser, Timestamp: &ts, SearchText: "x", HasText: true})
		if ts.After(s.UpdatedAt) {
			s.UpdatedAt = ts
		}
	}
	if s.CreatedAt.IsZero() && len(times) > 0 {
		s.CreatedAt = times[0]
	}
	s.SourceKind, s.SourcePath, s.Title = domain.SourceJSONL, src, s.Key.NativeID
	tx.PutSession(ctx, s)
	if err := tx.Commit(ctx, sqlite.SyncState{Status: "ok"}); err != nil {
		t.Fatal(err)
	}
}

func (e *statsEnv) patterns(t *testing.T, q AnalyticsQuery) PatternsView {
	t.Helper()
	a := NewAnalyticsService(e.store, e.svc, discard)
	a.now = func() time.Time { return e.now }
	p := NewPatternsService(e.store, a, discard, nil)
	v, err := p.Patterns(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func at(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, brt) }

func sessOf(id, project string) domain.Session {
	return domain.Session{Key: ck2(id), ProjectPath: project}
}

func TestSessionFactsResumedGap(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(22, 10, 0), at(22, 10, 29))
	e.put(t, sessOf("b", "/p"), at(22, 10, 0), at(22, 10, 30))
	f := sqlite.StatsFilter{From: at(22, 0, 0), To: at(23, 0, 0), Offset: -3 * 3600}
	facts, err := e.store.SessionFacts(context.Background(), f, resumeGap)
	if err != nil || len(facts) != 2 {
		t.Fatalf("%+v %v", facts, err)
	}
	for _, s := range facts {
		switch {
		case s.ResumedAt == nil && s.MessagesInPeriod == 2:
			if s.LastTs == nil || *s.LastTs-*s.FirstTs != 29*60_000 {
				t.Errorf("a: %+v", s)
			}
		case s.ResumedAt != nil && *s.ResumedAt == at(22, 10, 30).UnixMilli():
		default:
			t.Errorf("fato inesperado %+v", s)
		}
	}
}

func TestSessionFactsResumedAcrossPeriod(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(21, 23, 0), at(22, 8, 0))
	v := e.patterns(t, AnalyticsQuery{Period: "today"})
	if v.Resumed.Value != 1 || v.Started.Value != 0 {
		t.Errorf("retomadas %d, iniciadas %d", v.Resumed.Value, v.Started.Value)
	}
}

func TestPatternsStreaks(t *testing.T) {
	e := newStatsEnv(t)
	for i, d := range []int{18, 19, 20, 22} {
		e.put(t, sessOf(string(rune('a'+i)), "/p"), at(d, 10, 0))
	}
	v := e.patterns(t, AnalyticsQuery{Period: "30d"})
	if v.Frequency.CurrentStreak != 1 || v.Frequency.LongestStreak != 3 || v.Frequency.ActiveDays != 4 || v.Frequency.ActiveWeeks != 1 { // 16/11 é segunda: 18–22 na mesma semana
		t.Errorf("%+v", v.Frequency)
	}
	if v.Frequency.SessionsPerActiveDay != 1 {
		t.Errorf("sessões por dia ativo %v", v.Frequency.SessionsPerActiveDay)
	}
	p := e.patterns(t, AnalyticsQuery{Period: "prev_month"})
	if p.Frequency.CurrentStreak != 0 || p.Frequency.LongestStreak != 0 || len(p.Calendar) != 31 {
		t.Errorf("mês anterior: %+v (%d dias)", p.Frequency, len(p.Calendar))
	}
	// Calendário: um dia por data do período, com as sessões.
	if len(v.Calendar) != 30 || v.Calendar[29].Day != "2026-11-22" || v.Calendar[29].Sessions != 1 || v.Calendar[28].Sessions != 0 {
		t.Errorf("calendário %+v", v.Calendar[27:])
	}
}

func TestPatternsMultiProvider(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(22, 10, 0))
	e.put(t, domain.Session{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "x"}, ProjectPath: "/p"}, at(21, 10, 0))
	e.put(t, sessOf("b", "/q"), at(22, 11, 0))
	v := e.patterns(t, AnalyticsQuery{Period: "7d"})
	if len(v.MultiProvider) != 1 || v.MultiProvider[0].Path != "/p" || len(v.MultiProvider[0].Providers) != 2 {
		t.Errorf("%+v", v.MultiProvider)
	}
	if len(v.ByProvider) != 2 {
		t.Errorf("por agente %+v", v.ByProvider)
	}
}

func TestPatternsDistributions(t *testing.T) {
	e := newStatsEnv(t)
	for i, mins := range []int{1, 10, 40, 200} {
		start := at(20, 8+i, 0)
		e.put(t, sessOf(string(rune('a'+i)), "/p"), start, start.Add(time.Duration(mins)*time.Minute))
	}
	v := e.patterns(t, AnalyticsQuery{Period: "7d"})
	d := v.Duration
	if d.Median != 10*60_000 || d.P90 != 200*60_000 {
		t.Errorf("mediana %d, p90 %d", d.Median, d.P90)
	}
	for i, b := range d.Buckets {
		if b.Count != 1 {
			t.Errorf("faixa %d (%s) = %d", i, b.Label, b.Count)
		}
	}
	if v.MessagesPerSession.Median != 2 || v.MessagesPerSession.Buckets[0].Count != 4 {
		t.Errorf("mensagens %+v", v.MessagesPerSession)
	}
}

func TestPatternsGaps(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(20, 10, 0))
	e.put(t, sessOf("b", "/p"), at(20, 12, 0))
	e.put(t, sessOf("c", "/p"), at(21, 12, 0))
	v := e.patterns(t, AnalyticsQuery{Period: "7d"})
	g := v.Gaps
	if g.Median != 2*3600_000 || g.Buckets[1].Count != 1 || g.Buckets[2].Count != 1 || v.Started.Value != 3 {
		t.Errorf("%+v (iniciadas %d)", g, v.Started.Value)
	}
}

func TestPatternsRecurring(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(20, 10, 0))
	e.put(t, sessOf("b", "/p"), at(21, 10, 0))
	e.put(t, sessOf("c", "/q"), at(21, 11, 0), at(21, 12, 0))
	v := e.patterns(t, AnalyticsQuery{Period: "7d"})
	if v.RecurringProjects != 1 {
		t.Errorf("recorrentes %d", v.RecurringProjects)
	}
	if len(v.TopProjects) != 2 || v.TopProjects[0].Path != "/p" || v.TopProjects[0].ActiveDays != 2 || v.TopProjects[0].DaysSinceLast != 1 {
		t.Errorf("%+v", v.TopProjects)
	}
}

func TestPatternsParityWithActivity(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), daysAgo(0, 10), daysAgo(0, 11))
	e.put(t, sessOf("b", "/q"), daysAgo(3, 23), daysAgo(40, 9))
	e.put(t, domain.Session{Key: domain.SessionKey{Provider: domain.ProviderCodex, NativeID: "x"}, ProjectPath: "/p"}, daysAgo(10, 2))
	a := NewAnalyticsService(e.store, e.svc, discard)
	a.now = func() time.Time { return e.now }
	for _, per := range []string{"7d", "30d", "all"} {
		act, err := a.Activity(context.Background(), AnalyticsQuery{Period: per})
		if err != nil {
			t.Fatal(err)
		}
		v := e.patterns(t, AnalyticsQuery{Period: per})
		if v.Frequency.ActiveDays != act.ActiveDays || v.Heatmap != act.Heatmap || v.Frequency.Days != act.Days {
			t.Errorf("%s: dias %d/%d (%d/%d)", per, v.Frequency.ActiveDays, act.ActiveDays, v.Frequency.Days, act.Days)
		}
	}
}

func TestPatternsExcludeHidden(t *testing.T) {
	e := newStatsEnv(t)
	parent := ck2("p")
	e.put(t, domain.Session{Key: ck2("sub"), Parent: &parent, ProjectPath: "/p"}, at(22, 10, 0))
	e.put(t, domain.Session{Key: ck2("auto"), Automated: true, ProjectPath: "/p"}, at(22, 10, 0), at(22, 11, 0))
	v := e.patterns(t, AnalyticsQuery{Period: "7d"})
	if v.Started.Value != 0 || v.Resumed.Value != 0 || v.Frequency.ActiveDays != 0 || len(v.TopProjects) != 0 || len(v.ByProvider) != 0 {
		t.Errorf("ocultas contadas: %+v", v)
	}
}

func TestPatternsCopy(t *testing.T) {
	e := newStatsEnv(t)
	e.put(t, sessOf("a", "/p"), at(22, 10, 0))
	b, _ := json.Marshal(e.patterns(t, AnalyticsQuery{Period: "7d"}))
	low := strings.ToLower(string(b))
	for _, w := range []string{"produtiv", "eficiên", "eficien", "productiv"} {
		if strings.Contains(low, w) {
			t.Errorf("termo %q na resposta", w)
		}
	}
}

func TestPatternsInvalidFilter(t *testing.T) {
	e := newStatsEnv(t)
	a := NewAnalyticsService(e.store, e.svc, discard)
	p := NewPatternsService(e.store, a, discard, nil)
	if _, err := p.Patterns(context.Background(), AnalyticsQuery{Period: "semana"}); err != ErrInvalidPeriod {
		t.Errorf("%v", err)
	}
}
