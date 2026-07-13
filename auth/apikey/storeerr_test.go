package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

// errStoreBoom is a sentinel returned by boomStore to prove Verify/Create
// wrap unexpected store errors with %w instead of swallowing them.
var errStoreBoom = errors.New("boom")

// boomStore implements apikey.Store, returning errStoreBoom from
// GetByHash and Create; the other methods are never exercised by these
// tests and return zero values.
type boomStore struct{}

func (boomStore) Create(context.Context, apikey.Key) error { return errStoreBoom }

func (boomStore) Get(context.Context, id.UUID) (apikey.Key, error) {
	return apikey.Key{}, errStoreBoom
}

func (boomStore) GetByHash(context.Context, string) (apikey.Key, error) {
	return apikey.Key{}, errStoreBoom
}

func (boomStore) List(context.Context, apikey.Filter) ([]apikey.Key, error) {
	return nil, errStoreBoom
}

func (boomStore) Revoke(context.Context, id.UUID, time.Time) error { return errStoreBoom }
func (boomStore) Expire(context.Context, id.UUID, time.Time) error { return errStoreBoom }
func (boomStore) Touch(context.Context, id.UUID, time.Time) error  { return errStoreBoom }

func TestCreate_StoreErrorWrapped(t *testing.T) {
	t.Parallel()
	mgr := apikey.New(boomStore{})
	_, _, err := mgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	require.Error(t, err)
	assert.ErrorIs(t, err, errStoreBoom)
}

func TestVerify_StoreErrorWrapped(t *testing.T) {
	t.Parallel()
	// Mint a structurally-valid plaintext from a real memory-store manager
	// sharing the same prefix — validKey only checks prefix/length/checksum,
	// so it satisfies boomStore's manager too and reaches GetByHash.
	memMgr := apikey.New(apikey.NewMemoryStore(), apikey.WithPrefix("sk_live"))
	_, plaintext, err := memMgr.Create(context.Background(), apikey.CreateParams{Subject: "u1"})
	require.NoError(t, err)

	mgr := apikey.New(boomStore{}, apikey.WithPrefix("sk_live"))
	_, err = mgr.Verify(context.Background(), plaintext)
	require.Error(t, err)
	assert.ErrorIs(t, err, errStoreBoom)
}
