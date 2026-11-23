package service

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"slices"
	"sort"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// resumeGap: pausa mínima, dentro de uma sessão, para a mensagem seguinte
// contar como retomada (docs/ANALYTICS.md §Padrões).
const resumeGap = 30 * time.Minute

// calendarMaxDays: o calendário mostra no máximo o último ano do período.
const calendarMaxDays = 366

// BucketCount é uma faixa de uma distribuição.
type BucketCount struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Distribution resume valores: mediana e p90 (nearest-rank) e faixas.
type Distribution struct {
	Median  int64         `json:"median"`
	P90     int64         `json:"p90"`
	Count   int           `json:"count"`
	Buckets []BucketCount `json:"buckets"`
}

// Frequency: dias e semanas com atividade e sequências de dias seguidos.
type Frequency struct {
	ActiveDays           int     `json:"activeDays"`
	ActiveWeeks          int     `json:"activeWeeks"`
	Days                 int     `json:"days"` // dias do período até hoje
	CurrentStreak        int     `json:"currentStreak"`
	LongestStreak        int     `json:"longestStreak"`
	SessionsPerActiveDay float64 `json:"sessionsPerActiveDay"`
}

// DayCount é um dia do calendário.
type DayCount struct {
	Day      string `json:"day"` // AAAA-MM-DD local
	Sessions int    `json:"sessions"`
	Messages int    `json:"messages"`
}

// ProjectActivity é a atividade de um projeto no período.
type ProjectActivity struct {
	Path          string `json:"path"`
	Name          string `json:"name"`
	Sessions      int    `json:"sessions"`
	Messages      int    `json:"messages"`
	ActiveDays    int    `json:"activeDays"`
	DaysSinceLast int    `json:"daysSinceLast"` // até a data de referência; -1 = sem atividade antes dela
}

// ProviderCount: sessões de um agente.
type ProviderCount struct {
	Provider domain.ProviderID `json:"provider"`
	Sessions int               `json:"sessions"`
}

// ProjectProviders é um projeto com sessões de mais de um agente.
type ProjectProviders struct {
	Path      string          `json:"path"`
	Name      string          `json:"name"`
	Providers []ProviderCount `json:"providers"`
}

// ProviderPattern compara os agentes.
type ProviderPattern struct {
	Provider       domain.ProviderID `json:"provider"`
	Sessions       int               `json:"sessions"`
	Messages       int               `json:"messages"`
	ActiveDays     int               `json:"activeDays"`
	MedianDuration int64             `json:"medianDuration"` // ms
}

// PatternsView são as métricas de atividade observada. Não medem
// produtividade, qualidade, esforço ou valor entregue.
type PatternsView struct {
	Window             WindowInfo         `json:"window"`
	Started            Metric             `json:"started"`
	StartedSeries      []SeriesPoint      `json:"startedSeries"`
	PrevStarted        []int              `json:"prevStarted"`
	Resumed            Metric             `json:"resumed"`
	Frequency          Frequency          `json:"frequency"`
	Calendar           []DayCount         `json:"calendar"`
	TopProjects        []ProjectActivity  `json:"topProjects"`
	RecurringProjects  int                `json:"recurringProjects"`
	Heatmap            [7][24]int         `json:"heatmap"`
	Duration           Distribution       `json:"duration"`           // ms entre a primeira e a última mensagem
	MessagesPerSession Distribution       `json:"messagesPerSession"` // mensagens com texto no período
	Gaps               Distribution       `json:"gaps"`               // ms entre inícios de sessões consecutivas
	MultiProvider      []ProjectProviders `json:"multiProvider"`
	ByProvider         []ProviderPattern  `json:"byProvider"`
}

// PatternsService calcula os padrões de atividade.
type PatternsService struct {
	index     *sqlite.IndexStore
	analytics *AnalyticsService
	log       *slog.Logger
	save      func(defaultName string) (string, error)
}

// NewPatternsService cria o serviço; save é o diálogo de salvar (CSV).
func NewPatternsService(index *sqlite.IndexStore, analytics *AnalyticsService, log *slog.Logger, save func(string) (string, error)) *PatternsService {
	return &PatternsService{index: index, analytics: analytics, log: log, save: save}
}

const (
	minute = int64(time.Minute / time.Millisecond)
	hour   = 60 * minute
	dayMs  = 24 * hour
)

type band struct {
	label string
	upTo  int64 // exclusivo; MaxInt64 = sem limite
}

