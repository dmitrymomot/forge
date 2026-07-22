package sse_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/sse"
)

// discard is a flushable ResponseWriter that throws the stream away.
type discard struct {
	header http.Header
	notify chan struct{}
}

func (d *discard) Header() http.Header { return d.header }
func (d *discard) Write(p []byte) (int, error) {
	if d.notify != nil {
		d.notify <- struct{}{}
	}
	return len(p), nil
}
func (d *discard) WriteHeader(int) {}
func (d *discard) Flush()          {}

// SetWriteDeadline models a real server connection (which supports write
// deadlines); without it the benchmarks measure ResponseController's
// wrapped not-supported error instead of the production path.
func (d *discard) SetWriteDeadline(time.Time) error { return nil }

func newBenchWriter(b *testing.B) *sse.Writer {
	b.Helper()
	w, err := sse.NewWriter(&discard{header: make(http.Header)})
	if err != nil {
		b.Fatal(err)
	}
	return w
}

func BenchmarkSendText(b *testing.B) {
	w := newBenchWriter(b)
	e := sse.Event{ID: "123456", Name: "update", Data: []byte(`{"unread":3,"total":128}`)}
	b.ReportAllocs()
	for b.Loop() {
		if err := w.Send(e); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSendMultiline(b *testing.B) {
	w := newBenchWriter(b)
	e := sse.Event{Name: "chunk", Data: []byte("line one\nline two\nline three\nline four")}
	b.ReportAllocs()
	for b.Loop() {
		if err := w.Send(e); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSendComment(b *testing.B) {
	w := newBenchWriter(b)
	e := sse.Comment("")
	b.ReportAllocs()
	for b.Loop() {
		if err := w.Send(e); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHandlerDelivery measures the full publish → hub dispatch → encode
// → frame → write round trip through a connected handler.
func BenchmarkHandlerDelivery(b *testing.B) {
	hub, err := fanout.New()
	if err != nil {
		b.Fatal(err)
	}
	defer hub.Close()
	h, err := sse.NewHandler(hub, func(*http.Request) ([]string, error) {
		return []string{"bench"}, nil
	}, sse.WithKeepAlive(0))
	if err != nil {
		b.Fatal(err)
	}

	w := &discard{header: make(http.Header), notify: make(chan struct{}, 1)}
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, "/events", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, req)
	}()

	ctx := b.Context()
	payload := []byte(`{"unread":3,"total":128}`)
	// Wait for the subscription: the first delivered publish proves it.
	for {
		if err := hub.Publish(ctx, "bench", payload); err != nil {
			b.Fatal(err)
		}
		select {
		case <-w.notify:
		case <-time.After(10 * time.Millisecond):
			continue
		}
		break
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := hub.Publish(ctx, "bench", payload); err != nil {
			b.Fatal(err)
		}
		<-w.notify
	}
	hub.Close()
	<-done
}
