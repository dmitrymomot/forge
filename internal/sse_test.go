package internal_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	forge "github.com/dmitrymomot/forge"
)

// sseHandler wraps a test function as a forge.Handler for SSE route registration.
type sseHandler struct {
	fn func(forge.Context) error
}

func (h sseHandler) Routes(r forge.Router) {
	r.GET("/sse", h.fn)
}

// mockComponent implements forge.Component for testing.
type mockComponent struct {
	html string
	err  error
}

func (m *mockComponent) Render(_ context.Context, w io.Writer) error {
	if m.err != nil {
		return m.err
	}
	_, err := io.WriteString(w, m.html)
	return err
}

func TestSSE_StringEvent(t *testing.T) {
	t.Parallel()

	t.Run("sends single string event", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("test", "hello")
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, "event: test\n")
		require.Contains(t, body, "data: hello\n")
	})
}

func TestSSE_StringEvent_MultiLine(t *testing.T) {
	t.Parallel()

	t.Run("sends multi-line string event with separate data lines", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("multiline", "line1\nline2\nline3")
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, "event: multiline\n")
		require.Contains(t, body, "data: line1\n")
		require.Contains(t, body, "data: line2\n")
		require.Contains(t, body, "data: line3\n")
	})
}

func TestSSE_JSONEvent(t *testing.T) {
	t.Parallel()

	t.Run("sends JSON event with correct serialization", func(t *testing.T) {
		t.Parallel()

		type payload struct {
			Name string
			Age  int
		}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEJSON("update", payload{Name: "alice", Age: 30})
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, "event: update\n")
		require.Contains(t, body, `data: {"Name":"alice","Age":30}`)
	})
}

func TestSSE_TemplEvent(t *testing.T) {
	t.Parallel()

	t.Run("sends template event with rendered HTML", func(t *testing.T) {
		t.Parallel()

		component := &mockComponent{html: "<div>hello</div>"}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSETempl("render", component)
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, "event: render\n")
		require.Contains(t, body, "data: <div>hello</div>\n")
	})
}

func TestSSE_CommentEvent(t *testing.T) {
	t.Parallel()

	t.Run("sends comment event with correct format", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEComment("my comment")
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, ": my comment\n\n")
	})
}

func TestSSE_RetryEvent(t *testing.T) {
	t.Parallel()

	t.Run("sends retry event with correct milliseconds", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSERetry(5000)
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, "retry: 5000\n\n")
	})
}

func TestSSE_Headers(t *testing.T) {
	t.Parallel()

	t.Run("sets correct SSE headers", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("test", "data")
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		headers := rec.Header()
		require.Equal(t, "text/event-stream", headers.Get("Content-Type"))
		require.Equal(t, "no-cache", headers.Get("Cache-Control"))
		require.Equal(t, "keep-alive", headers.Get("Connection"))
		require.Equal(t, "no", headers.Get("X-Accel-Buffering"))
	})
}

func TestSSE_ChannelClose(t *testing.T) {
	t.Parallel()

	t.Run("handler returns nil after channel close", func(t *testing.T) {
		t.Parallel()

		var handlerErr error
		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("event1", "data1")
				ch <- forge.SSEString("event2", "data2")
			}()
			handlerErr = c.SSE(ch)
			return handlerErr
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, handlerErr)
		body := rec.Body.String()
		require.Contains(t, body, "event: event1\n")
		require.Contains(t, body, "event: event2\n")
	})
}

func TestSSE_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("handler returns nil when context is cancelled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		var handlerErr error

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("event1", "data1")
				time.Sleep(20 * time.Millisecond)
				cancel() // Cancel context after first event
				time.Sleep(20 * time.Millisecond)
				ch <- forge.SSEString("event2", "data2") // This may not be processed
			}()
			handlerErr = c.SSE(ch)
			return handlerErr
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.NoError(t, handlerErr)
		body := rec.Body.String()
		require.Contains(t, body, "event: event1\n")
	})
}

