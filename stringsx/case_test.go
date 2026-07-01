package stringsx_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/stringsx"
)

func TestToSnake(t *testing.T) {
	cases := map[string]string{
		"UserID":        "user_id",
		"HTTPServer":    "http_server",
		"userName":      "user_name",
		"Already_Snake": "already_snake",
		"kebab-case":    "kebab_case",
		"with space":    "with_space",
		"":              "",
	}
	for in, want := range cases {
		assert.Equal(t, want, stringsx.ToSnake(in), "ToSnake(%q)", in)
	}
}

func TestToKebab(t *testing.T) {
	assert.Equal(t, "user-id", stringsx.ToKebab("UserID"))
	assert.Equal(t, "http-server", stringsx.ToKebab("HTTPServer"))
	assert.Equal(t, "user-name", stringsx.ToKebab("user_name"))
}

func TestToCamel(t *testing.T) {
	cases := map[string]string{
		"user_id":     "userId", // mechanical: no acronym special-casing
		"user-name":   "userName",
		"HTTP server": "httpServer",
		"Already":     "already",
		"":            "",
	}
	for in, want := range cases {
		assert.Equal(t, want, stringsx.ToCamel(in), "ToCamel(%q)", in)
	}
}

func TestToCamelWith(t *testing.T) {
	assert.Equal(t, "userID", stringsx.ToCamelWith("user_id", "ID"))
	assert.Equal(t, "apiURL", stringsx.ToCamelWith("api_url", "URL"))
	assert.Equal(t, "getUserOAuthToken", stringsx.ToCamelWith("get_user_oauth_token", "OAuth"))
	assert.Equal(t, "idToken", stringsx.ToCamelWith("id_token", "ID"), "leading word always lowercased")
	assert.Equal(t, "userName", stringsx.ToCamelWith("user_name", "ID"), "unmatched words title-cased")
}
