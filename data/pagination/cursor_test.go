package pagination_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/crypto/sign"
	"github.com/dmitrymomot/forge/data/pagination"
)

func newCodec(t *testing.T, opts ...pagination.Option) *pagination.Codec {
	t.Helper()
	c, err := pagination.NewCodec(opts...)
	if err != nil {
		t.Fatalf("NewCodec: %v", err)
	}
	return c
}

func newSigner(t *testing.T) *sign.Signer {
	t.Helper()
	s, err := sign.New([]byte("cursor-signing-key-0123456789"))
	if err != nil {
		t.Fatalf("sign.New: %v", err)
	}
	return s
}

func TestCodecRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cur  pagination.Cursor
	}{
		{"forward multi", pagination.Cursor{Keys: []any{"2026-01-01T00:00:00Z", int64(7)}}},
		{"backward flag", pagination.Cursor{Keys: []any{int64(7)}, Backward: true}},
		{"mixed scalar types", pagination.Cursor{Keys: []any{int64(3), "x", true, 1.5}}},
	}
	for _, sign := range []bool{false, true} {
		var opts []pagination.Option
		if sign {
			opts = append(opts, pagination.WithSigner(newSigner(t)))
		}
		codec := newCodec(t, opts...)
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				enc, err := codec.Encode(tt.cur)
				if err != nil {
					t.Fatalf("Encode: %v", err)
				}
				got, err := codec.Decode(enc)
				if err != nil {
					t.Fatalf("Decode: %v", err)
				}
				if got.Backward != tt.cur.Backward {
					t.Errorf("Backward: got %v want %v", got.Backward, tt.cur.Backward)
				}
				if len(got.Keys) != len(tt.cur.Keys) {
					t.Fatalf("Keys len: got %d want %d", len(got.Keys), len(tt.cur.Keys))
				}
				for i := range got.Keys {
					if got.Keys[i] != tt.cur.Keys[i] {
						t.Errorf("Keys[%d]: got %#v (%T) want %#v (%T)", i, got.Keys[i], got.Keys[i], tt.cur.Keys[i], tt.cur.Keys[i])
					}
				}
			})
		}
	}
}

func TestCodecZeroCursor(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	enc, err := codec.Encode(pagination.Cursor{})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if enc != "" {
		t.Errorf("zero cursor should encode to empty string, got %q", enc)
	}
	got, err := codec.Decode("")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("empty string should decode to zero cursor, got %#v", got)
	}
}

func TestCodecInt64Precision(t *testing.T) {
	t.Parallel()
	// A value beyond 2^53 would corrupt if decoded through float64.
	const big = int64(9007199254740993) // 2^53 + 1
	codec := newCodec(t)
	enc, err := codec.Encode(pagination.Cursor{Keys: []any{big}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := codec.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	iv, ok := got.Keys[0].(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", got.Keys[0])
	}
	if iv != big {
		t.Errorf("precision lost: got %d want %d", iv, big)
	}
}

func TestCodecNonIntegralFloat(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	enc, _ := codec.Encode(pagination.Cursor{Keys: []any{2.5}})
	got, err := codec.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if fv, ok := got.Keys[0].(float64); !ok || fv != 2.5 {
		t.Errorf("got %#v (%T), want float64 2.5", got.Keys[0], got.Keys[0])
	}
}

func TestCodecSignedRejectsTamper(t *testing.T) {
	t.Parallel()
	codec := newCodec(t, pagination.WithSigner(newSigner(t)))
	enc, _ := codec.Encode(pagination.Cursor{Keys: []any{int64(7)}})

	// Flip a character in the body (before the "." signature separator).
	body, _, _ := strings.Cut(enc, ".")
	tampered := flipFirst(body) + enc[len(body):]
	if _, err := codec.Decode(tampered); !errors.Is(err, pagination.ErrBadCursor) {
		t.Errorf("tampered body: got %v want ErrBadCursor", err)
	}

	// Drop the signature entirely.
	if _, err := codec.Decode(body); !errors.Is(err, pagination.ErrBadCursor) {
		t.Errorf("missing signature: got %v want ErrBadCursor", err)
	}
}

func TestCodecSignedRejectsForeignKey(t *testing.T) {
	t.Parallel()
	issuer := newCodec(t, pagination.WithSigner(newSigner(t)))
	enc, _ := issuer.Encode(pagination.Cursor{Keys: []any{int64(7)}})

	other, err := sign.New([]byte("a-completely-different-key-value"))
	if err != nil {
		t.Fatalf("sign.New: %v", err)
	}
	verifier := newCodec(t, pagination.WithSigner(other))
	if _, err := verifier.Decode(enc); !errors.Is(err, pagination.ErrBadCursor) {
		t.Errorf("foreign signer: got %v want ErrBadCursor", err)
	}
}

func TestCodecDecodeMalformed(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	bad := []string{
		"!!!not base64!!!",
		"////",                                  // valid base64 chars but not our alphabet edge
		base64Raw(`{"k":`),                      // truncated JSON
		base64Raw(`["not","an","object"]`),      // wrong JSON shape
		base64Raw(`{"k":[1,2,` + "\x00" + `]}`), // control byte
		base64Raw(`{"k":[1e400]}`),              // number beyond float64 range
	}
	for _, s := range bad {
		if _, err := codec.Decode(s); !errors.Is(err, pagination.ErrBadCursor) {
			t.Errorf("Decode(%q): got %v want ErrBadCursor", s, err)
		}
	}
}

func TestUnsignedIgnoresSignature(t *testing.T) {
	t.Parallel()
	// An unsigned codec treats the whole string as the body, so a value that
	// happens to contain "." must not be split.
	unsigned := newCodec(t)
	enc, _ := unsigned.Encode(pagination.Cursor{Keys: []any{"has.dot.inside"}})
	got, err := unsigned.Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Keys[0] != "has.dot.inside" {
		t.Errorf("got %#v", got.Keys[0])
	}
}

func TestCodecEncodeUnmarshalable(t *testing.T) {
	t.Parallel()
	codec := newCodec(t)
	// A channel cannot be JSON-encoded; Encode must surface the error, not
	// return a silent empty cursor.
	if _, err := codec.Encode(pagination.Cursor{Keys: []any{make(chan int)}}); err == nil {
		t.Fatal("expected an encode error for an unmarshalable key")
	}
}

func TestWithSignerNil(t *testing.T) {
	t.Parallel()
	if _, err := pagination.NewCodec(pagination.WithSigner(nil)); !errors.Is(err, pagination.ErrNilSigner) {
		t.Errorf("got %v want ErrNilSigner", err)
	}
}
