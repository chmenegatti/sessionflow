package service

import (
	"testing"
	"time"
)

// Dias seguidos (hardening, item 6): o dia de hoje ainda sem atividade não
// interrompe a sequência; um dia inteiro sem atividade interrompe.

func (e *statsEnv) streakAt(t *testing.T, now time.Time) int {
	t.Helper()
	e.now = now
	return e.patterns(t, AnalyticsQuery{Period: "30d"}).Frequency.CurrentStreak
}

func TestStreakMorningBeforeFirstActivity(t *testing.T) {
	e := newStatsEnv(t)
	for i, d := range []int{16, 17, 18} { // segunda a quarta
		e.put(t, sessOf(string(rune('a'+i)), "/p"), at(d, 10, 0))
	}
	if got := e.streakAt(t, at(19, 8, 0)); got != 3 {
		t.Errorf("quinta 08h sem atividade: %d, esperado 3", got)
	}
	e.put(t, sessOf("d", "/p"), at(19, 7, 30))
	if got := e.streakAt(t, at(19, 8, 0)); got != 4 {
		t.Errorf("quinta 08h depois da primeira atividade: %d, esperado 4", got)
	}
	if got := e.streakAt(t, at(20, 23, 59)); got != 4 {
		t.Errorf("sexta 23h59 sem atividade: %d, esperado 4", got)
	}
	if got := e.streakAt(t, at(21, 8, 0)); got != 0 {
		t.Errorf("sábado: sexta inteira sem atividade, %d, esperado 0", got)
	}
}

func TestStreakConsecutiveDays(t *testing.T) {
	e := newStatsEnv(t)
	for i, d := range []int{10, 11, 12, 13} {
		e.put(t, sessOf(string(rune('a'+i)), "/p"), at(d, 22, 0))
	}
	if got := e.streakAt(t, at(13, 23, 0)); got != 4 {
		t.Errorf("dias consecutivos: %d, esperado 4", got)
	}
}

func TestStreakAcrossMonthAndYear(t *testing.T) {
	e := newStatsEnv(t)
	for i, d := range []time.Time{at(0, 9, 0) /* 31/10 */, at(1, 9, 0)} {
		e.put(t, sessOf(string(rune('a'+i)), "/p"), d)
	}
	e.put(t, sessOf("c", "/p"), time.Date(2026, 10, 30, 20, 0, 0, 0, brt))
	if got := e.streakAt(t, at(2, 7, 0)); got != 3 {
		t.Errorf("virada de mês: %d, esperado 3", got)
	}
	y := newStatsEnv(t)
	for i, d := range []time.Time{
		time.Date(2025, 12, 30, 12, 0, 0, 0, brt), time.Date(2025, 12, 31, 23, 0, 0, 0, brt), time.Date(2026, 1, 1, 0, 30, 0, 0, brt),
	} {
		y.put(t, sessOf(string(rune('a'+i)), "/p"), d)
	}
	if got := y.streakAt(t, time.Date(2026, 1, 2, 6, 0, 0, 0, brt)); got != 3 {
		t.Errorf("virada de ano: %d, esperado 3", got)
	}
}

// currentStreak com datas civis em fusos com horário de verão.
func TestCurrentStreakDST(t *testing.T) {
	for _, tc := range []struct {
		zone string
		days []string // com atividade
		ref  string   // hoje
		want int
	}{
		// Início do horário de verão em Nova York: 08/03/2026 tem 23 h.
		{"America/New_York", []string{"2026-03-07", "2026-03-08", "2026-03-09"}, "2026-03-10", 3},
		// Fim: 01/11/2026 tem 25 h.
		{"America/New_York", []string{"2026-10-31", "2026-11-01", "2026-11-02"}, "2026-11-02", 3},
		// São Paulo, 04/11/2018: a meia-noite não existiu (00h → 01h).
		{"America/Sao_Paulo", []string{"2018-11-03", "2018-11-04", "2018-11-05"}, "2018-11-06", 3},
		{"America/Sao_Paulo", []string{"2018-11-03", "2018-11-05"}, "2018-11-05", 1},
		{"America/Sao_Paulo", []string{"2018-11-02", "2018-11-03"}, "2018-11-04", 2}, // hoje = o dia sem meia-noite
		{"America/Sao_Paulo", []string{"2018-11-03", "2018-11-04"}, "2018-11-04", 2},
	} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Skipf("fuso %s indisponível: %v", tc.zone, err)
		}
		active := map[string]bool{}
		for _, d := range tc.days {
			active[d] = true
		}
		day := func(s string) time.Time { // 10h: a meia-noite pode não existir
			v, _ := time.ParseInLocation(time.DateTime, s+" 10:00:00", loc)
			return v
		}
		from := day("2018-01-01")
		got := currentStreak(func(t time.Time) bool { return active[t.Format(time.DateOnly)] }, from, day(tc.ref), true)
		if got != tc.want {
			t.Errorf("%s %v hoje %s: %d, esperado %d", tc.zone, tc.days, tc.ref, got, tc.want)
		}
	}
}

// Ponta a ponta: o dia de cada mensagem usa o deslocamento do fuso
// naquele instante, não o de agora (Nova York, fim do horário de verão em
// 01/11/2026).
func TestStreakAcrossDSTEndToEnd(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	e := newStatsEnv(t)
	for i, d := range []time.Time{
		time.Date(2026, 10, 29, 12, 0, 0, 0, ny),
		time.Date(2026, 10, 30, 12, 0, 0, 0, ny),
		time.Date(2026, 10, 31, 0, 30, 0, 0, ny), // EDT (-4); com o offset de agora (EST, -5) cairia em 30/10
	} {
		e.put(t, sessOf(string(rune('a'+i)), "/p"), d)
	}
	if got := e.streakAt(t, time.Date(2026, 11, 1, 10, 0, 0, 0, ny)); got != 3 {
		t.Errorf("sequência na troca de horário: %d, esperado 3", got)
	}
	v := e.patterns(t, AnalyticsQuery{Period: "30d"})
	for _, d := range v.Calendar {
		if d.Day == "2026-10-31" && d.Sessions != 1 {
			t.Errorf("31/10 com %d sessões no calendário, esperado 1", d.Sessions)
		}
	}
}
