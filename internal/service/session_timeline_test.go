package service

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

// Linha do tempo do consumo de uma sessão (hardening, item 8).

func timelineEnv(t *testing.T) (*AnalyticsService, *sql.DB) {
	t.Helper()
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	udb, err := sqlite.OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udb.Close() })
	user := sqlite.NewUserStore(udb)
	stats := NewStatsService(e.Store, NewSessionService(e.Store, user, discard), user, discard)
	stats.now = func() time.Time { return statNow }
	a := NewAnalyticsService(e.Store, stats, discard)
	a.now = func() time.Time { return statNow }
	return a, e.DB
}

func timeline(t *testing.T, a *AnalyticsService, q AnalyticsQuery, root int64) SessionTimeline {
	t.Helper()
	tl, err := a.SessionTimeline(context.Background(), q, root)
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// reconcile: intervalos + registros sem horário = total da sessão na
// tabela de consumo, campo a campo (soma e quantos informaram: parcial e
// indisponível preservados).
func reconcile(t *testing.T, name string, tl SessionTimeline, want sqlite.TokenSums) {
	t.Helper()
	var sum sqlite.TokenSums
	var cum int64
	for i, p := range tl.Points {
		if p.Tokens.Records == 0 {
			t.Errorf("%s: intervalo %d sem registros (ponto artificial)", name, i)
		}
		if i > 0 && p.Start <= tl.Points[i-1].Start {
			t.Errorf("%s: intervalos fora de ordem em %d", name, i)
		}
		sum.Add(p.Tokens)
		cum += p.Tokens.Total.Sum
		if p.Cumulative != cum {
			t.Errorf("%s: acumulado %d no intervalo %d, esperado %d", name, p.Cumulative, i, cum)
		}
	}
	sum.Add(tl.Untimed)
	if sum != want || tl.Total != want {
		t.Errorf("%s: intervalos %+v, total %+v; sessão %+v", name, sum, tl.Total, want)
	}
}

func TestSessionTimelineReconcilesEveryRoot(t *testing.T) {
	a, _ := timelineEnv(t)
	ctx := context.Background()
	for _, q := range []AnalyticsQuery{{Period: "all"}, {Period: "30d"}} {
		p, err := a.UsageSessions(ctx, q, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Rows) == 0 {
			t.Fatal("fixture sem sessões com consumo")
		}
		multi, single, withSub := false, false, false
		for _, r := range p.Rows {
			tl := timeline(t, a, q, r.SessionID)
			reconcile(t, string(r.Key.Provider)+":"+r.Key.NativeID, tl, r.Tokens)
			timed := r.Tokens.Records - tl.Untimed.Records
			multi = multi || timed > 1
			single = single || timed == 1 && len(tl.Points) == 1
			origins, _ := a.SessionOrigins(ctx, q, r.SessionID)
			for _, o := range origins {
				withSub = withSub || o.Origin == "subagent" && o.Tokens.Records > 0
			}
		}
		if !multi || !single || !withSub {
			t.Errorf("%s: fixture não cobre vários eventos (%v), um evento (%v), subagente (%v)", q.Period, multi, single, withSub)
		}
	}
}

func TestSessionTimelineNoUsage(t *testing.T) {
	a, _ := timelineEnv(t)
	tl := timeline(t, a, AnalyticsQuery{Period: "all"}, 987654)
	if len(tl.Points) != 0 || tl.Total.Records != 0 || tl.Untimed.Records != 0 {
		t.Errorf("sessão sem consumo com gráfico: %+v", tl)
	}
}

// Mesmo horário em dois registros: um intervalo só, nada somado em dobro.
func TestSessionTimelineEqualTimestamps(t *testing.T) {
	a, db := timelineEnv(t)
	ctx := context.Background()
	q := AnalyticsQuery{Period: "all"}
	p, _ := a.UsageSessions(ctx, q, 0)
	var root sqlite.UsageSessionRow
	for _, r := range p.Rows {
		if r.Tokens.Records >= 3 {
			root = r
			break
		}
	}
	if root.SessionID == 0 {
		t.Fatal("sem sessão com 3 registros")
	}
	if _, err := db.Exec(`UPDATE usage SET ts = 1795000000000 WHERE session_id = ? AND ts IS NOT NULL`, root.SessionID); err != nil {
		t.Fatal(err)
	}
	p, _ = a.UsageSessions(ctx, q, 0)
	for _, r := range p.Rows {
		if r.SessionID != root.SessionID {
			continue
		}
		tl := timeline(t, a, q, r.SessionID)
		reconcile(t, "mesmo horário", tl, r.Tokens)
		n := 0
		for _, pt := range tl.Points {
			if pt.Start <= 1795000000000 && 1795000000000 < pt.Start+tl.StepMs {
				n++
			}
		}
		if n != 1 {
			t.Errorf("registros no mesmo horário em %d intervalos", n)
		}
	}
}

// Retomada: pausa de 30 min ou mais entre registros marca o intervalo.
func TestSessionTimelineResumes(t *testing.T) {
	a, db := timelineEnv(t)
	ctx := context.Background()
	q := AnalyticsQuery{Period: "all"}
	p, _ := a.UsageSessions(ctx, q, 0)
	var root int64
	for _, r := range p.Rows {
		if r.Tokens.Records >= 2 {
			root = r.SessionID
			break
		}
	}
	// Dois blocos: tudo às 12h00 e o último registro 2 h depois.
	base := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := db.Exec(`UPDATE usage SET ts = ? WHERE session_id = ? AND ts IS NOT NULL`, base, root); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE usage SET ts = ? WHERE id = (SELECT max(id) FROM usage WHERE session_id = ? AND ts IS NOT NULL)`, base+2*time.Hour.Milliseconds(), root); err != nil {
		t.Fatal(err)
	}
	tl := timeline(t, a, q, root)
	if tl.Resumes != 1 {
		t.Fatalf("retomadas = %d, esperado 1", tl.Resumes)
	}
	marked := 0
	for _, pt := range tl.Points {
		if pt.Resumed {
			marked++
			if pt.Start < base+2*time.Hour.Milliseconds()-tl.StepMs {
				t.Errorf("retomada marcada no intervalo errado: %d", pt.Start)
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d intervalos marcados como retomada", marked)
	}
}

// Passo: sem pontos demais nem de menos para a duração.
func TestTimelineStep(t *testing.T) {
	for _, tc := range []struct {
		span time.Duration
		want time.Duration
	}{
		{0, time.Minute},
		{20 * time.Minute, time.Minute},
		{3 * time.Hour, 5 * time.Minute},
		{2 * 24 * time.Hour, time.Hour},
		{40 * 24 * time.Hour, 24 * time.Hour},
	} {
		if got := timelineStep(tc.span); got != tc.want {
			t.Errorf("span %v: passo %v, esperado %v", tc.span, got, tc.want)
		}
	}
}
