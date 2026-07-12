package fingerprint_test

import (
	"bytes"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/fingerprint"
)

// FuzzIngest feeds arbitrary bytes as the IngestHandler's POST body. The
// handler must never panic on untrusted input: malformed JSON, oversized
// payloads, and bad tokens are all expected to be rejected with an HTTP error
// response, not a crash.
func FuzzIngest(f *testing.F) {
	f.Add([]byte(`{"token":"x.y","data":{"timezone":"UTC"}}`))
	cfg := fingerprint.Config{Secret: "s", Version: 1, TokenTTL: time.Minute}
	fp, err := fingerprint.New(cfg)
	if err != nil {
		f.Fatal(err)
	}
	h := fp.IngestHandler()
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest("POST", "/_fp/ingest", bytes.NewReader(body))
		h.ServeHTTP(httptest.NewRecorder(), req) // must never panic
	})
}
