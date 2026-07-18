package pagination_test

import "encoding/base64"

// base64Raw encodes s the way the codec encodes a cursor body, so tests can
// hand-craft malformed payloads.
func base64Raw(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// flipFirst returns s with its first byte changed, to corrupt a signed body.
func flipFirst(s string) string {
	if s == "" {
		return "x"
	}
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}
