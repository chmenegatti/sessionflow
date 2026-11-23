package service

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Limites do Dashboard.
const (
	topN          = 5
	recentN       = 5
	pinnedN       = 3
	resumeScan    = 20      // sessões recentes examinadas para "Retomar última" e "Abertas"
	weeksUpToDays = 2 * 366 // "Tudo": por semana até ~2 anos de histórico, depois por mês
)

// periods: dias de cada período ("all" é tratado à parte).
var periods = map[string]int{"7d": 7, "30d": 30, "90d": 90}

// StatsQuery recorta o Dashboard. Providers vazio = todos.
type StatsQuery struct {
	Period    string              `json:"period"` // "7d" | "30d" | "90d" | "all"
	Providers []domain.ProviderID `json:"providers"`
}

// Metric é um número do período e o do período anterior de mesmo tamanho.
type Metric struct {
	Value   int  `json:"value"`
	Prev    int  `json:"prev"`
	HasPrev bool `json:"hasPrev"` // false em "all"
}

// SeriesPoint é um intervalo do gráfico. Start em ms (início local).
type SeriesPoint struct {
	Start      int64                     `json:"start"`
	ByProvider map[domain.ProviderID]int `json:"byProvider"`
	Total      int                       `json:"total"`
}

// ProviderShare são as sessões de um agente no período.
type ProviderShare struct {
	Provider domain.ProviderID `json:"provider"`
	Sessions int               `json:"sessions"`
}

// RecentTabs são as abas da tabela de sessões recentes (até 5 cada).
type RecentTabs struct {
	All       []SessionItem `json:"all"`
	Open      []SessionItem `json:"open"`
	Favorites []SessionItem `json:"favorites"`
}

// Dashboard é tudo o que a tela inicial mostra, numa chamada.
type Dashboard struct {
	Period          string              `json:"period"`
	From            int64               `json:"from"`
	To              int64               `json:"to"`
	Bucket          string              `json:"bucket"` // day | week | month
	Sessions        Metric              `json:"sessions"`
	Messages        Metric              `json:"messages"`
	ActiveProjects  Metric              `json:"activeProjects"`
	TotalProjects   int                 `json:"totalProjects"`
	Favorites       int                 `json:"favorites"`
	FavoritesAdded  int                 `json:"favoritesAdded"`
	OpenNow         int                 `json:"openNow"`
	SessionsSeries  []SeriesPoint       `json:"sessionsSeries"`
	MessagesSeries  []SeriesPoint       `json:"messagesSeries"`
	PrevSessions    []int               `json:"prevSessions"` // vazio em "all"
	PrevMessages    []int               `json:"prevMessages"`
	ProjectsSeries  []int               `json:"projectsSeries"`
	FavoritesSeries []int               `json:"favoritesSeries"`
	ByProvider      []ProviderShare     `json:"byProvider"`
	TopProjects     []sqlite.NamedCount `json:"topProjects"`
	TopTools        []sqlite.NamedCount `json:"topTools"`
	Peak            int64               `json:"peak"` // início do intervalo com mais mensagens; 0 = sem atividade
	Recent          RecentTabs          `json:"recent"`
	ResumeID        string              `json:"resumeId"` // sessão principal mais recente não aberta; "" = nenhuma
}

// StatsService agrega o índice para o Dashboard e a barra lateral.
type StatsService struct {
	index    *sqlite.IndexStore
	sessions *SessionService // converte linhas em SessionItem (favorito, tags)
	user     *sqlite.UserStore
	log      *slog.Logger
	now      func() time.Time
}

// NewStatsService cria o serviço.
func NewStatsService(index *sqlite.IndexStore, sessions *SessionService, user *sqlite.UserStore, log *slog.Logger) *StatsService {
	return &StatsService{index: index, sessions: sessions, user: user, log: log, now: time.Now}
}

// Dashboard monta a tela inicial.
func (s *StatsService) Dashboard(ctx context.Context, q StatsQuery) (Dashboard, error) {
	for _, p := range q.Providers {
		if !p.Valid() {
			return Dashboard{}, ErrInvalidFilter
		}
	}
	w, err := s.window(ctx, q)
	if err != nil {
		return Dashboard{}, err
	}
	d := Dashboard{
		Period: q.Period, From: w.From.UnixMilli(), To: w.To.UnixMilli(), Bucket: string(w.Bucket),
		PrevSessions: []int{}, PrevMessages: []int{}, ByProvider: []ProviderShare{},
		Recent: RecentTabs{All: []SessionItem{}, Open: []SessionItem{}, Favorites: []SessionItem{}},
	}
	f := sqlite.StatsFilter{From: w.From, To: w.To, Providers: q.Providers, Bucket: w.Bucket, Offset: w.Offset}
	if err := s.series(ctx, f, w, &d); err != nil {
		return Dashboard{}, s.fail("séries", err)
	}
	if err := s.totals(ctx, f, w, &d); err != nil {
		return Dashboard{}, s.fail("totais", err)
	}
	if err := s.favorites(ctx, q.Providers, w, &d); err != nil {
		return Dashboard{}, s.fail("favoritas", err)
	}
	if err := s.recent(ctx, q.Providers, w, &d); err != nil {
		return Dashboard{}, s.fail("recentes", err)
	}
	return d, nil
}

