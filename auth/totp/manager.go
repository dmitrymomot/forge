package totp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/secret"
)

// Manager orchestrates the full 2FA lifecycle — enroll, confirm, verify,
// recover, disable — over a Store. Secrets are sealed with an AEAD box
// before they reach the store and backup codes are stored as hashes;
// encryption is mandatory by construction. Safe for concurrent use.
type Manager struct {
	t     *TOTP
	store Store
	box   *secret.Box
}

// Enrollment is what BeginEnroll hands the UI: the plaintext secret for
// manual entry and the otpauth:// URI to render as a QR (core/qrcode's
// DataURI composes). Neither is persisted in this form.
type Enrollment struct {
	Secret string
	URI    string
}

// VerifyResult reports which path verified the code.
type VerifyResult struct {
	// UsedBackupCode is true when a one-time backup code (now consumed)
	// matched instead of a TOTP code.
	UsedBackupCode bool
	// BackupRemaining is the number of backup codes left after this call;
	// meaningful only when UsedBackupCode is true.
	BackupRemaining int
}

// NewManager builds a Manager sealing secrets with key (32 bytes,
// AES-256-GCM). Use ManagerFromKeyset for key rotation.
func NewManager(store Store, key []byte, opts ...Option) (*Manager, error) {
	box, err := secret.New(key)
	if err != nil {
		return nil, fmt.Errorf("totp: %w", err)
	}
	return newManager(store, box, opts)
}

// ManagerFromKeyset builds a Manager whose box encrypts under the keyset's
// primary key and decrypts under any version — rotation without re-enrolling.
func ManagerFromKeyset(store Store, ks *keyset.Keyset, opts ...Option) (*Manager, error) {
	box, err := secret.FromKeyset(ks)
	if err != nil {
		return nil, fmt.Errorf("totp: %w", err)
	}
	return newManager(store, box, opts)
}

func newManager(store Store, box *secret.Box, opts []Option) (*Manager, error) {
	if store == nil {
		return nil, errors.New("totp: nil store")
	}
	t, err := New(opts...)
	if err != nil {
		return nil, err
	}
	if t.cfg.backupCount < 1 {
		return nil, fmt.Errorf("totp: backup code count must be >= 1, got %d", t.cfg.backupCount)
	}
	if t.cfg.backupLength < 8 {
		return nil, fmt.Errorf("totp: backup code length must be >= 8, got %d", t.cfg.backupLength)
	}
	return &Manager{t: t, store: store, box: box}, nil
}

// tenant resolves the scope hook, failing closed: with a hook configured, a
// hook error or empty scope aborts the operation with ErrScope so a scoped
// operation can never fall through to the unscoped ("") namespace.
func (m *Manager) tenant(ctx context.Context) (string, error) {
	if m.t.cfg.scope == nil {
		return "", nil
	}
	s, err := m.t.cfg.scope(ctx)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrScope, err)
	}
	if s == "" {
		return "", ErrScope
	}
	return s, nil
}

// record fetches and translates the store sentinel: absent = ErrNotEnrolled
// at the Manager surface.
func (m *Manager) record(ctx context.Context, tenant, subject string) (*Record, error) {
	r, err := m.store.Get(ctx, tenant, subject)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("totp: store get: %w", err)
	}
	return r, nil
}

// openSecret unseals a record's secret. Failure means wrong key material —
// an operator problem surfaced as secret.ErrDecryptFailed, never as
// ErrInvalidCode.
func (m *Manager) openSecret(r *Record) (string, error) {
	plain, err := m.box.Decrypt(r.Secret)
	if err != nil {
		return "", fmt.Errorf("totp: unseal secret: %w", err)
	}
	return string(plain), nil
}

// BeginEnroll starts (or restarts) enrollment for subject: it generates a
// fresh secret, seals it, saves an unconfirmed record — overwriting any
// earlier unconfirmed one, so re-showing the QR is safe — and returns the
// plaintext secret and provisioning URI for the UI. ErrAlreadyEnrolled if a
// confirmed enrollment exists; Disable first to re-enroll.
func (m *Manager) BeginEnroll(ctx context.Context, subject, account string) (*Enrollment, error) {
	if subject == "" {
		return nil, errors.New("totp: empty subject")
	}
	tenant, err := m.tenant(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := m.store.Get(ctx, tenant, subject)
	switch {
	case err == nil && existing.Confirmed:
		return nil, ErrAlreadyEnrolled
	case err != nil && !errors.Is(err, ErrNotFound):
		return nil, fmt.Errorf("totp: store get: %w", err)
	}
	plain, err := m.t.GenerateSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := m.box.Encrypt([]byte(plain))
	if err != nil {
		return nil, fmt.Errorf("totp: seal secret: %w", err)
	}
	if err := m.store.Save(ctx, tenant, subject, &Record{Secret: sealed}); err != nil {
		return nil, fmt.Errorf("totp: store save: %w", err)
	}
	return &Enrollment{Secret: plain, URI: m.t.ProvisioningURI(plain, account)}, nil
}

// ConfirmEnroll proves the authenticator was enrolled by verifying its
// first code, activates the record, and returns the one-time backup codes —
// the only time they exist in plaintext. ErrNotEnrolled without a pending
// record; ErrInvalidCode leaves the record pending for another attempt.
func (m *Manager) ConfirmEnroll(ctx context.Context, subject, code string) ([]string, error) {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := m.record(ctx, tenant, subject)
	if err != nil {
		return nil, err
	}
	if rec.Confirmed {
		return nil, ErrAlreadyEnrolled
	}
	plain, err := m.openSecret(rec)
	if err != nil {
		return nil, err
	}
	matchedAt, err := m.t.Verify(plain, code, time.Time{})
	if err != nil {
		return nil, err
	}
	codes, hashes, err := GenerateBackupCodes(m.t.cfg.backupCount, m.t.cfg.backupLength)
	if err != nil {
		return nil, err
	}
	rec.Confirmed = true
	rec.LastUsedAt = matchedAt
	rec.BackupHashes = hashes
	if err := m.store.Save(ctx, tenant, subject, rec); err != nil {
		return nil, fmt.Errorf("totp: store save: %w", err)
	}
	return codes, nil
}