func TestSSE_KeepAlive(t *testing.T) {
	t.Parallel()

	t.Run("sends keepalive comments when no events", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{},
			forge.WithSSEKeepAlive(50*time.Millisecond),
			forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
				ch := make(chan forge.SSEEvent)
				go func() {
					defer close(ch)
					time.Sleep(120 * time.Millisecond) // Wait for keepalive to fire
					ch <- forge.SSEString("done", "bye")
				}()
				return c.SSE(ch)
			}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.Contains(t, body, ": keepalive\n\n")
		require.Contains(t, body, "event: done\n")
	})
}

func TestSSE_JSONMarshalError(t *testing.T) {
	t.Parallel()

	t.Run("returns error when JSON marshaling fails", func(t *testing.T) {
		t.Parallel()

		var handlerErr error
		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				// math.Inf cannot be marshaled to JSON
				ch <- forge.SSEJSON("bad", map[string]any{"value": math.Inf(1)})
			}()
			handlerErr = c.SSE(ch)
			return handlerErr
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, handlerErr)
	})
}

func TestSSE_TemplRenderError(t *testing.T) {
	t.Parallel()

	t.Run("returns error when template rendering fails", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("render failed")
		component := &mockComponent{err: expectedErr}

		var handlerErr error
		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSETempl("render", component)
			}()
			handlerErr = c.SSE(ch)
			return handlerErr
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Error(t, handlerErr)
		require.ErrorIs(t, handlerErr, expectedErr)
	})
}

func TestSSE_MultipleEvents(t *testing.T) {
	t.Parallel()

	t.Run("sends multiple events of different types in order", func(t *testing.T) {
		t.Parallel()

		component := &mockComponent{html: "<p>content</p>"}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("string", "text")
				ch <- forge.SSEJSON("json", map[string]int{"count": 42})
				ch <- forge.SSEComment("this is a comment")
				ch <- forge.SSETempl("html", component)
				ch <- forge.SSERetry(3000)
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()

		// Verify all events appear in the output
		require.Contains(t, body, "event: string\n")
		require.Contains(t, body, "data: text\n")
		require.Contains(t, body, "event: json\n")
		require.Contains(t, body, `data: {"count":42}`)
		require.Contains(t, body, ": this is a comment\n")
		require.Contains(t, body, "event: html\n")
		require.Contains(t, body, "data: <p>content</p>\n")
		require.Contains(t, body, "retry: 3000\n")

		// Verify order by checking string indices
		stringIdx := strings.Index(body, "event: string\n")
		jsonIdx := strings.Index(body, "event: json\n")
		commentIdx := strings.Index(body, ": this is a comment\n")
		htmlIdx := strings.Index(body, "event: html\n")
		retryIdx := strings.Index(body, "retry: 3000\n")

		require.Less(t, stringIdx, jsonIdx)
		require.Less(t, jsonIdx, commentIdx)
		require.Less(t, commentIdx, htmlIdx)
		require.Less(t, htmlIdx, retryIdx)
	})
}

func TestSSE_EmptyEventName(t *testing.T) {
	t.Parallel()

	t.Run("omits event line when event name is empty", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEString("", "hello")
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.NotContains(t, body, "event:")
		require.Contains(t, body, "data: hello\n")
	})

	t.Run("omits event line for JSON with empty event name", func(t *testing.T) {
		t.Parallel()

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSEJSON("", map[string]string{"msg": "test"})
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.NotContains(t, body, "event:")
		require.Contains(t, body, `data: {"msg":"test"}`)
	})

	t.Run("omits event line for template with empty event name", func(t *testing.T) {
		t.Parallel()

		component := &mockComponent{html: "<span>test</span>"}

		app := forge.New(forge.AppConfig{}, forge.WithHandlers(sseHandler{fn: func(c forge.Context) error {
			ch := make(chan forge.SSEEvent)
			go func() {
				defer close(ch)
				ch <- forge.SSETempl("", component)
			}()
			return c.SSE(ch)
		}}))

		req := httptest.NewRequest(http.MethodGet, "/sse", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		body := rec.Body.String()
		require.NotContains(t, body, "event:")
		require.Contains(t, body, "data: <span>test</span>\n")
	})
}
