package htmx_test

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"

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
	cfg := htmx.NewConfig()
	if cfg == nil {
		t.Fatal("NewConfig returned nil")
	}
}

func TestWithRetarget(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithRetarget("#content"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Retarget")
	if got != "#content" {
		t.Errorf("HX-Retarget = %q, want %q", got, "#content")
	}
}

func TestWithReswap(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithReswap(htmx.SwapOuterHTML))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Reswap")
	if got != "outerHTML" {
		t.Errorf("HX-Reswap = %q, want %q", got, "outerHTML")
	}
}

func TestWithReselect(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithReselect(".items"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Reselect")
	if got != ".items" {
		t.Errorf("HX-Reselect = %q, want %q", got, ".items")
	}
}

func TestWithPushURL(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithPushURL("/contacts/123"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Push-Url")
	if got != "/contacts/123" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "/contacts/123")
	}
}

func TestWithPushURLFalse(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithPushURL("false"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Push-Url")
	if got != "false" {
		t.Errorf("HX-Push-Url = %q, want %q", got, "false")
	}
}

func TestWithReplaceURL(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithReplaceURL("/new-url"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Replace-Url")
	if got != "/new-url" {
		t.Errorf("HX-Replace-Url = %q, want %q", got, "/new-url")
	}
}

func TestWithTrigger(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithTrigger("contacts-updated"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Trigger")
	if got != "contacts-updated" {
		t.Errorf("HX-Trigger = %q, want %q", got, "contacts-updated")
	}
}

func TestWithTriggerMultiple(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithTrigger("event1", "event2", "event3"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Trigger")
	want := "event1, event2, event3"
	if got != want {
		t.Errorf("HX-Trigger = %q, want %q", got, want)
	}
}

func TestWithTriggerAfterSwap(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithTriggerAfterSwap("swapped"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Trigger-After-Swap")
	if got != "swapped" {
		t.Errorf("HX-Trigger-After-Swap = %q, want %q", got, "swapped")
	}
}

func TestWithTriggerAfterSettle(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithTriggerAfterSettle("settled"))
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Trigger-After-Settle")
	if got != "settled" {
		t.Errorf("HX-Trigger-After-Settle = %q, want %q", got, "settled")
	}
}

func TestWithRefresh(t *testing.T) {
	cfg := htmx.NewConfig(htmx.WithRefresh())
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Refresh")
	if got != "true" {
		t.Errorf("HX-Refresh = %q, want %q", got, "true")
	}
}

func TestWithOOB(t *testing.T) {
	comp1 := mockComponent{content: "<div>1</div>"}
	comp2 := mockComponent{content: "<div>2</div>"}

	cfg := htmx.NewConfig(htmx.WithOOB(comp1, comp2))

	if len(cfg.OOBComponents) != 2 {
		t.Errorf("OOBComponents len = %d, want 2", len(cfg.OOBComponents))
	}
}

func TestWithOOBAppends(t *testing.T) {
	comp1 := mockComponent{content: "<div>1</div>"}
	comp2 := mockComponent{content: "<div>2</div>"}

	cfg := htmx.NewConfig(
		htmx.WithOOB(comp1),
		htmx.WithOOB(comp2),
	)

	if len(cfg.OOBComponents) != 2 {
		t.Errorf("OOBComponents len = %d, want 2", len(cfg.OOBComponents))
	}
}

func TestMultipleOptions(t *testing.T) {
	cfg := htmx.NewConfig(
		htmx.WithRetarget("#main"),
		htmx.WithReswap(htmx.SwapInnerHTML),
		htmx.WithTrigger("updated"),
		htmx.WithPushURL("/new"),
	)
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	tests := []struct {
		header string
		want   string
	}{
		{"HX-Retarget", "#main"},
		{"HX-Reswap", "innerHTML"},
		{"HX-Trigger", "updated"},
		{"HX-Push-Url", "/new"},
	}

	for _, tt := range tests {
		got := rec.Header().Get(tt.header)
		if got != tt.want {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestEmptyOptions(t *testing.T) {
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
		if got := rec.Header().Get(h); got != "" {
			t.Errorf("%s = %q, want empty", h, got)
		}
	}
}

func TestNilConfigApplyHeaders(t *testing.T) {
	var cfg *htmx.Config
	rec := httptest.NewRecorder()

	// Should not panic
	cfg.ApplyHeaders(rec)
}

func TestTriggerChaining(t *testing.T) {
	cfg := htmx.NewConfig(
		htmx.WithTrigger("event1"),
		htmx.WithTrigger("event2"),
	)
	rec := httptest.NewRecorder()

	cfg.ApplyHeaders(rec)

	got := rec.Header().Get("HX-Trigger")
	want := "event1, event2"
	if got != want {
		t.Errorf("HX-Trigger = %q, want %q", got, want)
	}
}