var (
	durationBands = []band{{"< 5 min", 5 * minute}, {"5–30 min", 30 * minute}, {"30 min–2 h", 2 * hour}, {"> 2 h", math.MaxInt64}}
	messageBands  = []band{{"1–5", 6}, {"6–20", 21}, {"21–50", 51}, {"> 50", math.MaxInt64}}
	gapBands      = []band{{"< 1 h", hour}, {"1–24 h", dayMs}, {"1–7 dias", 7 * dayMs}, {"> 7 dias", math.MaxInt64}}
)

// distribution: mediana e p90 por nearest-rank (o valor de posição ⌈p·n⌉).
func distribution(vals []int64, bands []band) Distribution {
	d := Distribution{Count: len(vals), Buckets: make([]BucketCount, len(bands))}
	for i, b := range bands {
		d.Buckets[i].Label = b.label
	}
	if len(vals) == 0 {
		return d
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	rank := func(p float64) int64 { return s[int(math.Ceil(p*float64(len(s))))-1] }
	d.Median, d.P90 = rank(0.5), rank(0.9)
	for _, v := range s {
		for i, b := range bands {
			if v < b.upTo {
				d.Buckets[i].Count++
				break
			}
		}
	}
	return d
}

// activityWindow resolve o período da atividade (primeira atividade em
// "all"), igual à tela Atividade.
func (a *AnalyticsService) activityWindow(ctx context.Context, q AnalyticsQuery) (Window, sqlite.StatsFilter, error) {
	if err := validProviders(q.Providers); err != nil {
		return Window{}, sqlite.StatsFilter{}, err
	}
	w, err := resolveWindow(a.now(), q.Period, q.From, q.To, func() (time.Time, error) {
		t, err := a.index.FirstActivity(ctx, q.Providers)
		if err != nil {
			return t, a.fail("primeira atividade", err)
		}
		return t, nil
	})
	if err != nil {
		return w, sqlite.StatsFilter{}, err
	}
	return w, sqlite.StatsFilter{From: w.From, To: w.To, Providers: q.Providers, Project: q.Project, Bucket: w.Bucket, Offset: w.Offset}, nil
}

func localDay(ms int64, loc *time.Location) time.Time {
	return dayStart(time.UnixMilli(ms).In(loc))
}

// Patterns calcula as métricas de atividade do recorte.
func (p *PatternsService) Patterns(ctx context.Context, q AnalyticsQuery) (PatternsView, error) {
	a := p.analytics
	w, f, err := a.activityWindow(ctx, q)
	if err != nil {
		return PatternsView{}, err
	}
	now := w.Now
	loc := now.Location()
	v := PatternsView{Window: info(w), PrevStarted: []int{}, Calendar: []DayCount{}, TopProjects: []ProjectActivity{},
		MultiProvider: []ProjectProviders{}, ByProvider: []ProviderPattern{}}

	facts, err := p.index.SessionFacts(ctx, f, resumeGap)
	if err != nil {
		return v, a.fail("fatos das sessões", err)
	}
	df := f
	df.Zones = zoneSpans(w.From, w.To, loc)
	days, err := p.index.DailyActivity(ctx, df)
	if err != nil {
		return v, a.fail("atividade diária", err)
	}
	starts, err := p.index.SessionStarts(ctx, f)
	if err != nil {
		return v, a.fail("inícios", err)
	}
	if v.Heatmap, err = p.index.ActivityHeatmap(ctx, f); err != nil {
		return v, a.fail("mapa de calor", err)
	}

	// Iniciadas e retomadas, com o anterior (mesmo tempo decorrido).
	v.Started.Value = len(starts)
	for _, x := range facts {
		if x.ResumedAt != nil {
			v.Resumed.Value++
		}
	}
	v.StartedSeries = make([]SeriesPoint, len(w.Starts))
	for i, t := range w.Starts {
		v.StartedSeries[i] = SeriesPoint{Start: t.UnixMilli()}
	}
	addTo := func(series []int, startsAt []time.Time, ms int64) {
		t := time.UnixMilli(ms)
		i := sort.Search(len(startsAt), func(i int) bool { return startsAt[i].After(t) }) - 1
		if i >= 0 && i < len(series) {
			series[i]++
		}
	}
	counts := make([]int, len(w.Starts))
	for _, s := range starts {
		addTo(counts, w.Starts, s)
	}
	for i := range counts {
		v.StartedSeries[i].Total = counts[i]
	}
	if w.HasPrev() {
		pf := f
		pf.From, pf.To = w.PrevTotalsFrom, w.PrevTotalsTo
		prevStarts, err := p.index.SessionStarts(ctx, pf)
		if err != nil {
			return v, a.fail("inícios anteriores", err)
		}
		prevFacts, err := p.index.SessionFacts(ctx, pf, resumeGap)
		if err != nil {
			return v, a.fail("fatos anteriores", err)
		}
		v.Started.Prev, v.Started.HasPrev = len(prevStarts), true
		for _, x := range prevFacts {
			if x.ResumedAt != nil {
				v.Resumed.Prev++
			}
		}
		v.Resumed.HasPrev = true
		// Série anterior: intervalos inteiros, como no Dashboard.
		sf := f
		sf.From, sf.To = w.PrevFrom, w.PrevTo
		full, err := p.index.SessionStarts(ctx, sf)
		if err != nil {
			return v, a.fail("série anterior", err)
		}
		_, prevStartsAt := buckets(w.PrevFrom, w.PrevTo, w.Bucket)
		prevStartsAt = prevStartsAt[:min(len(prevStartsAt), len(w.PrevKeys))]
		v.PrevStarted = make([]int, len(prevStartsAt))
		for _, s := range full {
			addTo(v.PrevStarted, prevStartsAt, s)
		}
	}

	// Frequência, calendário e sequências (data de referência: fim do
	// período limitado a agora).
	ref := dayStart(minTime(now, w.To.Add(-time.Millisecond)))
	perDay := map[string]*DayCount{}
	projDays := map[string]map[string]bool{}
	provDays := map[domain.ProviderID]map[string]bool{}
	for _, d := range days {
		c := perDay[d.Day]
		if c == nil {
			c = &DayCount{Day: d.Day}
			perDay[d.Day] = c
		}
		c.Sessions += d.Sessions
		c.Messages += d.Messages
		if d.Project != "" {
			if projDays[d.Project] == nil {
				projDays[d.Project] = map[string]bool{}
			}
			projDays[d.Project][d.Day] = true
		}
		if provDays[d.Provider] == nil {
			provDays[d.Provider] = map[string]bool{}
		}
		provDays[d.Provider][d.Day] = true
	}
	v.Frequency.ActiveDays = len(perDay)
	v.Frequency.Days = daysBetween(w.From, minTime(w.To, dayStart(now).AddDate(0, 0, 1)))
	weeks := map[string]bool{}
	run := 0
	calFrom := w.From
	if calTo := minTime(w.To, dayStart(now).AddDate(0, 0, 1)); daysBetween(calFrom, calTo) > calendarMaxDays {
		calFrom = calTo.AddDate(0, 0, -calendarMaxDays)
	}
	for t := w.From; t.Before(w.To) && !t.After(ref); t = t.AddDate(0, 0, 1) {
		key := t.Format(time.DateOnly)
		c := perDay[key]
		if !t.Before(calFrom) {
			if c != nil {
				v.Calendar = append(v.Calendar, *c)
			} else {
				v.Calendar = append(v.Calendar, DayCount{Day: key})
			}
		}
		if c == nil {
			run = 0
			continue
		}
		weeks[bucketStart(t, sqlite.BucketWeek).Format(time.DateOnly)] = true
		run++
		v.Frequency.LongestStreak = max(v.Frequency.LongestStreak, run)
	}
	// Hoje ainda sem atividade não interrompe a sequência: ela vale até ontem.
	refAt := minTime(now, w.To.Add(-time.Millisecond)) // horário (não meia-noite): a data civil sai certa no fuso
	v.Frequency.CurrentStreak = currentStreak(func(t time.Time) bool { return perDay[t.Format(time.DateOnly)] != nil },
		w.From, refAt, refAt.Format(time.DateOnly) == now.Format(time.DateOnly))
	v.Frequency.ActiveWeeks = len(weeks)
	if v.Frequency.ActiveDays > 0 {
		v.Frequency.SessionsPerActiveDay = math.Round(float64(len(facts))/float64(v.Frequency.ActiveDays)*10) / 10
	}

	// Projetos, agentes e distribuições a partir dos fatos.
	type projAcc struct {
		ProjectActivity
		prov map[domain.ProviderID]int
	}
	projects := map[string]*projAcc{}
	type provAcc struct {
		ProviderPattern
		durations []int64
	}
	provs := map[domain.ProviderID]*provAcc{}
	var durations, messages []int64
	for _, x := range facts {
		messages = append(messages, int64(x.MessagesInPeriod))
		var dur int64 = -1
		if x.FirstTs != nil && x.LastTs != nil {
			dur = *x.LastTs - *x.FirstTs
			durations = append(durations, dur)
		}
		pa := provs[x.Provider]
		if pa == nil {
			pa = &provAcc{ProviderPattern: ProviderPattern{Provider: x.Provider}}
			provs[x.Provider] = pa
		}
		pa.Sessions++
		pa.Messages += x.MessagesInPeriod
		if dur >= 0 {
			pa.durations = append(pa.durations, dur)
		}
		if x.Project == "" {
			continue
		}
		pj := projects[x.Project]
		if pj == nil {
			pj = &projAcc{ProjectActivity: ProjectActivity{Path: x.Project, Name: x.ProjectName, DaysSinceLast: -1}, prov: map[domain.ProviderID]int{}}
			projects[x.Project] = pj
		}
		pj.Sessions++
		pj.Messages += x.MessagesInPeriod
		pj.prov[x.Provider]++
	}
	v.Duration = distribution(durations, durationBands)
	v.MessagesPerSession = distribution(messages, messageBands)
	var gaps []int64
	for i := 1; i < len(starts); i++ {
		gaps = append(gaps, starts[i]-starts[i-1])
	}
	v.Gaps = distribution(gaps, gapBands)

	last, err := p.index.LastActivityByProject(ctx, q.Providers, ref.AddDate(0, 0, 1))
	if err != nil {
		return v, a.fail("última atividade", err)
	}
	for path, pj := range projects {
		pj.ActiveDays = len(projDays[path])
		if pj.ActiveDays >= 2 {
			v.RecurringProjects++
		}
		if ts, ok := last[path]; ok {
			pj.DaysSinceLast = daysBetween(localDay(ts, loc), ref)
		}
		v.TopProjects = append(v.TopProjects, pj.ProjectActivity)
		if len(pj.prov) >= 2 {
			mp := ProjectProviders{Path: path, Name: pj.Name}
			for _, id := range domain.AllProviders() {
				if n := pj.prov[id]; n > 0 {
					mp.Providers = append(mp.Providers, ProviderCount{Provider: id, Sessions: n})
				}
			}
			v.MultiProvider = append(v.MultiProvider, mp)
		}
	}
	slices.SortFunc(v.TopProjects, func(a, b ProjectActivity) int {
		return cmp.Or(cmp.Compare(b.Sessions, a.Sessions), cmp.Compare(b.Messages, a.Messages), cmp.Compare(a.Path, b.Path))
	})
	v.TopProjects = v.TopProjects[:min(len(v.TopProjects), analyticsTopN)]
	slices.SortFunc(v.MultiProvider, func(a, b ProjectProviders) int { return cmp.Compare(a.Path, b.Path) })
	for _, id := range domain.AllProviders() {
		if pa := provs[id]; pa != nil {
			pa.ActiveDays = len(provDays[id])
			pa.MedianDuration = distribution(pa.durations, durationBands).Median
			v.ByProvider = append(v.ByProvider, pa.ProviderPattern)
		}
	}
	return v, nil
}

// currentStreak conta os dias civis seguidos com atividade que terminam na
// data de ref (qualquer horário do dia), sem passar da data de from. Com today, ref é o dia de
// hoje: se ainda não teve atividade, a sequência é a que terminou ontem
// (o dia não acabou). Anda por data civil, ao meio-dia (AddDate), não por
// 24 h: dias de 23 ou 25 h contam como um dia, e uma meia-noite que não
// existe (início do horário de verão em São Paulo até 2018) não pula nem
// repete data.
func currentStreak(active func(day time.Time) bool, from, ref time.Time, today bool) int {
	noon := func(t time.Time) time.Time {
		y, m, d := t.Date()
		return time.Date(y, m, d, 12, 0, 0, 0, t.Location())
	}
	d, from := noon(ref), noon(from)
	if !active(d) {
		if !today {
			return 0
		}
		d = d.AddDate(0, 0, -1)
	}
	n := 0
	for !d.Before(from) && active(d) {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
}

// zoneSpans: os trechos de deslocamento constante do fuso entre from e to
// (time.ZoneBounds), para o dia civil de cada mensagem. Limitado a 1000
// trechos (2 por ano em fusos com horário de verão).
func zoneSpans(from, to time.Time, loc *time.Location) []sqlite.ZoneSpan {
	t := from.In(loc)
	if from.IsZero() {
		t = to.In(loc).AddDate(-100, 0, 0)
	}
	var out []sqlite.ZoneSpan
	for len(out) < 1000 {
		_, off := t.Zone()
		_, end := t.ZoneBounds()
		if end.IsZero() || !end.Before(to) {
			return append(out, sqlite.ZoneSpan{Until: 1 << 62, Offset: off})
		}
		out = append(out, sqlite.ZoneSpan{Until: end.UnixMilli(), Offset: off})
		t = end
	}
	return out
}
