package sse_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/realtime/fanout"
	"github.com/dmitrymomot/forge/realtime/sse"
)

// event is one parsed SSE frame.
type event struct {
	id, name, data, comment, retry string
}

// readEvent parses one frame (lines up to a blank line) from br. Frames that
// carry only a comment come back with just the comment set.
func readEvent(br *bufio.Reader) (event, error) {
	var e event
	var dataLines []string
	seen := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return e, err
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			if !seen {
				continue // leading blank line between frames
			}
			e.data = strings.Join(dataLines, "\n")
			return e, nil
		}
		seen = true
		switch {
		case strings.HasPrefix(line, ": "):
			e.comment = line[2:]
		case strings.HasPrefix(line, "id: "):
			e.id = line[4:]
		case strings.HasPrefix(line, "event: "):
			e.name = line[7:]
		case strings.HasPrefix(line, "data: "):
			dataLines = append(dataLines, line[6:])
		case strings.HasPrefix(line, "retry: "):
			e.retry = line[7:]
		default:
			return e, fmt.Errorf("unexpected line %q", line)
		}
	}
}

// connect opens a stream against srv. The response having arrived guarantees
// the handler's fanout subscription is registered, so publishing after
// connect returns is race-free.
func connect(t *testing.T, srv *httptest.Server, header http.Header) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	maps.Copy(req.Header, header)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	return bufio.NewReader(resp.Body)
}

// newServer serves h for the test. Registering Close as a cleanup — before
// connect registers the response-body close — matters: cleanups run LIFO, the
// body close disconnects the client and lets the handler return, and only
// then can Close (which waits for in-flight handlers) finish.
func newServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func newHub(t *testing.T, opts ...fanout.Option) *fanout.Hub {
	t.Helper()
	hub, err := fanout.New(opts...)
	require.NoError(t, err)
	t.Cleanup(hub.Close)
	return hub
}

func staticTopics(topics ...string) sse.TopicsFunc {
	return func(*http.Request) ([]string, error) { return topics, nil }
}

func TestHandlerStream(t *testing.T) {
	t.Parallel()

	hub := newHub(t)
	h, err := sse.NewHandler(hub, staticTopics("notifications.42"))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	require.NoError(t, hub.Publish(t.Context(), "notifications.42", []byte("first")))
	require.NoError(t, hub.Publish(t.Context(), "notifications.42", []byte("a\nb")))

	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "notifications.42", got.name)
	assert.Equal(t, "first", got.data)
	epoch, seq, ok := strings.Cut(got.id, ".")
	require.True(t, ok, "default cursor must be epoch-namespaced")
	assert.NotEmpty(t, epoch)
	assert.Equal(t, "1", seq)

	got, err = readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "a\nb", got.data, "multi-line payloads must survive framing")
	assert.Equal(t, epoch+".2", got.id, "same instance keeps its epoch and advances the cursor")
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()

	h, err := sse.NewHandler(newHub(t), staticTopics("x"))
	require.NoError(t, err)
	srv := newServer(t, h)

	resp, err := srv.Client().Post(srv.URL, "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, http.MethodGet, resp.Header.Get("Allow"))
}

func TestHandlerTopicsError(t *testing.T) {
	t.Parallel()

	h, err := sse.NewHandler(newHub(t), func(*http.Request) ([]string, error) {
		return nil, errors.New("unknown channel")
	})
	require.NoError(t, err)
	srv := newServer(t, h)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandlerScopeMissing(t *testing.T) {
	t.Parallel()

	hub := newHub(t, fanout.WithScope(func(context.Context) (string, error) {
		return "", nil // no tenant on this request
	}))
	h, err := sse.NewHandler(hub, staticTopics("x"))
	require.NoError(t, err)
	srv := newServer(t, h)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "missing tenant scope must fail closed")
}

func TestHandlerHubClosed(t *testing.T) {
	t.Parallel()

	hub, err := fanout.New()
	require.NoError(t, err)
	hub.Close()
	h, err := sse.NewHandler(hub, staticTopics("x"))
	require.NoError(t, err)
	srv := newServer(t, h)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHandlerResume(t *testing.T) {
	t.Parallel()

	hub := newHub(t, fanout.WithReplay(16))
	// The subscribe options must survive alongside the resume cursor the
	// handler appends per request.
	h, err := sse.NewHandler(hub, staticTopics("feed"),
		sse.WithSubscribeOptions(fanout.WithBuffer(8), fanout.WithPolicy(fanout.DropNewest)))
	require.NoError(t, err)
	srv := newServer(t, h)

	// Learn a real cursor from a live event, exactly as a browser does — the
	// default cursor is epoch-namespaced, so a fabricated "1" would be foreign.
	br := connect(t, srv, nil)
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("one")))
	first, err := readEvent(br)
	require.NoError(t, err)
	require.Equal(t, "one", first.data)
	epoch, _, _ := strings.Cut(first.id, ".")

	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("two")))
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("three")))

	// Reconnect resuming after the captured cursor: replay serves the rest.
	br = connect(t, srv, http.Header{"Last-Event-Id": {first.id}})
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, event{id: epoch + ".2", name: "feed", data: "two"}, got)
	got, err = readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, event{id: epoch + ".3", name: "feed", data: "three"}, got)
}

