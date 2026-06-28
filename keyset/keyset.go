package keyset

import (
	"errors"
	"iter"
	"sort"
)

// Keyset is an in-memory versioned keyring: a primary key for new operations plus any
// number of retired keys for decrypting/verifying older material during rotation.
type Keyset struct {
	keys    map[int][]byte
	primary int
}

// New builds a Keyset from the given options. It returns ErrNoPrimary if no primary was
// set and joins any option errors (ErrBadKeyMaterial).
func New(opts ...Option) (*Keyset, error) {
	c := &config{keys: make(map[int][]byte)}
	for _, o := range opts {
		o(c)
	}
	if len(c.errs) > 0 {
		return nil, errors.Join(c.errs...)
	}
	if !c.hasPrimary {
		return nil, ErrNoPrimary
	}
	return &Keyset{keys: c.keys, primary: c.primary}, nil
}

// Primary returns the current primary version and key.
func (k *Keyset) Primary() (int, []byte) { return k.primary, k.keys[k.primary] }

// ByVersion returns the key for v, and whether it exists.
func (k *Keyset) ByVersion(v int) ([]byte, bool) {
	key, ok := k.keys[v]
	return key, ok
}

// All iterates every (version, key) pair in descending version order.
func (k *Keyset) All() iter.Seq2[int, []byte] {
	return func(yield func(int, []byte) bool) {
		versions := make([]int, 0, len(k.keys))
		for v := range k.keys {
			versions = append(versions, v)
		}
		sort.Sort(sort.Reverse(sort.IntSlice(versions)))
		for _, v := range versions {
			if !yield(v, k.keys[v]) {
				return
			}
		}
	}
}
