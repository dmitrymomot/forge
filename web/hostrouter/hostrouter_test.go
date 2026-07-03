package hostrouter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"plain", "example.com", "example.com"},
		{"uppercase", "API.Example.COM", "api.example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"trailing dot", "example.com.", "example.com"},
		{"port and case", "API.example.com:443", "api.example.com"},
		{"ipv6 with port", "[::1]:8080", "::1"},
		{"ipv6 no port", "[::1]", "::1"},
		{"ipv6 bracketless", "::1", "::1"},
		{"ipv6 missing bracket", "[::1", ""},
		{"subdomain", "foo.example.com", "foo.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeHost(tt.in))
		})
	}
}

// handlerWriting returns a handler that writes id to the response body.
func handlerWriting(id string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(id))
	})
}

func TestRouter_Routing(t *testing.T) {
	r := New(
		WithHost("api.example.com", handlerWriting("api")),
		WithHost("example.com", handlerWriting("apex")),
		WithHost("*.example.com", handlerWriting("wild")),
		WithFallback(handlerWriting("fallback")),
	)
	tests := []struct{ name, host, want string }{
		{"exact wins over wildcard", "api.example.com", "api"},
		{"apex exact", "example.com", "apex"},
		{"wildcard single label", "foo.example.com", "wild"},
		{"wildcard case and port", "FOO.example.com:8443", "wild"},
		{"multi level no match", "a.b.example.com", "fallback"},
		{"unknown host", "other.com", "fallback"},
		{"empty host", "", "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, tt.want, rec.Body.String())
		})
	}
}

func TestRouter_DefaultFallbackIs404(t *testing.T) {
	r := New(WithHost("api.example.com", handlerWriting("api")))
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "unknown.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRouter_NoRoutesAllFallback(t *testing.T) {
	r := New()
	req := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	req.Host = "anything.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSplitFirstLabel(t *testing.T) {
	tests := []struct {
		name, in, label, parent string
		ok                      bool
	}{
		{"no dot", "localhost", "", "", false},
		{"strips first label", "foo.example.com", "foo", "example.com", true},
		{"strips only first of many", "a.b.example.com", "a", "b.example.com", true},
		{"leading dot", ".example.com", "", "", false},
		{"trailing dot", "example.", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, parent, ok := splitFirstLabel(tt.in)
			assert.Equal(t, tt.label, label)
			assert.Equal(t, tt.parent, parent)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
