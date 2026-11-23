package service

import (
	"errors"
	"testing"
	"time"

	"sessionflow/internal/repository/sqlite"
)

func noFirst() (time.Time, error) { return time.Time{}, nil }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, brt) }

func TestWindowPeriods(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, brt)
	for _, c := range []struct {
		period string
		n      int
	}{{"7d", 7}, {"30d", 30}, {"90d", 90}} {
		w, err := resolveWindow(now, c.period, "", "", noFirst)
		if err != nil {
			t.Fatal(err)
		}
		if !w.From.Equal(day(2026, 11, 23-c.n)) || !w.To.Equal(day(2026, 11, 23)) || len(w.Keys) != c.n || w.Bucket != sqlite.BucketDay {
			t.Errorf("%s: %v–%v, %d intervalos, %s", c.period, w.From, w.To, len(w.Keys), w.Bucket)
		}
		if !w.PrevFrom.Equal(w.From.AddDate(0, 0, -c.n)) || !w.PrevTo.Equal(w.From) || len(w.PrevKeys) != c.n {
			t.Errorf("%s anterior: %v–%v", c.period, w.PrevFrom, w.PrevTo)
		}
		// Totais do anterior: mesmo tempo decorrido (regra do Dashboard).
		if !w.PrevTotalsFrom.Equal(w.From.Add(-now.Sub(w.From))) || !w.PrevTotalsTo.Equal(w.From) {
			t.Errorf("%s totais anteriores: %v–%v", c.period, w.PrevTotalsFrom, w.PrevTotalsTo)
		}
	}
}

func TestWindowTodayAfterMidnight(t *testing.T) {
	now := time.Date(2026, 11, 22, 0, 10, 0, 0, brt)
	w, err := resolveWindow(now, "today", "", "", noFirst)
	if err != nil {
		t.Fatal(err)
	}
	// A série vai só até a hora atual: horas futuras não viram zeros.
	if w.Bucket != sqlite.BucketHour || len(w.Keys) != 1 || w.Keys[0] != "2026-11-22T00" || !w.From.Equal(day(2026, 11, 22)) {
		t.Errorf("hoje: %s %d %v %v", w.Bucket, len(w.Keys), w.Keys, w.From)
	}
	if len(w.Starts) != 1 || len(w.PrevKeys) != 1 || w.PrevKeys[0] != "2026-11-21T00" {
		t.Errorf("anterior: %v (starts %d)", w.PrevKeys, len(w.Starts))
	}
	w2, _ := resolveWindow(time.Date(2026, 11, 22, 15, 30, 0, 0, brt), "today", "", "", noFirst)
	if len(w2.Keys) != 16 || w2.Keys[len(w2.Keys)-1] != "2026-11-22T15" || len(w2.PrevKeys) != 16 {
		t.Errorf("15h30: %d intervalos (último %v), anterior %d", len(w2.Keys), w2.Keys[len(w2.Keys)-1], len(w2.PrevKeys))
	}
	// Hoje compara com ontem até a mesma hora (00:00–00:10), não com o fim
	// de ontem.
	if !w.PrevTotalsFrom.Equal(day(2026, 11, 21)) || !w.PrevTotalsTo.Equal(day(2026, 11, 21).Add(10*time.Minute)) {
		t.Errorf("totais anteriores %v–%v; quer ontem 00:00–00:10", w.PrevTotalsFrom, w.PrevTotalsTo)
	}
}

func TestWindowThisMonth(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, brt)
	w, err := resolveWindow(now, "month", "", "", noFirst)
	if err != nil {
		t.Fatal(err)
	}
	if !w.From.Equal(day(2026, 11, 1)) || !w.To.Equal(day(2026, 11, 23)) || len(w.Keys) != 22 {
		t.Errorf("este mês: %v–%v %d", w.From, w.To, len(w.Keys))
	}
	if !w.PrevFrom.Equal(day(2026, 10, 1)) || !w.PrevTo.Equal(day(2026, 11, 1)) {
		t.Errorf("anterior: %v–%v", w.PrevFrom, w.PrevTo)
	}
	if want := day(2026, 10, 1).Add(now.Sub(w.From)); !w.PrevTotalsFrom.Equal(day(2026, 10, 1)) || !w.PrevTotalsTo.Equal(want) {
		t.Errorf("totais anteriores %v–%v; quer out/01–%v", w.PrevTotalsFrom, w.PrevTotalsTo, want)
	}
}

func TestWindowPrevMonthJanuary(t *testing.T) {
	now := time.Date(2027, 1, 15, 9, 0, 0, 0, brt)
	w, err := resolveWindow(now, "prev_month", "", "", noFirst)
	if err != nil {
		t.Fatal(err)
	}
	if !w.From.Equal(day(2026, 12, 1)) || !w.To.Equal(day(2027, 1, 1)) || len(w.Keys) != 31 {
		t.Errorf("mês anterior: %v–%v %d", w.From, w.To, len(w.Keys))
	}
	if !w.PrevTotalsFrom.Equal(day(2026, 11, 1)) || !w.PrevTotalsTo.Equal(day(2026, 12, 1)) {
		t.Errorf("comparação: %v–%v; quer novembro inteiro", w.PrevTotalsFrom, w.PrevTotalsTo)
	}
}

func TestWindowCustom(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, brt)
	w, err := resolveWindow(now, "custom", "2026-11-01", "2026-11-10", noFirst)
	if err != nil {
		t.Fatal(err)
	}
	if !w.From.Equal(day(2026, 11, 1)) || !w.To.Equal(day(2026, 11, 11)) || len(w.Keys) != 10 {
		t.Errorf("personalizado: %v–%v %d", w.From, w.To, len(w.Keys))
	}
	if !w.PrevTotalsFrom.Equal(day(2026, 10, 22)) || !w.PrevTotalsTo.Equal(day(2026, 11, 1)) {
		t.Errorf("anterior: %v–%v", w.PrevTotalsFrom, w.PrevTotalsTo)
	}
	// Mais de 92 dias: semanas.
	w, _ = resolveWindow(now, "custom", "2026-01-01", "2026-11-22", noFirst)
	if w.Bucket != sqlite.BucketWeek {
		t.Errorf("intervalo longo em %s", w.Bucket)
	}
}

func TestWindowCustomInvalid(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, brt)
	for _, c := range [][2]string{{"2026-11-10", "2026-11-01"}, {"ontem", "hoje"}, {"", "2026-11-01"}} {
		if _, err := resolveWindow(now, "custom", c[0], c[1], noFirst); !errors.Is(err, ErrInvalidPeriod) {
			t.Errorf("%v: %v", c, err)
		}
	}
	if _, err := resolveWindow(now, "semana", "", "", noFirst); !errors.Is(err, ErrInvalidPeriod) {
		t.Errorf("período desconhecido: %v", err)
	}
}
