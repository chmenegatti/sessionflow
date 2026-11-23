package service

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"sessionflow/internal/config"
	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// EventCostsUpdated: estimativas recalculadas (depois de uma sync ou de uma
// mudança de preço); o frontend recarrega a tela de custos.
const EventCostsUpdated = "costs:updated"

// CostDeps são as dependências do CostService.
type CostDeps struct {
	Index     *sqlite.IndexStore
	Costs     *sqlite.CostStore
	Prices    *sqlite.PriceStore
	Analytics *AnalyticsService
	Config    *ConfigStore
	Log       *slog.Logger
	Notify    func()                                   // emite EventCostsUpdated; nil = não emite
	Save      func(defaultName string) (string, error) // diálogo de salvar; "" = cancelado
}

// CostService estima custos com os preços cadastrados pelo usuário
// (docs/ANALYTICS.md §Custos). Refresh, Recalculate e o cadastro de preços
// passam pelo mesmo mutex: nunca rodam ao mesmo tempo.
type CostService struct {
	d     CostDeps
	mu    sync.Mutex
	now   func() time.Time
	async func(func())
	hook  func(stage string) // só nos testes: pausa num ponto com o mutex preso
}

func (c *CostService) stage(name string) {
	if c.hook != nil {
		c.hook(name)
	}
}

// NewCostService cria o serviço.
func NewCostService(d CostDeps) *CostService {
	return &CostService{d: d, now: time.Now, async: func(fn func()) { go fn() }}
}

// RefreshReport: estimativas novas e atualizadas num Refresh.
type RefreshReport struct {
	Calculated int `json:"calculated"`
	Updated    int `json:"updated"`
}

// RecalcPreview mostra o efeito de Recalculate no recorte: quantas
// estimativas mudam e o total por moeda (deduplicado, como nos relatórios)
// antes e depois. Token identifica o plano da prévia: Recalculate só grava
// se o plano refeito na confirmação tiver o mesmo token.
type RecalcPreview struct {
	Changes int               `json:"changes"`
	Before  []sqlite.CostSums `json:"before"`
	After   []sqlite.CostSums `json:"after"`
	Token   string            `json:"token"`
}

// ErrStalePreview: os dados do recorte mudaram (sincronização, preço ou
// outro recálculo) entre a prévia e a confirmação; nada foi gravado.
var ErrStalePreview = errors.New("os dados mudaram desde a prévia; confira a nova prévia antes de recalcular")

// CostGroup é o custo de um agente, modelo ou projeto, por moeda (Currency
// "" = registros sem custo calculado).
type CostGroup struct {
	Key   string            `json:"key"`
	Label string            `json:"label"`
	Sums  []sqlite.CostSums `json:"sums"`
}

// CostPoint é o custo de um intervalo, por moeda.
type CostPoint struct {
	Start      int64                   `json:"start"`
	ByCurrency map[string]domain.Nanos `json:"byCurrency"`
	Records    int                     `json:"records"`
	Covered    int                     `json:"covered"` // registros com custo calculado
}

// CostView é a tela de custos.
type CostView struct {
	Window     WindowInfo        `json:"window"`
	HasPrices  bool              `json:"hasPrices"`
	Totals     []sqlite.CostSums `json:"totals"` // por moeda, só registros com custo
	Prev       []sqlite.CostSums `json:"prev"`   // mesmo tempo decorrido do período anterior
	Quality    sqlite.CostSums   `json:"quality"`
	ByProvider []CostGroup       `json:"byProvider"`
	ByModel    []CostGroup       `json:"byModel"`
	ByProject  []CostGroup       `json:"byProject"`
	Series     []CostPoint       `json:"series"`
}

// CostSessionPage é uma página da tabela de custo por sessão.
type CostSessionPage struct {
	Rows   []sqlite.CostSessionRow `json:"rows"`
	Total  int                     `json:"total"`
	Offset int                     `json:"offset"`
	Limit  int                     `json:"limit"`
}

// UncalculablePage é uma página das sessões sem custo calculável.
type UncalculablePage struct {
	Rows   []sqlite.UncalculableRow `json:"rows"`
	Total  int                      `json:"total"`
	Offset int                      `json:"offset"`
	Limit  int                      `json:"limit"`
}

// PriceInput é o formulário de preço. Preço vazio ou nil = categoria sem
// preço. ValidFrom e ValidTo são "AAAA-MM-DD" locais, inclusivos; ValidTo
// "" = sem fim.
type PriceInput struct {
	Provider      domain.ProviderID `json:"provider"`
	Model         string            `json:"model"`
	Currency      string            `json:"currency"`
	UnitTokens    int64             `json:"unitTokens"`
	Input         *string           `json:"input"`
	Output        *string           `json:"output"`
	CacheRead     *string           `json:"cacheRead"`
	CacheCreation *string           `json:"cacheCreation"`
	ValidFrom     string            `json:"validFrom"`
	ValidTo       string            `json:"validTo"`
	Source        string            `json:"source"`
	Note          string            `json:"note"`
}

