//go:build integration

package pgstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
	"github.com/dmitrymomot/forge/auth/totp/pgstore"
	"github.com/dmitrymomot/forge/core/random"
	"github.com/dmitrymomot/forge/data/migration"
	"github.com/dmitrymomot/forge/data/postgres"
	"github.com/dmitrymomot/forge/testkit/pgtest"
)

var _ totp.Store = (*pgstore.Store)(nil)

func newStore(t *testing.T) *pgstore.Store {
	t.Helper()
	cfg := postgres.DefaultConfig()
	cfg.URL = pgtest.DSN(t)
	pool, err := postgres.Open(context.Background(), postgres.WithConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, migration.New(pgstore.Migrations, migration.WithTable("forge_totp_schema")).Up(context.Background(), db))
	return pgstore.New(pool)
}

// subj returns a unique subject per call: the table persists across test
// runs, so deterministic keys would collide on the PK.
func subj() string { return "subj-" + random.String(12) }

func rec(confirmed bool) *totp.Record {
	return &totp.Record{
		Secret:       []byte("ciphertext-bytes"),
		Confirmed:    confirmed,
		BackupHashes: [][]byte{{1, 2, 3}, {4, 5, 6}},
	}
}

func TestPg_SaveGetRoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sub := subj()

	_, err := s.Get(ctx, "", sub)
	assert.ErrorIs(t, err, totp.ErrNotFound)

	r := rec(true)
	r.LastUsedAt = time.Unix(1_700_000_010, 0).UTC() // step-start: whole seconds
	require.NoError(t, s.Save(ctx, "", sub, r))

	got, err := s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.Equal(t, r.Secret, got.Secret)
	assert.True(t, got.Confirmed)
	assert.True(t, got.LastUsedAt.Equal(r.LastUsedAt), "timestamptz round-trip is exact for whole seconds")
	assert.Equal(t, r.BackupHashes, got.BackupHashes)

	// Upsert: full replace.
	r2 := rec(false)
	require.NoError(t, s.Save(ctx, "", sub, r2))
	got, err = s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.False(t, got.Confirmed)
	assert.True(t, got.LastUsedAt.IsZero(), "NULL maps back to zero time")
}

func TestPg_SavePendingAndConfirm(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sub := subj()
	secret := []byte("sealed-" + sub)

	// SavePending on absent → stores a pending record with no backup codes.
	ok, err := s.SavePending(ctx, "", sub, &totp.Record{Secret: secret})
	require.NoError(t, err)
	assert.True(t, ok)
	got, err := s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.False(t, got.Confirmed)
	assert.Equal(t, secret, got.Secret)
	assert.Empty(t, got.BackupHashes)
	assert.True(t, got.LastUsedAt.IsZero())

	// SavePending again overwrites the still-pending secret.
	secret2 := []byte("sealed2-" + sub)
	ok, err = s.SavePending(ctx, "", sub, &totp.Record{Secret: secret2})
	require.NoError(t, err)
	assert.True(t, ok)

	// Confirm with the stale secret → refused (a racing SavePending swapped it).
	at := time.Unix(1_700_000_050, 0).UTC()
	ok, err = s.Confirm(ctx, "", sub, secret, at, [][]byte{{1}})
	require.NoError(t, err)
	assert.False(t, ok, "secret mismatch must not confirm")

	// Confirm with the current secret → activates.
	ok, err = s.Confirm(ctx, "", sub, secret2, at, [][]byte{{1, 2}, {3, 4}})
	require.NoError(t, err)
	assert.True(t, ok)
	got, err = s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.True(t, got.Confirmed)
	assert.True(t, got.LastUsedAt.Equal(at))
	assert.Equal(t, [][]byte{{1, 2}, {3, 4}}, got.BackupHashes)

	// Confirm again → false (already confirmed).
	ok, err = s.Confirm(ctx, "", sub, secret2, at, [][]byte{{9}})
	require.NoError(t, err)
	assert.False(t, ok)

	// SavePending must refuse to clobber a confirmed enrollment.
	ok, err = s.SavePending(ctx, "", sub, &totp.Record{Secret: []byte("sealed3")})
	require.NoError(t, err)
	assert.False(t, ok)
	got, err = s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.True(t, got.Confirmed)
	assert.Equal(t, secret2, got.Secret, "confirmed record untouched")
}

