package compress_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/web/compress"
	"github.com/dmitrymomot/forge/web/middleware"
	"github.com/dmitrymomot/forge/web/timeout"
)

// A handler that times out without writing must still yield 504 when compress
// is composed inside timeout — compress must not commit a spurious 200 that
// defeats timeout's !Wrote() check. Regression test for the timeout+compress
// composition bug (PR #33 review).
func TestDoesNotDefeatOuterTimeout(t *testing.T) {
	comp, err := compress.New()
	if err != nil {
		t.Fatal(err)
	}
	to, err := timeout.New(timeout.WithConfig(timeout.Config{Timeout: 10 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // ctx-respecting handler that never writes
	})
	// Wrap(h, to, comp) => to(comp(h)): timeout outermost, compress inner.
	h := middleware.Wrap(handler, to, comp)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504 (compress must not commit a spurious 200 that defeats timeout)", rec.Code)
	}
}
