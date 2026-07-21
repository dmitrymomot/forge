package session_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/core/id"
)

type data struct {
	Cart  []string `json:"cart,omitempty"`
	Theme string   `json:"theme,omitempty"`
}

func newManager(t *testing.T, opts ...session.Option) (*session.Manager[data], *session.MemoryStore, *clock.Mock) {
	t.Helper()
	ck := clock.NewMock(time.Now())
	store := session.NewMemoryStore()
	mgr, err := session.New[data](store, append([]session.Option{session.WithClock(ck)}, opts...)...)
	require.NoError(t, err)
	return mgr, store, ck
}

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		store session.Store
		opts  []session.Option
	}{
		{"nil store", nil, nil},
		{"non-positive ttl", session.NewMemoryStore(), []session.Option{session.WithTTL(0)}},
		{"lifetime below ttl", session.NewMemoryStore(), []session.Option{session.WithTTL(time.Hour), session.WithLifetime(time.Minute)}},
		{"unknown mode", session.NewMemoryStore(), []session.Option{session.WithFingerprint(session.Mode(9))}},
		{"nil clock", session.NewMemoryStore(), []session.Option{session.WithClock(nil)}},
		{"nil logger", session.NewMemoryStore(), []session.Option{session.WithLogger(nil)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := session.New[data](tc.store, tc.opts...)
			assert.ErrorIs(t, err, session.ErrInvalidConfig)
		})
	}
}

func TestLifecycle_RoundTrip(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()

	s := mgr.Start(ctx)
	assert.Empty(t, s.Token, "token must not exist before Save")
	assert.False(t, s.ID.IsZero())

	s.Data.Cart = []string{"sku-1"}
	s.Data.Theme = "dark"
	require.NoError(t, mgr.Save(ctx, s))
	require.NotEmpty(t, s.Token)

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	assert.Equal(t, s.Data, got.Data)
	assert.Equal(t, s.Token, got.Token)
	assert.Empty(t, got.UserID)
}

func TestLoad_UnknownAndEmptyToken(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	_, err := mgr.Load(t.Context(), "no-such-token")
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = mgr.Load(t.Context(), "")
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestLoad_UnsavedStartIsNotFound(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	s := mgr.Start(t.Context())
	_, err := mgr.Load(t.Context(), s.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestExpiry_IdleTTL(t *testing.T) {
	t.Parallel()
	mgr, store, ck := newManager(t, session.WithTTL(time.Hour))
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	ck.Advance(time.Hour)
	_, err := mgr.Load(ctx, s.Token)
	assert.ErrorIs(t, err, session.ErrExpired)
	// The expired record is deleted, not just refused.
	_, err = store.Load(ctx, s.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestExpiry_SaveSlidesIdleDeadline(t *testing.T) {
	t.Parallel()
	mgr, _, ck := newManager(t, session.WithTTL(time.Hour))
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	ck.Advance(30 * time.Minute)
	require.NoError(t, mgr.Save(ctx, s))

	ck.Advance(45 * time.Minute) // 75min after start: alive only because Save slid the deadline
	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
}

func TestExpiry_AbsoluteLifetimeCapsSliding(t *testing.T) {
	t.Parallel()
	mgr, _, ck := newManager(t, session.WithTTL(time.Hour), session.WithLifetime(2*time.Hour))
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	// Keep the session active; the absolute cap must still kill it.
	for range 3 {
		ck.Advance(30 * time.Minute)
		require.NoError(t, mgr.Save(ctx, s))
	}
	ck.Advance(30 * time.Minute) // 2h after start
	_, err := mgr.Load(ctx, s.Token)
	assert.ErrorIs(t, err, session.ErrExpired)

	// Saving a session past its absolute lifetime must not resurrect it.
	err = mgr.Save(ctx, s)
	assert.ErrorIs(t, err, session.ErrExpired)
}

func TestRotate(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()
	s := mgr.Start(ctx)
	s.Data.Theme = "dark"
	require.NoError(t, mgr.Save(ctx, s))
	oldToken, oldID, oldCreated := s.Token, s.ID, s.CreatedAt

	require.NoError(t, mgr.Rotate(ctx, s))
	assert.NotEqual(t, oldToken, s.Token, "rotation must mint a new token")
	assert.Equal(t, oldID, s.ID, "identity survives rotation")
	assert.Equal(t, oldCreated, s.CreatedAt, "absolute deadline anchor survives rotation")

	_, err := mgr.Load(ctx, oldToken)
	assert.ErrorIs(t, err, session.ErrNotFound, "old token must die with the rotation")

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, "dark", got.Data.Theme)
}

func TestRotate_NeverSaved(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	s := mgr.Start(t.Context())
	require.NoError(t, mgr.Rotate(t.Context(), s))
	require.NotEmpty(t, s.Token)
	_, err := mgr.Load(t.Context(), s.Token)
	require.NoError(t, err)
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))
	anon := s.Token

	require.NoError(t, mgr.Authenticate(ctx, s, "user-1"))
	assert.Equal(t, "user-1", s.UserID)
	assert.NotEqual(t, anon, s.Token, "login must rotate the token (fixation)")

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", got.UserID)

	assert.ErrorIs(t, mgr.Authenticate(ctx, s, ""), session.ErrInvalidInput)
}

func TestDestroy(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))
	token := s.Token

	require.NoError(t, mgr.Destroy(ctx, s))
	assert.Empty(t, s.Token)
	assert.Empty(t, s.UserID)
	_, err := mgr.Load(ctx, token)
	assert.ErrorIs(t, err, session.ErrNotFound)

	// Destroying a never-saved session is a no-op.
	require.NoError(t, mgr.Destroy(ctx, mgr.Start(ctx)))
}

