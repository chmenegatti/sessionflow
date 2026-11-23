package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

func priceDB(t *testing.T) (*sql.DB, *PriceStore) {
	t.Helper()
	db, err := OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, NewPriceStore(db)
}

func sp(s string) *string { return &s }

var pt0 = time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)

func sampleRev() domain.PriceRevision {
	return domain.PriceRevision{
		PriceKey: "k1", Provider: domain.ProviderClaude, Model: "claude-opus-*", Currency: "USD", UnitTokens: 1_000_000,
		Input: sp("3"), Output: sp("15"), CacheRead: nil, CacheCreation: sp("3.75"),
		ValidFrom: pt0, Source: "página de preços", Note: "n", CreatedAt: pt0.Add(time.Hour),
	}
}

func TestPriceStoreRoundTrip(t *testing.T) {
	_, s := priceDB(t)
	ctx := context.Background()
	r := sampleRev()
	id, err := s.InsertRevision(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	to := pt0.AddDate(0, 1, 0)
	r2 := sampleRev()
	r2.PriceKey, r2.ValidTo = "k2", &to
	if _, err := s.InsertRevision(ctx, r2); err != nil {
		t.Fatal(err)
	}
	got, err := s.Revisions(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	r.ID = id
	if !reflect.DeepEqual(got[0], r) {
		t.Errorf("\n got %+v\nwant %+v", got[0], r)
	}
	if got[1].ValidTo == nil || !got[1].ValidTo.Equal(to) {
		t.Errorf("valid_to %v", got[1].ValidTo)
	}
}

func TestReplaceRevisionTwiceFails(t *testing.T) {
	db, s := priceDB(t)
	ctx := context.Background()
	old, _ := s.InsertRevision(ctx, sampleRev())
	nr := sampleRev()
	nr.Input = sp("6")
	nid, err := s.ReplaceRevision(ctx, old, nr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceRevision(ctx, old, nr); !errors.Is(err, ErrRevisionReplaced) {
		t.Errorf("segunda troca: %v", err)
	}
	var n int
	db.QueryRow("SELECT count(*) FROM price_revisions").Scan(&n)
	revs, _ := s.Revisions(ctx)
	if n != 2 || revs[0].ReplacedBy == nil || *revs[0].ReplacedBy != nid {
		t.Errorf("%d linhas; %+v", n, revs[0].ReplacedBy)
	}
}

func TestCloseValidityOnce(t *testing.T) {
	_, s := priceDB(t)
	ctx := context.Background()
	id, _ := s.InsertRevision(ctx, sampleRev())
	to, at := pt0.AddDate(0, 0, 10), pt0.AddDate(0, 0, 2)
	if err := s.CloseValidity(ctx, id, to, at); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseValidity(ctx, id, to, at); !errors.Is(err, ErrValidityClosed) {
		t.Errorf("segunda: %v", err)
	}
	revs, _ := s.Revisions(ctx)
	if revs[0].ClosedAt == nil || !revs[0].ClosedAt.Equal(at) || !revs[0].ValidTo.Equal(to) || revs[0].ReplacedBy != nil {
		t.Errorf("%+v", revs[0])
	}
}

func TestDisableRevision(t *testing.T) {
	_, s := priceDB(t)
	ctx := context.Background()
	id, _ := s.InsertRevision(ctx, sampleRev())
	if err := s.Disable(ctx, id, pt0); err != nil {
		t.Fatal(err)
	}
	revs, _ := s.Revisions(ctx)
	if revs[0].DisabledAt == nil || revs[0].Current() {
		t.Errorf("%+v", revs[0])
	}
}

func estimate(key EstimateKey, rev *int64, total int64) StoredEstimate {
	n := domain.Nanos(total)
	in := int64(10)
	ts := pt0.UnixMilli()
	return StoredEstimate{Key: key, UsageTs: &ts, CalculatedAt: pt0, Estimate: domain.Estimate{
		RevisionID: rev, Currency: "USD", UnitTokens: 1_000_000,
		Tokens: domain.UsageTokens{Input: &in}, InputCost: &n, Total: &n,
		Status: domain.EstimatePartial, Reasons: []string{"saída indisponível", "cache lido indisponível"},
	}}
}

func TestSaveEstimatesUpsert(t *testing.T) {
	db, s := priceDB(t)
	ctx := context.Background()
	id, _ := s.InsertRevision(ctx, sampleRev())
	k := EstimateKey{Provider: domain.ProviderClaude, NativeID: "s1", ResponseID: "r1"}
	if err := s.SaveEstimates(ctx, []StoredEstimate{estimate(k, &id, 5)}); err != nil {
		t.Fatal(err)
	}
	second := estimate(k, &id, 7)
	if err := s.SaveEstimates(ctx, []StoredEstimate{second}); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow("SELECT count(*) FROM cost_estimates").Scan(&n)
	got, err := s.Estimates(ctx)
	if err != nil || n != 1 || !reflect.DeepEqual(got[k], second) {
		t.Errorf("%d linhas; %+v\nquer %+v (%v)", n, got[k], second, err)
	}
	// Sem preço: sem revisão nem custo.
	k2 := EstimateKey{Provider: domain.ProviderCodex, NativeID: "s2", ResponseID: "r2"}
	np := StoredEstimate{Key: k2, CalculatedAt: pt0, Estimate: domain.Estimate{Status: domain.EstimateNoPrice, Reasons: []string{domain.ReasonNoPrice}}}
	if err := s.SaveEstimates(ctx, []StoredEstimate{np}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Estimates(ctx)
	if !reflect.DeepEqual(got[k2], np) {
		t.Errorf("sem preço: %+v", got[k2])
	}
}

func TestEstimateUses(t *testing.T) {
	_, s := priceDB(t)
	ctx := context.Background()
	a, _ := s.InsertRevision(ctx, sampleRev())
	r := sampleRev()
	r.PriceKey = "k2"
	b, _ := s.InsertRevision(ctx, r)
	var es []StoredEstimate
	for i, rev := range []int64{a, a, b} {
		es = append(es, estimate(EstimateKey{domain.ProviderClaude, "s", string(rune('a' + i))}, &rev, 1))
	}
	if err := s.SaveEstimates(ctx, es); err != nil {
		t.Fatal(err)
	}
	uses, err := s.EstimateUses(ctx)
	if err != nil || uses[a] != 2 || uses[b] != 1 {
		t.Errorf("%v %v", uses, err)
	}
}

func TestUserMigration0002(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "user.db")
	db, err := open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	migs, err := loadMigrations(migrationsFS, "migrations/user")
	if err != nil || len(migs) < 2 {
		t.Fatalf("%d migrations %v", len(migs), err)
	}
	if err := migrate(ctx, db, migs[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO favorites (provider, native_id, created_at) VALUES ('claude', 'x', 1)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = OpenUser(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var fav, revs int
	db.QueryRow("SELECT count(*) FROM favorites").Scan(&fav)
	if err := db.QueryRow("SELECT count(*) FROM price_revisions").Scan(&revs); err != nil || fav != 1 {
		t.Errorf("favoritos %d; price_revisions: %v", fav, err)
	}
}

func TestCloseAndInsertAtomic(t *testing.T) {
	db, s := priceDB(t)
	ctx := context.Background()
	id, _ := s.InsertRevision(ctx, sampleRev())
	to := pt0.AddDate(0, 0, 10)
	nr := sampleRev()
	nr.PriceKey, nr.ValidFrom = "k2", to
	if _, err := s.CloseAndInsert(ctx, id, to, pt0, nr); err != nil {
		t.Fatal(err)
	}
	// Já encerrada: falha sem inserir.
	if _, err := s.CloseAndInsert(ctx, id, to, pt0, nr); !errors.Is(err, ErrValidityClosed) {
		t.Errorf("segunda: %v", err)
	}
	var n int
	db.QueryRow("SELECT count(*) FROM price_revisions").Scan(&n)
	if n != 2 {
		t.Errorf("%d revisões", n)
	}
}
