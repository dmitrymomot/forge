package totp_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/auth/totp"
	"github.com/dmitrymomot/forge/core/clock"
	"github.com/dmitrymomot/forge/crypto/keyset"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

// fixedNow pins Manager clocks; individual tests advance via a fresh Manager.
var fixedNow = time.Unix(1_700_000_000, 0).UTC()

func newManager(t *testing.T, store totp.Store, opts ...totp.Option) *totp.Manager {
	t.Helper()
	opts = append([]totp.Option{
		totp.WithIssuer("Acme"),
		totp.WithClock(clock.NewMock(fixedNow)),
	}, opts...)
	m, err := totp.NewManager(store, testKey, opts...)
	require.NoError(t, err)
	return m
}

// codeFor computes the current valid code for an enrollment's secret.
func codeFor(t *testing.T, enr *totp.Enrollment, at time.Time) string {
	t.Helper()
	tp, err := totp.New()
	require.NoError(t, err)
	code, err := tp.Code(enr.Secret, at)
	require.NoError(t, err)
	return code
}

func TestNewManager_Validation(t *testing.T) {
	t.Parallel()
	_, err := totp.NewManager(nil, testKey)
	assert.Error(t, err, "nil store")
	_, err = totp.NewManager(totp.NewMemoryStore(), []byte("short"))
	assert.Error(t, err, "bad key size")
	_, err = totp.NewManager(totp.NewMemoryStore(), testKey, totp.WithBackupCodeCount(0))
	assert.Error(t, err)
	_, err = totp.NewManager(totp.NewMemoryStore(), testKey, totp.WithBackupCodeLength(7))
	assert.Error(t, err)
}

func TestManagerFromKeyset(t *testing.T) {
	t.Parallel()
	ks, err := keyset.New(keyset.WithPrimary(1, testKey))
	require.NoError(t, err)
	m, err := totp.ManagerFromKeyset(totp.NewMemoryStore(), ks, totp.WithIssuer("Acme"))
	require.NoError(t, err)
	require.NotNil(t, m)
}

func TestBeginEnroll(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	assert.NotEmpty(t, enr.Secret)
	assert.Contains(t, enr.URI, "otpauth://totp/")
	assert.Contains(t, enr.URI, "issuer=Acme")

	// The stored secret is ciphertext, not the plaintext we returned.
	rec, err := store.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.False(t, rec.Confirmed)
	assert.NotContains(t, string(rec.Secret), enr.Secret)

	// Re-begin before confirm: idempotent overwrite, fresh secret.
	enr2, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)
	assert.NotEqual(t, enr.Secret, enr2.Secret)

	// Empty subject is caller error.
	_, err = m.BeginEnroll(ctx, "", "x@acme.com")
	assert.Error(t, err)
}

func TestConfirmEnroll(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m := newManager(t, store)
	ctx := t.Context()

	// No pending record.
	_, err := m.ConfirmEnroll(ctx, "alice", "123456")
	assert.ErrorIs(t, err, totp.ErrNotEnrolled)

	enr, err := m.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)

	// Wrong first code: record stays pending.
	_, err = m.ConfirmEnroll(ctx, "alice", "000000")
	assert.ErrorIs(t, err, totp.ErrInvalidCode)
	rec, err := store.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.False(t, rec.Confirmed)

	// Correct first code: confirmed, backup codes issued, step persisted.
	codes, err := m.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	require.NoError(t, err)
	assert.Len(t, codes, 10)
	rec, err = store.Get(ctx, "", "alice")
	require.NoError(t, err)
	assert.True(t, rec.Confirmed)
	assert.Len(t, rec.BackupHashes, 10)
	assert.False(t, rec.LastUsedAt.IsZero())

	// BeginEnroll after confirm: refused.
	_, err = m.BeginEnroll(ctx, "alice", "alice@acme.com")
	assert.ErrorIs(t, err, totp.ErrAlreadyEnrolled)

	// ConfirmEnroll after confirm: refused.
	_, err = m.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	assert.ErrorIs(t, err, totp.ErrAlreadyEnrolled)
}

func TestConfirmEnroll_BackupOptionsRespected(t *testing.T) {
	t.Parallel()
	m := newManager(t, totp.NewMemoryStore(),
		totp.WithBackupCodeCount(4), totp.WithBackupCodeLength(15))
	ctx := t.Context()
	enr, err := m.BeginEnroll(ctx, "bob", "bob@acme.com")
	require.NoError(t, err)
	codes, err := m.ConfirmEnroll(ctx, "bob", codeFor(t, enr, fixedNow))
	require.NoError(t, err)
	require.Len(t, codes, 4)
	assert.Equal(t, "xxxxx-xxxxx-xxxxx", func() string {
		out := []byte{}
		for _, r := range codes[0] {
			if r == '-' {
				out = append(out, '-')
			} else {
				out = append(out, 'x')
			}
		}
		return string(out)
	}())
}

func TestManager_WrongKeyDoesNotReadAsBadCode(t *testing.T) {
	t.Parallel()
	store := totp.NewMemoryStore()
	m1 := newManager(t, store)
	ctx := t.Context()
	enr, err := m1.BeginEnroll(ctx, "alice", "alice@acme.com")
	require.NoError(t, err)

	// A manager with different key material cannot decrypt: the error must
	// NOT be ErrInvalidCode — this is an operator problem, not a user typo.
	m2, err := totp.NewManager(store, []byte("ffffffffffffffffffffffffffffffff"),
		totp.WithClock(clock.NewMock(fixedNow)))
	require.NoError(t, err)
	_, err = m2.ConfirmEnroll(ctx, "alice", codeFor(t, enr, fixedNow))
	require.Error(t, err)
	assert.NotErrorIs(t, err, totp.ErrInvalidCode)
}
