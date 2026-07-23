package sse_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/sse"
)

// noFlush is a ResponseWriter without http.Flusher, like a buffering wrapper
// that does not implement Unwrap.
type noFlush struct {
	header http.Header
	code   int
}

func (w *noFlush) Header() http.Header         { return w.header }
func (w *noFlush) Write(p []byte) (int, error) { return len(p), nil }
func (w *noFlush) WriteHeader(code int)        { w.code = code }

// countingFlusher records flushes and write errors around a recorder.
type countingFlusher struct {
	rec      *httptest.ResponseRecorder
	writeErr error
	flushes  int
}

func (w *countingFlusher) Header() http.Header { return w.rec.Header() }
func (w *countingFlusher) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.rec.Write(p)
}
func (w *countingFlusher) WriteHeader(code int) { w.rec.WriteHeader(code) }
func (w *countingFlusher) Flush()               { w.flushes++ }

func TestNewWriterHeaders(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	// A recorder cannot set write deadlines, so opt out of the send-timeout
	// bound explicitly; this test is about the stream headers.
	_, err := sse.NewWriter(rec, sse.WithSendTimeout(0))
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, rec.Flushed, "headers must be flushed so the client sees the stream open")
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "no", rec.Header().Get("X-Accel-Buffering"))
}

func TestNewWriterStreamingUnsupported(t *testing.T) {
	t.Parallel()

	w := &noFlush{header: make(http.Header)}
	_, err := sse.NewWriter(w)
	require.ErrorIs(t, err, sse.ErrStreamingUnsupported)
	assert.Empty(t, w.header.Get("Content-Type"), "stream headers must be reset on failure")
	assert.Empty(t, w.header.Get("Cache-Control"))
	assert.Empty(t, w.header.Get("X-Accel-Buffering"))
}

// deadlineFail flushes fine but hard-fails deadline changes (not a mere
// not-supported), forcing NewWriter's clear-deadline error path.
type deadlineFail struct {
	header http.Header
}

func (w *deadlineFail) Header() http.Header              { return w.header }
func (w *deadlineFail) Write(p []byte) (int, error)      { return len(p), nil }
func (w *deadlineFail) WriteHeader(int)                  {}
func (w *deadlineFail) Flush()                           {}
func (w *deadlineFail) SetWriteDeadline(time.Time) error { return errors.New("deadline broken") }

func TestNewWriterDeadlineErrorResetsHeaders(t *testing.T) {
	t.Parallel()

	w := &deadlineFail{header: make(http.Header)}
	_, err := sse.NewWriter(w)
	require.Error(t, err)
	assert.Empty(t, w.header.Get("Content-Type"), "stream headers must be reset on failure")
	assert.Empty(t, w.header.Get("Cache-Control"))
	assert.Empty(t, w.header.Get("X-Accel-Buffering"))
}

func TestSendFlushesEveryEvent(t *testing.T) {
	t.Parallel()

	cw := &countingFlusher{rec: httptest.NewRecorder()}
	w, err := sse.NewWriter(cw, sse.WithSendTimeout(0)) // recorder has no deadline control
	require.NoError(t, err)
	flushed := cw.flushes // header flush

	require.NoError(t, w.Send(sse.Text("", "one")))
	require.NoError(t, w.Send(sse.Text("", "two")))
	assert.Equal(t, flushed+2, cw.flushes)
	assert.Equal(t, "data: one\n\ndata: two\n\n", cw.rec.Body.String())
}

func TestSendWriteError(t *testing.T) {
	t.Parallel()

	cw := &countingFlusher{rec: httptest.NewRecorder()}
	w, err := sse.NewWriter(cw, sse.WithSendTimeout(0)) // recorder has no deadline control
	require.NoError(t, err)

	cw.writeErr = errors.New("client gone")
	require.Error(t, w.Send(sse.Text("", "lost")))
}

func TestSendTimeoutUnblocksStalledClient(t *testing.T) {
	t.Parallel()

	errCh := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wr, err := sse.NewWriter(w, sse.WithSendTimeout(100*time.Millisecond))
		if err != nil {
			errCh <- err
			return
		}
		e := sse.Event{Data: bytes.Repeat([]byte("x"), 1<<20)}
		for {
			if err := wr.Send(e); err != nil {
				errCh <- err
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	// Never read the body: the client stalls, kernel buffers fill, and the
	// per-send deadline must fail the blocked write instead of pinning the
	// handler forever.
	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("stalled client never unblocked the writer")
	}
}

func TestSendTimeoutValidation(t *testing.T) {
	t.Parallel()

	_, err := sse.NewWriter(httptest.NewRecorder(), sse.WithSendTimeout(-time.Second))
	require.Error(t, err)
}

// deadlinelessFlusher flushes but cannot set write deadlines and does not
// implement Unwrap — a middleware wrapper that forgot Unwrap.
type deadlinelessFlusher struct {
	header http.Header
}

func (w *deadlinelessFlusher) Header() http.Header         { return w.header }
func (w *deadlinelessFlusher) Write(p []byte) (int, error) { return len(p), nil }
func (w *deadlinelessFlusher) WriteHeader(int)             {}
func (w *deadlinelessFlusher) Flush()                      {}

func TestNewWriterSendTimeoutUnsupported(t *testing.T) {
	t.Parallel()

	// A positive send timeout that cannot be enforced fails closed before the
	// response is committed, rather than silently dropping the protection.
	w := &deadlinelessFlusher{header: make(http.Header)}
	_, err := sse.NewWriter(w)
	require.Error(t, err)
	assert.Empty(t, w.header.Get("Content-Type"), "stream headers must be reset on failure")

	// Opting out of the bound accepts unbounded writes and succeeds.
	w2 := &deadlinelessFlusher{header: make(http.Header)}
	_, err = sse.NewWriter(w2, sse.WithSendTimeout(0))
	require.NoError(t, err)
}

func TestWriterConcurrentSend(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	w, err := sse.NewWriter(rec, sse.WithSendTimeout(0)) // recorder has no deadline control
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for range 50 {
				_ = w.Send(sse.Event{ID: "1", Data: []byte{byte('a' + i)}})
			}
		})
	}
	wg.Wait()

	// Frames must never interleave: every frame is exactly id line + data
	// line + blank line.
	body := rec.Body.String()
	require.NotEmpty(t, body)
	for line := range strings.SplitSeq(body, "\n") {
		switch {
		case line == "" || line == "id: 1":
		case len(line) == 7 && strings.HasPrefix(line, "data: "):
		default:
			t.Fatalf("torn frame line: %q", line)
		}
	}
}
