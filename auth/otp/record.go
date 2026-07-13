package otp

import "encoding/binary"

// recordVersion is the storage-format version byte. Records with any other
// version decode as absent, so a future format change invalidates in-flight
// codes instead of misreading them.
const recordVersion = 0x01

// recordSize is version(1) + attempts(1) + expiresAt(8) + HMAC-SHA256(32).
const recordSize = 42

type record struct {
	expiresAt int64
	codeHash  [32]byte
	attempts  uint8
}

func encodeRecord(r record) []byte {
	buf := make([]byte, recordSize)
	buf[0] = recordVersion
	buf[1] = r.attempts
	binary.BigEndian.PutUint64(buf[2:10], uint64(r.expiresAt))
	copy(buf[10:], r.codeHash[:])
	return buf
}

func decodeRecord(b []byte) (record, bool) {
	if len(b) != recordSize || b[0] != recordVersion {
		return record{}, false
	}
	var r record
	r.attempts = b[1]
	r.expiresAt = int64(binary.BigEndian.Uint64(b[2:10]))
	copy(r.codeHash[:], b[10:])
	return r, true
}
