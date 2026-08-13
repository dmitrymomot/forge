package apikey_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/apikey"
	"github.com/dmitrymomot/forge/core/id"
)

// mustConfig builds a validated Config or fails the test.
func mustConfig(t *testing.T, opts ...apikey.Option) apikey.Config {
	t.Helper()
	cfg, err := apikey.NewConfig(opts...)
	require.NoError(t, err)
	return cfg
}

func TestNewConfig_RejectsBadPrefix(t *testing.T) {
	t.Parallel()
	_, err := apikey.NewConfig(apikey.WithPrefix(""))
	assert.ErrorIs(t, err, apikey.ErrConfig)
	_, err = apikey.NewConfig(apikey.WithPrefix("SK-Live"))
	assert.ErrorIs(t, err, apikey.ErrConfig)
}

func TestZeroConfigRejected(t *testing.T) {
	t.Parallel()
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	var zero apikey.Config

	_, _, err := apikey.Create(ctx, zero, apikey.CreateParams{Subject: "u1"}, mem.Save)
	assert.ErrorIs(t, err, apikey.ErrConfig)
	_, err = apikey.Get(ctx, zero, id.UUID{15: 1}, mem.Load)
	assert.ErrorIs(t, err, apikey.ErrConfig)
	_, err = apikey.List(ctx, zero, apikey.Filter{}, mem.List)
	assert.ErrorIs(t, err, apikey.ErrConfig)
	_, err = apikey.Verify(ctx, zero, "key_whatever", mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrConfig)
	_, err = apikey.NewVerifier(zero, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrConfig)
}

func TestNilEffectRejected(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()

	_, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, nil)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
	_, err = apikey.Get(ctx, cfg, id.UUID{15: 1}, nil)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
	_, err = apikey.List(ctx, cfg, apikey.Filter{}, nil)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
	assert.ErrorIs(t, apikey.Revoke(ctx, cfg, id.UUID{15: 1}, mem.Load, nil), apikey.ErrNilEffect)
	_, _, err = apikey.Rotate(ctx, cfg, id.UUID{15: 1}, 0, mem.Load, nil)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
	_, err = apikey.Verify(ctx, cfg, "key_whatever", nil, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
	_, err = apikey.NewVerifier(cfg, nil, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrNilEffect)
}

func TestCreate_KeyAnatomy(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t, apikey.WithPrefix("sk_live"))
	mem := apikey.NewMemoryStore()
	ctx := context.Background()

	k, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{
		Subject: "user_42", Tenant: "org_7", Name: "CI deploy", Scopes: []string{"deploy:write"},
	}, mem.Save)
	require.NoError(t, err)

	assert.Len(t, plaintext, len("sk_live")+1+43+6)
	assert.True(t, strings.HasPrefix(plaintext, "sk_live_"))
	for _, c := range plaintext[len("sk_live_"):] {
		assert.Contains(t, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", string(c))
	}
	assert.Equal(t, plaintext[:12], k.Preview)
	assert.NotContains(t, k.Hash, plaintext[8:20])
	assert.False(t, k.ID.IsZero())
	assert.False(t, k.CreatedAt.IsZero())

	stored, err := mem.Load(ctx, k.ID)
	require.NoError(t, err)
	assert.Equal(t, "user_42", stored.Subject)
	assert.Equal(t, "org_7", stored.Tenant)
}

// TestCreate_SaveClosureOwnsTheWrite pins the point of the design: the call
// site's own closure performs the write and may carry data the package
// never models.
func TestCreate_SaveClosureOwnsTheWrite(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()

	var sawOrg string
	appOrgID := "org_from_request"
	_, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"},
		func(ctx context.Context, k apikey.Key) error {
			sawOrg = appOrgID
			return mem.Save(ctx, k)
		})
	require.NoError(t, err)
	assert.Equal(t, appOrgID, sawOrg)
}

func TestCreate_SubjectRequired(t *testing.T) {
	t.Parallel()
	mem := apikey.NewMemoryStore()
	_, _, err := apikey.Create(context.Background(), mustConfig(t), apikey.CreateParams{}, mem.Save)
	assert.ErrorIs(t, err, apikey.ErrSubjectRequired)
}

func TestCreate_DefaultPrefix(t *testing.T) {
	t.Parallel()
	mem := apikey.NewMemoryStore()
	_, plaintext, err := apikey.Create(context.Background(), mustConfig(t),
		apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(plaintext, "key_"))
}

