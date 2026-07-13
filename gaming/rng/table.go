package rng

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// Entry is one weighted outcome in a Table. Key is the stable identity
// used in the audit version hash and pity hit sets; Item is the payload.
type Entry[T any] struct {
	Item   T
	Key    string
	Weight uint64
}

// TableOption configures NewTable.
type TableOption func(*tableConfig)

type tableConfig struct{}

// Table is an immutable weighted outcome table (lootbox drop table,
// wheel segments). Safe for concurrent use.
type Table[T any] struct {
	version string
	entries []Entry[T]
	cum     []uint64 // cumulative weights; cum[len-1] is the total
}

// NewTable validates entries and builds an immutable table. Entries need
// non-empty unique keys and weights > 0; the weight sum must fit int64.
func NewTable[T any](entries []Entry[T], opts ...TableOption) (*Table[T], error) {
	var cfg tableConfig
	for _, o := range opts {
		o(&cfg)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no entries", ErrInvalidTable)
	}
	seen := make(map[string]struct{}, len(entries))
	cum := make([]uint64, len(entries))
	var total uint64
	h := sha256.New()
	var buf [8]byte
	for i, e := range entries {
		if e.Key == "" {
			return nil, fmt.Errorf("%w: empty key at index %d", ErrInvalidTable, i)
		}
		if _, dup := seen[e.Key]; dup {
			return nil, fmt.Errorf("%w: duplicate key %q", ErrInvalidTable, e.Key)
		}
		seen[e.Key] = struct{}{}
		if e.Weight == 0 {
			return nil, fmt.Errorf("%w: zero weight for key %q", ErrInvalidTable, e.Key)
		}
		if total > math.MaxUint64-e.Weight {
			return nil, fmt.Errorf("%w: weight sum overflows uint64", ErrInvalidTable)
		}
		total += e.Weight
		cum[i] = total
		// Version hash: length-prefixed key then weight, both big-endian —
		// unambiguous for any key content.
		binary.BigEndian.PutUint64(buf[:], uint64(len(e.Key)))
		_, _ = h.Write(buf[:])
		_, _ = h.Write([]byte(e.Key))
		binary.BigEndian.PutUint64(buf[:], e.Weight)
		_, _ = h.Write(buf[:])
	}
	if total > math.MaxInt64 {
		return nil, fmt.Errorf("%w: weight sum must fit int64", ErrInvalidTable)
	}
	return &Table[T]{
		entries: append([]Entry[T](nil), entries...),
		cum:     cum,
		version: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

// Pick draws one entry (rng/v1 weighted pick: IntN(totalWeight), first
// cumulative bucket in entry order wins).
func (t *Table[T]) Pick(s *Stream) Entry[T] {
	draw := uint64(s.IntN(int(t.cum[len(t.cum)-1])))
	i := sort.Search(len(t.cum), func(i int) bool { return draw < t.cum[i] })
	return t.entries[i]
}

// Version is the audit anchor: lowercase-hex SHA-256 over the ordered
// (key, weight) pairs. Store it on the game round to prove which drop
// configuration was live; the Item payload does not affect it.
func (t *Table[T]) Version() string { return t.version }
