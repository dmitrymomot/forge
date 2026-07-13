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

type tableConfig struct {
	pityHitKeys   []string
	pityThreshold uint64
}

// WithPity guarantees a hit within threshold picks: when the caller's
// miss counter reaches threshold-1, the next pick is forced to the hit
// set (weighted among hit entries only, still drawn from the stream). A
// natural or forced hit resets the counter. Hit keys must exist in the
// table.
func WithPity(threshold uint64, hitKeys ...string) TableOption {
	return func(c *tableConfig) {
		c.pityThreshold = threshold
		c.pityHitKeys = hitKeys
	}
}

// Table is an immutable weighted outcome table (lootbox drop table,
// wheel segments). Safe for concurrent use.
type Table[T any] struct {
	hitSet        map[string]bool
	hitTable      *Table[T] // sub-table over hit entries; nil without WithPity
	version       string
	entries       []Entry[T]
	cum           []uint64 // cumulative weights; cum[len-1] is the total
	pityThreshold uint64
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
	t := &Table[T]{
		entries: append([]Entry[T](nil), entries...),
		cum:     cum,
		version: hex.EncodeToString(h.Sum(nil)),
	}
	if cfg.pityThreshold > 0 || len(cfg.pityHitKeys) > 0 {
		if cfg.pityThreshold == 0 {
			return nil, fmt.Errorf("%w: pity threshold must be > 0", ErrInvalidTable)
		}
		if len(cfg.pityHitKeys) == 0 {
			return nil, fmt.Errorf("%w: pity requires at least one hit key", ErrInvalidTable)
		}
		t.hitSet = make(map[string]bool, len(cfg.pityHitKeys))
		var hits []Entry[T]
		for _, k := range cfg.pityHitKeys {
			if _, ok := seen[k]; !ok {
				return nil, fmt.Errorf("%w: pity hit key %q not in table", ErrInvalidTable, k)
			}
			if t.hitSet[k] {
				return nil, fmt.Errorf("%w: duplicate pity hit key %q", ErrInvalidTable, k)
			}
			t.hitSet[k] = true
		}
		for _, e := range t.entries {
			if t.hitSet[e.Key] {
				hits = append(hits, e)
			}
		}
		hitTable, err := NewTable(hits) // no options: plain weighted sub-table
		if err != nil {
			return nil, err // unreachable: hits are validated entries
		}
		t.hitTable = hitTable
		t.pityThreshold = cfg.pityThreshold
	}
	return t, nil
}

// Pick draws one entry (rng/v1 weighted pick: IntN(totalWeight), first
// cumulative bucket in entry order wins).
func (t *Table[T]) Pick(s *Stream) Entry[T] {
	draw := uint64(s.IntN(int(t.cum[len(t.cum)-1])))
	i := sort.Search(len(t.cum), func(i int) bool { return draw < t.cum[i] })
	return t.entries[i]
}

// PickWithPity draws one entry under the pity rule and returns the
// updated miss counter for the caller to persist (next to the player row
// — the package never stores it). Requires WithPity; panics otherwise.
func (t *Table[T]) PickWithPity(s *Stream, misses uint64) (Entry[T], uint64) {
	if t.hitTable == nil {
		panic("rng: PickWithPity requires a table built with WithPity")
	}
	if misses+1 >= t.pityThreshold {
		return t.hitTable.Pick(s), 0
	}
	e := t.Pick(s)
	if t.hitSet[e.Key] {
		return e, 0
	}
	return e, misses + 1
}

// Version is the audit anchor: lowercase-hex SHA-256 over the ordered
// (key, weight) pairs. Store it on the game round to prove which drop
// configuration was live; the Item payload does not affect it.
func (t *Table[T]) Version() string { return t.version }
