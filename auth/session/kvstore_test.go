package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/resilience/cache"
)

func newCacheStore(t *testing.T) cache.Store {
	t.Helper()
	kv := cache.NewMemoryStore()
	t.Cleanup(func() { _ = kv.Close() })
	return kv
}

func TestKVStore_Lifecycle(t *testing.T) {
	t.Parallel()
	kv, err := session.NewKVStore(newCacheStore(t))
	require.NoError(t, err)
	mgr, err := session.New[data](kv)
	require.NoError(t, err)
	ctx := t.Context()

	s := mgr.Start(ctx)
	s.Data.Cart = []string{"sku-1", "sku-2"}
	require.NoError(t, mgr.Save(ctx, s))

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	assert.Equal(t, s.Data, got.Data)
	assert.WithinDuration(t, s.ExpiresAt, got.ExpiresAt, time.Second)

	require.NoError(t, mgr.Rotate(ctx, s))
	_, err = mgr.Load(ctx, got.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)

	require.NoError(t, mgr.Destroy(ctx, s))
}

func TestKVStore_NilBackend(t *testing.T) {
	t.Parallel()
	_, err := session.NewKVStore(nil)
	assert.ErrorIs(t, err, session.ErrInvalidConfig)
}

func TestKVStore_ExpiredRecordNeverStored(t *testing.T) {
	t.Parallel()
	kv, err := session.NewKVStore(newCacheStore(t))
	require.NoError(t, err)

	// Direct Store use: a record already past its deadline must not be
	// written (a backend treating TTL<=0 as eternal would keep it forever).
	rec := session.Record{ExpiresAt: time.Now().Add(-time.Minute)}
	tok, err := kv.Save(t.Context(), "tok", rec)
	require.NoError(t, err)
	assert.Equal(t, "tok", tok)
	_, err = kv.Load(t.Context(), "tok")
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestKVStore_ExpiredSaveReplacesLiveRecord(t *testing.T) {
	t.Parallel()
	kv, err := session.NewKVStore(newCacheStore(t))
	require.NoError(t, err)
	ctx := t.Context()

	live := session.Record{ExpiresAt: time.Now().Add(time.Hour)}
	_, err = kv.Save(ctx, "tok", live)
	require.NoError(t, err)

	dead := session.Record{ExpiresAt: time.Now().Add(-time.Minute)}
	_, err = kv.Save(ctx, "tok", dead)
	require.NoError(t, err)
	_, err = kv.Load(ctx, "tok")
	assert.ErrorIs(t, err, session.ErrNotFound, "an expired save must revoke, not retain, the live record")
}