func TestUserSessions(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()

	var tokens [3]string
	for i := range 3 {
		s := mgr.Start(ctx)
		require.NoError(t, mgr.Authenticate(ctx, s, "user-1"))
		tokens[i] = s.Token
	}
	other := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, other, "user-2"))

	list, err := mgr.ListUserSessions(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, list, 3)
	for _, s := range list {
		assert.Equal(t, "user-1", s.UserID)
		assert.Empty(t, s.Token, "listings must not expose bearer tokens")
	}

	// Log out everywhere, then re-persist the current device.
	current, err := mgr.Load(ctx, tokens[2])
	require.NoError(t, err)
	require.NoError(t, mgr.DeleteUserSessions(ctx, "user-1"))
	require.NoError(t, mgr.Save(ctx, current))

	list, err = mgr.ListUserSessions(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, current.ID, list[0].ID)

	for _, tok := range tokens[:2] {
		_, err := mgr.Load(ctx, tok)
		assert.ErrorIs(t, err, session.ErrNotFound)
	}
	// The other user is untouched.
	_, err = mgr.Load(ctx, other.Token)
	require.NoError(t, err)

	_, err = mgr.ListUserSessions(ctx, "")
	assert.ErrorIs(t, err, session.ErrInvalidInput)
	assert.ErrorIs(t, mgr.DeleteUserSessions(ctx, ""), session.ErrInvalidInput)
}

func TestLogoutOthers(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()

	var others [2]*session.Session[data]
	for i := range others {
		s := mgr.Start(ctx)
		require.NoError(t, mgr.Authenticate(ctx, s, "user-1"))
		others[i] = s
	}
	current := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, current, "user-1"))

	require.NoError(t, mgr.LogoutOthers(ctx, current))

	// The current device survives, every other one is gone.
	_, err := mgr.Load(ctx, current.Token)
	require.NoError(t, err)
	for _, o := range others {
		_, err := mgr.Load(ctx, o.Token)
		assert.ErrorIs(t, err, session.ErrNotFound)
	}
	list, err := mgr.ListUserSessions(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, current.ID, list[0].ID)

	// Anonymous or never-saved sessions cannot anchor a LogoutOthers.
	anon := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, anon))
	assert.ErrorIs(t, mgr.LogoutOthers(ctx, anon), session.ErrInvalidInput)
	fresh := mgr.Start(ctx)
	fresh.UserID = "user-1"
	assert.ErrorIs(t, mgr.LogoutOthers(ctx, fresh), session.ErrInvalidInput)
}

func TestRevokeUserSession(t *testing.T) {
	t.Parallel()
	mgr, _, _ := newManager(t)
	ctx := t.Context()

	victim := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, victim, "user-1"))
	keeper := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, keeper, "user-1"))

	require.NoError(t, mgr.RevokeUserSession(ctx, "user-1", victim.ID))
	_, err := mgr.Load(ctx, victim.Token)
	assert.ErrorIs(t, err, session.ErrNotFound)
	_, err = mgr.Load(ctx, keeper.Token)
	require.NoError(t, err)

	// Another user naming the same id revokes nothing (IDOR guard).
	other := mgr.Start(ctx)
	require.NoError(t, mgr.Authenticate(ctx, other, "user-2"))
	require.NoError(t, mgr.RevokeUserSession(ctx, "user-1", other.ID))
	_, err = mgr.Load(ctx, other.Token)
	require.NoError(t, err, "a session id under the wrong user must revoke nothing")

	// Idempotent: revoking an already-gone session is a no-op.
	require.NoError(t, mgr.RevokeUserSession(ctx, "user-1", victim.ID))

	assert.ErrorIs(t, mgr.RevokeUserSession(ctx, "", keeper.ID), session.ErrInvalidInput)
	assert.ErrorIs(t, mgr.RevokeUserSession(ctx, "user-1", id.UUID{}), session.ErrInvalidInput)
}

