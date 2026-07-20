package auditlog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
)

// ComputeHash returns the hex SHA-256 chain hash of e: a deterministic
// digest of PrevHash and every payload field (ID, Time, Tenant, Actor,
// Action, Resource, Outcome, sorted Meta). Every field is length-prefixed
// so no two distinct events can collide by field concatenation. Nil and
// empty Meta hash identically.
//
// The canonical message is assembled in one exactly-sized buffer and
// hashed with sha256.Sum256 — 1.7x faster and 33 -> 8 allocs versus the
// streaming sha256.New form (see BenchmarkComputeHash).
func ComputeHash(e Event) string {
	size := 8*8 + 8 + // eight field length prefixes + the meta pair count
		len(e.PrevHash) + len(e.ID) + 8 + // 8 = encoded Time
		len(e.Tenant) + len(e.Actor) + len(e.Action) + len(e.Resource) + len(e.Outcome)
	for k, v := range e.Meta {
		size += 2*8 + len(k) + len(v)
	}
	buf := make([]byte, 0, size)
	buf = appendField(buf, e.PrevHash)
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(e.ID)))
	buf = append(buf, e.ID[:]...)
	buf = binary.BigEndian.AppendUint64(buf, 8)
	buf = binary.BigEndian.AppendUint64(buf, uint64(e.Time.UTC().UnixMicro()))
	buf = appendField(buf, e.Tenant)
	buf = appendField(buf, e.Actor)
	buf = appendField(buf, e.Action)
	buf = appendField(buf, e.Resource)
	buf = appendField(buf, string(e.Outcome))
	buf = binary.BigEndian.AppendUint64(buf, uint64(len(e.Meta)))
	for _, k := range slices.Sorted(maps.Keys(e.Meta)) {
		buf = appendField(buf, k)
		buf = appendField(buf, e.Meta[k])
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// appendField appends s with a fixed-width length prefix, making the
// overall message injective over field sequences.
func appendField(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint64(b, uint64(len(s)))
	return append(b, s...)
}

// VerifyChain checks that events, in order, extend the chain whose head
// hash is prev (use "" when verifying from the genesis of a stream) and
// returns the new head. It fails with ErrChainBroken at the first event
// whose PrevHash does not match the running head or whose Hash does not
// match its recomputed value. Verifying a large trail in batches is just
// threading the returned head into the next call.
func VerifyChain(prev string, events []Event) (string, error) {
	for i := range events {
		e := &events[i]
		if e.PrevHash != prev {
			return prev, fmt.Errorf("%w: event %s: prev hash mismatch", ErrChainBroken, e.ID)
		}
		if ComputeHash(*e) != e.Hash {
			return prev, fmt.Errorf("%w: event %s: hash mismatch", ErrChainBroken, e.ID)
		}
		prev = e.Hash
	}
	return prev, nil
}