// PinnedProjects devolve os 3 projetos com mais mensagens nos últimos 30
// dias (barra lateral).
func (s *StatsService) PinnedProjects(ctx context.Context) ([]sqlite.NamedCount, error) {
	now := s.now()
	_, off := now.Zone()
	f := sqlite.StatsFilter{From: dayStart(now).AddDate(0, 0, -29), To: dayStart(now).AddDate(0, 0, 1), Bucket: sqlite.BucketDay, Offset: off}
	top, err := s.index.TopProjects(ctx, f, pinnedN)
	if err != nil {
		return nil, s.fail("projetos fixados", err)
	}
	return nonNilCounts(top), nil
}

func (s *StatsService) window(ctx context.Context, q StatsQuery) (Window, error) {
	return resolveWindow(s.now(), q.Period, "", "", func() (time.Time, error) {
		first, err := s.index.FirstActivity(ctx, q.Providers)
		if err != nil {
			return first, s.fail("primeira atividade", err)
		}
		return first, nil
	})
}

func (s *StatsService) series(ctx context.Context, f sqlite.StatsFilter, w Window, d *Dashboard) error {
	rows, err := s.index.Activity(ctx, f)
	if err != nil {
		return err
	}
	d.SessionsSeries, d.MessagesSeries = points(w), points(w)
	idx := indexOf(w.Keys)
	for _, r := range rows {
		i, ok := idx[r.Bucket]
		if !ok {
			continue
		}
		d.SessionsSeries[i].ByProvider[r.Provider] += r.Sessions
		d.SessionsSeries[i].Total += r.Sessions
		d.MessagesSeries[i].ByProvider[r.Provider] += r.Messages
		d.MessagesSeries[i].Total += r.Messages
	}
	best := 0
	for i, p := range d.MessagesSeries {
		if p.Total > best {
			best, d.Peak = p.Total, w.Starts[i].UnixMilli()
		}
	}
	per, err := s.index.ProjectsPerBucket(ctx, f)
	if err != nil {
		return err
	}
	d.ProjectsSeries = make([]int, len(w.Keys))
	for k, n := range per {
		if i, ok := idx[k]; ok {
			d.ProjectsSeries[i] = n
		}
	}
	if w.PrevFrom.IsZero() {
		return nil
	}
	pf := f
	pf.From, pf.To = w.PrevFrom, w.PrevTo
	prev, err := s.index.Activity(ctx, pf)
	if err != nil {
		return err
	}
	d.PrevSessions, d.PrevMessages = make([]int, len(w.PrevKeys)), make([]int, len(w.PrevKeys))
	pidx := indexOf(w.PrevKeys)
	for _, r := range prev {
		if i, ok := pidx[r.Bucket]; ok {
			d.PrevSessions[i] += r.Sessions
			d.PrevMessages[i] += r.Messages
		}
	}
	return nil
}

func (s *StatsService) totals(ctx context.Context, f sqlite.StatsFilter, w Window, d *Dashboard) error {
	cur, byProv, err := s.index.StatsTotals(ctx, f)
	if err != nil {
		return err
	}
	d.Sessions.Value, d.Messages.Value, d.ActiveProjects.Value = cur.Sessions, cur.Messages, cur.Projects
	if !w.PrevFrom.IsZero() {
		// Mesma duração do período atual até agora: comparar com o anterior
		// inteiro faria a manhã parecer sempre em queda. (As séries do
		// anterior seguem com os intervalos cheios, para o gráfico.)
		pf := f
		pf.From, pf.To = w.PrevTotalsFrom, w.PrevTotalsTo
		prev, _, err := s.index.StatsTotals(ctx, pf)
		if err != nil {
			return err
		}
		d.Sessions.Prev, d.Messages.Prev, d.ActiveProjects.Prev = prev.Sessions, prev.Messages, prev.Projects
		d.Sessions.HasPrev, d.Messages.HasPrev, d.ActiveProjects.HasPrev = true, true, true
	}
	for _, p := range domain.AllProviders() {
		if n := byProv[p]; n > 0 {
			d.ByProvider = append(d.ByProvider, ProviderShare{Provider: p, Sessions: n})
		}
	}
	if d.TotalProjects, err = s.index.TotalProjects(ctx, f.Providers); err != nil {
		return err
	}
	if d.TopProjects, err = s.index.TopProjects(ctx, f, topN); err != nil {
		return err
	}
	if d.TopTools, err = s.index.TopTools(ctx, f, topN); err != nil {
		return err
	}
	d.TopProjects, d.TopTools = nonNilCounts(d.TopProjects), nonNilCounts(d.TopTools)
	return nil
}