// PriceItem é um preço: a revisão atual (a última) e o histórico, com
// quantas estimativas usam cada revisão.
type PriceItem struct {
	Current domain.PriceRevision   `json:"current"`
	History []domain.PriceRevision `json:"history"` // da mais antiga para a atual
	Uses    map[int64]int          `json:"uses"`
}

func (c *CostService) fail(op string, err error) error {
	if errors.Is(err, domain.ErrInvalidPrice) || err == ErrInvalidFilter || err == ErrInvalidPeriod || err == errInternal {
		return err
	}
	c.d.Log.Error("custos", "op", op, "err", err)
	return errInternal
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{domain.ErrInvalidPrice}, a...)...)
}

// --- Estimativas ---

func matchFor(revs []domain.PriceRevision, it sqlite.UsageItem) *domain.PriceRevision {
	if it.Ts == nil {
		return nil
	}
	return domain.MatchPrice(revs, it.Key.Provider, it.Model, time.UnixMilli(*it.Ts))
}

func (c *CostService) compute(rev *domain.PriceRevision, it sqlite.UsageItem) sqlite.StoredEstimate {
	e := domain.ComputeEstimate(rev, it.Key.Provider, it.Model, it.Ts != nil, it.Tokens)
	return sqlite.StoredEstimate{Key: it.Key, Estimate: e, UsageTs: it.Ts, CalculatedAt: c.now().UTC()}
}

// sameEstimate: mesmo resultado (ignora a data do cálculo).
func sameEstimate(a, b sqlite.StoredEstimate) bool {
	eqI := func(x, y *int64) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	eqN := func(x, y *domain.Nanos) bool { return (x == nil) == (y == nil) && (x == nil || *x == *y) }
	return a.Status == b.Status && eqI(a.RevisionID, b.RevisionID) && a.Currency == b.Currency &&
		a.Tokens.Equal(b.Tokens) && eqN(a.Total, b.Total) && eqN(a.InputCost, b.InputCost) &&
		eqN(a.OutputCost, b.OutputCost) && eqN(a.CacheReadCost, b.CacheReadCost) &&
		eqN(a.CacheCreationCost, b.CacheCreationCost) && slices.Equal(a.Reasons, b.Reasons) && eqI(a.UsageTs, b.UsageTs)
}

// Refresh grava as estimativas que faltam ou ficaram desatualizadas:
//   - registro sem estimativa: preço vigente no horário da resposta;
//   - sem preço ou sem dados: recalcula (não há preço a preservar);
//   - tokens mudaram (resposta crescendo): recalcula com a revisão gravada.
//
// Estimativas completas e parciais com os mesmos tokens nunca mudam aqui.
func (c *CostService) Refresh(ctx context.Context) (RefreshReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rep, err := c.refreshLocked(ctx)
	if err != nil {
		return rep, c.fail("atualizar estimativas", err)
	}
	return rep, nil
}

func (c *CostService) refreshLocked(ctx context.Context) (RefreshReport, error) {
	revs, err := c.d.Prices.Revisions(ctx)
	if err != nil {
		return RefreshReport{}, err
	}
	stored, err := c.d.Prices.Estimates(ctx)
	if err != nil {
		return RefreshReport{}, err
	}
	save, rep, err := c.refreshPlan(ctx, revs, stored)
	if err != nil {
		return rep, err
	}
	if err := c.d.Prices.SaveEstimates(ctx, save); err != nil {
		return rep, err
	}
	if len(save) > 0 {
		c.d.Log.Info("estimativas de custo", "novas", rep.Calculated, "atualizadas", rep.Updated)
	}
	return rep, nil
}

// refreshPlan calcula, sem gravar, o que o Refresh gravaria.
func (c *CostService) refreshPlan(ctx context.Context, revs []domain.PriceRevision, stored map[sqlite.EstimateKey]sqlite.StoredEstimate) ([]sqlite.StoredEstimate, RefreshReport, error) {
	var rep RefreshReport
	items, err := c.d.Index.UsageForEstimate(ctx, sqlite.UsageFilter{NoTime: true})
	if err != nil {
		return nil, rep, err
	}
	byID := map[int64]*domain.PriceRevision{}
	for i := range revs {
		byID[revs[i].ID] = &revs[i]
	}
	var save []sqlite.StoredEstimate
	for _, it := range items {
		old, ok := stored[it.Key]
		var next sqlite.StoredEstimate
		switch {
		case !ok || old.Status == domain.EstimateNoPrice || old.Status == domain.EstimateInsufficient:
			next = c.compute(matchFor(revs, it), it)
		case !old.Tokens.Equal(it.Tokens):
			rev := matchFor(revs, it)
			if p := preserved(byID, old, it); p != nil {
				rev = p // preço preservado, mesmo desativado
			}
			next = c.compute(rev, it)
		default:
			continue
		}
		if ok && sameEstimate(old, next) {
			continue
		}
		save = append(save, next)
		if ok {
			rep.Updated++
		} else {
			rep.Calculated++
		}
	}
	return save, rep, nil
}

