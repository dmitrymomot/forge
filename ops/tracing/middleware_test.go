package tracing_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/core/ctxkey"
	"github.com/dmitrymomot/forge/ops/tracing"
	"github.com/dmitrymomot/forge/web/middleware"
)

// recordingTracer is a test double capturing everything the middleware does to
// its spans, layered on the real propagation tracer for identity handling.
type recordingTracer struct {
	mu    sync.Mutex
	spans []*recordedSpan
}

type recordedSpan struct {
	mu       sync.Mutex
	sc       tracing.SpanContext
	name     string
	kind     tracing.SpanKind
	attrs    []slog.Attr
	events   []string
	errs     []error
	status   tracing.Status
	statusD  string
	endCalls int
}

func (t *recordingTracer) Start(ctx context.Context, name string, opts ...tracing.StartOption) (context.Context, tracing.Span) {
	cfg := tracing.NewStartConfig(opts...)
	ctx, id := tracing.New().Start(ctx, name, opts...)
	s := &recordedSpan{sc: id.Context(), name: name, kind: cfg.Kind, attrs: cfg.Attrs}
	t.mu.Lock()
	t.spans = append(t.spans, s)
	t.mu.Unlock()
	return tracing.ContextWithSpan(ctx, s), s
}

func (t *recordingTracer) last(tb testing.TB) *recordedSpan {
	tb.Helper()
	t.mu.Lock()
	defer t.mu.Unlock()
	require.NotEmpty(tb, t.spans)
	return t.spans[len(t.spans)-1]
}

func (s *recordedSpan) Context() tracing.SpanContext { return s.sc }
func (s *recordedSpan) IsRecording() bool            { return true }

func (s *recordedSpan) SetName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.name = name
}

func (s *recordedSpan) SetAttributes(attrs ...slog.Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attrs = append(s.attrs, attrs...)
}

func (s *recordedSpan) AddEvent(name string, _ ...slog.Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, name)
}

func (s *recordedSpan) RecordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *recordedSpan) SetStatus(code tracing.Status, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.statusD = code, description
}

func (s *recordedSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endCalls++
}

func (s *recordedSpan) attr(tb testing.TB, key string) slog.Value {
	tb.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.attrs {
		if a.Key == key {
			return a.Value
		}
	}
	tb.Fatalf("attribute %q not recorded", key)
	return slog.Value{}
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestMiddlewareStartsServerSpanWithRouteName(t *testing.T) {
	tr := &recordingTracer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	h := middleware.Wrap(mux, tracing.NewMiddleware(tr))

	serve(h, httptest.NewRequest(http.MethodGet, "/users/42", nil))

	span := tr.last(t)
	assert.Equal(t, "GET /users/{id}", span.name)
	assert.Equal(t, tracing.KindServer, span.kind)
	assert.Equal(t, 1, span.endCalls)
	assert.Equal(t, "GET", span.attr(t, "http.request.method").String())
	assert.Equal(t, "/users/42", span.attr(t, "url.path").String())
	assert.Equal(t, "/users/{id}", span.attr(t, "http.route").String())
	assert.Equal(t, int64(http.StatusCreated), span.attr(t, "http.response.status_code").Int64())
	assert.Equal(t, tracing.StatusUnset, span.status)
}

func TestMiddlewareUnmatchedRouteKeepsMethodName(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	serve(h, httptest.NewRequest(http.MethodGet, "/whatever", nil))

	span := tr.last(t)
	assert.Equal(t, "GET", span.name)
	assert.Equal(t, int64(http.StatusOK), span.attr(t, "http.response.status_code").Int64(), "implicit 200")
}

func TestMiddlewareNonStandardMethodNormalized(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	serve(h, httptest.NewRequest("PROPFIND", "/x", nil))

	assert.Equal(t, "OTHER", tr.last(t).name)
}

func TestMiddlewareContinuesInboundTrace(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	r.Header.Set("tracestate", "vendor=a")
	r.Header.Add("tracestate", "other=b")
	serve(h, r)

	sc := tr.last(t).sc
	assert.Equal(t, sampleTraceHex, sc.TraceID.String())
	assert.NotEqual(t, sampleSpanHex, sc.SpanID.String())
	assert.Equal(t, "vendor=a,other=b", sc.TraceState)
}

func TestMiddlewareIgnoresInvalidTraceparent(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", "00-garbage-garbage-01")
	serve(h, r)

	sc := tr.last(t).sc
	assert.True(t, sc.IsValid())
	assert.NotEqual(t, sampleTraceHex, sc.TraceID.String())
}

func TestMiddlewareDropsOversizedTracestate(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	r.Header.Set("tracestate", "v="+strings.Repeat("x", 600))
	serve(h, r)

	assert.Empty(t, tr.last(t).sc.TraceState)
}

func TestMiddlewareTrustInboundDisabled(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr, tracing.WithTrustInbound(false))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("traceparent", sampleTraceparent)
	serve(h, r)

	assert.NotEqual(t, sampleTraceHex, tr.last(t).sc.TraceID.String())
}

