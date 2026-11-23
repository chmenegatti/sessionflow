package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sessionflow/internal/domain"
	"sessionflow/internal/repository/sqlite"
)

// Concorrência no custo (hardening, item 5). Os cenários controlam a ordem
// com canais (gancho c.svc.hook, que pausa com o mutex preso) e conferem o
// estado final do banco e os invariantes, não só a ausência de pânico:
//   - uma única revisão atual por preço, sem vigências sobrepostas;
//   - toda estimativa aponta para revisão existente;
//   - a estimativa tem os tokens atuais do índice (nada perdido).
// go test -race cobre data race em memória; os invariantes cobrem corrida
// lógica, conflito no banco e atualização perdida nos cenários abaixo.

// pause prende o serviço no estágio stage: reached avisa que chegou (com o
// mutex preso) e release o solta.
func pause(c *costEnv, stage string) (reached, release chan struct{}) {
	reached, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	c.svc.hook = func(s string) {
		if s == stage {
			once.Do(func() {
				close(reached)
				<-release
			})
		}
	}
	return reached, release
}

// checkInvariants confere o estado final de preços e estimativas.
func checkInvariants(t *testing.T, c *costEnv) {
	t.Helper()
	ctx := context.Background()
	revs, err := c.prices.Revisions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	current := map[string]int{}
	for _, r := range revs {
		ids[r.ID] = true
		if r.ReplacedBy == nil {
			current[r.PriceKey]++
		}
	}
	for k, n := range current {
		if n != 1 {
			t.Errorf("preço %s com %d revisões atuais", k, n)
		}
	}
	for _, r := range revs {
		if !r.Current() {
			continue
		}
		if err := domain.ValidateRevision(r, revs); err != nil {
			t.Errorf("revisão %d inválida no estado final: %v", r.ID, err)
		}
	}
	es, err := c.prices.Estimates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items, err := c.svc.d.Index.UsageForEstimate(ctx, sqlite.UsageFilter{NoTime: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		e, ok := es[it.Key]
		if !ok {
			t.Errorf("%v sem estimativa", it.Key)
			continue
		}
		if !e.Tokens.Equal(it.Tokens) {
			t.Errorf("%v com tokens antigos (atualização perdida)", it.Key)
		}
	}
	for k, e := range es {
		if e.RevisionID != nil && !ids[*e.RevisionID] {
			t.Errorf("%v aponta para revisão inexistente %d", k, *e.RevisionID)
		}
	}
}

// Duas edições do mesmo preço ao mesmo tempo: uma grava, a outra é
// recusada; nunca duas sucessoras.
func TestConcurrentEditSamePrice(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	reached, release := pause(c, "mutate")
	first := make(chan error, 1)
	go func() {
		_, err := c.svc.EditPrice(ctx, r.ID, doubled(claudeUSD))
		first <- err
	}()
	<-reached // a primeira edição está com o mutex, antes de ler as revisões
	second := make(chan error, 1)
	go func() {
		_, err := c.svc.EditPrice(ctx, r.ID, claudeUSD) // mesma revisão de origem
		second <- err
	}()
	if c.svc.mu.TryLock() {
		c.svc.mu.Unlock()
		t.Fatal("mutex livre com a primeira edição em andamento")
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("primeira edição: %v", err)
	}
	if err := <-second; !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("segunda edição da mesma revisão: %v, esperado recusa", err)
	}
	revs, _ := c.prices.Revisions(ctx)
	if len(revs) != 2 {
		t.Errorf("%d revisões, esperado 2 (original + uma sucessora)", len(revs))
	}
	checkInvariants(t, c)
}

// Encerrar e "novo preço a partir de" no mesmo preço ao mesmo tempo:
// só um muda a vigência.
func TestConcurrentValidityUpdates(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	next := doubled(claudeUSD)
	next.ValidFrom = "2026-06-01"
	reached, release := pause(c, "mutate")
	first := make(chan error, 1)
	go func() {
		_, err := c.svc.ClosePrice(ctx, r.ID, "2026-03-31")
		first <- err
	}()
	<-reached // o primeiro encerramento está com o mutex
	others := make(chan error, 2)
	go func() { _, err := c.svc.ClosePrice(ctx, r.ID, "2026-09-30"); others <- err }()
	go func() { _, err := c.svc.NewPriceFrom(ctx, r.ID, next); others <- err }()
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("primeiro encerramento: %v", err)
	}
	for range 2 {
		if err := <-others; !errors.Is(err, domain.ErrInvalidPrice) {
			t.Errorf("mudança concorrente de vigência aceita: %v", err)
		}
	}
	revs, _ := c.prices.Revisions(ctx)
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, statNow.Location())
	if len(revs) != 1 || revs[0].ValidTo == nil || !revs[0].ValidTo.Equal(want) {
		t.Errorf("estado final: %d revisões, fim %v (esperado %v)", len(revs), revs[0].ValidTo, want)
	}
	checkInvariants(t, c)
}

