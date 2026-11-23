package service

import (
	"context"
	"time"

	"sessionflow/internal/repository/sqlite"
)

// Linha do tempo do consumo de uma sessão raiz: os registros já indexados
// (mesma deduplicação e mesma agregação de descendentes da tabela de
// consumo), agrupados por intervalo. Só há ponto onde há registro; nada é
// estimado a partir do texto.

// timelineMaxPoints limita a quantidade de intervalos da duração inteira.
const timelineMaxPoints = 60

var timelineSteps = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// timelineStep: o menor passo que cobre span em até timelineMaxPoints
// intervalos (dia como teto).
func timelineStep(span time.Duration) time.Duration {
	for _, s := range timelineSteps {
		if span/s < timelineMaxPoints {
			return s
		}
	}
	return 24 * time.Hour
}

// TimelinePoint é o consumo de um intervalo com registros.
type TimelinePoint struct {
	Start      int64            `json:"start"` // ms, início do intervalo (fuso local)
	Tokens     sqlite.TokenSums `json:"tokens"`
	Cumulative int64            `json:"cumulative"` // total acumulado até aqui (só registros com horário)
	Resumed    bool             `json:"resumed"`    // começa depois de uma pausa de resumeGap ou mais
}

// SessionTimeline é a linha do tempo de uma sessão raiz no recorte.
// Points + Untimed = Total = a linha da sessão na tabela de consumo.
type SessionTimeline struct {
	StepMs  int64            `json:"stepMs"`
	Points  []TimelinePoint  `json:"points"`
	Untimed sqlite.TokenSums `json:"untimed"` // registros sem horário
	Total   sqlite.TokenSums `json:"total"`
	Resumes int              `json:"resumes"`
}

// SessionTimeline monta a linha do tempo da sessão raiz rootID.
func (a *AnalyticsService) SessionTimeline(ctx context.Context, q AnalyticsQuery, rootID int64) (SessionTimeline, error) {
	tl := SessionTimeline{Points: []TimelinePoint{}}
	w, f, err := a.usageWindow(ctx, q)
	if err != nil {
		return tl, err
	}
	f.Model = q.Model
	recs, err := a.index.UsageSessionRecords(ctx, f, rootID)
	if err != nil {
		return tl, a.fail("linha do tempo", err)
	}
	var first, last int64
	timed := 0
	for _, r := range recs {
		tl.Total.Add(r.Tokens)
		if r.Ts == nil {
			tl.Untimed.Add(r.Tokens)
			continue
		}
		if timed == 0 {
			first = *r.Ts
		}
		last = *r.Ts
		timed++
	}
	step := timelineStep(time.Duration(last-first) * time.Millisecond)
	tl.StepMs = step.Milliseconds()
	loc := w.Now.Location()
	bucket := func(ms int64) int64 {
		t := time.UnixMilli(ms).In(loc)
		day := dayStart(t)
		return day.Add(t.Sub(day).Truncate(step)).UnixMilli()
	}
	var prev, cum int64
	for i, r := range recs[:timed] { // com horário vêm primeiro, em ordem
		b := bucket(*r.Ts)
		resumed := i > 0 && time.Duration(*r.Ts-prev)*time.Millisecond >= resumeGap
		prev = *r.Ts
		if n := len(tl.Points); n == 0 || tl.Points[n-1].Start != b {
			tl.Points = append(tl.Points, TimelinePoint{Start: b})
		}
		p := &tl.Points[len(tl.Points)-1]
		p.Tokens.Add(r.Tokens)
		cum += r.Tokens.Total.Sum
		p.Cumulative = cum
		if resumed {
			p.Resumed = true
			tl.Resumes++
		}
	}
	return tl, nil
}
