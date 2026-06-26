package htmx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/htmx"
)

func TestSimpleSetters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		apply  func(http.ResponseWriter)
		header string
		want   string
	}{
		{"PushURL", func(w http.ResponseWriter) { htmx.PushURL(w, "/items/42") }, "HX-Push-Url", "/items/42"},
		{"PushURL prevent", func(w http.ResponseWriter) { htmx.PushURL(w, htmx.PreventHistory) }, "HX-Push-Url", "false"},
		{"ReplaceURL", func(w http.ResponseWriter) { htmx.ReplaceURL(w, "/x") }, "HX-Replace-Url", "/x"},
		{"Refresh", func(w http.ResponseWriter) { htmx.Refresh(w) }, "HX-Refresh", "true"},
		{"Reswap", func(w http.ResponseWriter) { htmx.Reswap(w, htmx.SwapOuterHTML) }, "HX-Reswap", "outerHTML"},
		{"Retarget", func(w http.ResponseWriter) { htmx.Retarget(w, "#cart") }, "HX-Retarget", "#cart"},
		{"Reselect", func(w http.ResponseWriter) { htmx.Reselect(w, "#rows") }, "HX-Reselect", "#rows"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			tc.apply(rec)
			assert.Equal(t, tc.want, rec.Header().Get(tc.header))
		})
	}
}

func TestTriggerNames(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Trigger(rec, "cart:updated", "toast")
	assert.Equal(t, "cart:updated, toast", rec.Header().Get("HX-Trigger"))
}

func TestTriggerNoNamesIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Trigger(rec)
	assert.Empty(t, rec.Header().Get("HX-Trigger"))
}

func TestTriggerWith(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerWith(rec, map[string]any{"toast": map[string]any{"level": "info"}})
	require.NoError(t, err)

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger")), &got))
	assert.Equal(t, "info", got["toast"]["level"])
}

func TestTriggerWithEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerWith(rec, map[string]any{}))
	assert.Empty(t, rec.Header().Get("HX-Trigger"))
}

func TestTriggerWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerWith(rec, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	assert.Empty(t, rec.Header().Get("HX-Trigger")) // nothing written on marshal failure
}

func TestTriggerAfterVariants(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.TriggerAfterSettle(rec, "settled")
	htmx.TriggerAfterSwap(rec, "swapped")
	assert.Equal(t, "settled", rec.Header().Get("HX-Trigger-After-Settle"))
	assert.Equal(t, "swapped", rec.Header().Get("HX-Trigger-After-Swap"))

	bad := httptest.NewRecorder()
	require.Error(t, htmx.TriggerAfterSettleWith(bad, map[string]any{"x": make(chan int)}))
	require.NoError(t, htmx.TriggerAfterSwapWith(httptest.NewRecorder(), map[string]any{"y": 1}))
}
