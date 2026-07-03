package clientip_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/clientip"
)

func TestMiddlewareCachesAndGetReads(t *testing.T) {
	var got string
	h := clientip.Middleware(clientip.TrustPrivateProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = clientip.Get(r) }),
	)
	r := req("10.0.0.2:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"}})
	h.ServeHTTP(httptest.NewRecorder(), r)
	assert.Equal(t, "203.0.113.5", got)
}

func TestFromReportsRanEvenWhenEmpty(t *testing.T) {
	var ip string
	var ok bool
	h := clientip.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, ok = clientip.From(r.Context())
	}))
	h.ServeHTTP(httptest.NewRecorder(), req("garbage", nil)) // resolves to ""
	assert.True(t, ok)                                       // middleware ran
	assert.Equal(t, "", ip)
}

func TestGetFallsBackWhenMiddlewareAbsent(t *testing.T) {
	r := req("192.0.2.5:9", nil)
	assert.Equal(t, "192.0.2.5", clientip.Get(r)) // safe RemoteAddr fallback
}

func TestFromAbsentReportsNotRun(t *testing.T) {
	_, ok := clientip.From(context.Background())
	assert.False(t, ok)
}

func TestLogExtractor(t *testing.T) {
	ctx := context.Background()
	_, ok := clientip.LogExtractor(ctx)
	assert.False(t, ok) // no value -> skip

	var captured string
	h := clientip.Middleware(clientip.TrustPrivateProxies())(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attr, ok := clientip.LogExtractor(r.Context())
			if ok {
				captured = attr.Value.String()
			}
			assert.Equal(t, slog.String("client_ip", "203.0.113.5"), attr)
		}),
	)
	h.ServeHTTP(httptest.NewRecorder(), req("10.0.0.2:80", map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.0.0.2"}}))
	assert.Equal(t, "203.0.113.5", captured)
}