// preserved: a revisão gravada na estimativa, se ainda existe e a vigência
// cobre o horário da resposta. Desativada continua valendo (o preço da
// estimativa é preservado); fora da vigência (encerrada depois) não.
func preserved(byID map[int64]*domain.PriceRevision, old sqlite.StoredEstimate, it sqlite.UsageItem) *domain.PriceRevision {
	if old.RevisionID == nil || it.Ts == nil {
		return nil
	}
	r := byID[*old.RevisionID]
	if r == nil || !r.Covers(time.UnixMilli(*it.Ts)) {
		return nil
	}
	return r
}

func (c *CostService) refreshAndNotify() {
	if _, err := c.Refresh(context.Background()); err == nil && c.d.Notify != nil {
		c.d.Notify()
	}
}

// filter resolve o recorte de custos (inclui o modelo).
func (c *CostService) filter(ctx context.Context, q AnalyticsQuery) (Window, sqlite.UsageFilter, error) {
	w, f, err := c.d.Analytics.usageWindow(ctx, q)
	f.Model = q.Model
	return w, f, err
}

// recalcPlan: o que Recalculate gravaria no recorte, sem gravar nada. Parte
// do estado que o Refresh deixaria (calculado em memória: a prévia não
// grava), aplica os preços vigentes a todas as cópias do recorte e soma os
// totais deduplicados antes e depois. writes = pendências do Refresh +
// estimativas que mudam; o token cobre writes, as chaves deduplicadas e os
// totais mostrados.
func (c *CostService) recalcPlan(ctx context.Context, q AnalyticsQuery) ([]sqlite.StoredEstimate, RecalcPreview, error) {
	var p RecalcPreview
	_, f, err := c.filter(ctx, q)
	if err != nil {
		return nil, p, err
	}
	revs, err := c.d.Prices.Revisions(ctx)
	if err != nil {
		return nil, p, err
	}
	stored, err := c.d.Prices.Estimates(ctx)
	if err != nil {
		return nil, p, err
	}
	// Quem ainda não tem estimativa (ou ficou desatualizado) entra antes,
	// pelas regras do Refresh.
	pending, _, err := c.refreshPlan(ctx, revs, stored)
	if err != nil {
		return nil, p, err
	}
	base := maps.Clone(stored)
	writes := map[sqlite.EstimateKey]sqlite.StoredEstimate{}
	for _, e := range pending {
		base[e.Key] = e
		writes[e.Key] = e
	}
	items, err := c.d.Index.UsageForEstimate(ctx, f)
	if err != nil {
		return nil, p, err
	}
	keys, err := c.d.Index.UsageKeys(ctx, f)
	if err != nil {
		return nil, p, err
	}
	next := map[sqlite.EstimateKey]sqlite.StoredEstimate{}
	for _, it := range items {
		e := c.compute(matchFor(revs, it), it)
		next[it.Key] = e
		if old, ok := base[it.Key]; !ok || !sameEstimate(old, e) {
			p.Changes++
			writes[it.Key] = e
		}
	}
	before, after := map[string]*sqlite.CostSums{}, map[string]*sqlite.CostSums{}
	add := func(m map[string]*sqlite.CostSums, e sqlite.StoredEstimate) {
		if e.Total == nil {
			return
		}
		s := m[e.Currency]
		if s == nil {
			s = &sqlite.CostSums{Currency: e.Currency}
			m[e.Currency] = s
		}
		s.Add(sumsOf(e.Estimate))
	}
	for _, k := range keys {
		add(before, base[k])
		add(after, next[k])
	}
	p.Before, p.After = sortedSums(before), sortedSums(after)
	// O token cobre só o recorte (o que a prévia mostra); as pendências do
	// Refresh fora dele são gravadas junto, recalculadas na confirmação.
	var shown []sqlite.StoredEstimate
	for k, e := range writes {
		if _, ok := next[k]; ok {
			shown = append(shown, e)
		}
	}
	p.Token = planToken(shown, stored, keys, p)
	return slices.Collect(maps.Values(writes)), p, nil
}

