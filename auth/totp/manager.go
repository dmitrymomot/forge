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
//
// The ErrAlreadyEnrolled guard is check-then-act, not atomic: a BeginEnroll
// racing a concurrent ConfirmEnroll for the same subject can read the record
// while it is still pending and then overwrite the just-confirmed enrollment
// back to unconfirmed, orphaning the backup codes ConfirmEnroll returned.
// Enroll and confirm are sequential user actions, so this needs two in-flight
// requests for one subject at the same instant; serialize per-subject
// enrollment (or gate on a conditional store write) if that is reachable in
// your deployment.
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

// Verify checks code for subject: TOTP first (replay-safe — the matched
// step is atomically claimed in the store), then falls back to one-time
// backup codes (atomically consumed). One call serves a single "enter your
// code or a backup code" input. ErrNotEnrolled for absent or unconfirmed
// records; ErrReplayed when the step was already claimed (including by a
// concurrent request); ErrInvalidCode otherwise.
func (m *Manager) Verify(ctx context.Context, subject, code string) (VerifyResult, error) {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return VerifyResult{}, err
	}
	rec, err := m.record(ctx, tenant, subject)
	if err != nil {
		return VerifyResult{}, err
	}
	if !rec.Confirmed {
		return VerifyResult{}, ErrNotEnrolled
	}
	plain, err := m.openSecret(rec)
	if err != nil {
		return VerifyResult{}, err
	}

	matchedAt, verr := m.t.Verify(plain, code, rec.LastUsedAt)
	switch {
	case verr == nil:
		ok, err := m.store.MarkUsed(ctx, tenant, subject, matchedAt)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("totp: store mark used: %w", err)
		}
		if !ok {
			return VerifyResult{}, ErrReplayed
		}
		return VerifyResult{}, nil
	case errors.Is(verr, ErrReplayed):
		return VerifyResult{}, ErrReplayed
	}

	// TOTP mismatch — try the backup codes.
	idx, ok := VerifyBackupCode(code, rec.BackupHashes)
	if !ok {
		return VerifyResult{}, ErrInvalidCode
	}
	consumed, err := m.store.ConsumeBackup(ctx, tenant, subject, rec.BackupHashes[idx])
	if err != nil {
		return VerifyResult{}, fmt.Errorf("totp: store consume backup: %w", err)
	}
	if !consumed {
		// Lost a race: someone spent this code between Get and here.
		return VerifyResult{}, ErrInvalidCode
	}
	return VerifyResult{
		UsedBackupCode: true,
		// Count from the snapshot we verified against; a concurrent consume
		// may make this off by one — it is advisory UI data, not state.
		BackupRemaining: len(rec.BackupHashes) - 1,
	}, nil
}

// Enabled reports whether subject has a confirmed enrollment. Absent and
// pending both read false — it is a policy query, not an error path.
func (m *Manager) Enabled(ctx context.Context, subject string) (bool, error) {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return false, err
	}
	rec, err := m.store.Get(ctx, tenant, subject)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("totp: store get: %w", err)
	}
	return rec.Confirmed, nil
}

// LastVerified returns the step-start time of the last successful TOTP
// verification — the input to a grace-period check (see doc.go). Zero time
// with a nil error cannot happen for a confirmed record (ConfirmEnroll
// stamps the first step); ErrNotEnrolled for absent or pending records.
func (m *Manager) LastVerified(ctx context.Context, subject string) (time.Time, error) {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return time.Time{}, err
	}
	rec, err := m.record(ctx, tenant, subject)
	if err != nil {
		return time.Time{}, err
	}
	if !rec.Confirmed {
		return time.Time{}, ErrNotEnrolled
	}
	return rec.LastUsedAt, nil
}

// RegenerateBackupCodes invalidates every outstanding backup code and
// returns a fresh set — the only time the new codes exist in plaintext.
// ErrNotEnrolled unless a confirmed enrollment exists.
func (m *Manager) RegenerateBackupCodes(ctx context.Context, subject string) ([]string, error) {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return nil, err
	}
	rec, err := m.record(ctx, tenant, subject)
	if err != nil {
		return nil, err
	}
	if !rec.Confirmed {
		return nil, ErrNotEnrolled
	}
	codes, hashes, err := GenerateBackupCodes(m.t.cfg.backupCount, m.t.cfg.backupLength)
	if err != nil {
		return nil, err
	}
	rec.BackupHashes = hashes
	if err := m.store.Save(ctx, tenant, subject, rec); err != nil {
		return nil, fmt.Errorf("totp: store save: %w", err)
	}
	return codes, nil
}

// Disable removes the enrollment entirely — secret, backup codes, state.
// Idempotent: disabling an absent enrollment is a no-op.
func (m *Manager) Disable(ctx context.Context, subject string) error {
	tenant, err := m.tenant(ctx)
	if err != nil {
		return err
	}
	if err := m.store.Delete(ctx, tenant, subject); err != nil {
		return fmt.Errorf("totp: store delete: %w", err)
	}
	return nil
}

// DisableTenant bulk-deletes every enrollment in the scope-resolved tenant
// (offboarding, GDPR erasure) and returns the count. It requires WithScope:
// an unscoped Manager has no tenant to delete and returns ErrScope.
// Platform-level jobs run it with a context bound to the target tenant.
func (m *Manager) DisableTenant(ctx context.Context) (int, error) {
	if m.t.cfg.scope == nil {
		return 0, ErrScope
	}
	tenant, err := m.tenant(ctx)
	if err != nil {
		return 0, err
	}
	n, err := m.store.DeleteTenant(ctx, tenant)
	if err != nil {
		return 0, fmt.Errorf("totp: store delete tenant: %w", err)
	}
	return n, nil
}
