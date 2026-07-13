package magiclink

import (
	"context"
	"errors"
	"fmt"

	"github.com/dmitrymomot/forge/crypto/keyset"
	"github.com/dmitrymomot/forge/crypto/token"
)

// envelope wraps the consumer payload inside the signed token.
type envelope[T any] struct {
	Payload T `json:"pld"`
}

// Manager issues and redeems magic-link tokens carrying a payload of type T.
type Manager[T any] struct {
	codec *token.Codec[envelope[T]]
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
	return &Manager[T]{codec: codec}, nil
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
	return &Manager[T]{codec: codec}, nil
}

// Issue creates a signed link token for payload.
func (m *Manager[T]) Issue(ctx context.Context, payload T) (string, error) {
	return m.codec.Issue(envelope[T]{Payload: payload})
}

// Peek verifies a link without consuming it. Serve it on GET so email
// scanners that prefetch links cannot burn them.
func (m *Manager[T]) Peek(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	return env.Payload, nil
}

// Redeem verifies a link and, when a store is configured, atomically consumes
// it. Without a store it is verify-only and multi-use.
func (m *Manager[T]) Redeem(ctx context.Context, link string) (T, error) {
	var zero T
	env, err := m.verify(ctx, link)
	if err != nil {
		return zero, err
	}
	return env.Payload, nil
}

// verify parses the token and maps crypto/token errors to package sentinels.
// Signature verification runs before any store I/O.
func (m *Manager[T]) verify(ctx context.Context, link string) (envelope[T], error) {
	env, err := m.codec.Parse(link)
	if err != nil {
		return envelope[T]{}, mapTokenErr(err)
	}
	return env, nil
}

func mapTokenErr(err error) error {
	if errors.Is(err, token.ErrExpired) {
		return fmt.Errorf("%w: %w", ErrExpired, err)
	}
	return fmt.Errorf("%w: %w", ErrInvalid, err)
}
