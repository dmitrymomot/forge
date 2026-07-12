package tlsprint

import (
	"strings"
	"testing"
)

func TestJA4Assembly(t *testing.T) {
	h := clientHello{
		version:    0x0304, // TLS 1.3
		sni:        true,
		ciphers:    []uint16{0x1301, 0x1302, 0x1303},
		extensions: []uint16{0x0005, 0x000a, 0x000b, 0x0023},
		alpn:       []string{"h2"},
		sigAlgs:    []uint16{0x0403, 0x0804},
	}
	got := ja4(h)
	// a = t13 d 03 04 h2  (3 ciphers, 4 extensions, alpn h2)
	if !strings.HasPrefix(got, "t13d0304h2_") {
		t.Fatalf("JA4_a wrong: %s", got)
	}
	parts := strings.Split(got, "_")
	if len(parts) != 3 || len(parts[1]) != 12 || len(parts[2]) != 12 {
		t.Fatalf("JA4 shape wrong: %q", got)
	}
	if ja4(h) != got {
		t.Fatal("JA4 not deterministic")
	}
}

func TestGreaseStripping(t *testing.T) {
	h := clientHello{
		version:    0x0304,
		sni:        false,
		ciphers:    []uint16{0x0a0a, 0x1301}, // 0x0a0a is GREASE
		extensions: []uint16{0x1a1a, 0x0005},
	}
	// After stripping GREASE: 1 cipher, 1 extension -> a = t13 i 01 01 00
	if !strings.HasPrefix(ja4(h), "t13i010100_") {
		t.Fatalf("grease not stripped / counts wrong: %s", ja4(h))
	}
}