// O banco também recusa o segundo encerramento, mesmo sem o mutex do
// serviço (UPDATE condicional): conflito transacional, não só em memória.
func TestConcurrentCloseValidityInStore(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			to := statNow.AddDate(0, i+1, 0)
			errs[i] = c.prices.CloseValidity(ctx, r.ID, to, statNow)
		}()
	}
	close(start)
	wg.Wait()
	ok, closed := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, sqlite.ErrValidityClosed):
			closed++
		case strings.Contains(err.Error(), "locked") || strings.Contains(err.Error(), "busy"):
			t.Errorf("conflito virou erro de banco ocupado: %v", err)
		default:
			t.Errorf("erro inesperado: %v", err)
		}
	}
	if ok != 1 || closed != 1 {
		t.Errorf("aceitos %d, recusados %d; esperado 1 e 1", ok, closed)
	}
}

// Leitura, sincronização e Refresh enquanto o Recalcular está no meio da
// gravação: a leitura vê o estado anterior inteiro, o Refresh espera, e a
// mudança da sincronização não se perde.
func TestRecalcWithConcurrentSyncReadAndRefresh(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	before := c.total(t, allClaude, "USD")
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	reached, release := pause(c, "recalc:save")
	done := make(chan error, 1)
	go func() {
		_, err := c.svc.Recalculate(ctx, allClaude, p.Token)
		done <- err
	}()
	<-reached // plano feito, mutex preso, nada gravado ainda

	if got := c.total(t, allClaude, "USD"); got != before {
		t.Errorf("leitura durante o recálculo: %s, esperado o estado anterior %s", got, before)
	}
	if c.svc.mu.TryLock() {
		c.svc.mu.Unlock()
		t.Fatal("mutex livre durante a gravação do recálculo")
	}
	id, _ := c.lastUsageID(t, domain.ProviderClaude)
	c.syncTokens(t, id) // sincronização no meio do recálculo
	// O Refresh pedido agora só pode entrar depois da gravação (o mutex
	// está preso, conferido acima); no fim, tem de ter visto a sincronização.
	refreshed := make(chan error, 1)
	go func() {
		_, err := c.svc.Refresh(ctx)
		refreshed <- err
	}()

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-refreshed; err != nil {
		t.Fatal(err)
	}
	if got := c.usesRevision(t, next.ID); got == 0 {
		t.Error("recálculo não gravou")
	}
	if got := c.total(t, allClaude, "USD"); got <= before {
		t.Errorf("total depois %s, antes %s: preço dobrado não aplicado", got, before)
	}
	checkInvariants(t, c) // o registro sincronizado tem os tokens novos
}

// Uma operação falha (gravação do recálculo) enquanto outra concorrente
// dá certo: a que falhou não deixa rastro, a outra fica inteira.
func TestConcurrentFailureIsolated(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	_, resp := c.lastUsageID(t, domain.ProviderClaude)
	if _, err := c.user.Exec(`CREATE TRIGGER fail_one BEFORE INSERT ON cost_estimates
	  WHEN NEW.response_id = '` + resp + `' AND NEW.revision_id = ` + strconv.FormatInt(next.ID, 10) + `
	  BEGIN SELECT RAISE(ABORT, 'falha simulada'); END`); err != nil {
		t.Fatal(err)
	}
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	reached, release := pause(c, "recalc:save")
	failed := make(chan error, 1)
	go func() {
		_, err := c.svc.Recalculate(ctx, allClaude, p.Token)
		failed <- err
	}()
	<-reached
	codex := PriceInput{Provider: domain.ProviderCodex, Model: "gpt-*", Currency: "USD", UnitTokens: 1_000_000,
		Input: ptr("1"), Output: ptr("2"), ValidFrom: "2026-01-01"}
	added := make(chan error, 1)
	go func() {
		_, err := c.svc.AddPrice(ctx, codex)
		added <- err
	}()
	close(release)
	if err := <-failed; err == nil {
		t.Fatal("falha simulada não chegou ao chamador")
	}
	if err := <-added; err != nil {
		t.Fatalf("operação concorrente afetada pela falha: %v", err)
	}
	if got := c.usesRevision(t, next.ID); got != 0 {
		t.Errorf("rollback incompleto: %d estimativas com o preço novo", got)
	}
	items, _ := c.svc.Prices(ctx)
	if len(items) != 2 {
		t.Errorf("%d preços, esperado 2", len(items))
	}
	checkInvariants(t, c)
}

// A exportação lê grupos e sessões no mesmo estado: nenhum Recalcular
// grava entre as duas leituras.
func TestExportReadsUnderLock(t *testing.T) {
	c, _ := editedEnv(t)
	save, _ := csvFile(t)
	c.svc.d.Save = save
	reached, release := pause(c, "export:read")
	done := make(chan error, 1)
	go func() {
		_, err := c.svc.ExportCSV(context.Background(), allClaude)
		done <- err
	}()
	<-reached
	locked := !c.svc.mu.TryLock()
	if !locked {
		c.svc.mu.Unlock()
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !locked {
		t.Error("exportação lê grupos e sessões sem o mutex")
	}
}
