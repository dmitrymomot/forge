// Package cookiestore is the stateless session.Store driver: the whole
// record is AEAD-encrypted (crypto/secret.Box) and the ciphertext IS the
// token, so there is no server-side state at all. Every Save re-encrypts and
// returns a fresh token — re-set the client cookie after each Save.
//
// The trade-offs of statelessness, accepted knowingly:
//
//   - No revocation: Delete is a documented no-op. Destroying a session
//     means clearing the client's cookie; a stolen token stays valid until
//     its deadline expires. Keep TTLs short, or use a server-side store when
//     revocation matters.
//   - No UserIndex: multi-device listings and "log out everywhere" are
//     impossible without server-side state (session.ErrNoUserIndex).
//   - Size: the token must fit a cookie. Save fails with ErrTooLarge when
//     the encoding exceeds the configured limit (default 3800 bytes, leaving
//     headroom for the cookie name and attributes under the common 4096-byte
//     browser cap). Keep session payloads small.
//
// Build the Box from a keyset to rotate encryption keys without logging
// everyone out:
//
//	ks, _ := keyset.New(keyset.WithBase64Keys(os.Getenv("SESSION_KEYS")))
//	box, _ := secret.FromKeyset(ks)
//	store, _ := cookiestore.New(box)
//	mgr, _ := session.New[Data](store)
package cookiestore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/auth/session"
	"github.com/dmitrymomot/forge/crypto/secret"
)

// DefaultMaxLen caps the returned token length in bytes.
const DefaultMaxLen = 3800

var (
	// ErrTooLarge reports an encoded session exceeding the size limit.
	ErrTooLarge = errors.New("cookiestore: encoded session exceeds size limit")
	// ErrInvalidConfig reports invalid constructor input.
	ErrInvalidConfig = errors.New("cookiestore: invalid config")
)

// Store is the stateless cookie-backed session.Store. Build it with New.
type Store struct {
	box    *secret.Box
	maxLen int
}

var _ session.Store = (*Store)(nil)

// Option configures a Store.
type Option func(*Store)

// WithMaxLen overrides the token size limit (bytes). Lower it when cookie
// attributes or a long cookie name eat into the 4096-byte browser cap.
func WithMaxLen(n int) Option { return func(s *Store) { s.maxLen = n } }

// New builds a Store encrypting records with box (see secret.New /
// secret.FromKeyset).
func New(box *secret.Box, opts ...Option) (*Store, error) {
	if box == nil {
		return nil, fmt.Errorf("%w: box is required", ErrInvalidConfig)
	}
	s := &Store{box: box, maxLen: DefaultMaxLen}
	for _, opt := range opts {
		opt(s)
	}
	if s.maxLen <= 0 {
		return nil, fmt.Errorf("%w: max length must be positive", ErrInvalidConfig)
	}
	return s, nil
}

// Save encrypts rec and returns the ciphertext as the new token; the token
// argument (the previous encoding) is discarded.
func (s *Store) Save(_ context.Context, _ string, rec session.Record) (string, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	ct, err := s.box.Encrypt(b)
	if err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(ct)
	if len(token) > s.maxLen {
		return "", fmt.Errorf("%w: %d > %d bytes", ErrTooLarge, len(token), s.maxLen)
	}
	return token, nil
}

// Load decrypts token into its record. Any tamper, truncation, or foreign-key
// ciphertext is indistinguishable from an unknown token and returns
// session.ErrNotFound.
func (s *Store) Load(_ context.Context, token string) (session.Record, error) {
	ct, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return session.Record{}, session.ErrNotFound
	}
	b, err := s.box.Decrypt(ct)
	if err != nil {
		return session.Record{}, session.ErrNotFound
	}
	var rec session.Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return session.Record{}, session.ErrNotFound
	}
	return rec, nil
}

// Delete is a no-op: stateless tokens cannot be revoked server-side.
// Clearing the client's cookie is the real destroy.
func (s *Store) Delete(_ context.Context, _ string) error { return nil }
