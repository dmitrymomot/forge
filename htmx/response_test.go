package htmx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestTriggerAfterSettle(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.TriggerAfterSettle(rec, "settled")
	assert.Equal(t, "settled", rec.Header().Get("HX-Trigger-After-Settle"))
}

func TestTriggerAfterSwap(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.TriggerAfterSwap(rec, "swapped")
	assert.Equal(t, "swapped", rec.Header().Get("HX-Trigger-After-Swap"))
}

func TestTriggerAfterSettleWith(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerAfterSettleWith(rec, map[string]any{"toast": map[string]any{"level": "info"}}))

	var got map[string]map[string]string
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger-After-Settle")), &got))
	assert.Equal(t, "info", got["toast"]["level"])
}

func TestTriggerAfterSettleWithEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerAfterSettleWith(rec, map[string]any{}))
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Settle"))
}

func TestTriggerAfterSettleWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerAfterSettleWith(rec, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Settle")) // nothing written on marshal failure
}

func TestTriggerAfterSwapWith(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerAfterSwapWith(rec, map[string]any{"y": 1}))

	var got map[string]int
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Trigger-After-Swap")), &got))
	assert.Equal(t, 1, got["y"])
}

func TestTriggerAfterSwapWithEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	require.NoError(t, htmx.TriggerAfterSwapWith(rec, map[string]any{}))
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap"))
}

func TestTriggerAfterSwapWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.TriggerAfterSwapWith(rec, map[string]any{"bad": make(chan int)})
	require.Error(t, err)
	assert.Empty(t, rec.Header().Get("HX-Trigger-After-Swap")) // nothing written on marshal failure
}

func htmxRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("HX-Request", "true")
	return r
}

func TestRedirectHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Redirect(rec, htmxRequest(), "/dashboard") // status omitted — HTMX ignores it

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Redirect"))
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestRedirectNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.Redirect(rec, r, "/dashboard", http.StatusTemporaryRedirect) // explicit fallback status

	assert.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}

func TestLocationHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Location(rec, htmxRequest(), "/dashboard") // status omitted — HTMX ignores it

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Location"))
}

func TestLocationNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.Location(rec, r, "/dashboard") // status omitted — defaults to 303 See Other

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}

func TestLocationWithHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.LocationWith(rec, htmxRequest(), "/dashboard",
		htmx.LocationOptions{Target: "#main", Swap: htmx.SwapInnerHTML}) // status omitted — HTMX ignores it
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Path   string `json:"path"`
		Target string `json:"target"`
		Swap   string `json:"swap"`
	}
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &got))
	assert.Equal(t, "/dashboard", got.Path)
	assert.Equal(t, "#main", got.Target)
	assert.Equal(t, "innerHTML", got.Swap)
}

func TestLocationWithNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	err := htmx.LocationWith(rec, r, "/dashboard",
		htmx.LocationOptions{Target: "#main"}) // status omitted — defaults to 303 See Other
	require.NoError(t, err)

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}

func TestLocationWithMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	err := htmx.LocationWith(rec, htmxRequest(), "/dashboard",
		htmx.LocationOptions{Values: map[string]any{"bad": make(chan int)}})
	require.Error(t, err)
	assert.Empty(t, rec.Header().Get("HX-Location")) // nothing written
}

func TestSwapTypeIsTyped(t *testing.T) {
	t.Parallel()

	s := htmx.SwapInnerHTML // compile-time: the Swap* constants are typed Swap
	assert.Equal(t, htmx.Swap("innerHTML"), s)
}

func TestReswapModifierStaysTyped(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.Reswap(rec, htmx.SwapInnerHTML+" swap:1s") // untyped string constant added to a Swap stays a Swap
	assert.Equal(t, "innerHTML swap:1s", rec.Header().Get("HX-Reswap"))
}

func TestRedirectExternalHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.RedirectExternal(rec, htmxRequest(), "https://example.com/oauth")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "https://example.com/oauth", rec.Header().Get("HX-Redirect")) // full-page nav, cross-origin safe
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestRedirectExternalNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.RedirectExternal(rec, r, "https://example.com/oauth")

	assert.Equal(t, http.StatusSeeOther, rec.Code) // default fallback 303
	assert.Equal(t, "https://example.com/oauth", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Redirect"))
}

func TestRedirectBackHonorsSafeLocal(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?redirect=/dashboard", nil)
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}

func TestRedirectBackHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?redirect=/dashboard", nil)
	r.Header.Set("HX-Request", "true")
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("HX-Redirect"))
}

func TestRedirectBackFallsBackOnUnsafeTarget(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, target string }{
		{"absolute", "https://evil.com"},
		{"protocol relative", "//evil.com"},
		{"backslash", "/\\evil.com"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/login?redirect="+url.QueryEscape(tc.target), nil)
			htmx.RedirectBack(rec, r, "/home")
			assert.Equal(t, http.StatusSeeOther, rec.Code)
			assert.Equal(t, "/home", rec.Header().Get("Location"))
		})
	}
}

func TestRedirectBackFallsBackWhenParamMissing(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login", nil)
	htmx.RedirectBack(rec, r, "/home")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/home", rec.Header().Get("Location"))
}

func TestRedirectBackParamCustomName(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?next=/dashboard", nil)
	htmx.RedirectBackParam(rec, r, "next", "/home")

	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
}

func TestRedirectBackParamFallsBackOnUnsafe(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/login?next="+url.QueryEscape("https://evil.com"), nil)
	htmx.RedirectBackParam(rec, r, "next", "/home")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/home", rec.Header().Get("Location"))
}

func TestLocationTargetHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	htmx.LocationTarget(rec, htmxRequest(), "/dashboard", "#main")

	assert.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Path   string `json:"path"`
		Target string `json:"target"`
	}
	require.NoError(t, json.Unmarshal([]byte(rec.Header().Get("HX-Location")), &got))
	assert.Equal(t, "/dashboard", got.Path)
	assert.Equal(t, "#main", got.Target)
}

func TestLocationTargetNonHTMX(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	htmx.LocationTarget(rec, r, "/dashboard", "#main")

	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/dashboard", rec.Header().Get("Location"))
	assert.Empty(t, rec.Header().Get("HX-Location"))
}