func TestGetRevoke(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	k, plaintext, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)

	got, err := apikey.Get(ctx, cfg, k.ID, mem.Load)
	require.NoError(t, err)
	assert.Equal(t, "u1", got.Subject)

	require.NoError(t, apikey.Revoke(ctx, cfg, k.ID, mem.Load, mem.Revoke))
	got, err = apikey.Get(ctx, cfg, k.ID, mem.Load)
	require.NoError(t, err)
	assert.False(t, got.RevokedAt.IsZero())

	_, err = apikey.Verify(ctx, cfg, plaintext, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	assert.ErrorIs(t, apikey.Revoke(ctx, cfg, id.UUID{15: 9}, mem.Load, mem.Revoke), apikey.ErrNotFound)
}

func TestList(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	_, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)
	_, _, err = apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u2"}, mem.Save)
	require.NoError(t, err)

	all, err := apikey.List(ctx, cfg, apikey.Filter{}, mem.List)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	mine, err := apikey.List(ctx, cfg, apikey.Filter{Subject: "u1"}, mem.List)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "u1", mine[0].Subject)
}

func TestRotate(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t, apikey.WithPrefix("sk_live"))
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	old, oldPlain, err := apikey.Create(ctx, cfg, apikey.CreateParams{
		Subject: "u1", Tenant: "t1", Name: "prod", Scopes: []string{"a"}, Meta: map[string]string{"m": "1"},
	}, mem.Save)
	require.NoError(t, err)

	grace := time.Hour
	before := time.Now().UTC()
	fresh, freshPlain, err := apikey.Rotate(ctx, cfg, old.ID, grace, mem.Load, mem.Swap)
	require.NoError(t, err)

	assert.Equal(t, old.Subject, fresh.Subject)
	assert.Equal(t, old.Tenant, fresh.Tenant)
	assert.Equal(t, old.Name, fresh.Name)
	assert.Equal(t, old.Scopes, fresh.Scopes)
	assert.Equal(t, old.Meta, fresh.Meta)
	assert.NotEqual(t, old.ID, fresh.ID)
	assert.NotEqual(t, oldPlain, freshPlain)

	_, err = apikey.Verify(ctx, cfg, oldPlain, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)
	_, err = apikey.Verify(ctx, cfg, freshPlain, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)

	oldStored, err := apikey.Get(ctx, cfg, old.ID, mem.Load)
	require.NoError(t, err)
	assert.WithinDuration(t, before.Add(grace), oldStored.ExpiresAt, 5*time.Second)
}

func TestRotate_ZeroGraceCutsOver(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	old, oldPlain, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)

	_, freshPlain, err := apikey.Rotate(ctx, cfg, old.ID, 0, mem.Load, mem.Swap)
	require.NoError(t, err)

	_, err = apikey.Verify(ctx, cfg, oldPlain, mem.LoadByHash, mem.Touch)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
	_, err = apikey.Verify(ctx, cfg, freshPlain, mem.LoadByHash, mem.Touch)
	assert.NoError(t, err)
}

func TestRotate_DeadKeysRejected(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()

	revoked, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)
	require.NoError(t, apikey.Revoke(ctx, cfg, revoked.ID, mem.Load, mem.Revoke))
	_, _, err = apikey.Rotate(ctx, cfg, revoked.ID, time.Hour, mem.Load, mem.Swap)
	assert.ErrorIs(t, err, apikey.ErrKeyRevoked)

	expired, _, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u2"}, mem.Save)
	require.NoError(t, err)
	require.NoError(t, mem.Expire(ctx, expired.ID, time.Now().UTC().Add(-time.Minute)))
	_, _, err = apikey.Rotate(ctx, cfg, expired.ID, time.Hour, mem.Load, mem.Swap)
	assert.ErrorIs(t, err, apikey.ErrKeyExpired)
}

// TestRotate_FailedSwapChangesNothing pins what the atomic SwapFunc buys:
// a failed rotation leaves the old key valid and persists no replacement,
// so there is nothing to compensate for.
func TestRotate_FailedSwapChangesNothing(t *testing.T) {
	t.Parallel()
	cfg := mustConfig(t)
	mem := apikey.NewMemoryStore()
	ctx := context.Background()
	errSwapBoom := errors.New("swap boom")

	old, oldPlain, err := apikey.Create(ctx, cfg, apikey.CreateParams{Subject: "u1"}, mem.Save)
	require.NoError(t, err)

	_, _, err = apikey.Rotate(ctx, cfg, old.ID, time.Hour, mem.Load,
		func(context.Context, id.UUID, time.Time, apikey.Key) error { return errSwapBoom })
	require.Error(t, err)
	assert.ErrorIs(t, err, errSwapBoom)

	_, err = apikey.Verify(ctx, cfg, oldPlain, mem.LoadByHash, mem.Touch)
	require.NoError(t, err)

	all, err := apikey.List(ctx, cfg, apikey.Filter{}, mem.List)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}
