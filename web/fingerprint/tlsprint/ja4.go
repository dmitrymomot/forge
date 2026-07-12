package tlsprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// isGREASE reports whether v is a GREASE code point (RFC 8701). JA4 removes
// GREASE from the cipher and extension lists before counting and sorting. The
// check follows the JA4 spec's mask exactly: both low nibbles are 0xa.
func isGREASE(v uint16) bool { return v&0x0f0f == 0x0a0a }

// stripGREASE returns a new slice with GREASE code points removed. It copies so
// callers may sort the result without mutating the source clientHello (keeping
// ja4 deterministic across repeated calls).
func stripGREASE(in []uint16) []uint16 {
	out := make([]uint16, 0, len(in))
	for _, v := range in {
		if !isGREASE(v) {
			out = append(out, v)
		}
	}
	return out
}

// ja4 builds the FoxIO JA4 TLS client fingerprint "a_b_c" from a parsed
// ClientHello. See ja4a/ja4b/ja4c for each segment.
func ja4(h clientHello) string {
	return ja4a(h) + "_" + ja4b(h) + "_" + ja4c(h)
}

// ja4a is the human-readable prefix:
//
//	t + 2-digit TLS version + (d if SNI else i) +
//	2-digit GREASE-stripped cipher count + 2-digit GREASE-stripped extension
//	count (both capped at 99) + first ALPN's (firstchar+lastchar) or "00".
func ja4a(h clientHello) string {
	sni := byte('i')
	if h.sni {
		sni = 'd'
	}
	nCiphers := len(stripGREASE(h.ciphers))
	nExts := len(stripGREASE(h.extensions))
	return fmt.Sprintf("t%s%c%s%s%s",
		versionCode(h.version), sni, count2(nCiphers), count2(nExts), alpnCode(h.alpn))
}

// ja4b is the first 12 hex chars of sha256 over the GREASE-stripped cipher
// suites, each lowercase 4-hex, sorted ascending, joined by ",".
func ja4b(h clientHello) string {
	ciphers := stripGREASE(h.ciphers)
	slices.Sort(ciphers)
	return hash12(strings.Join(hexList(ciphers), ","))
}

// ja4c is the first 12 hex chars of sha256 over
// "<sorted extensions>_<signature algorithms in order>". Extensions are
// GREASE-stripped, then SNI(0x0000) and ALPN(0x0010) are excluded and the rest
// sorted ascending; signature algorithms stay in wire order (not GREASE-stripped).
func ja4c(h clientHello) string {
	exts := stripGREASE(h.extensions)
	filtered := make([]uint16, 0, len(exts))
	for _, e := range exts {
		if e == extServerName || e == extALPN {
			continue
		}
		filtered = append(filtered, e)
	}
	slices.Sort(filtered)
	input := strings.Join(hexList(filtered), ",") + "_" + strings.Join(hexList(h.sigAlgs), ",")
	return hash12(input)
}

// versionCode maps a TLS version to its 2-digit JA4 code, or "00" if unknown.
func versionCode(v uint16) string {
	switch v {
	case 0x0304:
		return "13"
	case 0x0303:
		return "12"
	case 0x0302:
		return "11"
	case 0x0301:
		return "10"
	default:
		return "00"
	}
}

// count2 formats a non-negative count as 2 digits, capped at 99 per the JA4 spec.
func count2(n int) string {
	if n > 99 {
		n = 99
	}
	return fmt.Sprintf("%02d", n)
}

// alpnCode is the first ALPN protocol's first and last character (e.g. "h2",
// "http/1.1" -> "h1"), or "00" when no ALPN protocol is present.
func alpnCode(alpn []string) string {
	if len(alpn) == 0 || alpn[0] == "" {
		return "00"
	}
	first := alpn[0]
	return string(first[0]) + string(first[len(first)-1])
}

// hexList renders each code point as a lowercase 4-hex string.
func hexList(vs []uint16) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = hex4(v)
	}
	return out
}

// hex4 encodes a uint16 as a lowercase 4-character hex string (e.g. 0x1301 ->
// "1301").
func hex4(v uint16) string {
	return hex.EncodeToString([]byte{byte(v >> 8), byte(v)})
}

// hash12 returns the first 12 hex characters of sha256(s).
func hash12(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}
