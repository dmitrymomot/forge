package htmx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmitrymomot/forge/htmx"
	"github.com/stretchr/testify/assert"
)

func TestRequestBooleans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		value  string
		fn     func(*http.Request) bool
		want   bool
	}{
		{"IsRequest true", "HX-Request", "true", htmx.IsRequest, true},
		{"IsRequest false value", "HX-Request", "false", htmx.IsRequest, false},
		{"IsRequest absent", "", "", htmx.IsRequest, false},
		{"IsBoosted true", "HX-Boosted", "true", htmx.IsBoosted, true},
		{"IsBoosted absent", "", "", htmx.IsBoosted, false},
		{"IsHistoryRestore true", "HX-History-Restore-Request", "true", htmx.IsHistoryRestore, true},
		{"IsHistoryRestore absent", "", "", htmx.IsHistoryRestore, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.header != "" {
				r.Header.Set(tc.header, tc.value)
			}
			assert.Equal(t, tc.want, tc.fn(r))
		})
	}
}

func TestRequestStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		value  string
		fn     func(*http.Request) string
	}{
		{"CurrentURL", "HX-Current-URL", "https://example.com/x", htmx.CurrentURL},
		{"Prompt", "HX-Prompt", "yes", htmx.Prompt},
		{"Target", "HX-Target", "#main", htmx.Target},
		{"TriggerID", "HX-Trigger", "save-btn", htmx.TriggerID},
		{"TriggerName", "HX-Trigger-Name", "save", htmx.TriggerName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(tc.header, tc.value)
			assert.Equal(t, tc.value, tc.fn(r))

			absent := httptest.NewRequest(http.MethodGet, "/", nil)
			assert.Empty(t, tc.fn(absent))
		})
	}
}
