package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/web/request"
)

func TestAccept(t *testing.T) {
	cases := []struct {
		accept string
		media  string
		want   bool
	}{
		{"application/json", "application/json", true},
		{"*/*", "application/json", true},
		{"text/*", "text/html", true},
		{"text/*", "application/json", false},
		{"application/json;q=0", "application/json", false},
		{"", "application/json", true},
		{"text/html, application/json;q=0.9", "application/json", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		assert.Equalf(t, c.want, request.Accept(r, c.media), "Accept %q media %q", c.accept, c.media)
	}
}

func TestAcceptsJSON(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept", "application/json")
	assert.True(t, request.AcceptsJSON(r))
}
