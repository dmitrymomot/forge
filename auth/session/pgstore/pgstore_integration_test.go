//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/pgstore"
	"github.com/dmitrymomot/forge/core/id"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
	"github.com/dmitrymomot/forge/web/fingerprint"
)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_session_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// mkRecord builds a record whose user/scope are unique per call: the table
// persists across test runs, so deterministic values would inflate
// ListByUser counts on re-runs.
func mkRecord(t *testing.T) session.Record {
	t.Helper()
	uid := id.NewUUID()
	return session.Record{
		ID:        uid,
		UserID:    "user-" + uid.String(),
		Scope:     "scope-" + uid.String(),
		Data:      []byte(`{"theme":"dark"}`),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
}

func newToken() string { return random.URLSafe(32) }

func TestPg_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord(t)
	rec.Fingerprint = fingerprint.Digest{
		Parts:   map[string]string{"ua": "aa", "ip": "bb"},
		Hash:    "combined",
		Version: 1,
	}
	token := newToken()

	returned, err := s.Save(ctx, token, rec)
	require.NoError(t, err)
	assert.Equal(t, token, returned, "server-side stores must not rewrite the token")

	got, err := s.Load(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, rec.ID, got.ID)
	assert.Equal(t, rec.UserID, got.UserID)
	assert.Equal(t, rec.Scope, got.Scope)
	assert.Equal(t, rec.Data, got.Data)
	assert.Equal(t, rec.Fingerprint, got.Fingerprint)
	// timestamptz stores microseconds; compare within that precision.
	assert.WithinDuration(t, rec.CreatedAt, got.CreatedAt, time.Millisecond)
	assert.WithinDuration(t, rec.ExpiresAt, got.ExpiresAt, time.Millisecond)
}

func TestPg_LoadUnknown(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	_, err := s.Load(context.Background(), newToken())
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestPg_SaveUpsertsAndFingerprintNull(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord(t)
	token := newToken()

	_, err := s.Save(ctx, token, rec)
	require.NoError(t, err)

	rec.UserID = "rebound-" + rec.UserID
	rec.Data = []byte(`{"theme":"light"}`)
	rec.ExpiresAt = rec.ExpiresAt.Add(time.Hour)
	_, err = s.Save(ctx, token, rec)
	require.NoError(t, err, "second save under the same token must upsert")

	got, err := s.Load(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, rec.UserID, got.UserID)
	assert.Equal(t, rec.Data, got.Data)
	assert.Empty(t, got.Fingerprint.Hash, "a zero digest must round-trip as zero (SQL NULL)")
}

func TestPg_Delete(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()
	rec := mkRecord(t)
	token := newToken()
	_, err := s.Save(ctx, token, rec)
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, token))
	_, err = s.Load(ctx, token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	require.NoError(t, s.Delete(ctx, token), "deleting an absent token is a no-op")
}

func TestPg_UserIndex(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	scope := "scope-" + id.NewUUID().String()
	user := "user-" + id.NewUUID().String()
	gen := id.NewGenerator(id.WithMonotonic())

	var ids [3]id.UUID
	for i := range 3 {
		rec := mkRecord(t)
		rec.ID = gen.UUID()
		rec.Scope, rec.UserID = scope, user
		ids[i] = rec.ID
		_, err := s.Save(ctx, newToken(), rec)
		require.NoError(t, err)
	}
	// Same user in a sibling scope must stay invisible.
	other := mkRecord(t)
	other.UserID = user
	otherToken := newToken()
	_, err := s.Save(ctx, otherToken, other)
	require.NoError(t, err)

	list, err := s.ListByUser(ctx, scope, user)
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, ids[2], list[0].ID, "newest first")
	assert.Equal(t, ids[0], list[2].ID)

	require.NoError(t, s.DeleteByUser(ctx, scope, user))
	list, err = s.ListByUser(ctx, scope, user)
	require.NoError(t, err)
	assert.Empty(t, list)
	_, err = s.Load(ctx, otherToken)
	require.NoError(t, err, "sibling-scope session must survive DeleteByUser")

	require.NoError(t, s.DeleteByUser(ctx, scope, user), "deleting a user with no sessions is a no-op")
}

func TestPg_DeleteExpired(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ctx := context.Background()

	dead := mkRecord(t)
	dead.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	deadToken := newToken()
	_, err := s.Save(ctx, deadToken, dead)
	require.NoError(t, err)
	live := mkRecord(t)
	liveToken := newToken()
	_, err = s.Save(ctx, liveToken, live)
	require.NoError(t, err)

	n, err := s.DeleteExpired(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1), "parallel tests may add other expired rows")

	_, err = s.Load(ctx, deadToken)
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = s.Load(ctx, liveToken)
	require.NoError(t, err)
}

// TestPg_ManagerEndToEnd runs the full Manager lifecycle against Postgres.
func TestPg_ManagerEndToEnd(t *testing.T) {
	t.Parallel()
	type data struct {
		Theme string `json:"theme,omitempty"`
	}
	mgr, err := session.New[data](newStore(t))
	require.NoError(t, err)
	ctx := context.Background()

	s := mgr.Start(ctx)
	s.Data.Theme = "dark"
	require.NoError(t, mgr.Save(ctx, s))
	require.NoError(t, mgr.Authenticate(ctx, s, "user-"+s.ID.String()))

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, "dark", got.Data.Theme)

	list, err := mgr.ListUserSessions(ctx, s.UserID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, mgr.Destroy(ctx, s))
	_, err = mgr.Load(ctx, got.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
}
