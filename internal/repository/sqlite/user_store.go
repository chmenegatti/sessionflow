package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"sessionflow/internal/domain"
)

// UserStore grava favoritos e tags (user.db). Sessões são referenciadas
// pela chave estável (provider, native_id), nunca pelo id do índice.
type UserStore struct{ db *sql.DB }

// NewUserStore cria o store sobre um banco aberto por OpenUser.
func NewUserStore(db *sql.DB) *UserStore { return &UserStore{db: db} }

// SetFavorite marca ou desmarca uma sessão como favorita.
func (u *UserStore) SetFavorite(ctx context.Context, k domain.SessionKey, on bool) error {
	if !on {
		_, err := u.db.ExecContext(ctx, "DELETE FROM favorites WHERE provider = ? AND native_id = ?", k.Provider, k.NativeID)
		return err
	}
	_, err := u.db.ExecContext(ctx,
		"INSERT INTO favorites (provider, native_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
		k.Provider, k.NativeID, time.Now().UnixMilli())
	return err
}

// FavoriteKeys devolve as chaves ("provider:id") de todas as favoritas.
func (u *UserStore) FavoriteKeys(ctx context.Context) ([]string, error) {
	return u.strings(ctx, "SELECT provider || ':' || native_id FROM favorites")
}

// KeysWithTag devolve as chaves das sessões com a tag (sem diferenciar
// maiúsculas).
func (u *UserStore) KeysWithTag(ctx context.Context, name string) ([]string, error) {
	return u.strings(ctx, `
		SELECT st.provider || ':' || st.native_id FROM session_tags st
		JOIN tags t ON t.id = st.tag_id WHERE t.name = ?`, name)
}

// Favorites informa quais das chaves são favoritas.
func (u *UserStore) Favorites(ctx context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(keys) == 0 {
		return out, nil
	}
	list, err := u.strings(ctx, `
		SELECT provider || ':' || native_id FROM favorites
		WHERE provider || ':' || native_id IN (SELECT value FROM json_each(?))`, jsonList(keys))
	for _, k := range list {
		out[k] = true
	}
	return out, err
}

// TagsOf devolve as tags de cada chave, em ordem alfabética.
func (u *UserStore) TagsOf(ctx context.Context, keys []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := u.db.QueryContext(ctx, `
		SELECT st.provider || ':' || st.native_id, t.name FROM session_tags st
		JOIN tags t ON t.id = st.tag_id
		WHERE st.provider || ':' || st.native_id IN (SELECT value FROM json_each(?))
		ORDER BY t.name COLLATE NOCASE`, jsonList(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, name string
		if err := rows.Scan(&k, &name); err != nil {
			return nil, err
		}
		out[k] = append(out[k], name)
	}
	return out, rows.Err()
}

// AddTag associa a tag à sessão, criando-a se preciso. Uma tag existente
// com outra capitalização é reaproveitada.
func (u *UserStore) AddTag(ctx context.Context, k domain.SessionKey, name string) error {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO tags (name) VALUES (?) ON CONFLICT DO NOTHING", name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO session_tags (tag_id, provider, native_id)
		SELECT id, ?, ? FROM tags WHERE name = ? ON CONFLICT DO NOTHING`, k.Provider, k.NativeID, name); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveTag tira a tag da sessão e apaga tags que ficaram sem uso.
func (u *UserStore) RemoveTag(ctx context.Context, k domain.SessionKey, name string) error {
	tx, err := u.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_tags WHERE provider = ? AND native_id = ?
		AND tag_id IN (SELECT id FROM tags WHERE name = ?)`, k.Provider, k.NativeID, name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM session_tags)"); err != nil {
		return err
	}
	return tx.Commit()
}

// TagKey é uma associação tag → sessão.
type TagKey struct {
	Name string
	Key  string // "provider:id"
}

// TagKeys devolve todas as associações, agrupadas por tag em ordem
// alfabética.
func (u *UserStore) TagKeys(ctx context.Context) ([]TagKey, error) {
	rows, err := u.db.QueryContext(ctx, `
		SELECT t.name, st.provider || ':' || st.native_id FROM tags t
		JOIN session_tags st ON st.tag_id = t.id
		ORDER BY t.name COLLATE NOCASE, t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagKey
	for rows.Next() {
		var tk TagKey
		if err := rows.Scan(&tk.Name, &tk.Key); err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

func (u *UserStore) strings(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := u.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func jsonList(keys []string) string {
	b, _ := json.Marshal(keys)
	return string(b)
}

// FavoritesSince devolve as favoritas marcadas a partir de since (zero =
// todas): chave "provider:id" → quando foi marcada.
func (u *UserStore) FavoritesSince(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	var from int64
	if !since.IsZero() {
		from = since.UnixMilli()
	}
	rows, err := u.db.QueryContext(ctx,
		"SELECT provider || ':' || native_id, created_at FROM favorites WHERE created_at >= ?", from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var k string
		var ms int64
		if err := rows.Scan(&k, &ms); err != nil {
			return nil, err
		}
		out[k] = time.UnixMilli(ms).UTC()
	}
	return out, rows.Err()
}
