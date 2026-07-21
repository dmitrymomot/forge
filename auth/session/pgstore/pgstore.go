package pgstore

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/crypto/digest"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations holds the goose migration creating forge_sessions, rooted so
// its .sql files sit at fsys root (data/migration.New globs fsys's root,
// not subdirectories). Apply via data/migration under its own version
// table ("forge_session_schema").
var Migrations fs.FS

func init() {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err) // unreachable: migrations/*.sql is embedded at compile time
	}
	Migrations = sub
}

// Store is the Postgres implementation of session.Store + session.UserIndex.
// Tokens are persisted only as SHA-256 digests, so a database leak exposes
// no usable credentials. The pool's lifecycle is the caller's.
type Store struct {
	pool *pgxpool.Pool
}

var (
	_ session.Store     = (*Store)(nil)
	_ session.UserIndex = (*Store)(nil)
)

// New builds a Postgres session Store. Apply Migrations first.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func tokenHash(token string) string {
	return hex.EncodeToString(digest.SHA256([]byte(token)))
}

const cols = `id, user_id, scope, ip, user_agent, data, fingerprint, created_at, expires_at, last_seen_at`

const saveSQL = `
INSERT INTO forge_sessions (token_hash, ` + cols + `)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (token_hash) DO UPDATE SET
	id = EXCLUDED.id, user_id = EXCLUDED.user_id, scope = EXCLUDED.scope,
	ip = EXCLUDED.ip, user_agent = EXCLUDED.user_agent,
	data = EXCLUDED.data, fingerprint = EXCLUDED.fingerprint,
	created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at,
	last_seen_at = EXCLUDED.last_seen_at`

// Save upserts rec under token's digest; the token is returned unchanged.
func (s *Store) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	var fp []byte
	if rec.Fingerprint.Hash != "" {
		b, err := json.Marshal(rec.Fingerprint)
		if err != nil {
			return "", err
		}
		fp = b
	}
	_, err := s.pool.Exec(ctx, saveSQL,
		tokenHash(token), rec.ID, rec.UserID, rec.Scope, rec.IP, rec.UserAgent, rec.Data, fp,
		rec.CreatedAt, rec.ExpiresAt, rec.LastSeenAt)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Load returns the record for token, or session.ErrNotFound.
func (s *Store) Load(ctx context.Context, token string) (session.Record, error) {
	return scanRecord(s.pool.QueryRow(ctx,
		`SELECT `+cols+` FROM forge_sessions WHERE token_hash = $1`, tokenHash(token)))
}

// Delete removes the record for token; absent tokens are a no-op.
func (s *Store) Delete(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM forge_sessions WHERE token_hash = $1`, tokenHash(token))
	return err
}

// ListByUser returns the records bound to userID within scope, newest first
// (UUIDv7 ids are time-ordered, so id DESC is creation order).
func (s *Store) ListByUser(ctx context.Context, scope, userID string) ([]session.Record, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+cols+` FROM forge_sessions WHERE scope = $1 AND user_id = $2 ORDER BY id DESC`,
		scope, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []session.Record{}
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteByUser removes every record bound to userID within scope, except the
// ids in keep.
func (s *Store) DeleteByUser(ctx context.Context, scope, userID string, keep ...id.UUID) error {
	// pgx encodes []id.UUID via each element's driver.Valuer; an explicit
	// string slice keeps the array encoding boring and predictable.
	keepIDs := make([]string, len(keep))
	for i, k := range keep {
		keepIDs[i] = k.String()
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM forge_sessions WHERE scope = $1 AND user_id = $2 AND NOT (id = ANY($3::uuid[]))`,
		scope, userID, keepIDs)
	return err
}

// DeleteOne removes the record for sessionID when it is bound to userID
// within scope; anything else is a no-op.
func (s *Store) DeleteOne(ctx context.Context, scope, userID string, sessionID id.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM forge_sessions WHERE scope = $1 AND user_id = $2 AND id = $3`,
		scope, userID, sessionID)
	return err
}

// DeleteExpired removes records whose deadline is at or before now and
// reports how many were dropped. Run it periodically (async/scheduler or
// cron) — expired rows are otherwise only deleted lazily on Load.
func (s *Store) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM forge_sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// row is satisfied by both pgx.Row and pgx.Rows.
type row interface{ Scan(dest ...any) error }

func scanRecord(r row) (session.Record, error) {
	var rec session.Record
	var fp []byte
	err := r.Scan(&rec.ID, &rec.UserID, &rec.Scope, &rec.IP, &rec.UserAgent, &rec.Data, &fp,
		&rec.CreatedAt, &rec.ExpiresAt, &rec.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.Record{}, session.ErrNotFound
	}
	if err != nil {
		return session.Record{}, err
	}
	if len(fp) > 0 {
		var d fingerprint.Digest
		if err := json.Unmarshal(fp, &d); err != nil {
			return session.Record{}, err
		}
		rec.Fingerprint = d
	}
	return rec, nil
}
