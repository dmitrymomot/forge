package pagination_test

import (
	"testing"

	"github.com/dmitrymomot/forge/data/pagination"
)

// FuzzDecode asserts Decode never panics and, on the rare valid input, always
// round-trips: re-encoding the decoded cursor and decoding again is stable.
func FuzzDecode(f *testing.F) {
	codec, err := pagination.NewCodec()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{
		"",
		"not-base64!!",
		base64Raw(`{"k":[1,"a",true]}`),
		base64Raw(`{"k":[9007199254740993],"b":true}`),
		base64Raw(`{}`),
		base64Raw(`[]`),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cur, err := codec.Decode(s)
		if err != nil {
			return // malformed input is expected to error, not panic
		}
		enc, err := codec.Encode(cur)
		if err != nil {
			t.Fatalf("re-encode of decoded cursor failed: %v", err)
		}
		again, err := codec.Decode(enc)
		if err != nil {
			t.Fatalf("re-decode failed: %v", err)
		}
		if again.Backward != cur.Backward || len(again.Keys) != len(cur.Keys) {
			t.Fatalf("round-trip drift: %#v -> %#v", cur, again)
		}
	})
}
