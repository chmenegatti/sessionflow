package opencode

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// tokens com tipo inesperado não pode derrubar a mensagem (viewer e busca);
// só o consumo dela é descartado.
func TestOpenCodeUsageMalformedKeepsMessage(t *testing.T) {
	root := usageDB(t)
	db, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	const id = "msg_usage00002AAAAAAAAAAAAAAAA"
	res, err := db.Exec(`UPDATE message SET data = json_set(data, '$.tokens.input', 1.5) WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("UPDATE afetou %d linhas", n)
	}
	db.Close()

	before, _, err := parseAll(t, adapter(t, usageDB(t)))
	if err != nil {
		t.Fatal(err)
	}
	sink, stats, err := parseAll(t, adapter(t, root))
	if err != nil {
		t.Fatal(err)
	}
	k := sink.Sessions[0].Key
	if stats.BadLines != 0 {
		t.Errorf("BadLines = %d; quer 0", stats.BadLines)
	}
	if got, want := len(sink.Messages[k]), len(before.Messages[before.Sessions[0].Key]); got != want {
		t.Errorf("%d mensagens; quer %d (a mensagem não pode sumir)", got, want)
	}
	for _, u := range sink.Usages[k] {
		if u.ResponseID == id {
			t.Error("consumo ilegível foi gravado")
		}
	}
	if len(sink.Usages[k]) != 2 {
		t.Errorf("%d registros; quer 2", len(sink.Usages[k]))
	}
	if stats.SkippedItems == 0 {
		t.Error("consumo descartado deveria contar em SkippedItems")
	}
}
