package validate

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"strings"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var big58 = big.NewInt(58)

// base58Decode decodes a Base58 string to bytes (leading '1's → leading zero bytes).
func base58Decode(s string) ([]byte, bool) {
	n := new(big.Int)
	for _, r := range s {
		idx := strings.IndexRune(base58Alphabet, r)
		if idx < 0 {
			return nil, false
		}
		n.Mul(n, big58)
		n.Add(n, big.NewInt(int64(idx)))
	}
	decoded := n.Bytes()
	zeros := 0
	for _, r := range s {
		if r != '1' {
			break
		}
		zeros++
	}
	out := make([]byte, zeros+len(decoded))
	copy(out[zeros:], decoded)
	return out, true
}

// base58CheckDecode decodes and verifies the 4-byte double-SHA-256 checksum,
// returning the version+payload (without the checksum).
func base58CheckDecode(s string) ([]byte, bool) {
	b, ok := base58Decode(s)
	if !ok || len(b) < 5 {
		return nil, false
	}
	payload, checksum := b[:len(b)-4], b[len(b)-4:]
	h := sha256.Sum256(payload)
	h = sha256.Sum256(h[:])
	if !bytes.Equal(h[:4], checksum) {
		return nil, false
	}
	return payload, true
}