func TestMiddlewareFiveHundredMarksError(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))

	span := tr.last(t)
	assert.Equal(t, tracing.StatusError, span.status)
	assert.Equal(t, http.StatusText(http.StatusBadGateway), span.statusD)
}

func TestMiddlewareFourHundredStaysUnset(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.Equal(t, tracing.StatusUnset, tr.last(t).status)
}

func TestMiddlewareSkip(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr, tracing.WithSkip(func(r *http.Request) bool {
		return r.URL.Path == "/livez"
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	serve(h, httptest.NewRequest(http.MethodGet, "/livez", nil))
	assert.Empty(t, tr.spans)

	serve(h, httptest.NewRequest(http.MethodGet, "/real", nil))
	assert.Len(t, tr.spans, 1)
}

func TestMiddlewareHandlerSeesSpanContext(t *testing.T) {
	tr := &recordingTracer{}
	var got tracing.SpanContext
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = tracing.SpanContextFromContext(r.Context())
	}))

	serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.Equal(t, tr.last(t).sc, got)
}

func TestMiddlewareEndsSpanOnPanic(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	assert.Panics(t, func() {
		serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))
	})
	assert.Equal(t, 1, tr.last(t).endCalls)
}

var tenantKey = ctxkey.New[string]("tenant")

func TestMiddlewareContextAttr(t *testing.T) {
	tr := &recordingTracer{}
	mw := tracing.NewMiddleware(tr, tracing.WithContextAttr("tenant", func(ctx context.Context) string {
		v, _ := tenantKey.From(ctx)
		return v
	}))
	h := mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	serve(h, r.WithContext(tenantKey.With(r.Context(), "acme")))
	assert.Equal(t, "acme", tr.last(t).attr(t, "tenant").String())

	serve(h, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, "unknown", tr.last(t).attr(t, "tenant").String(), "missing scope fails closed")
}

func TestMiddlewareContextAttrPanicsOnBadConfig(t *testing.T) {
	assert.Panics(t, func() { tracing.WithContextAttr("", func(context.Context) string { return "" }) })
	assert.Panics(t, func() { tracing.WithContextAttr("tenant", nil) })
}

func TestNewMiddlewarePanicsOnNilTracer(t *testing.T) {
	assert.Panics(t, func() { tracing.NewMiddleware(nil) })
}

func TestMiddlewareWithPathFunc(t *testing.T) {
	tr := &recordingTracer{}
	h := tracing.NewMiddleware(tr, tracing.WithPathFunc(func(*http.Request) string {
		return "/custom/:id"
	}))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	serve(h, httptest.NewRequest(http.MethodGet, "/custom/7", nil))

	span := tr.last(t)
	assert.Equal(t, "GET /custom/:id", span.name)
	assert.Equal(t, "/custom/:id", span.attr(t, "http.route").String())
}
