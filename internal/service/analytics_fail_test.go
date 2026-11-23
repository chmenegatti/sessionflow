package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"sessionflow/internal/indexer/indexertest"
	"sessionflow/internal/repository/sqlite"
)

// Falha do banco ao achar o primeiro registro ("Tudo") volta como erro
// genérico, como as demais consultas, sem o texto do SQLite.
func TestAnalyticsAllPeriodDBErrorIsInternal(t *testing.T) {
	e := indexertest.NewUsageEnv(t)
	udb, err := sqlite.OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { udb.Close() })
	user := sqlite.NewUserStore(udb)
	stats := NewStatsService(e.Store, NewSessionService(e.Store, user, discard), user, discard)
	a := NewAnalyticsService(e.Store, stats, discard)
	e.DB.Close()

	if _, err := a.Overview(context.Background(), allClaude); !errors.Is(err, errInternal) {
		t.Errorf("Overview: %v; quer errInternal", err)
	}
	if _, err := a.Activity(context.Background(), allClaude); !errors.Is(err, errInternal) {
		t.Errorf("Activity: %v; quer errInternal", err)
	}
}
