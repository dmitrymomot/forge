package i18n

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileAndRender(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		msg   string
		args  []any
		count int
		hasN  bool
		want  string
	}{
		{"literal", "Hello world", nil, 0, false, "Hello world"},
		{"one placeholder", "Hello, {{name}}!", []any{"name", "Juan"}, 0, false, "Hello, Juan!"},
		{"two placeholders", "{{a}} and {{b}}", []any{"a", "x", "b", "y"}, 0, false, "x and y"},
		{"missing arg keeps placeholder", "Hi {{name}}", nil, 0, false, "Hi {{name}}"},
		{"count injected", "{{count}} items", nil, 5, true, "5 items"},
		{"explicit count wins", "{{count}} items", []any{"count", "five"}, 5, true, "five items"},
		{"count not injected without hasCount", "{{count}} items", nil, 5, false, "{{count}} items"},
		{"int arg", "n={{n}}", []any{"n", 42}, 0, false, "n=42"},
		{"int64 arg", "n={{n}}", []any{"n", int64(-7)}, 0, false, "n=-7"},
		{"float arg", "f={{f}}", []any{"f", 1.5}, 0, false, "f=1.5"},
		{"unmatched open brace literal", "a {{b c", nil, 0, false, "a {{b c"},
		{"empty name literal", "a {{}} b", nil, 0, false, "a {{}} b"},
		{"spaced name literal", "a {{b c}} d", nil, 0, false, "a {{b c}} d"},
		{"adjacent", "{{a}}{{b}}", []any{"a", "1", "b", "2"}, 0, false, "12"},
		{"odd args ignore dangling", "x {{a}}", []any{"a", "ok", "dangling"}, 0, false, "x ok"},
		{"non-string key ignored", "x {{a}}", []any{1, "nope", "a", "ok"}, 0, false, "x ok"},
		{"unicode", "Привіт, {{name}}!", []any{"name", "Олю"}, 0, false, "Привіт, Олю!"},
		{"empty message", "", nil, 0, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m := compileMessage(c.msg)
			assert.Equalf(t, c.want, m.render(c.args, c.count, c.hasN), "render(%q)", c.msg)
		})
	}
}

func TestCompileSegments(t *testing.T) {
	t.Parallel()
	m := compileMessage("Hello, {{name}}! Bye")
	require.Len(t, m.segs, 3)
	assert.Equal(t, "Hello, ", m.segs[0].lit)
	assert.Equal(t, "name", m.segs[1].arg)
	assert.Equal(t, "! Bye", m.segs[2].lit)
}

func TestRenderLiteralFastPath(t *testing.T) {
	// No t.Parallel(): testing.AllocsPerRun forces GOMAXPROCS=1 and panics
	// ("AllocsPerRun called during parallel test") if the test runs in
	// parallel with others.
	// A literal-only message must not copy: render returns the interned literal.
	m := compileMessage("static")
	assert.Equal(t, 0.0, testing.AllocsPerRun(100, func() {
		_ = m.render(nil, 0, false)
	}), "literal-only render must not allocate")
}

func TestAppendToDoesNotClobber(t *testing.T) {
	t.Parallel()
	m := compileMessage("{{a}}")
	dst := []byte("prefix:")
	got := m.appendTo(dst, []any{"a", "v"}, 0, false)
	assert.Equal(t, "prefix:v", string(got))
}

func FuzzCompileMessage(f *testing.F) {
	f.Add("Hello, {{name}}!")
	f.Add("{{a}}{{b}}{{")
	f.Add("}}{{}}{{x y}}")
	f.Add("{{count}}")
	f.Fuzz(func(t *testing.T, s string) {
		m := compileMessage(s)
		// Must never panic. With no args and hasCount=false, every
		// placeholder segment (matched or not) falls back to rendering its
		// exact original "{{name}}" bytes, so the round trip holds for any
		// input, not just placeholder-free ones.
		got := m.render(nil, 0, false)
		assert.Equal(t, s, got)
	})
}
