package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

// Encerrar a vigência de um preço sem cadastrar sucessor (hardening, item 2).

// moveFirstClaude leva o primeiro registro Claude para 10/11 (o fixture
// tem todo o consumo num dia só): o fim da vigência em 15/11 divide o
// consumo em antes e depois.
func (c *costEnv) moveFirstClaude(t *testing.T) {
	t.Helper()
	ts := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := c.index.Exec(`UPDATE usage SET ts = ? WHERE id = (SELECT u.id FROM usage u JOIN sessions s ON s.id = u.session_id
	  WHERE s.provider = 'claude' AND u.ts IS NOT NULL ORDER BY u.id LIMIT 1)`, ts); err != nil {
		t.Fatal(err)
	}
}

func TestClosePriceWithoutSuccessor(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	c.moveFirstClaude(t)
	r := c.add(t, claudeUSD)
	last := time.Date(2026, 11, 15, 0, 0, 0, 0, statNow.Location()) // último dia de vigência
	res, err := c.svc.ClosePrice(ctx, r.ID, last.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	end := time.Date(last.Year(), last.Month(), last.Day()+1, 0, 0, 0, 0, last.Location())
	if res.Revision.ValidTo == nil || !res.Revision.ValidTo.Equal(end) {
		t.Fatalf("ValidTo = %v, esperado %v", res.Revision.ValidTo, end)
	}
	items, _ := c.svc.Prices(ctx)
	if len(items) != 1 || len(items[0].History) != 1 {
		t.Fatalf("encerrar criou revisão ou preço: %d preços", len(items))
	}
	if got := items[0].Current; got.ID != r.ID || got.ValidTo == nil || !got.ValidTo.Equal(end) || got.ClosedAt == nil {
		t.Errorf("revisão atual %+v", got)
	}
	if res.Stale == 0 {
		t.Error("nenhuma estimativa depois do fim; o teste não cobre o recálculo")
	}
	// Recalcular aplica a ausência de preço depois do fim.
	if _, err := c.recalc(ctx); err != nil {
		t.Fatal(err)
	}
	es, _ := c.prices.Estimates(ctx)
	before, after := 0, 0
	for k, e := range es {
		if k.Provider != domain.ProviderClaude || e.UsageTs == nil {
			continue
		}
		ts := time.UnixMilli(*e.UsageTs)
		switch {
		case ts.Before(end):
			before++
			if e.RevisionID == nil || *e.RevisionID != r.ID {
				t.Errorf("%v antes do fim sem o preço: %s", k, e.Status)
			}
		default:
			after++
			if e.Status != domain.EstimateNoPrice || e.RevisionID != nil {
				t.Errorf("%v depois do fim: %s rev %v (preço encerrado aplicado)", k, e.Status, e.RevisionID)
			}
		}
	}
	if before == 0 || after == 0 {
		t.Fatalf("antes %d, depois %d: a data não divide o consumo", before, after)
	}
}

func TestClosePriceBoundary(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	res, err := c.svc.ClosePrice(ctx, r.ID, "2026-03-31")
	if err != nil {
		t.Fatal(err)
	}
	revs, _ := c.prices.Revisions(ctx)
	end := *res.Revision.ValidTo
	for _, tc := range []struct {
		name string
		ts   time.Time
		want bool
	}{
		{"antes do encerramento", end.Add(-24 * time.Hour), true},
		{"último instante vigente", end.Add(-time.Millisecond), true},
		{"exatamente no encerramento", end, false},
		{"depois do encerramento", end.AddDate(0, 1, 0), false},
	} {
		got := domain.MatchPrice(revs, domain.ProviderClaude, "claude-opus-5-5", tc.ts) != nil
		if got != tc.want {
			t.Errorf("%s: casou = %v, esperado %v", tc.name, got, tc.want)
		}
	}
}

func TestClosePriceThenSuccessorNoOverlap(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	if _, err := c.svc.ClosePrice(ctx, r.ID, "2026-03-31"); err != nil {
		t.Fatal(err)
	}
	overlap := doubled(claudeUSD)
	overlap.ValidFrom = "2026-03-15"
	if _, err := c.svc.AddPrice(ctx, overlap); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("vigência sobreposta aceita: %v", err)
	}
	next := doubled(claudeUSD)
	next.ValidFrom = "2026-04-01"
	if _, err := c.svc.AddPrice(ctx, next); err != nil {
		t.Errorf("sucessor depois do fim recusado: %v", err)
	}
}

func TestClosePriceRules(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD) // vigente desde 2026-01-01
	if _, err := c.svc.ClosePrice(ctx, r.ID, "2025-12-31"); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("fim antes do início aceito: %v", err)
	}
	if _, err := c.svc.ClosePrice(ctx, r.ID, "31/03/2026"); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("data inválida aceita: %v", err)
	}
	if _, err := c.svc.ClosePrice(ctx, r.ID, "2026-03-31"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.ClosePrice(ctx, r.ID, "2026-04-30"); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("segundo encerramento aceito: %v", err)
	}
	// Com sucessor (NewPriceFrom) a vigência já tem fim: não reencerra.
	c2 := newCostEnv(t)
	r2 := c2.add(t, claudeUSD)
	next := doubled(claudeUSD)
	next.ValidFrom = "2026-06-01"
	if _, err := c2.svc.NewPriceFrom(ctx, r2.ID, next); err != nil {
		t.Fatal(err)
	}
	if _, err := c2.svc.ClosePrice(ctx, r2.ID, "2026-05-15"); !errors.Is(err, domain.ErrInvalidPrice) {
		t.Errorf("encerrar preço com sucessor aceito: %v", err)
	}
}

// Resposta crescendo depois do fim da vigência: o Refresh não mantém o
// preço encerrado (fica sem preço); desativado continuaria valendo.
func TestRefreshDoesNotKeepClosedPrice(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	r := c.add(t, claudeUSD)
	if _, err := c.svc.ClosePrice(ctx, r.ID, "2026-11-15"); err != nil { // consumo Claude é de 20/11
		t.Fatal(err)
	}
	id, resp := c.lastUsageID(t, domain.ProviderClaude)
	c.syncTokens(t, id)
	if _, err := c.svc.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	es, _ := c.prices.Estimates(ctx)
	for k, e := range es {
		if k.ResponseID == resp && (e.Status != domain.EstimateNoPrice || e.RevisionID != nil) {
			t.Errorf("%v depois do fim: %s rev %v", k, e.Status, e.RevisionID)
		}
	}
}
