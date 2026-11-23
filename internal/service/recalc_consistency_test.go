package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"sessionflow/internal/domain"
)

// Consistência entre a prévia do Recalcular e a gravação (hardening, item 1).

// usesRevision conta as estimativas gravadas com a revisão id.
func (c *costEnv) usesRevision(t *testing.T, id int64) int {
	t.Helper()
	es, err := c.prices.Estimates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range es {
		if e.RevisionID != nil && *e.RevisionID == id {
			n++
		}
	}
	return n
}

// lastUsageID: o último registro de consumo do provider no índice.
func (c *costEnv) lastUsageID(t *testing.T, p domain.ProviderID) (id int64, responseID string) {
	t.Helper()
	err := c.index.QueryRow(`SELECT u.id, u.response_id FROM usage u JOIN sessions s ON s.id = u.session_id
	  WHERE s.provider = ? AND u.output_tokens IS NOT NULL ORDER BY u.id DESC LIMIT 1`, p).Scan(&id, &responseID)
	if err != nil {
		t.Fatal(err)
	}
	return id, responseID
}

// syncTokens simula uma sincronização que muda a saída de um registro.
func (c *costEnv) syncTokens(t *testing.T, usageID int64) {
	t.Helper()
	if _, err := c.index.Exec("UPDATE usage SET output_tokens = output_tokens + 1000 WHERE id = ?", usageID); err != nil {
		t.Fatal(err)
	}
}

// editedEnv: preço cadastrado e depois editado (Recalcular tem o que mudar).
func editedEnv(t *testing.T) (*costEnv, domain.PriceRevision) {
	t.Helper()
	c := newCostEnv(t)
	r := c.add(t, claudeUSD)
	next, err := c.svc.EditPrice(context.Background(), r.ID, doubled(claudeUSD))
	if err != nil {
		t.Fatal(err)
	}
	return c, next
}

func TestRecalcPreviewThenSaveUnchanged(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	p, err := c.svc.PreviewRecalculate(ctx, allClaude)
	if err != nil {
		t.Fatal(err)
	}
	if p.Token == "" || p.Changes == 0 {
		t.Fatalf("prévia sem token ou sem mudanças: %+v", p)
	}
	done, err := c.svc.Recalculate(ctx, allClaude, p.Token)
	if err != nil {
		t.Fatal(err)
	}
	if done.Changes != p.Changes {
		t.Errorf("gravou %d, prévia dizia %d", done.Changes, p.Changes)
	}
	if got := c.usesRevision(t, next.ID); got != p.Changes {
		t.Errorf("%d estimativas na revisão nova, esperado %d", got, p.Changes)
	}
}

func TestRecalcPreviewIsStable(t *testing.T) {
	c, _ := editedEnv(t)
	ctx := context.Background()
	a, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	b, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	if a.Token != b.Token {
		t.Errorf("mesmo estado, tokens diferentes: %s × %s", a.Token, b.Token)
	}
}

func TestRecalcRejectsPreviewAfterSync(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	id, _ := c.lastUsageID(t, domain.ProviderClaude)
	c.syncTokens(t, id) // só um registro muda
	snap, _ := c.prices.Estimates(ctx)
	if _, err := c.svc.Recalculate(ctx, allClaude, p.Token); !errors.Is(err, ErrStalePreview) {
		t.Fatalf("prévia desatualizada aceita: err = %v", err)
	}
	after, _ := c.prices.Estimates(ctx)
	for k, e := range snap {
		if !sameEstimate(e, after[k]) {
			t.Errorf("confirmação recusada gravou %v", k)
		}
	}
	if got := c.usesRevision(t, next.ID); got != 0 {
		t.Errorf("prévia rejeitada gravou %d estimativas com o preço novo", got)
	}
	// Prévia nova do estado atual grava normalmente.
	p2, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	if p2.Token == p.Token {
		t.Fatal("token não mudou depois da sincronização")
	}
	if _, err := c.svc.Recalculate(ctx, allClaude, p2.Token); err != nil {
		t.Fatal(err)
	}
	if got := c.usesRevision(t, next.ID); got != p2.Changes {
		t.Errorf("%d estimativas na revisão nova, esperado %d", got, p2.Changes)
	}
}

func TestRecalcIgnoresSyncOutsideRange(t *testing.T) {
	c, _ := editedEnv(t)
	ctx := context.Background()
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	id, _ := c.lastUsageID(t, domain.ProviderCodex) // fora do recorte (só Claude)
	c.syncTokens(t, id)
	if _, err := c.svc.Recalculate(ctx, allClaude, p.Token); err != nil {
		t.Errorf("mudança fora do recorte invalidou a prévia: %v", err)
	}
}

