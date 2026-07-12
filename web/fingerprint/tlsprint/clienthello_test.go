package tlsprint

import (
	"encoding/binary"
	"testing"
)

// buildHello assembles a minimal but valid TLS record framing a ClientHello with
// the given ciphers and an optional SNI(0x0000) + supported_versions(0x002b,
// advertising TLS 1.3) extension. It is the parser's executable spec: the wire
// layout is written out field by field with big-endian length back-patching.
//
// Layout (RFC 8446 §4 / RFC 5246 §7.4):
//
//	record:    0x16 0x03 0x01 <len16>
//	handshake: 0x01 <len24>
//	hello:     client_version(0x0303) random[32] session_id(0)
//	           cipher_suites(len16 + suites) compression(0x01 0x00)
//	           extensions(len16 + [ SNI, supported_versions ])
func buildHello(ciphers []uint16, withSNI bool) []byte {
	be := binary.BigEndian

	// --- extensions ---
	var exts []byte
	appendExt := func(extType uint16, body []byte) {
		hdr := make([]byte, 4)
		be.PutUint16(hdr[0:2], extType)
		be.PutUint16(hdr[2:4], uint16(len(body)))
		exts = append(exts, hdr...)
		exts = append(exts, body...)
	}

	if withSNI {
		host := []byte("example.com")
		// server_name entry: name_type(0x00 host_name) + uint16 len + host
		entry := make([]byte, 3+len(host))
		entry[0] = 0x00
		be.PutUint16(entry[1:3], uint16(len(host)))
		copy(entry[3:], host)
		// server_name_list: uint16 len + entry
		sni := make([]byte, 2+len(entry))
		be.PutUint16(sni[0:2], uint16(len(entry)))
		copy(sni[2:], entry)
		appendExt(0x0000, sni)
	}

	// supported_versions: uint8 list len + version(0x0304 TLS 1.3)
	appendExt(0x002b, []byte{0x02, 0x03, 0x04})

	// --- cipher_suites body ---
	cs := make([]byte, 2*len(ciphers))
	for i, c := range ciphers {
		be.PutUint16(cs[2*i:2*i+2], c)
	}

	// --- ClientHello body ---
	var body []byte
	body = append(body, 0x03, 0x03)          // legacy client_version TLS 1.2
	body = append(body, make([]byte, 32)...) // random
	body = append(body, 0x00)                // session_id length 0
	csLen := make([]byte, 2)
	be.PutUint16(csLen, uint16(len(cs)))
	body = append(body, csLen...)
	body = append(body, cs...)
	body = append(body, 0x01, 0x00) // compression: length 1, method null
	extLen := make([]byte, 2)
	be.PutUint16(extLen, uint16(len(exts)))
	body = append(body, extLen...)
	body = append(body, exts...)

	// --- handshake message: 0x01 <len24> body ---
	hs := make([]byte, 4+len(body))
	hs[0] = 0x01
	hs[1] = byte(len(body) >> 16)
	hs[2] = byte(len(body) >> 8)
	hs[3] = byte(len(body))
	copy(hs[4:], body)

	// --- TLS record: 0x16 0x03 0x01 <len16> handshake ---
	rec := make([]byte, 5+len(hs))
	rec[0] = 0x16
	rec[1] = 0x03
	rec[2] = 0x01
	be.PutUint16(rec[3:5], uint16(len(hs)))
	copy(rec[5:], hs)

	return rec
}

func TestParseClientHelloExtractsFields(t *testing.T) {
	rec := buildHello([]uint16{0x1301, 0x1302}, true)
	h, err := parseClientHello(rec)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ciphers) != 2 || h.ciphers[0] != 0x1301 {
		t.Fatalf("ciphers wrong: %v", h.ciphers)
	}
	if !h.sni {
		t.Fatal("SNI not detected")
	}
	if h.version != 0x0304 {
		t.Fatalf("version wrong: %#x", h.version)
	}
}
