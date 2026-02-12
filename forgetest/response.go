package forgetest

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Response wraps an httptest.ResponseRecorder with assertion helpers.
type Response struct {
	rec *httptest.ResponseRecorder
	t   testing.TB
}

// RequireStatus asserts that the response has the given status code.
func (r *Response) RequireStatus(t testing.TB, code int) {
	t.Helper()
	if r.rec.Code != code {
		t.Fatalf("expected status %d, got %d", code, r.rec.Code)
	}
}

// RequireRedirect asserts that the response has the given status code
// and a Location header matching the given URL.
func (r *Response) RequireRedirect(t testing.TB, code int, url string) {
	t.Helper()
	r.RequireStatus(t, code)
	loc := r.rec.Header().Get("Location")
	if loc == "" {
		// HTMX redirects use HX-Redirect header instead of Location.
		loc = r.rec.Header().Get("HX-Redirect")
	}
	if loc != url {
		t.Fatalf("expected redirect to %q, got %q", url, loc)
	}
}

// RequireHeader asserts that the response has a header with the given value.
func (r *Response) RequireHeader(t testing.TB, key, value string) {
	t.Helper()
	got := r.rec.Header().Get(key)
	if got != value {
		t.Fatalf("expected header %q to be %q, got %q", key, value, got)
	}
}

// RequireHTMXTrigger asserts that the HX-Trigger header contains the given event.
func (r *Response) RequireHTMXTrigger(t testing.TB, event string) {
	t.Helper()
	trigger := r.rec.Header().Get("HX-Trigger")
	if !strings.Contains(trigger, event) {
		t.Fatalf("expected HX-Trigger to contain %q, got %q", event, trigger)
	}
}

// RequireHTMXRetarget asserts that the HX-Retarget header matches.
func (r *Response) RequireHTMXRetarget(t testing.TB, sel string) {
	t.Helper()
	r.RequireHeader(t, "HX-Retarget", sel)
}

// RequireHTMXReswap asserts that the HX-Reswap header matches.
func (r *Response) RequireHTMXReswap(t testing.TB, strategy string) {
	t.Helper()
	r.RequireHeader(t, "HX-Reswap", strategy)
}

// RequireHTMXRefresh asserts that the HX-Refresh header is "true".
func (r *Response) RequireHTMXRefresh(t testing.TB) {
	t.Helper()
	r.RequireHeader(t, "HX-Refresh", "true")
}

// RequireHTMXPushURL asserts that the HX-Push-Url header matches.
func (r *Response) RequireHTMXPushURL(t testing.TB, url string) {
	t.Helper()
	r.RequireHeader(t, "HX-Push-Url", url)
}

// RequireHTMXReplaceURL asserts that the HX-Replace-Url header matches.
func (r *Response) RequireHTMXReplaceURL(t testing.TB, url string) {
	t.Helper()
	r.RequireHeader(t, "HX-Replace-Url", url)
}

// Body returns the response body as a string.
func (r *Response) Body() string {
	return r.rec.Body.String()
}

// HTML parses the response body as HTML and returns a Document for assertions.
func (r *Response) HTML() *Document {
	r.t.Helper()
	return newDocument(r.t, r.Body())
}

// StatusCode returns the response status code.
func (r *Response) StatusCode() int {
	return r.rec.Code
}

// Header returns the value of the given response header.
func (r *Response) Header(key string) string {
	return r.rec.Header().Get(key)
}

// Recorder returns the underlying httptest.ResponseRecorder for advanced use.
func (r *Response) Recorder() *httptest.ResponseRecorder {
	return r.rec
}