// favorites conta as favoritas dos agentes filtrados, as marcadas no
// período e a série por intervalo (data em que foram marcadas).
func (s *StatsService) favorites(ctx context.Context, providers []domain.ProviderID, w Window, d *Dashboard) error {
	all, err := s.user.FavoritesSince(ctx, time.Time{})
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		if key, err := domain.ParseSessionKey(k); err == nil && (len(providers) == 0 || slices.Contains(providers, key.Provider)) {
			keys = append(keys, k)
		}
	}
	exists, err := s.index.ExistingKeys(ctx, keys)
	if err != nil {
		return err
	}
	d.FavoritesSeries = make([]int, len(w.Keys))
	idx := indexOf(w.Keys)
	loc := s.now().Location()
	for _, k := range keys {
		if !exists[k] {
			continue // sessão saiu do índice
		}
		d.Favorites++
		at := all[k].In(loc)
		if at.Before(w.From) || !at.Before(w.To) {
			continue
		}
		d.FavoritesAdded++
		if i, ok := idx[bucketStart(dayStart(at), w.Bucket).Format(time.DateOnly)]; ok {
			d.FavoritesSeries[i]++
		}
	}
	return nil
}

// recent preenche as abas da tabela, "Abertas" e "Retomar última".
func (s *StatsService) recent(ctx context.Context, providers []domain.ProviderID, w Window, d *Dashboard) error {
	base := sqlite.SessionFilter{Providers: providers, UpdatedSince: w.From}
	if err := s.list(ctx, base, recentN, &d.Recent.All); err != nil {
		return err
	}
	favs, err := s.user.FavoriteKeys(ctx)
	if err != nil {
		return err
	}
	fav := base
	fav.Keys, fav.IncludeHidden = favs, true
	if favs == nil {
		fav.Keys = []string{}
	}
	if err := s.list(ctx, fav, recentN, &d.Recent.Favorites); err != nil {
		return err
	}
	// Abertas e "Retomar última" olham as sessões mais recentes de qualquer
	// período: o arquivo pode ter mudado depois da última indexação.
	rows, _, err := s.index.ListSessions(ctx, sqlite.SessionFilter{Providers: providers, Limit: resumeScan})
	if err != nil {
		return err
	}
	now := s.now()
	var open []domain.Session
	for _, r := range rows {
		if sessionActive(r, now) {
			open = append(open, r)
		} else if d.ResumeID == "" && r.Parent == nil {
			d.ResumeID = r.Key.String()
		}
	}
	d.OpenNow = len(open)
	if len(open) > recentN {
		open = open[:recentN]
	}
	items, err := s.sessions.items(ctx, open)
	if err != nil {
		return err
	}
	d.Recent.Open = items
	return nil
}

func (s *StatsService) list(ctx context.Context, f sqlite.SessionFilter, n int, out *[]SessionItem) error {
	f.Limit = n
	rows, _, err := s.index.ListSessions(ctx, f)
	if err != nil {
		return err
	}
	items, err := s.sessions.items(ctx, rows)
	if err != nil {
		return err
	}
	*out = items
	return nil
}

func (s *StatsService) fail(op string, err error) error {
	if err == ErrInvalidFilter || err == ErrInvalidPeriod || err == errInternal {
		return err
	}
	s.log.Error("estatísticas", "op", op, "err", err)
	return errInternal
}

// dayStart devolve a meia-noite local de t.
func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// bucketStart devolve o início do intervalo que contém o dia t.
func bucketStart(t time.Time, b sqlite.Bucket) time.Time {
	switch b {
	case sqlite.BucketWeek:
		back := (int(t.Weekday()) + 6) % 7 // segunda = 0
		return t.AddDate(0, 0, -back)
	case sqlite.BucketMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	}
	return t
}

// buckets lista os intervalos de [from, to), alinhados ao início.
func buckets(from, to time.Time, b sqlite.Bucket) ([]string, []time.Time) {
	var keys []string
	var starts []time.Time
	for t := bucketStart(from, b); t.Before(to); {
		if b == sqlite.BucketHour {
			keys = append(keys, t.Format(sqlite.HourKey))
		} else {
			keys = append(keys, t.Format(time.DateOnly))
		}
		starts = append(starts, t)
		switch b {
		case sqlite.BucketHour:
			t = t.Add(time.Hour)
		case sqlite.BucketWeek:
			t = t.AddDate(0, 0, 7)
		case sqlite.BucketMonth:
			t = t.AddDate(0, 1, 0)
		default:
			t = t.AddDate(0, 0, 1)
		}
	}
	return keys, starts
}

func points(w Window) []SeriesPoint {
	out := make([]SeriesPoint, len(w.Keys))
	for i, t := range w.Starts {
		out[i] = SeriesPoint{Start: t.UnixMilli(), ByProvider: map[domain.ProviderID]int{}}
	}
	return out
}

func indexOf(keys []string) map[string]int {
	m := make(map[string]int, len(keys))
	for i, k := range keys {
		m[k] = i
	}
	return m
}

func nonNilCounts(xs []sqlite.NamedCount) []sqlite.NamedCount {
	if xs == nil {
		return []sqlite.NamedCount{}
	}
	return xs
}
