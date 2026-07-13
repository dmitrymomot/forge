package magiclink

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/token"
	"github.com/dmitrymomot/forge/resilience/cache"
)

// envelope wraps the consumer payload inside the signed token.
type envelope[T any] struct {
	Payload T      `json:"pld"`
	Scope   string `json:"scp,omitempty"`
}

// Manager issues and redeems magic-link tokens carrying a payload of type T.
type Manager[T any] struct {
	codec   *token.Codec[envelope[T]]
	store   cache.Store
	scopeFn func(context.Context) (string, error)
	purpose string
	baseURL string
	param   string
	ttl     time.Duration
}

// New builds a single-key Manager. Purpose is required: two managers with the
// same key but different purposes never accept each other's links.
func New[T any](key []byte, purpose string, opts ...Option) (*Manager[T], error) {
	c, err := newConfig(purpose, opts...)
	if err != nil {
		return nil, err
	}
	codec, err := token.New[envelope[T]](key, c.codecOptions(purpose)...)
	if err != nil {
		return nil, err
	}
	return &Manager[T]{
		codec:   codec,
		store:   c.store,
		scopeFn: c.scopeFn,
		purpose: purpose,
		baseURL: c.baseURL,
		param:   c.param,
		ttl:     c.ttl,
	}, nil
}

// FromKeyset builds a rotation-aware Manager (signs under the primary key,
// verifies links signed under any version).
func FromKeyset[T any](ks *keyset.Keyset, purpose string, opts ...Option) (*Manager[T], error) {
	c, err := newConfig(purpose, opts...)
	if err != nil {
		return nil, err
	}
	codec, err := token.FromKeyset[envelope[T]](ks, c.codecOptions(purpose)...)
	if err != nil {
		return nil, err
	}
	return &Manager[T]{
		codec:   codec,
		store:   c.store,
		scopeFn: c.scopeFn,
		purpose: purpose,
		baseURL: c.baseURL,
		param:   c.param,
		ttl:     c.ttl,
	}, nil
}

// Issue creates a signed link token for payload. With a scope hook configured
// the resolved scope is stamped into the token; a hook error aborts issuance.
func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error) {
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return "", err
	}
	return m.codec.Issue(envelope[T]{Payload: payload, Scope: scope})
}

// parseAbsoluteURL parses u and requires an absolute URL (both scheme and
// host present), so a scheme-less base such as "app.example.com/verify" is
// rejected rather than silently yielding a broken relative link. Shared by
// WithBaseURL (construction) and IssueURL (per-call base) so the two paths
// cannot drift apart.
func parseAbsoluteURL(u string) (*url.URL, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("magiclink: invalid base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("magiclink: base URL must be absolute (scheme and host): %q", u)
	}
	return parsed, nil
}

// IssueURL issues a link token and appends it as a query parameter to base
// (multi-tenant/white-label callers pass the tenant's base per call). An
// empty base falls back to WithBaseURL; both empty is an error. Existing
// query parameters on the base are preserved.
func (m *Manager[T]) IssueURL(ctx context.Context, base string, payload T) (string, error) {
	if base == "" {
		base = m.baseURL
	}
	if base == "" {
		return "", errors.New("magiclink: no base URL")
	}
	u, err := parseAbsoluteURL(base)
	if err != nil {
		return "", err
	}
	link, err := m.Issue(ctx, payload)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set(m.param, link)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Peek verifies a link without consuming it. Serve it on GET so email
// scanners that prefetch links cannot burn them. With a store configured it
// reports ErrUsed for already-redeemed links (best-effort; the consuming
// Redeem is authoritative).
func (m *Manager[T]) Peek(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	if m.store != nil {
		used, err := m.store.Has(ctx, m.storeKey(link))
		if err != nil {
			return zero, fmt.Errorf("%w: %w", ErrStore, err)
		}
		if used {
			return zero, ErrUsed
		}
	}
	return env.Payload, nil
}

// Redeem verifies a link and, when a store is configured, atomically consumes
// it: the first call wins, replays return ErrUsed, store failures fail closed
// with ErrStore. Without a store it is verify-only and multi-use.
func (m *Manager[T]) Redeem(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	if m.store != nil {
		err := m.store.Set(ctx, m.storeKey(link), []byte{1},
			cache.WithTTL(m.ttl), cache.WithSetNonExist())
		switch {
		case errors.Is(err, cache.ErrExists):
			return zero, ErrUsed
		case err != nil:
			return zero, fmt.Errorf("%w: %w", ErrStore, err)
		}
	}
	return env.Payload, nil
}

// storeKey derives the single-use claim key. The token hash is globally
// unique (each token carries a random nonce), so scope is not needed here.
func (m *Manager[T]) storeKey(link string) string {
	sum := sha256.Sum256([]byte(link))
	return "magiclink:" + m.purpose + ":" + base64.RawURLEncoding.EncodeToString(sum[:])
}

// verify parses the token, maps crypto/token errors to package sentinels, and
// enforces scope. Signature verification runs before any store I/O.
func (m *Manager[T]) verify(ctx context.Context, link string) (envelope[T], error) {
	env, err := m.codec.Parse(link)
	if err != nil {
		return envelope[T]{}, mapTokenErr(err)
	}
	scope, err := m.resolveScope(ctx)
	if err != nil {
		return envelope[T]{}, err
	}
	if env.Scope != "" && env.Scope != scope {
		return envelope[T]{}, ErrScopeMismatch
	}
	return env, nil
}

func (m *Manager[T]) resolveScope(ctx context.Context) (string, error) {
	if m.scopeFn == nil {
		return "", nil
	}
	return m.scopeFn(ctx)
}

func mapTokenErr(err error) error {
	if errors.Is(err, token.ErrExpired) {
		return fmt.Errorf("%w: %w", ErrExpired, err)
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}
