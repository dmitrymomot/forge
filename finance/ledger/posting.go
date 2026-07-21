package ledger

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/money"
	"github.com/dmitrymomot/forge/data/postgres"
)

// Posting is one pairwise movement: Amount flows from Src to Dst, balanced by
// construction. On input to Post, Ref, Src, Dst, and Amount are required;
// GroupRef correlates the postings of one multi-part event and Adjusts
// back-references the ref of a posting this one corrects. Seq and CreatedAt
// are assigned by the ledger.
type Posting struct {
	CreatedAt time.Time
	Ref       string
	GroupRef  string
	Adjusts   string
	Amount    money.Money
	Seq       int64
	Src       id.UUID
	Dst       id.UUID
}

// postSQL is one data-modifying-CTE statement: replay gate, sorted row locks
// on the floored accounts only, floor check on the locked src row, balance
// moves, and the posting insert — all gated so that a failed check modifies
// nothing and leaves the caller's transaction usable. A concurrent insert of
// the same ref aborts the whole statement on the unique index (ErrRefRace).
const postSQL = `
WITH existing AS (
    SELECT seq, group_ref, src_account, dst_account, amount::text AS amount, currency, adjusts, created_at
    FROM ledger_postings WHERE ref = $1
), locked AS (
    SELECT id, balance, held, floor
    FROM ledger_accounts
    WHERE id IN ($2, $3) AND tenant = $8 AND floor IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM existing)
    ORDER BY id
    FOR UPDATE
), meta AS (
    SELECT id, currency FROM ledger_accounts WHERE id IN ($2, $3) AND tenant = $8
), ok AS (
    SELECT 1 AS yes
    WHERE NOT EXISTS (SELECT 1 FROM existing)
      AND (SELECT count(*) FROM meta) = 2
      AND NOT EXISTS (SELECT 1 FROM meta WHERE currency <> $5)
      AND NOT EXISTS (
          SELECT 1 FROM locked
          WHERE id = $2 AND balance - held - $4::numeric < floor
      )
), upd AS (
    UPDATE ledger_accounts a
    SET balance = a.balance + CASE WHEN a.id = $2 THEN -$4::numeric ELSE $4::numeric END
    FROM locked l
    WHERE a.id = l.id AND EXISTS (SELECT 1 FROM ok)
    RETURNING a.id
), ins AS (
    INSERT INTO ledger_postings (ref, group_ref, src_account, dst_account, amount, currency, adjusts, created_at)
    SELECT $1, $6, $2, $3, $4::numeric, $5, $7, $9
    WHERE EXISTS (SELECT 1 FROM ok)
    RETURNING seq
)
SELECT
    EXISTS (SELECT 1 FROM existing),
    EXISTS (SELECT 1 FROM meta WHERE id = $2),
    EXISTS (SELECT 1 FROM meta WHERE id = $3),
    EXISTS (SELECT 1 FROM meta WHERE currency <> $5),
    (SELECT seq FROM ins),
    e.seq, e.group_ref, e.src_account, e.dst_account, e.amount, e.currency, e.adjusts, e.created_at
FROM (SELECT 1) AS one
LEFT JOIN existing e ON true`

// Post records one pairwise posting inside the caller's transaction, moving
// p.Amount from p.Src to p.Dst. Floored accounts are row-locked in sorted
// order and the src floor is enforced by predicate — zero modified rows means
// ErrInsufficientFunds, with the transaction still usable. Floor-free
// accounts are never locked or updated; their balances derive from postings.
//
// Post is idempotent by Ref: a replay returns the original posting unchanged
// (ErrRefConflict if the replay carries different parameters). Two
// transactions racing the same new Ref resolve by unique index: the loser's
// transaction aborts with ErrRefRace and observes the replay on retry.
func (l *Ledger) Post(ctx context.Context, tx pgx.Tx, p Posting) (Posting, error) {
	if p.Ref == "" {
		return Posting{}, fmt.Errorf("%w: posting ref is required", ErrInvalidRef)
	}
	if !p.Amount.IsPositive() {
		return Posting{}, fmt.Errorf("%w: posting amount must be positive, got %s", ErrInvalidAmount, p.Amount)
	}
	if p.Src == p.Dst {
		return Posting{}, fmt.Errorf("%w: %s", ErrSameAccount, p.Src)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Posting{}, err
	}

	var (
		replay, srcFound, dstFound, mismatch bool
		newSeq                               *int64
		ex                                   existingPosting
	)
	now := l.clk.Now().UTC()
	err = tx.QueryRow(ctx, postSQL,
		p.Ref, p.Src, p.Dst,
		p.Amount.Amount().String(), p.Amount.Currency().Code,
		p.GroupRef, p.Adjusts, tenant, now,
	).Scan(&replay, &srcFound, &dstFound, &mismatch, &newSeq,
		&ex.seq, &ex.groupRef, &ex.src, &ex.dst, &ex.amount, &ex.currency, &ex.adjusts, &ex.createdAt)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return Posting{}, fmt.Errorf("%w: ref %q", ErrRefRace, p.Ref)
		}
		return Posting{}, fmt.Errorf("ledger: post %q: %w", p.Ref, err)
	}

	if replay {
		orig, err := ex.posting(p.Ref)
		if err != nil {
			return Posting{}, err
		}
		if err := matchReplay(orig, p); err != nil {
			return Posting{}, err
		}
		return orig, nil
	}
	switch {
	case !srcFound:
		return Posting{}, fmt.Errorf("%w: src %s", ErrAccountNotFound, p.Src)
	case !dstFound:
		return Posting{}, fmt.Errorf("%w: dst %s", ErrAccountNotFound, p.Dst)
	case mismatch:
		return Posting{}, fmt.Errorf("%w: posting is %s", ErrCurrencyMismatch, p.Amount.Currency().Code)
	case newSeq == nil:
		return Posting{}, fmt.Errorf("%w: %s from %s", ErrInsufficientFunds, p.Amount, p.Src)
	}
	p.Seq = *newSeq
	p.CreatedAt = now
	return p, nil
}

