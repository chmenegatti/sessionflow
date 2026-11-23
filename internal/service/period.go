package service

import (
	"time"

	"sessionflow/internal/repository/sqlite"
)

// Window é o recorte de tempo de uma consulta do Dashboard ou do Analytics.
// Limites em meia-noite local (fuso do sistema operacional).
type Window struct {
	Period   string
	From, To time.Time // séries: [From, To), intervalos inteiros
	Bucket   sqlite.Bucket
	Keys     []string    // início de cada intervalo (AAAA-MM-DD ou sqlite.HourKey)
	Starts   []time.Time // idem, como horário local
	Offset   int         // segundos do fuso local
	Now      time.Time

	// Período anterior para comparação; zero = sem comparação ("all").
	PrevFrom, PrevTo             time.Time // série anterior: intervalos inteiros
	PrevKeys                     []string
	PrevTotalsFrom, PrevTotalsTo time.Time // totais anteriores: mesma duração já decorrida
}

// HasPrev diz se há período anterior.
func (w Window) HasPrev() bool { return !w.PrevFrom.IsZero() }

// daysUpToDaily: intervalo personalizado de até 92 dias é por dia; acima,
// por semana (até weeksUpToDays) e depois por mês.
const daysUpToDaily = 92

// resolveWindow calcula a janela de um período: today, 7d, 30d, 90d, month
// (este mês), prev_month, custom (from/to "AAAA-MM-DD", inclusivos) ou all
// (desde a primeira atividade, que first informa).
func resolveWindow(now time.Time, period, from, to string, first func() (time.Time, error)) (Window, error) {
	_, off := now.Zone()
	today := dayStart(now)
	tomorrow := today.AddDate(0, 0, 1)
	w := Window{Period: period, Offset: off, Now: now, Bucket: sqlite.BucketDay}

	switch period {
	case "today":
		w.From, w.To, w.Bucket = today, tomorrow, sqlite.BucketHour
		w.PrevFrom, w.PrevTo = today.AddDate(0, 0, -1), today
	case "7d", "30d", "90d":
		n := periods[period]
		w.From, w.To = today.AddDate(0, 0, -(n-1)), tomorrow
		w.PrevFrom, w.PrevTo = w.From.AddDate(0, 0, -n), w.From
	case "month":
		w.From, w.To = monthStart(today), tomorrow
		w.PrevFrom, w.PrevTo = w.From.AddDate(0, -1, 0), w.From
	case "prev_month":
		w.To = monthStart(today)
		w.From = w.To.AddDate(0, -1, 0)
		w.PrevFrom, w.PrevTo = w.From.AddDate(0, -1, 0), w.From
	case "custom":
		a, err1 := time.ParseInLocation(time.DateOnly, from, now.Location())
		b, err2 := time.ParseInLocation(time.DateOnly, to, now.Location())
		if err1 != nil || err2 != nil || b.Before(a) {
			return w, ErrInvalidPeriod
		}
		w.From, w.To = a, b.AddDate(0, 0, 1)
		days := daysBetween(w.From, w.To)
		w.PrevFrom, w.PrevTo = w.From.AddDate(0, 0, -days), w.From
		w.Bucket = bucketFor(days)
	case "all":
		f, err := first()
		if err != nil {
			return w, err
		}
		w.To = tomorrow
		if f.IsZero() {
			w.From = today.AddDate(0, 0, -29) // sem histórico: um mês vazio, por dia
		} else {
			f = dayStart(f.In(now.Location()))
			w.Bucket = sqlite.BucketWeek
			if today.Sub(f) > weeksUpToDays*24*time.Hour {
				w.Bucket = sqlite.BucketMonth
			}
			w.From = bucketStart(f, w.Bucket)
		}
		w.Keys, w.Starts = buckets(w.From, w.To, w.Bucket)
		return w, nil
	default:
		return w, ErrInvalidPeriod
	}

	w.Keys, w.Starts = buckets(w.From, w.To, w.Bucket)
	w.PrevKeys, _ = buckets(w.PrevFrom, w.PrevTo, w.Bucket)
	if period == "today" {
		// A série de hoje vai até a hora atual: horas futuras não viram zeros
		// (nem diluem a média). O anterior é cortado igual, para comparar.
		n := 0
		for n < len(w.Starts) && !w.Starts[n].After(now) {
			n++
		}
		w.Keys, w.Starts = w.Keys[:n], w.Starts[:n]
		w.PrevKeys = w.PrevKeys[:min(n, len(w.PrevKeys))]
	}

	// Totais anteriores: o mesmo tempo já decorrido do período atual, para
	// não comparar um período em andamento com um inteiro. Mês contra mês
	// segue o calendário (o anterior começa no dia 1).
	elapsed := minTime(now, w.To).Sub(w.From)
	switch period {
	case "today", "month":
		// Ontem (ou o mês anterior) até o mesmo ponto do dia (ou do mês).
		w.PrevTotalsFrom, w.PrevTotalsTo = w.PrevFrom, minTime(w.PrevFrom.Add(elapsed), w.PrevTo)
	case "prev_month":
		w.PrevTotalsFrom, w.PrevTotalsTo = w.PrevFrom, w.PrevTo
	default:
		w.PrevTotalsFrom, w.PrevTotalsTo = w.From.Add(-elapsed), w.From
	}
	return w, nil
}

func bucketFor(days int) sqlite.Bucket {
	switch {
	case days <= daysUpToDaily:
		return sqlite.BucketDay
	case days <= weeksUpToDays:
		return sqlite.BucketWeek
	}
	return sqlite.BucketMonth
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func daysBetween(a, b time.Time) int {
	n := 0
	for t := a; t.Before(b); t = t.AddDate(0, 0, 1) {
		n++
	}
	return n
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
