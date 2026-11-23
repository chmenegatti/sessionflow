package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"sessionflow/internal/indexer"
	"sessionflow/internal/provider"
	"sessionflow/internal/provider/opencode"
	"sessionflow/internal/provider/providertest"
	"sessionflow/internal/repository/sqlite"
)

// OpenCode guarda várias sessões num banco: o viewer usa ParseSession e
// recebe só a sessão pedida.
func TestTranscriptOpenCodeSingleSession(t *testing.T) {
	root := t.TempDir()
	schema, _ := os.ReadFile(providertest.Fixture("opencode", "schema-v1.sql"))
	db, err := sql.Open("sqlite", filepath.Join(root, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{string(schema),
		`INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated)
		 VALUES ('ses_outra', 'p', 's', '/x', 'Outra', '1', 1768046500000, 1768046600000)`,
		`INSERT INTO message VALUES ('msg_o', 'ses_outra', 1768046500000, 1768046500000, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('prt_o', 'msg_o', 'ses_outra', 1, 1, '{"type":"text","text":"não deveria aparecer"}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	idx, _, err := sqlite.OpenIndex(context.Background(), filepath.Join(t.TempDir(), "index.db"), discard)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	store := sqlite.NewIndexStore(idx)
	oc := opencode.New(opencode.Options{RootDir: root, SnapshotDir: t.TempDir()})
	indexer.New(store, discard, oc).Run(context.Background(), indexer.RunOptions{})

	svc := NewTranscriptService(store, []provider.Provider{oc}, discard)
	defer svc.ServiceShutdown()
	page, err := svc.Open(context.Background(), "opencode:ses_fixture000001AAAAAAAAAAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	var all []MessageView
	for {
		all = append(all, page.Messages...)
		if page.Done {
			break
		}
		if page, err = svc.Next(context.Background(), page.Cursor); err != nil {
			t.Fatal(err)
		}
	}
	if len(all) != 2 || all[0].Blocks[0].Text != "Liste os arquivos Go do projeto" {
		t.Fatalf("mensagens: %+v", all)
	}
}
