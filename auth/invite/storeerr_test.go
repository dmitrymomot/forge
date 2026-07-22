package invite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/invite"
	"github.com/dmitrymomot/forge/core/id"
)

// errStoreBoom is a sentinel returned by boomStore to prove the manager
// wraps unexpected store errors with %w instead of swallowing them.
var errStoreBoom = errors.New("boom")

// boomStore implements invite.Store, failing every call with errStoreBoom.
type boomStore struct{}

func (boomStore) Create(context.Context, invite.Invite) error { return errStoreBoom }

func (boomStore) Get(context.Context, id.UUID) (invite.Invite, error) {
	return invite.Invite{}, errStoreBoom
}

func (boomStore) GetByHash(context.Context, string) (invite.Invite, error) {
	return invite.Invite{}, errStoreBoom
}

func (boomStore) List(context.Context, invite.Filter) ([]invite.Invite, error) {
	return nil, errStoreBoom
}

func (boomStore) Accept(context.Context, id.UUID, time.Time) error         { return errStoreBoom }
func (boomStore) Revoke(context.Context, id.UUID, time.Time) error         { return errStoreBoom }
func (boomStore) Rotate(context.Context, id.UUID, string, time.Time) error { return errStoreBoom }

func TestStoreErrorsWrapped(t *testing.T) {
	t.Parallel()
	mgr := invite.New(boomStore{})
	ctx := context.Background()

	_, _, err := mgr.Create(ctx, invite.CreateParams{Email: "a@b.com"})
	assert.ErrorIs(t, err, errStoreBoom)

	// A well-formed token reaches GetByHash; the failure must surface.
	real := invite.New(invite.NewMemoryStore())
	_, plaintext, err := real.Create(ctx, invite.CreateParams{Email: "a@b.com"})
	require.NoError(t, err)
	_, err = mgr.Accept(ctx, plaintext)
	assert.ErrorIs(t, err, errStoreBoom)
	_, err = mgr.Peek(ctx, plaintext)
	assert.ErrorIs(t, err, errStoreBoom)
}

// panicStore proves malformed tokens are rejected before any store access.
type panicStore struct{ invite.Store }

func (panicStore) GetByHash(context.Context, string) (invite.Invite, error) {
	panic("store touched for malformed token")
}

func TestMalformedTokenSkipsStore(t *testing.T) {
	t.Parallel()
	mgr := invite.New(panicStore{})
	_, err := mgr.Accept(context.Background(), "inv_garbage")
	assert.ErrorIs(t, err, invite.ErrMalformedToken)
	_, err = mgr.Peek(context.Background(), "inv_garbage")
	assert.ErrorIs(t, err, invite.ErrMalformedToken)
}
