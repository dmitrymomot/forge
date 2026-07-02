package slug_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/slug"
)

func TestMake_UnicodeFolding(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"accented-e", "Café", "cafe"},
		{"umlaut-u", "über", "uber"},
		{"n-tilde", "Mañana", "manana"},
		{"mixed-diacritics", "à á â ä ā ă ą", "a-a-a-a-a-a-a"},
		{"czech", "Příliš žluťoučký", "prilis-zlutoucky"},
		{"eszett", "Straße", "strasse"},
		{"o-slash", "Søren", "soren"},
		{"l-stroke", "Łódź", "lodz"},
		{"d-stroke", "Đorđe", "dorde"},
		{"ae-ligature", "Æther", "aether"},
		{"oe-ligature", "Œuvre", "oeuvre"},
		{"cjk-collapses", "你好世界", ""},
		{"cyrillic-collapses", "Привет", ""},
		{"latin-around-cjk", "hello 世界 world", "hello-world"},
		{"emoji-dropped", "hi 👋 there", "hi-there"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, slug.Make(tt.in), "Make(%q)", tt.in)
		})
	}
}