func TestHandlerResumeWithoutReplay(t *testing.T) {
	t.Parallel()

	hub := newHub(t) // no replay ring
	h, err := sse.NewHandler(hub, staticTopics("feed"))
	require.NoError(t, err)
	srv := newServer(t, h)

	// A cursor the hub cannot honor must degrade to a live-only stream.
	br := connect(t, srv, http.Header{"Last-Event-Id": {"99"}})
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("live")))
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "live", got.data)
}

func TestHandlerInvalidLastEventID(t *testing.T) {
	t.Parallel()

	hub := newHub(t, fanout.WithReplay(16))
	h, err := sse.NewHandler(hub, staticTopics("feed"))
	require.NoError(t, err)
	srv := newServer(t, h)

	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("old")))
	br := connect(t, srv, http.Header{"Last-Event-Id": {"not-a-cursor"}})
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("live")))
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "live", got.data, "an unparseable cursor starts a fresh live stream, no replay")
}

func TestHandlerResumeForeignEpoch(t *testing.T) {
	t.Parallel()

	hub := newHub(t, fanout.WithReplay(16))
	h, err := sse.NewHandler(hub, staticTopics("feed"))
	require.NoError(t, err)
	srv := newServer(t, h)

	// Buffer history whose numeric cursors (1, 2) this instance would honor.
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("old1")))
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("old2")))

	// A well-formed cursor from a different instance/boot: valid seq, foreign
	// epoch. It must not be applied against this instance's IDs (which would
	// replay old2); it degrades to a live-only stream instead.
	br := connect(t, srv, http.Header{"Last-Event-Id": {"0000000000000000.1"}})
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("live")))
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "live", got.data, "a foreign-epoch cursor must degrade to live-only, never replay another instance's IDs")
}

func TestHandlerInvalidEventSkipped(t *testing.T) {
	t.Parallel()

	hub := newHub(t)
	// A topic carrying a line break is legal for the hub but makes the default
	// encoder produce an invalid event name.
	h, err := sse.NewHandler(hub, staticTopics("bad\ntopic", "good"))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	require.NoError(t, hub.Publish(t.Context(), "bad\ntopic", []byte("dropped")))
	require.NoError(t, hub.Publish(t.Context(), "good", []byte("kept")))

	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "good", got.name)
	assert.Equal(t, "kept", got.data, "an invalid event is skipped, not treated as a transport failure")
}

func TestHandlerKeepAlive(t *testing.T) {
	t.Parallel()

	h, err := sse.NewHandler(newHub(t), staticTopics("x"), sse.WithKeepAlive(10*time.Millisecond))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, event{comment: ""}, got, "idle stream must carry heartbeat comments")
}

func TestHandlerRetryAnnounce(t *testing.T) {
	t.Parallel()

	h, err := sse.NewHandler(newHub(t), staticTopics("x"), sse.WithRetry(2*time.Second))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, "2000", got.retry)
}

func TestHandlerCustomEncoder(t *testing.T) {
	t.Parallel()

	hub := newHub(t)
	h, err := sse.NewHandler(hub, staticTopics("feed"), sse.WithEncoder(func(m fanout.Message) (sse.Event, error) {
		if string(m.Payload) == "bad" {
			return sse.Event{}, errors.New("unencodable")
		}
		return sse.Text("", string(m.Payload)+"!"), nil
	}))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("bad")))
	require.NoError(t, hub.Publish(t.Context(), "feed", []byte("good")))

	got, err := readEvent(br)
	require.NoError(t, err)
	assert.Equal(t, event{data: "good!"}, got, "encoder failure must skip the message and keep the stream alive")
}

func TestHandlerClientDisconnect(t *testing.T) {
	t.Parallel()

	hub := newHub(t)
	h, err := sse.NewHandler(hub, staticTopics("x"))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}
}

func TestHandlerHubCloseEndsStream(t *testing.T) {
	t.Parallel()

	hub, err := fanout.New()
	require.NoError(t, err)
	h, err := sse.NewHandler(hub, staticTopics("x"))
	require.NoError(t, err)
	srv := newServer(t, h)

	br := connect(t, srv, nil)
	hub.Close()
	_, err = readEvent(br)
	require.Error(t, err, "hub shutdown must end the response so clients reconnect")
}

func TestHandlerStreamingUnsupported(t *testing.T) {
	t.Parallel()

	h, err := sse.NewHandler(newHub(t), staticTopics("x"))
	require.NoError(t, err)

	w := &noFlush{header: make(http.Header)}
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/events", nil))
	assert.Equal(t, http.StatusInternalServerError, w.code)
}

func TestHandlerWriterOptionsForwarded(t *testing.T) {
	t.Parallel()

	// An invalid writer option fails only inside the per-request NewWriter
	// call, so the 500 proves the handler forwards WithWriterOptions to it.
	h, err := sse.NewHandler(newHub(t), staticTopics("x"),
		sse.WithWriterOptions(sse.WithSendTimeout(-time.Second)))
	require.NoError(t, err)
	srv := newServer(t, h)

	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestNewHandlerValidation(t *testing.T) {
	t.Parallel()

	hub := newHub(t)
	_, err := sse.NewHandler(nil, staticTopics("x"))
	require.Error(t, err)
	_, err = sse.NewHandler(hub, nil)
	require.Error(t, err)
	_, err = sse.NewHandler(hub, staticTopics("x"),
		sse.WithKeepAlive(-time.Second), sse.WithRetry(0), sse.WithEncoder(nil), sse.WithLogger(nil))
	require.Error(t, err)
}
