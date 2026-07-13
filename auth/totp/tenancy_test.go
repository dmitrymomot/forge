package totp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
)

type tenantCtxKey struct{}

func tenantCtx(t *testing.T, tenant string) context.Context {
	t.Helper()
	return context.WithValue(t.Context(), tenantCtxKey{}, tenant)
}

func scopeHook(ctx context.Context) (string, error) {
	v, _ := ctx.Value(tenantCtxKey{}).(string)
	return v, nil
}

// scopedEnroll enrolls subject under tenant and returns issued backup codes.
func scopedEnroll(t *testing.T, m *totp.Manager, ctx context.Context, subject string) (*totp.Enrollment, []string) {
	t.Helper()
	enr, err := m.BeginEnroll(ctx, subject, subject+"@acme.com")
	require.NoError(t, err)
	codes, err := m.ConfirmEnroll(ctx, subject, codeFor(t, enr, fixedNow))
	require.NoError(t, err)
	return enr, codes
}

func TestRegenerateBackupCodes(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m, _, oldCodes := enroll(t, store)
	ctx := t.Context()

	newCodes, err := m.RegenerateBackupCodes(ctx, "alice")
	require.NoError(t, err)
	assert.Len(t, newCodes, 10)

	// Old codes are dead, new ones live.
	_, err = m.Verify(ctx, "alice", oldCodes[0])
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
	res, err := m.Verify(ctx, "alice", newCodes[0])
	require.NoError(t, err)
	assert.True(t, res.UsedBackupCode)

	// Not enrolled / pending → ErrNotEnrolled.
	_, err = m.RegenerateBackupCodes(ctx, "ghost")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)
}

func TestDisable(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m, enr, _ := enroll(t, store)
	ctx := t.Context()

	require.NoError(t, m.Disable(ctx, "alice"))
	on, err := m.Enabled(ctx, "alice")
	require.NoError(t, err)
	assert.False(t, on)
	_, err = m.Verify(ctx, "alice", codeFor(t, enr, fixedNow))
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)

	// Disable is idempotent; re-enroll works after it.
	require.NoError(t, m.Disable(ctx, "alice"))
	_, err = m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
}

func TestScope_IsolatesTenants(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store, totp.WithScope(scopeHook))

	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")
	enrA, _ := scopedEnroll(t, m, ctxA, "alice")

	// Same subject, other tenant: independent lifecycle.
	on, err := m.Enabled(ctxB, "alice")
	require.NoError(t, err)
	assert.False(t, on, "tenant-b sees no enrollment")
	_, err = m.Verify(ctxB, "alice", codeFor(t, enrA, fixedNow))
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)

	enrB, _ := scopedEnroll(t, m, ctxB, "alice")
	assert.NotEqual(t, enrA.Secret, enrB.Secret, "independent secrets per tenant")

	// Disable in one tenant leaves the other intact.
	require.NoError(t, m.Disable(ctxB, "alice"))
	on, err = m.Enabled(ctxA, "alice")
	require.NoError(t, err)
	assert.True(t, on)
}

func TestScope_FailClosed(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()

	// Hook returning empty scope → ErrScope on every method.
	m := newManager(t, store, totp.WithScope(scopeHook))
	noTenant := t.Context() // no tenant value in context → hook returns ""
	_, err := m.BeginEnroll(noTenant, "alice", "a@acme.com")
	assert.ErrorIs(t, err, totp.ErrScope)
	_, err = m.ConfirmEnroll(noTenant, "alice", "123456")
	assert.ErrorIs(t, err, totp.ErrScope)
	_, err = m.Verify(noTenant, "alice", "123456")
	assert.ErrorIs(t, err, totp.ErrScope)
	_, err = m.Enabled(noTenant, "alice")
	assert.ErrorIs(t, err, totp.ErrScope)
	_, err = m.LastVerified(noTenant, "alice")
	assert.ErrorIs(t, err, totp.ErrScope)
	_, err = m.RegenerateBackupCodes(noTenant, "alice")
	assert.ErrorIs(t, err, totp.ErrScope)
	assert.ErrorIs(t, m.Disable(noTenant, "alice"), totp.ErrScope)
	_, err = m.DisableTenant(noTenant)
	assert.ErrorIs(t, err, totp.ErrScope)

	// Hook returning an error → ErrScope wrapping it.
	boom := errors.New("boom")
	mErr := newManager(t, store, totp.WithScope(func(context.Context) (string, error) {
		return "", boom
	}))
	_, err = mErr.Enabled(t.Context(), "alice")
	assert.ErrorIs(t, err, totp.ErrScope)
	assert.ErrorIs(t, err, boom)
}

func TestScope_UnscopedAndScopedNeverCollide(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	unscoped := newManager(t, store)
	scoped := newManager(t, store, totp.WithScope(scopeHook))

	enrU, _ := scopedEnroll(t, unscoped, t.Context(), "alice")
	_ = enrU

	on, err := scoped.Enabled(tenantCtx(t, "tenant-a"), "alice")
	require.NoError(t, err)
	assert.False(t, on, "scoped manager cannot see the unscoped record")
}

func TestDisableTenant(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store, totp.WithScope(scopeHook))
	ctxA, ctxB := tenantCtx(t, "tenant-a"), tenantCtx(t, "tenant-b")

	scopedEnroll(t, m, ctxA, "alice")
	scopedEnroll(t, m, ctxA, "bob")
	scopedEnroll(t, m, ctxB, "alice")

	n, err := m.DisableTenant(ctxA)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	on, err := m.Enabled(ctxA, "alice")
	require.NoError(t, err)
	assert.False(t, on)
	on, err = m.Enabled(ctxB, "alice")
	require.NoError(t, err)
	assert.True(t, on, "tenant-b untouched")

	// Unscoped manager has no tenant to delete.
	unscoped := newManager(t, store)
	_, err = unscoped.DisableTenant(t.Context())
	assert.ErrorIs(t, err, totp.ErrScope)
}
