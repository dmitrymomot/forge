package tlsprint

import "errors"

// errShortHello is returned by parseClientHello for any truncated or malformed
// record: a length prefix that overruns the buffer, a missing fixed field, or a
// wrong content/handshake type. It is a single sentinel so callers can treat all
// "not a parseable ClientHello" outcomes uniformly.
var errShortHello = errors.New("tlsprint: short or malformed ClientHello")

// clientHello holds the handful of ClientHello fields JA4 needs. Cipher and
// extension lists are GREASE-stripped by parseClientHello; sigAlgs are kept in
// wire order (JA4_c hashes them unsorted). Field layout is alignment-optimal
// (slices first) for betteralign — callers use keyed literals.
type clientHello struct {
	ciphers    []uint16
	extensions []uint16
	alpn       []string
	sigAlgs    []uint16
	version    uint16
	sni        bool
}

// TLS extension type numbers relevant to JA4.
const (
	extServerName       = 0x0000 // SNI
	extALPN             = 0x0010
	extSignatureAlgs    = 0x000d
	extSupportedVersion = 0x002b
)

// byteReader walks a byte slice with bounds-checked reads. Every read verifies
// the remaining length first and reports ok=false rather than slicing past the
// end, so the parser never panics on attacker-influenced input (it is fuzzed).
type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) remaining() int { return len(r.data) - r.pos }

func (r *byteReader) readUint8() (uint8, bool) {
	if r.remaining() < 1 {
		return 0, false
	}
	v := r.data[r.pos]
	r.pos++
	return v, true
}

func (r *byteReader) readUint16() (uint16, bool) {
	if r.remaining() < 2 {
		return 0, false
	}
	v := uint16(r.data[r.pos])<<8 | uint16(r.data[r.pos+1])
	r.pos += 2
	return v, true
}

func (r *byteReader) readUint24() (uint32, bool) {
	if r.remaining() < 3 {
		return 0, false
	}
	v := uint32(r.data[r.pos])<<16 | uint32(r.data[r.pos+1])<<8 | uint32(r.data[r.pos+2])
	r.pos += 3
	return v, true
}

