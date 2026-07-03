package recoverer_test

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/recoverer"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPanicBecomes500Problem(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	assert.NotContains(t, rec.Body.String(), "boom") // 5xx never leaks the panic value
}

func TestErrPanicIsMatchable(t *testing.T) {
	var captured error
	responder := func(w http.ResponseWriter, r *http.Request, err error) { captured = err }
	h := recoverer.New(recoverer.WithLogger(discard()), recoverer.WithResponder(responder))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	assert.ErrorIs(t, captured, recoverer.ErrPanic)
}

func TestAlreadyWrittenOnlyLogs(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
			panic("late")
		}),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code) // status already committed, unchanged
	assert.Equal(t, "partial", rec.Body.String())
}

func TestAbortHandlerRepanics(t *testing.T) {
	h := recoverer.New(recoverer.WithLogger(discard()))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }),
	)
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestNoPanicPassesThrough(t *testing.T) {
	h := recoverer.New()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestCustomResponderStatusHonored(t *testing.T) {
	// recoverer does NOT force 500 on a custom responder; its status is used verbatim.
	responder := func(w http.ResponseWriter, r *http.Request, err error) { w.WriteHeader(http.StatusTeapot) }
	h := recoverer.New(recoverer.WithLogger(discard()), recoverer.WithResponder(responder))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("x") }),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestPanicLogIncludesMethodAndPath(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h := recoverer.New(recoverer.WithLogger(log))(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/things", nil))
	out := buf.String()
	assert.Contains(t, out, "POST")
	assert.Contains(t, out, "/things")
}

var _ = errors.Is // keep errors imported if unused above
