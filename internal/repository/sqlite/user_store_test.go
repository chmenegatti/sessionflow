package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"sessionflow/internal/domain"
)

func TestFavoritesSince(t *testing.T) {
	db, err := OpenUser(context.Background(), filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	u := NewUserStore(db)
	ctx := context.Background()
	for i, id := range []string{"a", "b", "c"} {
		ms := time.Date(2026, 11, 1+i*10, 12, 0, 0, 0, time.UTC).UnixMilli()
		if _, err := db.Exec("INSERT INTO favorites (provider, native_id, created_at) VALUES (?, ?, ?)", domain.ProviderClaude, id, ms); err != nil {
			t.Fatal(err)
		}
	}
	got, err := u.FavoritesSince(ctx, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || len(got) != 2 || !got["claude:b"].Equal(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("desde 5/11: %v %v", got, err)
	}
	if all, _ := u.FavoritesSince(ctx, time.Time{}); len(all) != 3 {
		t.Errorf("todas: %v", all)
	}
}
