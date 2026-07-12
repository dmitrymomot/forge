package fingerprint

import (
	"context"
	"testing"
)

// The empty-Hash branch in LogExtractor is defensive: FromRequest's
// combineHash always produces a non-empty hash, so no request reaching
// LogExtractor through the public API can trigger it. Reaching it requires
// stashing a Result directly under the unexported resultKey, hence this
// white-box (package fingerprint) test.
func TestLogExtractorEmptyHash(t *testing.T) {
	ctx := resultKey.With(context.Background(), Result{})

	attr, ok := LogExtractor(ctx)
	if ok {
		t.Fatal("LogExtractor reported ok=true for a Result with empty Fingerprint.Hash")
	}
	if attr.Key != "" || attr.Value.Any() != nil {
		t.Fatalf("attr = %+v, want zero value", attr)
	}
}
