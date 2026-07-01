package ctxkey

import "context"

// keyID is the unexported context-key type. Each Key gets a distinct *keyID, so
// keys never collide across packages regardless of the name passed to New.
// The dummy field forces each keyID allocation to have a unique address,
// preventing Go's optimization that would otherwise reuse the zero address.
type keyID struct {
	_ [1]byte // Force non-zero allocation; the slice prevents value semantics
}

// Key is a typed, collision-free context key created by New. It is safe to copy;
// copies share the same identity and read the same stored value.
type Key[T any] struct {
	id   *keyID
	name string
}

// New returns a Key[T] with a fresh identity. name is used only for diagnostics
// (Name and the MustFrom panic message); it does not affect key identity.
func New[T any](name string) Key[T] {
	return Key[T]{id: &keyID{}, name: name}
}

// With returns a copy of ctx carrying v under k.
func (k Key[T]) With(ctx context.Context, v T) context.Context {
	return context.WithValue(ctx, k.id, v)
}

// From returns the value stored under k and whether it was present.
func (k Key[T]) From(ctx context.Context) (T, bool) {
	v, ok := ctx.Value(k.id).(T)
	return v, ok
}

// MustFrom returns the value stored under k, panicking if it is absent.
func (k Key[T]) MustFrom(ctx context.Context) T {
	v, ok := k.From(ctx)
	if !ok {
		panic("ctxkey: key " + k.name + " not present in context")
	}
	return v
}

// Name returns the diagnostic name passed to New.
func (k Key[T]) Name() string { return k.name }
