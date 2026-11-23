package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"sessionflow/internal/domain"
)

// ErrRevisionReplaced: a revisão já foi substituída por outra (edição
// concorrente ou repetida).
var ErrRevisionReplaced = errors.New("revisão já substituída")

// ErrValidityClosed: a vigência da revisão já tem fim.
var ErrValidityClosed = errors.New("vigência já encerrada")

// EstimateKey identifica uma resposta de forma estável entre rebuilds do
// índice: provider + sessão nativa + response_id.
type EstimateKey struct {
	Provider   domain.ProviderID `json:"provider"`
	NativeID   string            `json:"nativeId"`
	ResponseID string            `json:"responseId"`
}

// StoredEstimate é uma estimativa gravada.
type StoredEstimate struct {
	Key EstimateKey
	domain.Estimate
	UsageTs      *int64 // ms; horário da resposta no cálculo
	CalculatedAt time.Time
}

// PriceStore grava revisões de preço e estimativas no user.db.
type PriceStore struct{ db *sql.DB }

// NewPriceStore cria o store sobre um banco aberto por OpenUser.
func NewPriceStore(db *sql.DB) *PriceStore { return &PriceStore{db: db} }

const revCols = `id, price_key, provider, model, currency, unit_tokens, input, output, cache_read, cache_creation,
  valid_from, valid_to, source, note, created_at, replaced_by, disabled_at, closed_at`

func msTime(v int64) time.Time { return time.UnixMilli(v).UTC() }

func msPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := msTime(v.Int64)
	return &t
}

