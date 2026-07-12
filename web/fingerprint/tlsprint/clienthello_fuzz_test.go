package tlsprint

import "testing"

// FuzzParseClientHello feeds arbitrary bytes to parseClientHello. It parses
// attacker-controlled TLS records, so it must never panic on any input —
// only ever return errShortHello or a parsed clientHello.
func FuzzParseClientHello(f *testing.F) {
	f.Add([]byte{0x16, 0x03, 0x01, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseClientHello(b) // must never panic on any input
	})
}