func TestLastSeenAt_RefreshedOnSave(t *testing.T) {
	t.Parallel()
	mgr, _, ck := newManager(t)
	ctx := t.Context()
	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))
	first := s.LastSeenAt

	ck.Advance(time.Hour)
	require.NoError(t, mgr.Save(ctx, s))
	assert.Equal(t, first.Add(time.Hour), s.LastSeenAt)

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, s.LastSeenAt, got.LastSeenAt)
}

func TestUserSessions_NoUserIndex(t *testing.T) {
	t.Parallel()
	kv, err := session.NewKVStore(newCacheStore(t))
	require.NoError(t, err)
	mgr, err := session.New[data](kv)
	require.NoError(t, err)

	_, err = mgr.ListUserSessions(t.Context(), "user-1")
	assert.ErrorIs(t, err, session.ErrNoUserIndex)
	assert.ErrorIs(t, mgr.DeleteUserSessions(t.Context(), "user-1"), session.ErrNoUserIndex)
}

// failStore lets tests fail individual Store calls.
type failStore struct {
	session.Store
	saveErr   error
	loadErr   error
	deleteErr error
}

func (f *failStore) Save(ctx context.Context, token string, rec session.Record) (string, error) {
	if f.saveErr != nil {
		return "", f.saveErr
	}
	return f.Store.Save(ctx, token, rec)
}

func (f *failStore) Load(ctx context.Context, token string) (session.Record, error) {
	if f.loadErr != nil {
		return session.Record{}, f.loadErr
	}
	return f.Store.Load(ctx, token)
}

func (f *failStore) Delete(ctx context.Context, token string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.Store.Delete(ctx, token)
}

func TestStoreErrors_Wrapped(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	ctx := t.Context()

	fs := &failStore{Store: session.NewMemoryStore()}
	mgr, err := session.New[data](fs)
	require.NoError(t, err)

	fs.saveErr = boom
	s := mgr.Start(ctx)
	err = mgr.Save(ctx, s)
	assert.ErrorIs(t, err, session.ErrStore)
	assert.ErrorIs(t, err, boom)
	fs.saveErr = nil

	require.NoError(t, mgr.Save(ctx, s))
	fs.loadErr = boom
	_, err = mgr.Load(ctx, s.Token)
	assert.ErrorIs(t, err, session.ErrStore)
	fs.loadErr = nil

	fs.deleteErr = boom
	err = mgr.Destroy(ctx, s)
	assert.ErrorIs(t, err, session.ErrStore)
	assert.NotEmpty(t, s.Token, "a failed destroy must not pretend the session is gone")
}

func TestRotate_SaveFailureKeepsOldToken(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	ctx := t.Context()
	fs := &failStore{Store: session.NewMemoryStore()}
	mgr, err := session.New[data](fs)
	require.NoError(t, err)

	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))
	token := s.Token

	fs.saveErr = boom
	require.ErrorIs(t, mgr.Rotate(ctx, s), session.ErrStore)
	assert.Equal(t, token, s.Token, "failed rotation must keep the session loadable")
	fs.saveErr = nil
	_, err = mgr.Load(ctx, s.Token)
	require.NoError(t, err)
}

func TestRotate_DeleteFailureReported(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	ctx := t.Context()
	fs := &failStore{Store: session.NewMemoryStore()}
	mgr, err := session.New[data](fs)
	require.NoError(t, err)

	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))

	fs.deleteErr = boom
	err = mgr.Rotate(ctx, s)
	assert.ErrorIs(t, err, session.ErrStore, "a live old token must be reported, not swallowed")
}

func TestAuthenticate_FailureRestoresUser(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fs := &failStore{Store: session.NewMemoryStore()}
	mgr, err := session.New[data](fs)
	require.NoError(t, err)

	s := mgr.Start(ctx)
	require.NoError(t, mgr.Save(ctx, s))
	fs.saveErr = errors.New("boom")
	require.Error(t, mgr.Authenticate(ctx, s, "user-1"))
	assert.Empty(t, s.UserID, "failed login must not leave the session claiming a user")
}

func TestCodecError(t *testing.T) {
	t.Parallel()
	store := session.NewMemoryStore()
	mgr, err := session.New[chan int](store)
	require.NoError(t, err)
	s := mgr.Start(t.Context())
	s.Data = make(chan int)
	assert.ErrorIs(t, mgr.Save(t.Context(), s), session.ErrCodec)
}