func nullMs(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func nullStr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// Revisions devolve todas as revisões, por preço e criação.
func (s *PriceStore) Revisions(ctx context.Context) ([]domain.PriceRevision, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+revCols+" FROM price_revisions ORDER BY price_key, created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PriceRevision
	for rows.Next() {
		var r domain.PriceRevision
		var in, outp, cr, cc sql.NullString
		var from, created int64
		var to, repl, dis, closed sql.NullInt64
		if err := rows.Scan(&r.ID, &r.PriceKey, &r.Provider, &r.Model, &r.Currency, &r.UnitTokens, &in, &outp, &cr, &cc,
			&from, &to, &r.Source, &r.Note, &created, &repl, &dis, &closed); err != nil {
			return nil, err
		}
		r.Input, r.Output, r.CacheRead, r.CacheCreation = nullStr(in), nullStr(outp), nullStr(cr), nullStr(cc)
		r.ValidFrom, r.ValidTo, r.CreatedAt = msTime(from), msPtr(to), msTime(created)
		r.ReplacedBy, r.DisabledAt, r.ClosedAt = int64Ptr(repl), msPtr(dis), msPtr(closed)
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertRevision(ctx context.Context, x execer, r domain.PriceRevision) (int64, error) {
	res, err := x.ExecContext(ctx, `INSERT INTO price_revisions (price_key, provider, model, currency, unit_tokens,
	  input, output, cache_read, cache_creation, valid_from, valid_to, source, note, created_at, disabled_at, closed_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.PriceKey, r.Provider, r.Model, r.Currency, r.UnitTokens, r.Input, r.Output, r.CacheRead, r.CacheCreation,
		r.ValidFrom.UnixMilli(), nullMs(r.ValidTo), r.Source, r.Note, r.CreatedAt.UnixMilli(), nullMs(r.DisabledAt), nullMs(r.ClosedAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertRevision grava uma revisão nova (preço novo).
func (s *PriceStore) InsertRevision(ctx context.Context, r domain.PriceRevision) (int64, error) {
	return insertRevision(ctx, s.db, r)
}

// ReplaceRevision grava r como sucessora de oldID, numa transação.
// ErrRevisionReplaced se oldID já tiver sucessora.
func (s *PriceStore) ReplaceRevision(ctx context.Context, oldID int64, r domain.PriceRevision) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertRevision(ctx, tx, r)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, "UPDATE price_revisions SET replaced_by = ? WHERE id = ? AND replaced_by IS NULL", id, oldID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrRevisionReplaced
	}
	return id, tx.Commit()
}

// CloseValidity encerra a vigência de uma revisão ainda sem fim.
func (s *PriceStore) CloseValidity(ctx context.Context, id int64, to, at time.Time) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE price_revisions SET valid_to = ?, closed_at = ? WHERE id = ? AND valid_to IS NULL",
		to.UnixMilli(), at.UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrValidityClosed
	}
	return nil
}

// CloseAndInsert encerra a vigência de id em to e grava r (o preço novo a
// partir de to), numa transação: ou os dois, ou nenhum.
func (s *PriceStore) CloseAndInsert(ctx context.Context, id int64, to, at time.Time, r domain.PriceRevision) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		"UPDATE price_revisions SET valid_to = ?, closed_at = ? WHERE id = ? AND valid_to IS NULL",
		to.UnixMilli(), at.UnixMilli(), id)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrValidityClosed
	}
	nid, err := insertRevision(ctx, tx, r)
	if err != nil {
		return 0, err
	}
	return nid, tx.Commit()
}

// CountUsingFrom conta as estimativas que usam a revisão id para respostas
// a partir de from (ms): as que um preço novo a partir dessa data não muda
// até o Recalcular.
func (s *PriceStore) CountUsingFrom(ctx context.Context, id, from int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM cost_estimates WHERE revision_id = ? AND usage_ts >= ?", id, from).Scan(&n)
	return n, err
}

// Disable marca a revisão como desativada (não vale para cálculos novos).
func (s *PriceStore) Disable(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, "UPDATE price_revisions SET disabled_at = ? WHERE id = ? AND disabled_at IS NULL", at.UnixMilli(), id)
	return err
}

// EstimateUses conta as estimativas que usam cada revisão.
func (s *PriceStore) EstimateUses(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT revision_id, count(*) FROM cost_estimates WHERE revision_id IS NOT NULL GROUP BY revision_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

const estCols = `provider, native_id, response_id, revision_id, currency, unit_tokens,
  input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens,
  input_cost, output_cost, cache_read_cost, cache_creation_cost, total_cost,
  status, reasons, usage_ts, calculated_at`

func nanosPtr(v sql.NullInt64) *domain.Nanos {
	if !v.Valid {
		return nil
	}
	n := domain.Nanos(v.Int64)
	return &n
}

func nanosArg(n *domain.Nanos) any {
	if n == nil {
		return nil
	}
	return int64(*n)
}

// Estimates devolve todas as estimativas gravadas.
func (s *PriceStore) Estimates(ctx context.Context) (map[EstimateKey]StoredEstimate, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+estCols+" FROM cost_estimates")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[EstimateKey]StoredEstimate{}
	for rows.Next() {
		var e StoredEstimate
		var rev, unit, in, outp, cr, cc, cin, cout, ccr, ccc, tot, uts sql.NullInt64
		var cur sql.NullString
		var reasons string
		var calc int64
		if err := rows.Scan(&e.Key.Provider, &e.Key.NativeID, &e.Key.ResponseID, &rev, &cur, &unit,
			&in, &outp, &cr, &cc, &cin, &cout, &ccr, &ccc, &tot, &e.Status, &reasons, &uts, &calc); err != nil {
			return nil, err
		}
		e.RevisionID, e.Currency, e.UnitTokens = int64Ptr(rev), cur.String, unit.Int64
		e.Tokens = domain.UsageTokens{Input: int64Ptr(in), Output: int64Ptr(outp), CacheRead: int64Ptr(cr), CacheCreation: int64Ptr(cc)}
		e.InputCost, e.OutputCost, e.CacheReadCost, e.CacheCreationCost, e.Total = nanosPtr(cin), nanosPtr(cout), nanosPtr(ccr), nanosPtr(ccc), nanosPtr(tot)
		if reasons != "" {
			e.Reasons = strings.Split(reasons, ";")
		}
		e.UsageTs, e.CalculatedAt = int64Ptr(uts), msTime(calc)
		out[e.Key] = e
	}
	return out, rows.Err()
}

// SaveEstimates grava (ou substitui) as estimativas numa transação.
func (s *PriceStore) SaveEstimates(ctx context.Context, es []StoredEstimate) error {
	if len(es) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO cost_estimates ("+estCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer st.Close()
	for _, e := range es {
		var cur, unit any
		if e.Currency != "" {
			cur, unit = e.Currency, e.UnitTokens
		}
		t := e.Tokens
		if _, err := st.ExecContext(ctx, e.Key.Provider, e.Key.NativeID, e.Key.ResponseID, e.RevisionID, cur, unit,
			t.Input, t.Output, t.CacheRead, t.CacheCreation,
			nanosArg(e.InputCost), nanosArg(e.OutputCost), nanosArg(e.CacheReadCost), nanosArg(e.CacheCreationCost), nanosArg(e.Total),
			e.Status, strings.Join(e.Reasons, ";"), e.UsageTs, e.CalculatedAt.UnixMilli()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
