package encoding

import (
	"math/big"
	"strings"
)

// base62Alphabet matches math/big's Text(62) digit order (0-9, a-z, A-Z) so
// EncodeInt, Encode, and the big.Int byte path share one alphabet.
const base62Alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// EncodeInt encodes n in base62.
func EncodeInt(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [11]byte // ceil(64/log2(62)) = 11
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = base62Alphabet[n%62]
		n /= 62
	}
	return string(buf[i:])
}

// DecodeInt decodes a base62 string into a uint64, returning ErrInvalidEncoding
// on an invalid character or overflow.
func DecodeInt(s string) (uint64, error) {
	if s == "" {
		return 0, ErrInvalidEncoding
	}
	const maxUint64 = ^uint64(0)
	var n uint64
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(base62Alphabet, s[i])
		if idx < 0 {
			return 0, ErrInvalidEncoding
		}
		if n > (maxUint64-uint64(idx))/62 {
			return 0, ErrInvalidEncoding
		}
		n = n*62 + uint64(idx)
	}
	return n, nil
}

// Encode encodes an arbitrary byte slice in base62. Leading zero bytes are
// preserved by emitting one leading '0' per zero byte (base58-style), so
// Decode(Encode(b)) == b for any b.
func Encode(b []byte) string {
	z := 0
	for z < len(b) && b[z] == 0 {
		z++
	}
	var digits string
	if z < len(b) {
		digits = new(big.Int).SetBytes(b[z:]).Text(62)
	}
	return strings.Repeat("0", z) + digits
}

// Decode reverses Encode.
func Decode(s string) ([]byte, error) {
	z := 0
	for z < len(s) && s[z] == '0' {
		z++
	}
	out := make([]byte, z) // z leading zero bytes
	rest := s[z:]
	if rest == "" {
		return out, nil
	}
	n, ok := new(big.Int).SetString(rest, 62)
	if !ok {
		return nil, ErrInvalidEncoding
	}
	return append(out, n.Bytes()...), nil
}
