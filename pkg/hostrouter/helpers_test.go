package hostrouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/pkg/hostrouter"
)

func TestGetDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		expected string
	}{
		{
			name:     "simple domain",
			host:     "example.com",
			expected: "example.com",
		},
		{
			name:     "domain with port",
			host:     "example.com:8080",
			expected: "example.com",
		},
		{
			name:     "subdomain",
			host:     "api.example.com",
			expected: "api.example.com",
		},
		{
			name:     "subdomain with port",
			host:     "api.example.com:443",
			expected: "api.example.com",
		},
		{
			name:     "uppercase domain",
			host:     "Example.COM",
			expected: "example.com",
		},
		{
			name:     "mixed case with port",
			host:     "API.Example.Com:8080",
			expected: "api.example.com",
		},
		{
			name:     "IPv4 address",
			host:     "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv4 address with port",
			host:     "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv6 address",
			host:     "[::1]",
			expected: "[::1]",
		},
		{
			name:     "IPv6 address with port",
			host:     "[::1]:8080",
			expected: "[::1]",
		},
		{
			name:     "IPv6 full address",
			host:     "[2001:db8::1]",
			expected: "[2001:db8::1]",
		},
		{
			name:     "IPv6 full address with port",
			host:     "[2001:db8::1]:8080",
			expected: "[2001:db8::1]",
		},
		{
			name:     "localhost",
			host:     "localhost",
			expected: "localhost",
		},
		{
			name:     "localhost with port",
			host:     "localhost:3000",
			expected: "localhost",
		},
		{
			name:     "empty host",
			host:     "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host

			result := hostrouter.GetDomain(req)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubdomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		host       string
		baseDomain string
		expected   string
	}{
		{
			name:       "single subdomain",
			host:       "foo.example.com",
			baseDomain: "example.com",
			expected:   "foo",
		},
		{
			name:       "multi-level subdomain",
			host:       "bar.foo.example.com",
			baseDomain: "example.com",
			expected:   "bar.foo",
		},
		{
			name:       "deep subdomain",
			host:       "a.b.c.example.com",
			baseDomain: "example.com",
			expected:   "a.b.c",
		},
		{
			name:       "exact match returns empty",
			host:       "example.com",
			baseDomain: "example.com",
			expected:   "",
		},
		{
			name:       "different domain returns empty",
			host:       "other.com",
			baseDomain: "example.com",
			expected:   "",
		},
		{
			name:       "partial match returns empty",
			host:       "notexample.com",
			baseDomain: "example.com",
			expected:   "",
		},
		{
			name:       "subdomain of different domain returns empty",
			host:       "foo.other.com",
			baseDomain: "example.com",
			expected:   "",
		},
		{
			name:       "with port strips port first",
			host:       "foo.example.com:8080",
			baseDomain: "example.com",
			expected:   "foo",
		},
		{
			name:       "case insensitive host",
			host:       "FOO.Example.COM",
			baseDomain: "example.com",
			expected:   "foo",
		},
		{
			name:       "case insensitive base domain",
			host:       "foo.example.com",
			baseDomain: "Example.COM",
			expected:   "foo",
		},
		{
			name:       "empty host returns empty",
			host:       "",
			baseDomain: "example.com",
			expected:   "",
		},
		{
			name:       "empty base domain returns empty",
			host:       "foo.example.com",
			baseDomain: "",
			expected:   "",
		},
		{
			name:       "localhost subdomain",
			host:       "tenant1.localhost",
			baseDomain: "localhost",
			expected:   "tenant1",
		},
		{
			name:       "localhost exact match",
			host:       "localhost",
			baseDomain: "localhost",
			expected:   "",
		},
		{
			name:       "wildcard-like subdomain",
			host:       "www.example.com",
			baseDomain: "example.com",
			expected:   "www",
		},
		{
			name:       "api subdomain common case",
			host:       "api.myapp.com",
			baseDomain: "myapp.com",
			expected:   "api",
		},
		{
			name:       "tenant subdomain common case",
			host:       "acme.myapp.com",
			baseDomain: "myapp.com",
			expected:   "acme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host

			result := hostrouter.GetSubdomain(req, tt.baseDomain)
			require.Equal(t, tt.expected, result)
		})
	}
}
