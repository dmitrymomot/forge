package hostrouter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeHost(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"plain", "example.com", "example.com"},
		{"uppercase", "API.Example.COM", "api.example.com"},
		{"with port", "example.com:8080", "example.com"},
		{"trailing dot", "example.com.", "example.com"},
		{"port and case", "API.example.com:443", "api.example.com"},
		{"ipv6 with port", "[::1]:8080", "::1"},
		{"ipv6 no port", "[::1]", "::1"},
		{"ipv6 bracketless", "::1", "::1"},
		{"subdomain", "foo.example.com", "foo.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeHost(tt.in))
		})
	}
}

func TestSplitFirstLabel(t *testing.T) {
	tests := []struct {
		name, in, label, parent string
		ok                      bool
	}{
		{"single label", "localhost", "", "", false},
		{"two labels", "foo.example.com", "foo", "example.com", true},
		{"three labels", "a.b.example.com", "a", "b.example.com", true},
		{"leading dot", ".example.com", "", "", false},
		{"trailing dot", "example.", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, parent, ok := splitFirstLabel(tt.in)
			assert.Equal(t, tt.label, label)
			assert.Equal(t, tt.parent, parent)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
