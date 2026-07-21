package cookiestore_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/auth/session/cookiestore"
	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
)

type data struct {
	Theme string `json:"theme,omitempty"`
	Blob  string `json:"blob,omitempty"`
}

func newBox(t *testing.T) *secret.Box {
	t.Helper()
	box, err := secret.New([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	return box
}

func newStore(t *testing.T, opts ...cookiestore.Option) *cookiestore.Store {
	t.Helper()
	store, err := cookiestore.New(newBox(t), opts...)
	require.NoError(t, err)
	return store
}

func TestNew_Invalid(t *testing.T) {
	t.Parallel()
	_, err := cookiestore.New(nil)
	assert.ErrorIs(t, err, cookiestore.ErrInvalidConfig)
	_, err = cookiestore.New(newBox(t), cookiestore.WithMaxLen(0))
	assert.ErrorIs(t, err, cookiestore.ErrInvalidConfig)
}

func TestLifecycle(t *testing.T) {
	t.Parallel()
	mgr, err := session.New[data](newStore(t))
	require.NoError(t, err)
	ctx := t.Context()

	s := mgr.Start(ctx)
	s.Data.Theme = "dark"
	require.NoError(t, mgr.Save(ctx, s))
	require.NotEmpty(t, s.Token)

	got, err := mgr.Load(ctx, s.Token)
	require.NoError(t, err)
	assert.Equal(t, s.ID, got.ID)
	assert.Equal(t, "dark", got.Data.Theme)

	// Every Save re-encrypts: the token changes and both encodings decode
	// (statelessness means the old one cannot be revoked).
	old := s.Token
	require.NoError(t, mgr.Save(ctx, s))
	assert.NotEqual(t, old, s.Token)
	_, err = mgr.Load(ctx, old)
	require.NoError(t, err)
}

func TestLoad_TamperAndGarbage(t *testing.T) {
	t.Parallel()
	store := newStore(t)
	ctx := t.Context()
	tok, err := store.Save(ctx, "", session.Record{Data: []byte(`{}`)})
	require.NoError(t, err)

	cases := map[string]string{
		"garbage":     "not-a-token",
		"bad base64":  "!!!!",
		"empty":       "",
		"truncated":   tok[:len(tok)/2],
		"bit flipped": flipChar(tok),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := store.Load(ctx, token)
			assert.ErrorIs(t, err, session.ErrNotFound)
		})
	}
}

// flipChar changes one ciphertext character so authentication fails.
func flipChar(s string) string {
	i := len(s) / 2
	c := byte('A')
	if s[i] == 'A' {
		c = 'B'
	}
	return s[:i] + string(c) + s[i+1:]
}

func TestLoad_ForeignKeyRejected(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tok, err := newStore(t).Save(ctx, "", session.Record{Data: []byte(`{}`)})
	require.NoError(t, err)

	otherBox, err := secret.New([]byte("ffffffffffffffffffffffffffffffff"))
	require.NoError(t, err)
	other, err := cookiestore.New(otherBox)
	require.NoError(t, err)
	_, err = other.Load(ctx, tok)
	assert.ErrorIs(t, err, session.ErrNotFound)
}

func TestKeysetRotationKeepsOldTokensReadable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	oldKey, newKey := []byte("0123456789abcdef0123456789abcdef"), []byte("abcdef0123456789abcdef0123456789")

	oldBox, err := secret.New(oldKey)
	require.NoError(t, err)
	oldStore, err := cookiestore.New(oldBox)
	require.NoError(t, err)
	tok, err := oldStore.Save(ctx, "", session.Record{UserID: "u1"})
	require.NoError(t, err)

	ks, err := keyset.New(keyset.WithPrimary(2, newKey), keyset.WithRetired(0, oldKey))
	require.NoError(t, err)
	box, err := secret.FromKeyset(ks)
	require.NoError(t, err)
	rotated, err := cookiestore.New(box)
	require.NoError(t, err)

	rec, err := rotated.Load(ctx, tok)
	require.NoError(t, err, "tokens sealed under the retired key must stay readable")
	assert.Equal(t, "u1", rec.UserID)
}

func TestSave_TooLarge(t *testing.T) {
	t.Parallel()
	mgr, err := session.New[data](newStore(t))
	require.NoError(t, err)
	s := mgr.Start(t.Context())
	s.Data.Blob = strings.Repeat("x", 4000)
	err = mgr.Save(t.Context(), s)
	assert.ErrorIs(t, err, cookiestore.ErrTooLarge)

	tight, err := session.New[data](newStore(t, cookiestore.WithMaxLen(100)))
	require.NoError(t, err)
	s = tight.Start(t.Context())
	assert.ErrorIs(t, tight.Save(t.Context(), s), cookiestore.ErrTooLarge)
}
