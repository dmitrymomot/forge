package tracing

// TraceID is the 16-byte W3C trace id shared by every span in a trace. The
// zero value is invalid per the spec.
type TraceID [16]byte

// IsValid reports whether the id is non-zero.
func (id TraceID) IsValid() bool { return id != TraceID{} }

// String returns the id as 32 lowercase hex characters.
func (id TraceID) String() string { return string(appendHex(make([]byte, 0, 32), id[:])) }

// SpanID is the 8-byte W3C span id (parent-id in traceparent terms). The zero
// value is invalid per the spec.
type SpanID [8]byte

// IsValid reports whether the id is non-zero.
func (id SpanID) IsValid() bool { return id != SpanID{} }

// String returns the id as 16 lowercase hex characters.
func (id SpanID) String() string { return string(appendHex(make([]byte, 0, 16), id[:])) }

// SpanContext is the propagatable identity of a span: what travels in the W3C
// traceparent/tracestate headers. TraceState is the raw tracestate value
// carried through unmodified (vendor key=value pairs, opaque to forge).
type SpanContext struct {
	TraceState string
	TraceID    TraceID
	SpanID     SpanID
	Sampled    bool
}

// IsValid reports whether both ids are non-zero.
func (sc SpanContext) IsValid() bool { return sc.TraceID.IsValid() && sc.SpanID.IsValid() }

// Traceparent renders the W3C header value
// "00-<trace-id>-<parent-id>-<trace-flags>", or "" when sc is invalid.
func (sc SpanContext) Traceparent() string {
	if !sc.IsValid() {
		return ""
	}
	b := make([]byte, 0, traceparentLen)
	b = append(b, "00-"...)
	b = appendHex(b, sc.TraceID[:])
	b = append(b, '-')
	b = appendHex(b, sc.SpanID[:])
	if sc.Sampled {
		b = append(b, "-01"...)
	} else {
		b = append(b, "-00"...)
	}
	return string(b)
}

// traceparentLen is the exact length of a version-00 traceparent value:
// "xx-<32 hex>-<16 hex>-xx".
const traceparentLen = 55

// ParseTraceparent parses a W3C traceparent header value. It enforces the
// spec: lowercase hex only, non-zero trace and parent ids, version ff
// rejected, and unknown future versions accepted as long as the version-00
// prefix parses (extra "-..." fields are ignored). The returned SpanContext
// has an empty TraceState — tracestate travels in its own header.
func ParseTraceparent(s string) (SpanContext, error) {
	if len(s) < traceparentLen || s[2] != '-' || s[35] != '-' || s[52] != '-' {
		return SpanContext{}, ErrInvalidTraceparent
	}
	version, ok := parseHexByte(s[0], s[1])
	if !ok || version == 0xff {
		return SpanContext{}, ErrInvalidTraceparent
	}
	// Version 00 is exactly 55 chars; future versions may append "-<field>"s.
	if len(s) > traceparentLen && (version == 0 || s[traceparentLen] != '-') {
		return SpanContext{}, ErrInvalidTraceparent
	}
	var sc SpanContext
	if !parseHex(sc.TraceID[:], s[3:35]) || !parseHex(sc.SpanID[:], s[36:52]) {
		return SpanContext{}, ErrInvalidTraceparent
	}
	if !sc.IsValid() {
		return SpanContext{}, ErrInvalidTraceparent
	}
	flags, ok := parseHexByte(s[53], s[54])
	if !ok {
		return SpanContext{}, ErrInvalidTraceparent
	}
	sc.Sampled = flags&0x01 != 0
	return sc, nil
}

const hexDigits = "0123456789abcdef"

func appendHex(dst, src []byte) []byte {
	for _, v := range src {
		dst = append(dst, hexDigits[v>>4], hexDigits[v&0x0f])
	}
	return dst
}

// parseHex decodes exactly len(dst)*2 lowercase hex characters (the W3C spec
// forbids uppercase, so encoding/hex would be too lenient).
func parseHex(dst []byte, s string) bool {
	for i := range dst {
		b, ok := parseHexByte(s[2*i], s[2*i+1])
		if !ok {
			return false
		}
		dst[i] = b
	}
	return true
}

func parseHexByte(hi, lo byte) (byte, bool) {
	h, ok1 := hexNibble(hi)
	l, ok2 := hexNibble(lo)
	return h<<4 | l, ok1 && ok2
}

func hexNibble(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}