func TestPg_MarkUsed(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sub := subj()
	require.NoError(t, s.Save(ctx, "", sub, rec(true)))

	t0 := time.Unix(1_700_000_010, 0).UTC()
	ok, err := s.MarkUsed(ctx, "", sub, t0)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = s.MarkUsed(ctx, "", sub, t0)
	require.NoError(t, err)
	assert.False(t, ok, "same step claimed once")

	ok, err = s.MarkUsed(ctx, "", sub, t0.Add(-30*time.Second))
	require.NoError(t, err)
	assert.False(t, ok, "earlier step rejected")

	ok, err = s.MarkUsed(ctx, "", sub, t0.Add(30*time.Second))
	require.NoError(t, err)
	assert.True(t, ok, "later step advances")

	ok, err = s.MarkUsed(ctx, "", "ghost-"+sub, t0)
	require.NoError(t, err)
	assert.False(t, ok, "absent record: false, nil")
}

func TestPg_ConsumeBackup(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	sub := subj()
	require.NoError(t, s.Save(ctx, "", sub, rec(true)))

	ok, err := s.ConsumeBackup(ctx, "", sub, []byte{1, 2, 3})
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = s.ConsumeBackup(ctx, "", sub, []byte{1, 2, 3})
	require.NoError(t, err)
	assert.False(t, ok, "single use")

	got, err := s.Get(ctx, "", sub)
	require.NoError(t, err)
	assert.Equal(t, [][]byte{{4, 5, 6}}, got.BackupHashes)
}

func TestPg_DeleteAndDeleteTenant(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	tenant := "tenant-" + random.String(12)
	subA, subB := subj(), subj()

	require.NoError(t, s.Save(ctx, tenant, subA, rec(true)))
	require.NoError(t, s.Save(ctx, tenant, subB, rec(true)))
	require.NoError(t, s.Save(ctx, "", subA, rec(true)))

	// Tenant isolation on point reads.
	_, err := s.Get(ctx, tenant, subA)
	require.NoError(t, err)
	_, err = s.Get(ctx, "other-"+tenant, subA)
	assert.ErrorIs(t, err, totp.ErrNotFound)

	// Delete one record; absent delete is a no-op.
	require.NoError(t, s.Delete(ctx, tenant, subB))
	require.NoError(t, s.Delete(ctx, tenant, subB))
	_, err = s.Get(ctx, tenant, subB)
	assert.ErrorIs(t, err, totp.ErrNotFound)

	// DeleteTenant removes exactly the tenant's records.
	n, err := s.DeleteTenant(ctx, tenant)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	_, err = s.Get(ctx, "", subA)
	assert.NoError(t, err, "unscoped record untouched")
}

func TestPg_ManagerLifecycleEndToEnd(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	key := []byte("0123456789abcdef0123456789abcdef")
	mgr, err := totp.NewManager(s, key, totp.WithIssuer("Acme"))
	require.NoError(t, err)
	sub := subj()

	enr, err := mgr.BeginEnroll(ctx, sub, sub+"@acme.com")
	require.NoError(t, err)
	tp, err := totp.New()
	require.NoError(t, err)
	code, err := tp.Code(enr.Secret, time.Now())
	require.NoError(t, err)
	backup, err := mgr.ConfirmEnroll(ctx, sub, code)
	require.NoError(t, err)
	require.Len(t, backup, 10)

	// TOTP re-use of the confirm step is replayed.
	_, err = mgr.Verify(ctx, sub, code)
	assert.ErrorIs(t, err, totp.ErrReplayed)

	// Backup code path works and consumes.
	res, err := mgr.Verify(ctx, sub, backup[0])
	require.NoError(t, err)
	assert.True(t, res.UsedBackupCode)
	assert.Equal(t, 9, res.BackupRemaining)
	_, err = mgr.Verify(ctx, sub, backup[0])
	assert.ErrorIs(t, err, totp.ErrInvalidCode)

	require.NoError(t, mgr.Disable(ctx, sub))
	on, err := mgr.Enabled(ctx, sub)
	require.NoError(t, err)
	assert.False(t, on)
}
