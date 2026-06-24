package htmx_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/htmx"
)

// mockComponent implements htmx.Renderable for testing.
type mockComponent struct {
	content string
}

func (m mockComponent) Render(_ context.Context, w io.Writer) error {
	_, err := w.Write([]byte(m.content))
	return err
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig()
	require.NotNil(t, cfg)
}

func TestWithRetarget(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithRetarget("#content"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "#content", rec.Header().Get("HX-Retarget"))
}

func TestWithReswap(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithReswap(htmx.SwapOuterHTML))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "outerHTML", rec.Header().Get("HX-Reswap"))
}

func TestWithReselect(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithReselect(".items"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, ".items", rec.Header().Get("HX-Reselect"))
}

func TestWithPushURL(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithPushURL("/contacts/123"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "/contacts/123", rec.Header().Get("HX-Push-Url"))
}

func TestWithPushURLFalse(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithPushURL("false"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "false", rec.Header().Get("HX-Push-Url"))
}

func TestWithReplaceURL(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithReplaceURL("/new-url"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "/new-url", rec.Header().Get("HX-Replace-Url"))
}

func TestWithTrigger(t *testing.T) {
	t.Parallel()

	t.Run("single event", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTrigger("contacts-updated"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		require.Equal(t, "contacts-updated", rec.Header().Get("HX-Trigger"))
	})

	t.Run("multiple events comma-joined", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTrigger("event1", "event2", "event3"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		require.Equal(t, "event1, event2, event3", rec.Header().Get("HX-Trigger"))
	})

	t.Run("event name with comma emits JSON object form", func(t *testing.T) {
		t.Parallel()

		// A comma in an event name would corrupt the comma-separated list form,
		// so ApplyHeaders must emit the JSON object form instead, preserving the
		// names verbatim.
		cfg := htmx.NewConfig(htmx.WithTrigger("a,b", "plain"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		raw := rec.Header().Get("HX-Trigger")

		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &got),
			"comma-containing trigger must be valid JSON object: %q", raw)

		require.Len(t, got, 2)
		require.Contains(t, got, "a,b")
		require.Contains(t, got, "plain")
		require.Nil(t, got["a,b"])
		require.Nil(t, got["plain"])
	})
}

func TestWithTriggerAfterSwap(t *testing.T) {
	t.Parallel()

	t.Run("single event", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTriggerAfterSwap("swapped"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		require.Equal(t, "swapped", rec.Header().Get("HX-Trigger-After-Swap"))
	})

	t.Run("event name with comma emits JSON object form", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTriggerAfterSwap("x,y"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger-After-Swap")), &got))
		require.Contains(t, got, "x,y")
	})
}

func TestWithTriggerAfterSettle(t *testing.T) {
	t.Parallel()

	t.Run("single event", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTriggerAfterSettle("settled"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		require.Equal(t, "settled", rec.Header().Get("HX-Trigger-After-Settle"))
	})

	t.Run("event name with comma emits JSON object form", func(t *testing.T) {
		t.Parallel()

		cfg := htmx.NewConfig(htmx.WithTriggerAfterSettle("p,q"))
		rec := httptest.NewRecorder()

		cfg.ApplyHeaders(rec)

		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger-After-Settle")), &got))
		require.Contains(t, got, "p,q")
	})
}

func TestWithRefresh(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(htmx.WithRefresh())
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "true", rec.Header().Get("HX-Refresh"))
}

func TestWithOOB(t *testing.T) {
	t.Parallel()

	comp1 := mockComponent{content: "<div>1</div>"}
	comp2 := mockComponent{content: "<div>2</div>"}

	cfg := htmx.NewConfig(htmx.WithOOB(comp1, comp2))

	require.Len(t, cfg.OOBComponents, 2)
}

func TestWithOOBAppends(t *testing.T) {
	t.Parallel()

	comp1 := mockComponent{content: "<div>1</div>"}
	comp2 := mockComponent{content: "<div>2</div>"}

	cfg := htmx.NewConfig(
		htmx.WithOOB(comp1),
		htmx.WithOOB(comp2),
	)

	require.Len(t, cfg.OOBComponents, 2)
}

func TestMultipleOptions(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(
		htmx.WithRetarget("#main"),
		htmx.WithReswap(htmx.SwapInnerHTML),
		htmx.WithTrigger("updated"),
		htmx.WithPushURL("/new"),
	)
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "#main", rec.Header().Get("HX-Retarget"))
	require.Equal(t, "innerHTML", rec.Header().Get("HX-Reswap"))
	require.Equal(t, "updated", rec.Header().Get("HX-Trigger"))
	require.Equal(t, "/new", rec.Header().Get("HX-Push-Url"))
}

func TestEmptyOptions(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig()
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	headers := []string{
		"HX-Retarget",
		"HX-Reswap",
		"HX-Reselect",
		"HX-Push-Url",
		"HX-Replace-Url",
		"HX-Trigger",
		"HX-Trigger-After-Swap",
		"HX-Trigger-After-Settle",
		"HX-Refresh",
	}

	for _, h := range headers {
		require.Empty(t, rec.Header().Get(h), "header %s must be empty", h)
	}
}

func TestNilConfigApplyHeaders(t *testing.T) {
	t.Parallel()

	var cfg *htmx.Config
	rec := httptest.NewRecorder()

	// Must not panic on a nil receiver.
	require.NotPanics(t, func() {
		cfg.ApplyHeaders(rec)
	})
}

func TestTriggerChaining(t *testing.T) {
	t.Parallel()

	cfg := htmx.NewConfig(
		htmx.WithTrigger("event1"),
		htmx.WithTrigger("event2"),
	)
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	require.Equal(t, "event1, event2", rec.Header().Get("HX-Trigger"))
}
