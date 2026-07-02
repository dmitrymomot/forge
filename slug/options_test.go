package slug_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/slug"
)

func TestMake_ASCIIBasics(t *testing.T) {
	tests := []struct {
		name string
		in   string
		opts []slug.Option
		want string
	}{
		{"simple", "Hello World", nil, "hello-world"},
		{"already-slug", "hello-world", nil, "hello-world"},
		{"collapse-spaces", "Hello    World", nil, "hello-world"},
		{"collapse-symbols", "hello!!!world", nil, "hello-world"},
		{"trim-separators", "  Hello World  ", nil, "hello-world"},
		{"leading-trailing-symbols", "***hello***", nil, "hello"},
		{"digits-kept", "Product 123", nil, "product-123"},
		{"empty", "", nil, ""},
		{"only-symbols", "!!!", nil, ""},
		{"keep-case", "Hello World", []slug.Option{slug.WithLowercase(false)}, "Hello-World"},
		{"custom-separator", "Hello World", []slug.Option{slug.WithSeparator("_")}, "hello_world"},
		{"custom-replace", "Tom & Jerry @ home", []slug.Option{slug.WithCustomReplace(map[string]string{"&": "and", "@": "at"})}, "tom-and-jerry-at-home"},
		{"strip-chars", "he'llo wo'rld", []slug.Option{slug.WithStripChars("'")}, "hello-world"},
		{"max-length", "hello world foobar", []slug.Option{slug.WithMaxLength(11)}, "hello-world"},
		{"max-length-no-trailing-sep", "hello world", []slug.Option{slug.WithMaxLength(6)}, "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slug.Make(tt.in, tt.opts...), "Make(%q)", tt.in)
		})
	}
}
