package apikey

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/dmitrymomot/forge/core/id"
)

// Manager issues, manages, and verifies API keys over a Store. It
// implements guard.Verifier. Safe for concurrent use.
type Manager struct {
	store Store
	cfg   config
}

// New builds a Manager. It panics on a nil store or an invalid prefix —
// wiring bugs caught at startup, like guard.New's nil-verifier panic.
func New(store Store, opts ...Option) *Manager {
	if store == nil {
		panic("apikey: nil store")
	}
	cfg := config{prefix: "key", touchInterval: time.Minute}
	for _, o := range opts {
		o(&cfg)
	}
	if !validPrefix(cfg.prefix) {
		panic(fmt.Sprintf("apikey: invalid prefix %q", cfg.prefix))
	}
	return &Manager{store: store, cfg: cfg}
}

// Create mints a key for p, returning the stored record and the plaintext.
// The plaintext is shown exactly once — only its hash is persisted.
func (m *Manager) Create(ctx context.Context, p CreateParams) (Key, string, error) {
	if p.Subject == "" {
		return Key{}, "", ErrSubjectRequired
	}
	plaintext := newKey(m.cfg.prefix)
	k := Key{
		ID:        id.NewUUID(),
		Hash:      hashKey(plaintext),
		Preview:   plaintext[:previewLen],
		Name:      p.Name,
		Subject:   p.Subject,
		Tenant:    p.Tenant,
		Scopes:    slices.Clone(p.Scopes),
		Meta:      maps.Clone(p.Meta),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: p.ExpiresAt,
	}
	if err := m.store.Create(ctx, k); err != nil {
		return Key{}, "", fmt.Errorf("apikey: create: %w", err)
	}
	return k, plaintext, nil
}