// matchReplay verifies a replayed Post carries the original parameters.
func matchReplay(orig, p Posting) error {
	eq, err := orig.Amount.Equal(p.Amount)
	if err != nil || !eq ||
		orig.Src != p.Src || orig.Dst != p.Dst ||
		orig.GroupRef != p.GroupRef || orig.Adjusts != p.Adjusts {
		return fmt.Errorf("%w: ref %q", ErrRefConflict, p.Ref)
	}
	return nil
}

// existingPosting scans the nullable replay columns of postSQL.
type existingPosting struct {
	seq       *int64
	groupRef  *string
	src       *id.UUID
	dst       *id.UUID
	amount    *string
	currency  *string
	adjusts   *string
	createdAt *time.Time
}

// posting materializes the replayed row.
func (e existingPosting) posting(ref string) (Posting, error) {
	amt, err := parseMoney(*e.amount, *e.currency)
	if err != nil {
		return Posting{}, err
	}
	return Posting{
		Seq: *e.seq, Ref: ref, GroupRef: *e.groupRef,
		Src: *e.src, Dst: *e.dst, Amount: amt,
		Adjusts: *e.adjusts, CreatedAt: *e.createdAt,
	}, nil
}

const postingSelect = `
SELECT seq, ref, group_ref, src_account, dst_account, amount::text, currency, adjusts, created_at
FROM ledger_postings p `

// tenantGuard scopes posting reads: the posting's src account must belong to
// the resolved tenant (src and dst always share a tenant — cross-tenant
// postings cannot be created).
const tenantGuard = `EXISTS (SELECT 1 FROM ledger_accounts ta WHERE ta.id = p.src_account AND ta.tenant = `

// PostingByRef fetches one posting by its external ref.
func (l *Ledger) PostingByRef(ctx context.Context, db DB, ref string) (Posting, error) {
	if ref == "" {
		return Posting{}, fmt.Errorf("%w: ref is required", ErrInvalidRef)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return Posting{}, err
	}
	row := db.QueryRow(ctx, postingSelect+`WHERE ref = $1 AND `+tenantGuard+`$2)`, ref, tenant)
	post, err := scanPosting(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Posting{}, fmt.Errorf("%w: ref %q", ErrPostingNotFound, ref)
		}
		return Posting{}, err
	}
	return post, nil
}

// postingsSQL lists an account's statement newest-first with keyset
// pagination; the UNION ALL arms each ride one (account, seq DESC) index.
const postingsSQL = postingSelect + `
WHERE p.seq IN (
    (SELECT seq FROM ledger_postings WHERE src_account = $1 AND seq < $2 ORDER BY seq DESC LIMIT $3)
    UNION ALL
    (SELECT seq FROM ledger_postings WHERE dst_account = $1 AND seq < $2 ORDER BY seq DESC LIMIT $3)
) AND ` + tenantGuard + `$4)
ORDER BY p.seq DESC LIMIT $3`

// Postings lists an account's postings (either side), newest first,
// keyset-paginated by Seq: pass beforeSeq 0 to start, the last returned Seq
// to continue.
func (l *Ledger) Postings(ctx context.Context, db DB, account id.UUID, beforeSeq int64, limit int) ([]Posting, error) {
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	if beforeSeq <= 0 {
		beforeSeq = math.MaxInt64 // start from the newest; keeps seq < $2 a plain index range
	}
	return l.queryPostings(ctx, db, postingsSQL, account, beforeSeq, limit, tenant)
}

// PostingsByGroup lists the postings of one correlated event in seq order.
func (l *Ledger) PostingsByGroup(ctx context.Context, db DB, groupRef string) ([]Posting, error) {
	if groupRef == "" {
		return nil, fmt.Errorf("%w: group ref is required", ErrInvalidRef)
	}
	tenant, err := l.resolveScope(ctx)
	if err != nil {
		return nil, err
	}
	return l.queryPostings(ctx, db,
		postingSelect+`WHERE group_ref = $1 AND `+tenantGuard+`$2) ORDER BY p.seq`,
		groupRef, tenant)
}

// queryPostings runs a postingSelect-shaped query and scans all rows.
func (l *Ledger) queryPostings(ctx context.Context, db DB, sql string, args ...any) ([]Posting, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("ledger: list postings: %w", err)
	}
	defer rows.Close()
	var out []Posting
	for rows.Next() {
		post, err := scanPosting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: list postings: %w", err)
	}
	return out, nil
}

// scanPosting reads one postingSelect row.
func scanPosting(row pgx.Row) (Posting, error) {
	var (
		post         Posting
		amount, code string
	)
	err := row.Scan(&post.Seq, &post.Ref, &post.GroupRef, &post.Src, &post.Dst,
		&amount, &code, &post.Adjusts, &post.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Posting{}, err
		}
		return Posting{}, fmt.Errorf("ledger: scan posting: %w", err)
	}
	post.Amount, err = parseMoney(amount, code)
	if err != nil {
		return Posting{}, err
	}
	return post, nil
}