// planToken resume o plano: cada escrita (chave, estimativa gravada e a
// nova, sem a data do cálculo), as chaves deduplicadas e os totais. Mudou um registro do recorte, um preço ou
// uma estimativa gravada que o plano altera, muda o token.
func planToken(changes []sqlite.StoredEstimate, stored map[sqlite.EstimateKey]sqlite.StoredEstimate, keys []sqlite.EstimateKey, p RecalcPreview) string {
	h := sha256.New()
	write := func(e sqlite.StoredEstimate) {
		i := func(p *int64) any {
			if p == nil {
				return "-"
			}
			return *p
		}
		n := func(p *domain.Nanos) any {
			if p == nil {
				return "-"
			}
			return int64(*p)
		}
		t := e.Tokens
		fmt.Fprintf(h, "%s|%v|%s|%d|%v,%v,%v,%v|%v,%v,%v,%v,%v|%s|%v;", e.Status, i(e.RevisionID), e.Currency, e.UnitTokens,
			i(t.Input), i(t.Output), i(t.CacheRead), i(t.CacheCreation),
			n(e.InputCost), n(e.OutputCost), n(e.CacheReadCost), n(e.CacheCreationCost), n(e.Total),
			strings.Join(e.Reasons, ";"), i(e.UsageTs))
	}
	sorted := slices.Clone(changes)
	slices.SortFunc(sorted, func(a, b sqlite.StoredEstimate) int {
		return cmp.Or(cmp.Compare(a.Key.Provider, b.Key.Provider), cmp.Compare(a.Key.NativeID, b.Key.NativeID), cmp.Compare(a.Key.ResponseID, b.Key.ResponseID))
	})
	for _, e := range sorted {
		fmt.Fprintf(h, "%q|%q|%q>", e.Key.Provider, e.Key.NativeID, e.Key.ResponseID)
		if old, ok := stored[e.Key]; ok {
			write(old)
		} else {
			h.Write([]byte("novo;"))
		}
		write(e)
		h.Write([]byte{'\n'})
	}
	// Respostas que contam nos totais (deduplicadas) e os totais mostrados:
	// registro novo no recorte ou outra cópia contando muda o token.
	ks := slices.Clone(keys)
	slices.SortFunc(ks, func(a, b sqlite.EstimateKey) int {
		return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(a.NativeID, b.NativeID), cmp.Compare(a.ResponseID, b.ResponseID))
	})
	for _, k := range ks {
		fmt.Fprintf(h, "k%q|%q|%q;", k.Provider, k.NativeID, k.ResponseID)
	}
	for _, ss := range [][]sqlite.CostSums{p.Before, p.After} {
		for _, x := range ss {
			fmt.Fprintf(h, "t%+v;", x)
		}
		h.Write([]byte{'|'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sumsOf(e domain.Estimate) sqlite.CostSums {
	v := func(n *domain.Nanos) domain.Nanos {
		if n == nil {
			return 0
		}
		return *n
	}
	s := sqlite.CostSums{Currency: e.Currency, Input: v(e.InputCost), Output: v(e.OutputCost), CacheRead: v(e.CacheReadCost),
		CacheCreation: v(e.CacheCreationCost), Total: v(e.Total), Records: 1}
	switch e.Status {
	case domain.EstimateComplete:
		s.Complete = 1
	case domain.EstimatePartial:
		s.Partial = 1
	case domain.EstimateNoPrice:
		s.NoPrice = 1
	case domain.EstimateInsufficient:
		s.Insufficient = 1
	}
	return s
}

func sortedSums(m map[string]*sqlite.CostSums) []sqlite.CostSums {
	out := []sqlite.CostSums{}
	for _, s := range m {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b sqlite.CostSums) int { return cmp.Compare(a.Currency, b.Currency) })
	return out
}

// PreviewRecalculate mostra o que Recalculate mudaria, sem gravar.
func (c *CostService) PreviewRecalculate(ctx context.Context, q AnalyticsQuery) (RecalcPreview, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, p, err := c.recalcPlan(ctx, q)
	if err != nil {
		return p, c.fail("prévia do recálculo", err)
	}
	return p, nil
}

// Recalculate recalcula com os preços vigentes todas as estimativas do
// recorte, inclusive as cópias descartadas pela deduplicação. token é o da
// prévia que o usuário confirmou: o plano é refeito sob o mutex (nenhuma
// outra escrita de estimativa roda junto) e só é gravado, numa transação,
// se for o mesmo da prévia; senão ErrStalePreview e nada muda.
func (c *CostService) Recalculate(ctx context.Context, q AnalyticsQuery, token string) (RecalcPreview, error) {
	c.mu.Lock()
	changes, p, err := c.recalcPlan(ctx, q)
	if err == nil && (token == "" || p.Token != token) {
		err = ErrStalePreview
	}
	if err == nil {
		c.stage("recalc:save")
		err = c.d.Prices.SaveEstimates(ctx, changes)
	}
	c.mu.Unlock()
	if errors.Is(err, ErrStalePreview) {
		return p, err
	}
	if err != nil {
		return p, c.fail("recalcular", err)
	}
	c.d.Log.Info("estimativas recalculadas", "alteradas", p.Changes)
	if c.d.Notify != nil {
		c.d.Notify()
	}
	return p, nil
}

// --- Relatórios ---

var costBuckets = map[string]sqlite.Bucket{"day": sqlite.BucketDay, "week": sqlite.BucketWeek, "month": sqlite.BucketMonth}

// Costs: total, comparação, qualidade, agrupamentos e série do recorte.
// bucket "" = intervalo automático do período; "day", "week" ou "month"
// força o intervalo da série.
func (c *CostService) Costs(ctx context.Context, q AnalyticsQuery, bucket string) (CostView, error) {
	w, f, err := c.filter(ctx, q)
	if err != nil {
		return CostView{}, err
	}
	if bucket != "" {
		b, ok := costBuckets[bucket]
		if !ok {
			return CostView{}, ErrInvalidFilter
		}
		w.Bucket, f.Bucket = b, b
		w.Keys, w.Starts = buckets(bucketStart(w.From, b), w.To, b)
	}
	v := CostView{Window: info(w), Totals: []sqlite.CostSums{}, Prev: []sqlite.CostSums{}}
	revs, err := c.d.Prices.Revisions(ctx)
	if err != nil {
		return v, c.fail("preços", err)
	}
	v.HasPrices = len(revs) > 0
	rows, err := c.d.Costs.Breakdown(ctx, f, true)
	if err != nil {
		return v, c.fail("custos", err)
	}
	v.Totals, v.Quality = currencyTotals(rows)
	if w.HasPrev() {
		pf := f
		pf.From, pf.To = w.PrevTotalsFrom, w.PrevTotalsTo
		prev, err := c.d.Costs.Breakdown(ctx, pf, false)
		if err != nil {
			return v, c.fail("custos anteriores", err)
		}
		v.Prev, _ = currencyTotals(prev)
	}
	v.ByProvider = groupCosts(rows, func(r sqlite.CostRow) (string, string) { return string(r.Provider), string(r.Provider) }, analyticsTopN)
	v.ByModel = groupCosts(rows, func(r sqlite.CostRow) (string, string) { return r.Model, r.Model }, analyticsGroup)
	v.ByProject = groupCosts(rows, func(r sqlite.CostRow) (string, string) { return r.Project, r.ProjectName }, analyticsGroup)
	v.Series = make([]CostPoint, len(w.Keys))
	for i, t := range w.Starts {
		v.Series[i] = CostPoint{Start: t.UnixMilli(), ByCurrency: map[string]domain.Nanos{}}
	}
	idx := indexOf(w.Keys)
	for _, r := range rows {
		i, ok := idx[r.Bucket]
		if !ok {
			continue
		}
		v.Series[i].Records += r.Sums.Records
		if r.Sums.Currency != "" {
			v.Series[i].ByCurrency[r.Sums.Currency] += r.Sums.Total
			v.Series[i].Covered += r.Sums.Records
		}
	}
	return v, nil
}

// currencyTotals: soma por moeda (só registros com custo) e as contagens de
// qualidade de todos os registros.
func currencyTotals(rows []sqlite.CostRow) ([]sqlite.CostSums, sqlite.CostSums) {
	m := map[string]*sqlite.CostSums{}
	var q sqlite.CostSums
	for _, r := range rows {
		q.Add(r.Sums)
		if r.Sums.Currency == "" {
			continue
		}
		s := m[r.Sums.Currency]
		if s == nil {
			s = &sqlite.CostSums{Currency: r.Sums.Currency}
			m[r.Sums.Currency] = s
		}
		s.Add(r.Sums)
	}
	q.Input, q.Output, q.CacheRead, q.CacheCreation, q.Total = 0, 0, 0, 0, 0 // moedas não se somam
	return sortedSums(m), q
}

func groupCosts(rows []sqlite.CostRow, key func(sqlite.CostRow) (string, string), limit int) []CostGroup {
	type acc struct {
		g   CostGroup
		cur map[string]int
	}
	idx := map[string]*acc{}
	var order []*acc
	for _, r := range rows {
		k, label := key(r)
		a := idx[k]
		if a == nil {
			a = &acc{g: CostGroup{Key: k, Label: label}, cur: map[string]int{}}
			idx[k] = a
			order = append(order, a)
		}
		i, ok := a.cur[r.Sums.Currency]
		if !ok {
			i = len(a.g.Sums)
			a.cur[r.Sums.Currency] = i
			a.g.Sums = append(a.g.Sums, sqlite.CostSums{Currency: r.Sums.Currency})
		}
		a.g.Sums[i].Add(r.Sums)
	}
	out := make([]CostGroup, len(order))
	for i, a := range order {
		slices.SortFunc(a.g.Sums, func(x, y sqlite.CostSums) int { return cmp.Compare(x.Currency, y.Currency) })
		out[i] = a.g
	}
	// Por moeda, nunca somando moedas (sqlite.CompareCostSums).
	slices.SortFunc(out, func(a, b CostGroup) int {
		return cmp.Or(sqlite.CompareCostSums(a.Sums, b.Sums), cmp.Compare(a.Key, b.Key))
	})
	// O limite vale em cada moeda: o grupo fica se estiver entre os limit
	// maiores de qualquer moeda que ele tem (um ranking longo em BRL não
	// esconde o maior gasto em USD de um grupo misto); sem custo, entre os
	// limit primeiros sem custo. A ordem continua a de CompareCostSums.
	keep := map[string]bool{}
	byCur := map[string][]CostGroup{}
	for _, g := range out {
		priced := false
		for _, s := range g.Sums {
			if s.Currency != "" {
				priced = true
				byCur[s.Currency] = append(byCur[s.Currency], g)
			}
		}
		if !priced {
			byCur[""] = append(byCur[""], g)
		}
	}
	totalIn := func(g CostGroup, cur string) domain.Nanos {
		for _, s := range g.Sums {
			if s.Currency == cur {
				return s.Total
			}
		}
		return 0
	}
	for cur, gs := range byCur {
		if cur != "" { // sem custo já está em ordem (respostas, chave)
			slices.SortStableFunc(gs, func(a, b CostGroup) int { return cmp.Compare(totalIn(b, cur), totalIn(a, cur)) })
		}
		for _, g := range gs[:min(limit, len(gs))] {
			keep[g.Key] = true
		}
	}
	kept := out[:0]
	for _, g := range out {
		if keep[g.Key] {
			kept = append(kept, g)
		}
	}
	return kept
}

// CostSessions: sessões raiz com custo, 50 por página.
func (c *CostService) CostSessions(ctx context.Context, q AnalyticsQuery, offset int) (CostSessionPage, error) {
	_, f, err := c.filter(ctx, q)
	if err != nil {
		return CostSessionPage{}, err
	}
	rows, total, err := c.d.Costs.Sessions(ctx, f, max(offset, 0), usagePageSize)
	if err != nil {
		return CostSessionPage{}, c.fail("custo por sessão", err)
	}
	return CostSessionPage{Rows: rows, Total: total, Offset: offset, Limit: usagePageSize}, nil
}

// Uncalculable: sessões raiz com registros sem custo calculável.
func (c *CostService) Uncalculable(ctx context.Context, q AnalyticsQuery, offset int) (UncalculablePage, error) {
	_, f, err := c.filter(ctx, q)
	if err != nil {
		return UncalculablePage{}, err
	}
	rows, total, err := c.d.Costs.Uncalculable(ctx, f, max(offset, 0), usagePageSize)
	if err != nil {
		return UncalculablePage{}, c.fail("sessões sem custo", err)
	}
	return UncalculablePage{Rows: rows, Total: total, Offset: offset, Limit: usagePageSize}, nil
}

// --- Preços ---

// Prices lista os preços: a revisão atual de cada um e o histórico.
func (c *CostService) Prices(ctx context.Context) ([]PriceItem, error) {
	revs, err := c.d.Prices.Revisions(ctx)
	if err != nil {
		return nil, c.fail("preços", err)
	}
	uses, err := c.d.Prices.EstimateUses(ctx)
	if err != nil {
		return nil, c.fail("uso dos preços", err)
	}
	byKey := map[string]*PriceItem{}
	var order []string
	for _, r := range revs { // já em ordem de price_key e criação
		it := byKey[r.PriceKey]
		if it == nil {
			it = &PriceItem{Uses: map[int64]int{}}
			byKey[r.PriceKey] = it
			order = append(order, r.PriceKey)
		}
		it.History = append(it.History, r)
		it.Uses[r.ID] = uses[r.ID]
		if r.ReplacedBy == nil {
			it.Current = r
		}
	}
	out := make([]PriceItem, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	slices.SortFunc(out, func(a, b PriceItem) int {
		return cmp.Or(cmp.Compare(a.Current.Provider, b.Current.Provider), cmp.Compare(strings.ToLower(a.Current.Model), strings.ToLower(b.Current.Model)),
			a.Current.ValidFrom.Compare(b.Current.ValidFrom))
	})
	return out, nil
}

// revision converte o formulário: decimais canônicos, moeda em maiúsculas,
// datas locais (ValidTo inclusivo vira a meia-noite seguinte).
func (c *CostService) revision(in PriceInput) (domain.PriceRevision, error) {
	loc := c.now().Location()
	r := domain.PriceRevision{Provider: in.Provider, Model: strings.TrimSpace(in.Model),
		Currency: strings.ToUpper(strings.TrimSpace(in.Currency)), UnitTokens: in.UnitTokens,
		Source: strings.TrimSpace(in.Source), Note: strings.TrimSpace(in.Note), CreatedAt: c.now().UTC()}
	from, err := time.ParseInLocation(time.DateOnly, in.ValidFrom, loc)
	if err != nil {
		return r, invalid("início da vigência inválido (AAAA-MM-DD)")
	}
	r.ValidFrom = from.UTC()
	if in.ValidTo != "" {
		to, err := time.ParseInLocation(time.DateOnly, in.ValidTo, loc)
		if err != nil {
			return r, invalid("fim da vigência inválido (AAAA-MM-DD)")
		}
		end := to.AddDate(0, 0, 1).UTC()
		r.ValidTo = &end
	}
	for _, p := range []struct {
		src *string
		dst **string
	}{{in.Input, &r.Input}, {in.Output, &r.Output}, {in.CacheRead, &r.CacheRead}, {in.CacheCreation, &r.CacheCreation}} {
		if p.src == nil || strings.TrimSpace(*p.src) == "" {
			continue
		}
		_, canon, err := domain.ParseDecimal(strings.TrimSpace(*p.src))
		if err != nil {
			return r, err
		}
		*p.dst = &canon
	}
	return r, nil
}

func newPriceKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// mutate roda fn sob o mutex e, se der certo, atualiza as estimativas em
// segundo plano (quem estava sem preço passa a ter custo).
func (c *CostService) mutate(op string, fn func(revs []domain.PriceRevision) error) error {
	c.mu.Lock()
	c.stage("mutate")
	revs, err := c.d.Prices.Revisions(context.Background())
	if err == nil {
		err = fn(revs)
	}
	c.mu.Unlock()
	if err != nil {
		if errors.Is(err, sqlite.ErrRevisionReplaced) || errors.Is(err, sqlite.ErrValidityClosed) {
			return invalid("%s: o preço mudou enquanto você editava; recarregue", op)
		}
		return c.fail(op, err)
	}
	c.async(c.refreshAndNotify)
	return nil
}

func find(revs []domain.PriceRevision, id int64) (domain.PriceRevision, error) {
	for _, r := range revs {
		if r.ID == id {
			return r, nil
		}
	}
	return domain.PriceRevision{}, invalid("preço não encontrado")
}

func without(revs []domain.PriceRevision, id int64) []domain.PriceRevision {
	return slices.DeleteFunc(slices.Clone(revs), func(r domain.PriceRevision) bool { return r.ID == id })
}

// AddPrice cadastra um preço novo.
func (c *CostService) AddPrice(ctx context.Context, in PriceInput) (domain.PriceRevision, error) {
	r, err := c.revision(in)
	if err != nil {
		return r, err
	}
	r.PriceKey = newPriceKey()
	err = c.mutate("cadastrar preço", func(revs []domain.PriceRevision) error {
		if err := domain.ValidateRevision(r, revs); err != nil {
			return err
		}
		r.ID, err = c.d.Prices.InsertRevision(ctx, r)
		return err
	})
	return r, err
}

// EditPrice corrige um preço: grava uma revisão nova do mesmo preço. As
// estimativas já calculadas continuam com a revisão antiga (Recalcular
// aplica a nova).
func (c *CostService) EditPrice(ctx context.Context, id int64, in PriceInput) (domain.PriceRevision, error) {
	r, err := c.revision(in)
	if err != nil {
		return r, err
	}
	err = c.mutate("editar preço", func(revs []domain.PriceRevision) error {
		old, err := find(revs, id)
		if err != nil {
			return err
		}
		if !old.Current() {
			return invalid("só a revisão atual e ativa pode ser editada")
		}
		r.PriceKey = old.PriceKey
		if err := domain.ValidateRevision(r, without(revs, id)); err != nil {
			return err
		}
		r.ID, err = c.d.Prices.ReplaceRevision(ctx, id, r)
		return err
	})
	return r, err
}

// NewPriceResult é o preço novo e quantas estimativas já calculadas a
// partir da data dele continuam com o preço anterior (até o Recalcular).
type NewPriceResult struct {
	Revision domain.PriceRevision `json:"revision"`
	Stale    int                  `json:"stale"`
}

// NewPriceFrom registra uma mudança de preço no tempo: encerra a vigência
// do preço id no início de in e cadastra o preço novo a partir dali.
// Estimativas já gravadas não mudam; Stale diz quantas ficaram com o
// preço anterior depois da data.
func (c *CostService) NewPriceFrom(ctx context.Context, id int64, in PriceInput) (NewPriceResult, error) {
	var res NewPriceResult
	r, err := c.revision(in)
	if err != nil {
		return res, err
	}
	r.PriceKey = newPriceKey()
	err = c.mutate("novo preço a partir de", func(revs []domain.PriceRevision) error {
		old, err := find(revs, id)
		if err != nil {
			return err
		}
		if !old.Current() || old.ValidTo != nil {
			return invalid("o preço anterior precisa estar ativo e sem fim de vigência")
		}
		if !r.ValidFrom.After(old.ValidFrom) {
			return invalid("o novo preço deve começar depois do início do anterior")
		}
		closed := old
		closed.ValidTo = &r.ValidFrom
		others := append(without(revs, id), closed)
		if err := domain.ValidateRevision(r, others); err != nil {
			return err
		}
		if r.ID, err = c.d.Prices.CloseAndInsert(ctx, id, r.ValidFrom, c.now().UTC(), r); err != nil {
			return err
		}
		res.Stale, err = c.d.Prices.CountUsingFrom(ctx, id, r.ValidFrom.UnixMilli())
		return err
	})
	res.Revision = r
	return res, err
}

// ClosePriceResult é a revisão com a vigência encerrada e quantas
// estimativas já calculadas depois do fim continuam com ela (até o
// Recalcular, que as deixa sem preço).
type ClosePriceResult struct {
	Revision domain.PriceRevision `json:"revision"`
	Stale    int                  `json:"stale"`
}

// ClosePrice encerra a vigência do preço id no fim do dia last (AAAA-MM-DD
// local, inclusivo) sem cadastrar sucessor: depois dali, sem outro preço
// vigente, o cálculo fica sem preço. Não cria revisão.
func (c *CostService) ClosePrice(ctx context.Context, id int64, last string) (ClosePriceResult, error) {
	var res ClosePriceResult
	day, err := time.ParseInLocation(time.DateOnly, last, c.now().Location())
	if err != nil {
		return res, invalid("fim da vigência inválido (AAAA-MM-DD)")
	}
	end := day.AddDate(0, 0, 1).UTC()
	err = c.mutate("encerrar vigência", func(revs []domain.PriceRevision) error {
		old, err := find(revs, id)
		if err != nil {
			return err
		}
		if !old.Current() || old.ValidTo != nil {
			return invalid("só um preço ativo e sem fim de vigência pode ser encerrado")
		}
		if !end.After(old.ValidFrom) {
			return invalid("o fim da vigência deve ser depois do início")
		}
		at := c.now().UTC()
		c.stage("close:write")
		if err := c.d.Prices.CloseValidity(ctx, id, end, at); err != nil {
			return err
		}
		res.Revision = old
		res.Revision.ValidTo, res.Revision.ClosedAt = &end, &at
		res.Stale, err = c.d.Prices.CountUsingFrom(ctx, id, end.UnixMilli())
		return err
	})
	return res, err
}

// DisablePrice desativa um preço: não vale para cálculos novos; as
// estimativas antigas o mantêm.
func (c *CostService) DisablePrice(ctx context.Context, id int64) error {
	return c.mutate("desativar preço", func(revs []domain.PriceRevision) error {
		old, err := find(revs, id)
		if err != nil {
			return err
		}
		if !old.Current() {
			return invalid("o preço já está desativado ou foi substituído")
		}
		return c.d.Prices.Disable(ctx, id, c.now().UTC())
	})
}

// ReactivatePrice reativa um preço desativado com uma revisão nova.
func (c *CostService) ReactivatePrice(ctx context.Context, id int64) (domain.PriceRevision, error) {
	var r domain.PriceRevision
	err := c.mutate("reativar preço", func(revs []domain.PriceRevision) error {
		old, err := find(revs, id)
		if err != nil {
			return err
		}
		if old.DisabledAt == nil || old.ReplacedBy != nil {
			return invalid("só um preço desativado pode ser reativado")
		}
		r = old
		r.ID, r.DisabledAt, r.ReplacedBy, r.CreatedAt = 0, nil, nil, c.now().UTC()
		if err := domain.ValidateRevision(r, without(revs, id)); err != nil {
			return err
		}
		r.ID, err = c.d.Prices.ReplaceRevision(ctx, id, r)
		return err
	})
	return r, err
}

// DefaultCurrency é a moeda sugerida no cadastro de preços.
func (c *CostService) DefaultCurrency() string {
	return cmp.Or(c.d.Config.Get().DefaultCurrency, "BRL")
}

// SetDefaultCurrency grava a moeda sugerida no config.json.
func (c *CostService) SetDefaultCurrency(_ context.Context, cur string) error {
	cur = strings.ToUpper(strings.TrimSpace(cur))
	err := c.d.Config.Update(func(cfg *config.Config) error {
		cfg.DefaultCurrency = cur
		if err := cfg.Validate(); err != nil {
			return invalid("moeda deve ter 3 letras (ex.: BRL)")
		}
		return nil
	})
	if errors.Is(err, ErrConfigUnreadable) {
		return err
	}
	if err != nil {
		return c.fail("moeda padrão", err)
	}
	return nil
}
