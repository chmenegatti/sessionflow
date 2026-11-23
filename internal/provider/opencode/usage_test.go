package opencode

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/domain"
	"sessionflow/internal/provider/providertest"
)

// usageDB monta root/opencode.db com o fixture de consumo (esquema incluído).
func usageDB(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	q, err := os.ReadFile(providertest.Fixture("usage", "opencode", "usage.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(q)); err != nil {
		t.Fatal(err)
	}
	return root
}

func ov(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestOpenCodeUsagePerMessage(t *testing.T) {
	sink, _, err := parseAll(t, adapter(t, usageDB(t)))
	if err != nil {
		t.Fatal(err)
	}
	us := sink.Usages[sink.Sessions[0].Key]
	if len(us) != 3 {
		t.Fatalf("%d registros; quer 3 (step-finish e usuário fora)", len(us))
	}
	want := [][]any{
		{int64(11440), int64(286), int64(0), int64(0), int64(0), int64(11726)},
		{int64(5059), int64(406), int64(100), int64(50), int64(0), int64(5615)},
		{int64(300), int64(80), int64(0), int64(0), int64(12), nil},
	}
	for i, u := range us {
		got := []any{ov(u.Input), ov(u.Output), ov(u.CacheRead), ov(u.CacheCreation), ov(u.Reasoning), ov(u.Total)}
		for j := range got {
			if got[j] != want[i][j] {
				t.Errorf("resposta %d campo %d = %v; quer %v", i+1, j, got[j], want[i][j])
			}
		}
	}
	if us[0].ResponseID != "msg_usage00002AAAAAAAAAAAAAAAA" || us[0].Model != "claude-opus-4.6" || us[2].Model != "gpt-5.4" {
		t.Errorf("identidade/modelo: %s %s %s", us[0].ResponseID, us[0].Model, us[2].Model)
	}
	if len(us[0].Derived) != 0 || len(us[1].Derived) != 1 || us[1].Derived[0] != domain.UsageTotal {
		t.Errorf("derived: %v %v", us[0].Derived, us[1].Derived)
	}
	st := []domain.UsageStatus{us[0].Status, us[1].Status, us[2].Status}
	if st[0] != domain.UsageStatusComplete || st[1] != domain.UsageStatusComplete || st[2] != domain.UsageStatusPartial {
		t.Errorf("status %v", st)
	}
}

func TestOpenCodeReasoningNotAdded(t *testing.T) {
	sink, _, _ := parseAll(t, adapter(t, usageDB(t)))
	for _, u := range sink.Usages[sink.Sessions[0].Key] {
		if u.Total != nil && u.Reasoning != nil && *u.Reasoning > 0 {
			t.Errorf("total derivado com reasoning > 0: %+v", u)
		}
	}
}
