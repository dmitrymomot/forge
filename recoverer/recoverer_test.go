package recoverer_test

import (
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

var _ = errors.Is // keep errors imported if unused above