// readBytes returns the next n bytes as a sub-slice (aliasing data; the parser
// only reads from it). It reports ok=false if n is negative or overruns.
func (r *byteReader) readBytes(n int) ([]byte, bool) {
	if n < 0 || r.remaining() < n {
		return nil, false
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, true
}

// parseClientHello parses a single TLS record framing a ClientHello handshake
// and extracts the fields JA4 needs. It returns errShortHello on any truncation,
// length-prefix overrun, or wrong record/handshake type, and never panics.
//
// Cipher suites and extension types are GREASE-stripped. version starts as the
// legacy client_version and is upgraded to the highest non-GREASE value offered
// in the supported_versions extension when present (so a TLS 1.3 hello reports
// 0x0304 rather than its 0x0303 legacy field).
func parseClientHello(record []byte) (clientHello, error) {
	var h clientHello
	r := &byteReader{data: record}

	// Record header: content type(0x16 handshake) + version(2) + length(2).
	ct, ok := r.readUint8()
	if !ok || ct != 0x16 {
		return h, errShortHello
	}
	if _, ok = r.readUint16(); !ok { // record layer version (unused)
		return h, errShortHello
	}
	recLen, ok := r.readUint16()
	if !ok {
		return h, errShortHello
	}
	// Constrain parsing to the record body so we never read across records.
	recBody, ok := r.readBytes(int(recLen))
	if !ok {
		return h, errShortHello
	}

	// Handshake header: type(0x01 ClientHello) + length(3).
	hs := &byteReader{data: recBody}
	ht, ok := hs.readUint8()
	if !ok || ht != 0x01 {
		return h, errShortHello
	}
	hsLen, ok := hs.readUint24()
	if !ok {
		return h, errShortHello
	}
	helloBody, ok := hs.readBytes(int(hsLen))
	if !ok {
		return h, errShortHello
	}

	b := &byteReader{data: helloBody}

	// client_version(2) + random(32).
	cv, ok := b.readUint16()
	if !ok {
		return h, errShortHello
	}
	h.version = cv
	if _, ok = b.readBytes(32); !ok {
		return h, errShortHello
	}

	// session_id: uint8 length + body.
	sidLen, ok := b.readUint8()
	if !ok {
		return h, errShortHello
	}
	if _, ok = b.readBytes(int(sidLen)); !ok {
		return h, errShortHello
	}

	// cipher_suites: uint16 length + body (each suite 2 bytes, GREASE stripped).
	csLen, ok := b.readUint16()
	if !ok {
		return h, errShortHello
	}
	csBody, ok := b.readBytes(int(csLen))
	if !ok {
		return h, errShortHello
	}
	cr := &byteReader{data: csBody}
	for cr.remaining() >= 2 {
		suite, _ := cr.readUint16()
		if !isGREASE(suite) {
			h.ciphers = append(h.ciphers, suite)
		}
	}

	// compression_methods: uint8 length + body.
	compLen, ok := b.readUint8()
	if !ok {
		return h, errShortHello
	}
	if _, ok = b.readBytes(int(compLen)); !ok {
		return h, errShortHello
	}

	// extensions are optional (TLS 1.0/1.1 hellos may omit the block entirely).
	if b.remaining() == 0 {
		return h, nil
	}
	extTotalLen, ok := b.readUint16()
	if !ok {
		return h, errShortHello
	}
	extBody, ok := b.readBytes(int(extTotalLen))
	if !ok {
		return h, errShortHello
	}
	if err := h.parseExtensions(extBody); err != nil {
		return h, err
	}

	return h, nil
}

// parseExtensions walks the extension block. Each extension header (type+length)
// is bounds-checked and its declared body must fit; anything else is
// errShortHello. Known extensions are decoded best-effort from their (already
// length-validated) bodies. GREASE extension types are dropped from the list.
func (h *clientHello) parseExtensions(extBody []byte) error {
	er := &byteReader{data: extBody}
	for er.remaining() > 0 {
		extType, ok := er.readUint16()
		if !ok {
			return errShortHello
		}
		extLen, ok := er.readUint16()
		if !ok {
			return errShortHello
		}
		body, ok := er.readBytes(int(extLen))
		if !ok {
			return errShortHello
		}

		if !isGREASE(extType) {
			h.extensions = append(h.extensions, extType)
		}

		switch extType {
		case extServerName:
			h.sni = true
		case extALPN:
			h.alpn = append(h.alpn, parseALPN(body)...)
		case extSignatureAlgs:
			h.sigAlgs = append(h.sigAlgs, parseSigAlgs(body)...)
		case extSupportedVersion:
			if v, ok := maxSupportedVersion(body); ok {
				h.version = v
			}
		}
	}
	return nil
}

// parseALPN collects protocol names from an ALPN extension body:
// uint16 list length, then entries of uint8 length + protocol bytes. Best-effort:
// a malformed tail stops collection rather than failing the whole hello.
func parseALPN(body []byte) []string {
	r := &byteReader{data: body}
	listLen, ok := r.readUint16()
	if !ok {
		return nil
	}
	list, ok := r.readBytes(int(listLen))
	if !ok {
		return nil
	}
	lr := &byteReader{data: list}
	var out []string
	for lr.remaining() > 0 {
		n, ok := lr.readUint8()
		if !ok {
			break
		}
		p, ok := lr.readBytes(int(n))
		if !ok {
			break
		}
		out = append(out, string(p))
	}
	return out
}

// parseSigAlgs collects signature-algorithm code points in wire order (JA4_c
// hashes them unsorted and does not GREASE-strip them): uint16 list length, then
// uint16 pairs. Best-effort on a malformed tail.
func parseSigAlgs(body []byte) []uint16 {
	r := &byteReader{data: body}
	listLen, ok := r.readUint16()
	if !ok {
		return nil
	}
	list, ok := r.readBytes(int(listLen))
	if !ok {
		return nil
	}
	lr := &byteReader{data: list}
	var out []uint16
	for lr.remaining() >= 2 {
		v, _ := lr.readUint16()
		out = append(out, v)
	}
	return out
}

// maxSupportedVersion returns the highest non-GREASE version from a
// supported_versions extension body (uint8 list length + uint16 versions), or
// ok=false if none is present. GREASE is skipped so a GREASE code point cannot
// masquerade as the "highest" offered version.
func maxSupportedVersion(body []byte) (uint16, bool) {
	r := &byteReader{data: body}
	listLen, ok := r.readUint8()
	if !ok {
		return 0, false
	}
	list, ok := r.readBytes(int(listLen))
	if !ok {
		return 0, false
	}
	lr := &byteReader{data: list}
	var maxVer uint16
	found := false
	for lr.remaining() >= 2 {
		v, _ := lr.readUint16()
		if isGREASE(v) {
			continue
		}
		if v > maxVer {
			maxVer = v
		}
		found = true
	}
	return maxVer, found
}
