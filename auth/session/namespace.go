package session

import (
	"encoding/json"
	"fmt"
)

// Namespace is a typed, independently-owned slice of the session payload. An
// app and its plugins each declare their own; they coexist without collisions,
// and a namespace nobody reads costs no JSON work.
type Namespace[T any] struct {
	name string
}

// NewNamespace declares a namespace, usually once at package scope. The name is
// the namespace's stable key in the payload, so it must be unique across the
// app: two namespaces sharing a name read and write the same key and silently
// overwrite each other. Choosing distinct, prefixed names ("cart",
// "billing.plan") is the caller's responsibility.
func NewNamespace[T any](name string) *Namespace[T] {
	if name == "" {
		panic("session: namespace name must not be empty")
	}
	return &Namespace[T]{name: name}
}

// Name returns the namespace's key in the payload.
func (n *Namespace[T]) Name() string { return n.name }

// Get decodes this namespace, caching the result for the rest of the request.
// A namespace that has never been written returns the zero value and a nil
// error; one that fails to decode returns the error, never a zero value, so
// corrupt or schema-drifted data fails closed.
func (n *Namespace[T]) Get(s *Session) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrNoSession
	}
	if v, ok := s.cache[n.name]; ok {
		typed, ok := v.(T)
		if !ok {
			return zero, fmt.Errorf("session: namespace %q cached as %T", n.name, v)
		}
		return typed, nil
	}
	if err := s.parse(); err != nil {
		return zero, fmt.Errorf("session: payload decode: %w", err)
	}
	b, ok := s.raw[n.name]
	if !ok || len(b) == 0 {
		return zero, nil
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return zero, fmt.Errorf("session: namespace %q decode: %w", n.name, err)
	}
	if s.cache == nil {
		s.cache = make(map[string]any, 1)
	}
	s.cache[n.name] = out
	return out, nil
}

// Set stores v and marks only this namespace dirty.
func (n *Namespace[T]) Set(s *Session, v T) { s.markDirty(n.name, v) }

// Clear removes this namespace from the payload.
func (n *Namespace[T]) Clear(s *Session) { s.markDirty(n.name, nil) }
