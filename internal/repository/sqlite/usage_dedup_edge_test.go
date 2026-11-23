package sqlite_test

import (
	"context"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/indexer/indexertest"
)

// A cópia da sessão principal sem horário não pode tirar a resposta dos
// períodos com data: vence a cópia que tem horário.
func TestUsageDedupWinnerWithoutTimestamp(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	main := "33333333-3333-4333-8333-333333333333"
	if _, err := e.DB.Exec(`UPDATE usage SET ts = NULL WHERE response_id = 'msg_FIX_B'
		AND session_id = (SELECT id FROM sessions WHERE native_id = ?)`, main); err != nil {
		t.Fatal(err)
	}
	c, err := e.Store.UsageTotals(context.Background(), only(domain.ProviderClaude))
	if err != nil {
		t.Fatal(err)
	}
	if c.Records != 7 {
		t.Errorf("%d registros no período; quer 7 (msg_FIX_B pela cópia com horário)", c.Records)
	}
	field(t, "total", c.Total, 142424, 6)
}

// response_id vazio não é identidade: dois registros ” de sessões
// diferentes não se anulam na deduplicação entre sessões.
func TestUsageDedupIgnoresEmptyResponseID(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	e.Run(t)
	if _, err := e.DB.Exec(`UPDATE usage SET response_id = '' WHERE response_id IN ('msg_FIX_F', 'msg_FIX_G')`); err != nil {
		t.Fatal(err)
	}
	c, err := e.Store.UsageTotals(context.Background(), only(domain.ProviderClaude))
	if err != nil {
		t.Fatal(err)
	}
	if c.Records != 7 {
		t.Errorf("%d registros; quer 7 (F e G continuam distintos)", c.Records)
	}
}
