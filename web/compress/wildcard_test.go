package compress_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAcceptEncodingWildcard covers RFC 9110 §12.5.3: a bare "*" in
// Accept-Encoding sets the acceptability of any content-coding not explicitly
// named, so "*" alone means gzip/deflate are acceptable and "*;q=0" disables
// any coding the client didn't list.
func TestAcceptEncodingWildcard(t *testing.T) {
	tests := []struct {
		name    string
		accept  string
		wantEnc string // "" means no compression
	}{
		{"bare wildcard prefers gzip", "*", "gzip"},
		{"gzip disabled, wildcard covers deflate", "gzip;q=0, *", "deflate"},
		{"deflate disabled, wildcard covers gzip", "deflate;q=0, *", "gzip"},
		{"wildcard q=0 disables all unlisted codings", "*;q=0", ""},
		{"explicit gzip alongside wildcard still gzip", "*, gzip", "gzip"},
		{"wildcard with both explicitly disabled", "gzip;q=0, deflate;q=0, *", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = io.WriteString(w, bigBody())
			})
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Accept-Encoding", tt.accept)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if got := rec.Header().Get("Content-Encoding"); got != tt.wantEnc {
				t.Fatalf("Accept-Encoding %q -> Content-Encoding %q, want %q", tt.accept, got, tt.wantEnc)
			}
		})
	}
}
