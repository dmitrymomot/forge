package pagination

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dmitrymomot/forge/crypto/sign"
)

// Cursor is a decoded keyset position: the boundary row's key values in the
// keyset's column order, plus the direction the caller is paging. A Cursor is
// opaque state produced and consumed by a Codec; construct it only via
// Codec.Decode (or the zero value for the first page).
type Cursor struct {
	// Keys holds the boundary row's keyset values, one per keyset column.
	Keys []any
	// Backward reports that the caller is paging toward the previous page.
	// Forward (next-page) cursors carry false.
	Backward bool
}

// IsZero reports whether the cursor carries no position — the first page,
// which needs no comparison.
func (c Cursor) IsZero() bool { return len(c.Keys) == 0 }

// cursorEnvelope is the JSON-serialized cursor body. Field names are terse to
// keep encoded cursors short.
type cursorEnvelope struct {
	K []any `json:"k"`
	B bool  `json:"b,omitempty"`
}

// Codec encodes and decodes opaque page cursors: base64url(JSON), optionally
// suffixed with an HMAC tag when built WithSigner. The signature makes a
// cursor tamper-evident; it is not required, because a cursor only names a
// position the caller could otherwise reach with ordinary query parameters.
type Codec struct {
	signer *sign.Signer // nil when unsigned
}

// NewCodec builds a cursor codec. With no options it produces unsigned
// cursors; WithSigner adds an HMAC tag.
func NewCodec(opts ...Option) (*Codec, error) {
	c, err := newConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &Codec{signer: c.signer}, nil
}

// Encode serializes cur into an opaque, URL-safe cursor string. A zero cursor
// encodes to the empty string, so the first page carries no cursor.
func (c *Codec) Encode(cur Cursor) (string, error) {
	if cur.IsZero() {
		return "", nil
	}
	raw, err := json.Marshal(cursorEnvelope{K: cur.Keys, B: cur.Backward})
	if err != nil {
		return "", fmt.Errorf("pagination: encode cursor: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	if c.signer == nil {
		return body, nil
	}
	return body + "." + c.signer.SignString(body), nil
}

// Decode parses an opaque cursor string back into a Cursor. The empty string
// decodes to the zero cursor (the first page). A malformed or, for a signed
// codec, tampered cursor returns ErrBadCursor.
//
// Numeric values decode losslessly: integral JSON numbers become int64 (so a
// bigint key survives beyond 2^53), non-integral ones float64.
func (c *Codec) Decode(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	body := s
	if c.signer != nil {
		b, sig, ok := strings.Cut(s, ".")
		if !ok || !c.signer.VerifyString(b, sig) {
			return Cursor{}, ErrBadCursor
		}
		body = b
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var env cursorEnvelope
	if err := dec.Decode(&env); err != nil {
		return Cursor{}, ErrBadCursor
	}
	if len(env.K) == 0 {
		// A position-less cursor is the zero cursor; a lingering Backward flag
		// would be a "backward first page" nonsense state. IsZero is the one
		// source of truth for "no position".
		return Cursor{}, nil
	}
	if err := normalizeNumbers(env.K); err != nil {
		return Cursor{}, err
	}
	return Cursor{Keys: env.K, Backward: env.B}, nil
}

// normalizeNumbers converts each json.Number in keys to int64 when integral,
// else float64, so decoded keys bind as ordinary Go scalars rather than the
// json.Number string type a driver would reject. A value representable as
// neither (magnitude beyond float64) is a malformed cursor: ErrBadCursor,
// never a json.Number leaking to the driver.
func normalizeNumbers(keys []any) error {
	for i, v := range keys {
		n, ok := v.(json.Number)
		if !ok {
			continue
		}
		if iv, err := n.Int64(); err == nil {
			keys[i] = iv
			continue
		}
		fv, err := n.Float64()
		if err != nil {
			return ErrBadCursor
		}
		keys[i] = fv
	}
	return nil
}
