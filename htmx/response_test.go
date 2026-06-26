package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/htmx"
	"github.com/stretchr/testify/assert"
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
