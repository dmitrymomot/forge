package session_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/id"
)

func TestMemoryStore_CallerCannotMutateStoredState(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	ctx := t.Context()

	rec := session.Record{ID: id.NewUUID(), Data: []byte(`{"a":1}`), ExpiresAt: time.Now().Add(time.Hour)}
	_, err := store.Save(ctx, "tok", rec)
	require.NoError(t, err)
	rec.Data[0] = 'X' // caller keeps mutating its copy after Save

	got, err := store.Load(ctx, "tok")
	require.NoError(t, err)
	assert.Equal(t, byte('{'), got.Data[0], "stored state leaked the caller's slice")
	got.Data[0] = 'Y' // and mutating a loaded record must not write through

	again, err := store.Load(ctx, "tok")
	require.NoError(t, err)
	assert.Equal(t, byte('{'), again.Data[0])
}

func TestMemoryStore_ListByUserNewestFirst(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	ctx := t.Context()

	// Monotonic ids: same-millisecond UUIDv7 ties are otherwise unordered.
	gen := id.NewGenerator(id.WithMonotonic())
	var ids [3]id.UUID
	for i := range 3 {
		rec := session.Record{ID: gen.UUID(), UserID: "u", ExpiresAt: time.Now().Add(time.Hour)}
		ids[i] = rec.ID
		_, err := store.Save(ctx, string(rune('a'+i)), rec)
		require.NoError(t, err)
	}
	list, err := store.ListByUser(ctx, "", "u")
	require.NoError(t, err)
	require.Len(t, list, 3)
	assert.Equal(t, ids[2], list[0].ID)
	assert.Equal(t, ids[0], list[2].ID)
}

func TestMemoryStore_PurgeExpired(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	ctx := t.Context()
	now := time.Now()

	_, err := store.Save(ctx, "dead", session.Record{ID: id.NewUUID(), ExpiresAt: now.Add(-time.Minute)})
	require.NoError(t, err)
	_, err = store.Save(ctx, "live", session.Record{ID: id.NewUUID(), ExpiresAt: now.Add(time.Hour)})
	require.NoError(t, err)

	assert.Equal(t, 1, store.PurgeExpired(now))
	_, err = store.Load(ctx, "dead")
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = store.Load(ctx, "live")
	require.NoError(t, err)
	assert.Equal(t, 0, store.PurgeExpired(now))
}