func TestRecalcRejectsEmptyToken(t *testing.T) {
	c, next := editedEnv(t)
	if _, err := c.svc.Recalculate(context.Background(), allClaude, ""); !errors.Is(err, ErrStalePreview) {
		t.Fatalf("sem prévia: err = %v", err)
	}
	if got := c.usesRevision(t, next.ID); got != 0 {
		t.Errorf("gravou %d sem prévia", got)
	}
}

func TestRecalcConcurrentSamePreview(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = c.svc.Recalculate(ctx, allClaude, p.Token)
		}()
	}
	close(start)
	wg.Wait()
	ok, stale := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrStalePreview):
			stale++
		default:
			t.Errorf("erro inesperado: %v", err)
		}
	}
	if ok != 1 || stale != 1 {
		t.Errorf("gravações aceitas = %d, rejeitadas = %d; esperado 1 e 1", ok, stale)
	}
	if got := c.usesRevision(t, next.ID); got != p.Changes {
		t.Errorf("%d estimativas na revisão nova, esperado %d", got, p.Changes)
	}
}

func TestRecalcWriteFailureRollsBack(t *testing.T) {
	c, next := editedEnv(t)
	ctx := context.Background()
	_, resp := c.lastUsageID(t, domain.ProviderClaude)
	// Falha no meio do lote: a última resposta Claude não pode ser gravada.
	if _, err := c.user.Exec(`CREATE TRIGGER fail_one BEFORE INSERT ON cost_estimates
	  WHEN NEW.response_id = '` + resp + `' BEGIN SELECT RAISE(ABORT, 'falha simulada'); END`); err != nil {
		t.Fatal(err)
	}
	before, _ := c.prices.Estimates(ctx)
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	if _, err := c.svc.Recalculate(ctx, allClaude, p.Token); err == nil {
		t.Fatal("falha na gravação não chegou ao chamador")
	}
	if got := c.usesRevision(t, next.ID); got != 0 {
		t.Errorf("rollback incompleto: %d estimativas com o preço novo", got)
	}
	after, _ := c.prices.Estimates(ctx)
	if len(after) != len(before) {
		t.Errorf("estimativas %d → %d depois da falha", len(before), len(after))
	}
	for k, e := range before {
		if !sameEstimate(e, after[k]) {
			t.Errorf("%v mudou apesar do rollback", k)
		}
	}
	// Sem a falha, a mesma prévia ainda vale (nada foi gravado).
	c.user.Exec("DROP TRIGGER fail_one")
	if _, err := c.svc.Recalculate(ctx, allClaude, p.Token); err != nil {
		t.Errorf("prévia válida rejeitada depois do rollback: %v", err)
	}
}

// recalc: prévia e confirmação em seguida (o fluxo normal da tela).
func (c *costEnv) recalc(ctx context.Context) (RecalcPreview, error) {
	p, err := c.svc.PreviewRecalculate(ctx, allClaude)
	if err != nil {
		return p, err
	}
	return c.svc.Recalculate(ctx, allClaude, p.Token)
}

// Prévia não grava nada (nem as estimativas que o Refresh atualizaria).
func TestRecalcPreviewWritesNothing(t *testing.T) {
	c, _ := editedEnv(t)
	ctx := context.Background()
	id, _ := c.lastUsageID(t, domain.ProviderClaude)
	c.syncTokens(t, id) // estimativa desatualizada pendente
	snap, _ := c.prices.Estimates(ctx)
	if _, err := c.svc.PreviewRecalculate(ctx, allClaude); err != nil {
		t.Fatal(err)
	}
	after, _ := c.prices.Estimates(ctx)
	for k, e := range snap {
		if !sameEstimate(e, after[k]) {
			t.Errorf("prévia gravou %v", k)
		}
	}
}

// Registro novo no recorte entre a prévia e a confirmação muda os totais
// mostrados: a prévia fica desatualizada mesmo que ele não "mude".
func TestRecalcRejectsPreviewAfterNewRecord(t *testing.T) {
	c := newCostEnv(t)
	ctx := context.Background()
	c.add(t, claudeUSD) // nada a recalcular: changes = 0
	p, _ := c.svc.PreviewRecalculate(ctx, allClaude)
	if _, err := c.index.Exec(`INSERT INTO usage (session_id, response_id, seq, ts, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, status, derived)
	  SELECT session_id, 'resp-novo', seq + 1000, ts, model, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, total_tokens, status, derived
	  FROM usage WHERE id = ?`, mustID(t, c)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Recalculate(ctx, allClaude, p.Token); !errors.Is(err, ErrStalePreview) {
		t.Errorf("registro novo no recorte não invalidou a prévia: %v", err)
	}
}

func mustID(t *testing.T, c *costEnv) int64 {
	id, _ := c.lastUsageID(t, domain.ProviderClaude)
	return id
}
