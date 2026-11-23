package service

import (
	"context"
	"log/slog"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Limites do Analytics.
const (
	analyticsTopN  = 10
	usagePageSize  = 50
	analyticsGroup = 50 // grupos por modelo/projeto na tela Usage
)

// AnalyticsQuery são os filtros compartilhados pelas telas de Analytics.
// Period: today, 7d, 30d, 90d, month, prev_month, custom (From/To
// "AAAA-MM-DD", inclusivos) ou all. Model só vale para consumo.
type AnalyticsQuery struct {
	Period    string              `json:"period"`
	From      string              `json:"from"`
	To        string              `json:"to"`
	Providers []domain.ProviderID `json:"providers"` // vazio = todos
	Project   string              `json:"project"`   // caminho; vazio = todos
	Model     string              `json:"model"`
}

// WindowInfo descreve o recorte aplicado.
type WindowInfo struct {
	Period  string `json:"period"`
	From    int64  `json:"from"` // ms
	To      int64  `json:"to"`
	Bucket  string `json:"bucket"` // hour | day | week | month
	HasPrev bool   `json:"hasPrev"`
}

// Availability é um campo que um provider não informa por natureza.
type Availability struct {
	Provider domain.ProviderID `json:"provider"`
	Field    string            `json:"field"` // domain.UsageReasoning…
	Reason   string            `json:"reason"`
}

// UsagePoint é o total de tokens de um intervalo, por provider (soma dos
// registros que informam o total).
type UsagePoint struct {
	Start      int64                       `json:"start"`
	ByProvider map[domain.ProviderID]int64 `json:"byProvider"`
	Total      int64                       `json:"total"`
	Records    int                         `json:"records"` // registros no intervalo
	Covered    int                         `json:"covered"` // dos quais informam o total
}

// Overview é a tela inicial do Analytics, orientada ao consumo.
type Overview struct {
	Window      WindowInfo          `json:"window"`
	Totals      sqlite.TokenSums    `json:"totals"`
	Prev        sqlite.TokenSums    `json:"prev"` // mesmo tempo decorrido do período anterior
	ByProvider  []sqlite.UsageGroup `json:"byProvider"`
	Series      []UsagePoint        `json:"series"`
	TopProjects []sqlite.UsageGroup `json:"topProjects"`
	Unavailable []Availability      `json:"unavailable"`
}

// UsageView detalha o consumo.
type UsageView struct {
	Window      WindowInfo          `json:"window"`
	Totals      sqlite.TokenSums    `json:"totals"`
	ByProvider  []sqlite.UsageGroup `json:"byProvider"`
	ByModel     []sqlite.UsageGroup `json:"byModel"`
	ByProject   []sqlite.UsageGroup `json:"byProject"`
	ByOrigin    []sqlite.UsageGroup `json:"byOrigin"`
	Unavailable []Availability      `json:"unavailable"`
}

// UsageSessionPage é uma página da tabela de consumo por sessão.
type UsageSessionPage struct {
	Rows   []sqlite.UsageSessionRow `json:"rows"`
	Total  int                      `json:"total"`
	Offset int                      `json:"offset"`
	Limit  int                      `json:"limit"`
}

// ActivityView é a atividade, com as mesmas regras e consultas do
// Dashboard: só sessões principais e não automáticas.
type ActivityView struct {
	Window         WindowInfo          `json:"window"`
	Sessions       Metric              `json:"sessions"`
	Messages       Metric              `json:"messages"`
	SessionsSeries []SeriesPoint       `json:"sessionsSeries"`
	MessagesSeries []SeriesPoint       `json:"messagesSeries"`
	PrevSessions   []int               `json:"prevSessions"`
	ByProvider     []ProviderShare     `json:"byProvider"`
	ByProject      []sqlite.NamedCount `json:"byProject"` // sessões por projeto
	Heatmap        [7][24]int          `json:"heatmap"`   // mensagens; segunda = 0, hora local
	ActiveDays     int                 `json:"activeDays"`
	Days           int                 `json:"days"` // dias do período até hoje
}

// unavailable: campos que cada provider não informa (docs/ANALYTICS.md).
var unavailable = []Availability{
	{domain.ProviderClaude, domain.UsageReasoning, "O Claude Code não informa reasoning separado: o raciocínio vem dentro da saída."},
	{domain.ProviderCodex, domain.UsageCacheCreation, "O Codex não informa tokens de criação de cache."},
}

// AnalyticsService agrega consumo e atividade. A atividade reaproveita as
// consultas e as regras do Dashboard (StatsService).
type AnalyticsService struct {
	index *sqlite.IndexStore
	stats *StatsService
	log   *slog.Logger
	now   func() time.Time
}

// NewAnalyticsService cria o serviço.
func NewAnalyticsService(index *sqlite.IndexStore, stats *StatsService, log *slog.Logger) *AnalyticsService {
	return &AnalyticsService{index: index, stats: stats, log: log, now: time.Now}
}

// usageWindow resolve o período do consumo ("all" começa no primeiro
// registro de consumo, que pode ser de subagente).
func (a *AnalyticsService) usageWindow(ctx context.Context, q AnalyticsQuery) (Window, sqlite.UsageFilter, error) {
	if err := validProviders(q.Providers); err != nil {
		return Window{}, sqlite.UsageFilter{}, err
	}
	w, err := resolveWindow(a.now(), q.Period, q.From, q.To, func() (time.Time, error) {
		t, err := a.index.UsageFirst(ctx, q.Providers)
		if err != nil {
			return t, a.fail("primeiro registro", err)
		}
		return t, nil
	})
	if err != nil {
		return w, sqlite.UsageFilter{}, err
	}
	f := sqlite.UsageFilter{From: w.From, To: w.To, NoTime: q.Period == "all", Providers: q.Providers,
		Project: q.Project, Bucket: w.Bucket, Offset: w.Offset}
	return w, f, nil
}

func validProviders(ps []domain.ProviderID) error {
	for _, p := range ps {
		if !p.Valid() {
			return ErrInvalidFilter
		}
	}
	return nil
}

func info(w Window) WindowInfo {
	return WindowInfo{Period: w.Period, From: w.From.UnixMilli(), To: w.To.UnixMilli(), Bucket: string(w.Bucket), HasPrev: w.HasPrev()}
}

// Overview: consumo total, comparação, por provider, série e projetos.
func (a *AnalyticsService) Overview(ctx context.Context, q AnalyticsQuery) (Overview, error) {
	w, f, err := a.usageWindow(ctx, q)
	if err != nil {
		return Overview{}, err
	}
	o := Overview{Window: info(w)}
	rows, err := a.index.UsageBreakdown(ctx, f)
	if err != nil {
		return o, a.fail("consumo", err)
	}
	o.Unavailable = unavailableFor(rows)
	o.Totals = sqlite.Sum(rows)
	o.ByProvider = sqlite.Group(rows, sqlite.UsageByProvider, analyticsTopN)
	o.TopProjects = sqlite.Group(rows, sqlite.UsageByProject, analyticsTopN)
	if w.HasPrev() {
		pf := f
		pf.From, pf.To = w.PrevTotalsFrom, w.PrevTotalsTo
		if o.Prev, err = a.index.UsageTotals(ctx, pf); err != nil {
			return o, a.fail("totais anteriores", err)
		}
	}
	pts, err := a.index.UsageSeries(ctx, f)
	if err != nil {
		return o, a.fail("série", err)
	}
	o.Series = make([]UsagePoint, len(w.Keys))
	for i, t := range w.Starts {
		o.Series[i] = UsagePoint{Start: t.UnixMilli(), ByProvider: map[domain.ProviderID]int64{}}
	}
	idx := indexOf(w.Keys)
	for _, p := range pts {
		if i, ok := idx[p.Bucket]; ok {
			o.Series[i].ByProvider[p.Provider] += p.Total.Sum
			o.Series[i].Total += p.Total.Sum
			o.Series[i].Records += p.Records
			o.Series[i].Covered += p.Total.Count
		}
	}
	return o, nil
}

// Usage: consumo por provider, modelo, projeto e origem.
func (a *AnalyticsService) Usage(ctx context.Context, q AnalyticsQuery) (UsageView, error) {
	w, f, err := a.usageWindow(ctx, q)
	if err != nil {
		return UsageView{}, err
	}
	f.Model = q.Model
	rows, err := a.index.UsageBreakdown(ctx, f)
	if err != nil {
		return UsageView{}, a.fail("consumo", err)
	}
	return UsageView{
		Window: info(w), Unavailable: unavailableFor(rows), Totals: sqlite.Sum(rows),
		ByProvider: sqlite.Group(rows, sqlite.UsageByProvider, analyticsGroup),
		ByModel:    sqlite.Group(rows, sqlite.UsageByModel, analyticsGroup),
		ByProject:  sqlite.Group(rows, sqlite.UsageByProject, analyticsGroup),
		ByOrigin:   sqlite.Group(rows, sqlite.UsageByOrigin, analyticsGroup),
	}, nil
}

// UsageSessions: uma página de sessões raiz com consumo consolidado.
func (a *AnalyticsService) UsageSessions(ctx context.Context, q AnalyticsQuery, offset int) (UsageSessionPage, error) {
	_, f, err := a.usageWindow(ctx, q)
	if err != nil {
		return UsageSessionPage{}, err
	}
	f.Model = q.Model
	offset = max(offset, 0)
	rows, total, err := a.index.UsageSessions(ctx, f, offset, usagePageSize)
	if err != nil {
		return UsageSessionPage{}, a.fail("sessões", err)
	}
	if rows == nil {
		rows = []sqlite.UsageSessionRow{}
	}
	return UsageSessionPage{Rows: rows, Total: total, Offset: offset, Limit: usagePageSize}, nil
}

// SessionOrigins detalha uma sessão raiz por origem.
func (a *AnalyticsService) SessionOrigins(ctx context.Context, q AnalyticsQuery, rootID int64) ([]sqlite.OriginShare, error) {
	_, f, err := a.usageWindow(ctx, q)
	if err != nil {
		return nil, err
	}
	f.Model = q.Model
	out, err := a.index.UsageSessionOrigins(ctx, f, rootID)
	if err != nil {
		return nil, a.fail("origens", err)
	}
	return out, nil
}

// Models lista os modelos com consumo (filtro da tela Usage).
func (a *AnalyticsService) Models(ctx context.Context) ([]string, error) {
	ms, err := a.index.UsageModels(ctx)
	if err != nil {
		return nil, a.fail("modelos", err)
	}
	return ms, nil
}

// Activity: mesmas consultas e regras do Dashboard, com filtro de projeto.
func (a *AnalyticsService) Activity(ctx context.Context, q AnalyticsQuery) (ActivityView, error) {
	w, f, err := a.activityWindow(ctx, q)
	if err != nil {
		return ActivityView{}, err
	}
	now := w.Now
	d := Dashboard{PrevSessions: []int{}, PrevMessages: []int{}, ByProvider: []ProviderShare{}}
	if err := a.stats.series(ctx, f, w, &d); err != nil {
		return ActivityView{}, a.fail("séries", err)
	}
	if err := a.stats.totals(ctx, f, w, &d); err != nil {
		return ActivityView{}, a.fail("totais", err)
	}
	v := ActivityView{Window: info(w), Sessions: d.Sessions, Messages: d.Messages,
		SessionsSeries: d.SessionsSeries, MessagesSeries: d.MessagesSeries, PrevSessions: d.PrevSessions, ByProvider: d.ByProvider}
	if v.ByProject, err = a.index.TopProjectsBySessions(ctx, f, analyticsTopN); err != nil {
		return v, a.fail("projetos", err)
	}
	v.ByProject = nonNilCounts(v.ByProject)
	if v.Heatmap, err = a.index.ActivityHeatmap(ctx, f); err != nil {
		return v, a.fail("mapa de calor", err)
	}
	if v.ActiveDays, err = a.index.ActiveDays(ctx, f); err != nil {
		return v, a.fail("dias ativos", err)
	}
	v.Days = daysBetween(w.From, minTime(w.To, dayStart(now).AddDate(0, 0, 1)))
	return v, nil
}

// unavailableFor lista os campos indisponíveis dos providers com consumo
// no recorte.
func unavailableFor(rows []sqlite.BreakdownRow) []Availability {
	present := map[domain.ProviderID]bool{}
	for _, r := range rows {
		present[r.Provider] = true
	}
	out := []Availability{}
	for _, u := range unavailable {
		if present[u.Provider] {
			out = append(out, u)
		}
	}
	return out
}

func (a *AnalyticsService) fail(op string, err error) error {
	if err == ErrInvalidFilter || err == ErrInvalidPeriod || err == errInternal {
		return err
	}
	a.log.Error("analytics", "op", op, "err", err)
	return errInternal
}
