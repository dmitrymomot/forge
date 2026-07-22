package markdown_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dmitrymomot/forge/comms/email/markdown"
)

const welcomeDoc = `---
subject: Confirm your email
preheader: One click and you're in.
---
# Almost there

Hi! Confirm your address to **activate** your account.

[Button: Confirm email](https://app.acme.example/confirm?t=abc)

If the button doesn't work, reply to this email.
`

func newRenderer(t *testing.T, opts ...markdown.Option) *markdown.Renderer {
	t.Helper()
	r, err := markdown.New(opts...)
	require.NoError(t, err)
	return r
}

func TestRender(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)

	msg, err := r.Render([]byte(welcomeDoc))
	require.NoError(t, err)
	assert.Equal(t, "Confirm your email", msg.Subject)
	assert.Empty(t, msg.From)
	assert.Empty(t, msg.To)

	assert.Contains(t, msg.HTML, "<!DOCTYPE html>")
	assert.Contains(t, msg.HTML, "One click and you&#39;re in.", "preheader rides the layout")
	assert.Contains(t, msg.HTML, "<h1>Almost there</h1>")
	assert.Contains(t, msg.HTML, "<strong>activate</strong>")
	assert.Contains(t, msg.HTML, `href="https://app.acme.example/confirm?t=abc"`)
	assert.Contains(t, msg.HTML, ">Confirm email</a>", "button label must drop the Button: prefix")
	assert.Contains(t, msg.HTML, `<table role="presentation"`, "button renders as table markup")
	assert.Contains(t, msg.HTML, "#2563eb", "default button color")

	assert.Equal(t, "Almost there\n\nHi! Confirm your address to activate your account.\n\nConfirm email: https://app.acme.example/confirm?t=abc\n\nIf the button doesn't work, reply to this email.", msg.Text)
}

func TestRenderTextBlocks(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	doc := `---
subject: Digest
---
## Items

- first *thing*
- second [docs](https://docs.example.com)
- third

1. alpha
2. beta

> Quoted wisdom
> continues here.

` + "```\ncode line one\ncode line two\n```" + `

---

Auto link: <https://acme.example> and www-less text.
`
	msg, err := r.Render([]byte(doc))
	require.NoError(t, err)

	expected := strings.Join([]string{
		"Items",
		"",
		"- first thing\n- second docs (https://docs.example.com)\n- third",
		"",
		"1. alpha\n2. beta",
		"",
		"> Quoted wisdom\n> continues here.",
		"",
		"code line one\ncode line two",
		"",
		"---",
		"",
		"Auto link: https://acme.example and www-less text.",
	}, "\n")
	assert.Equal(t, expected, msg.Text)
}

func TestRenderButtonRules(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)

	t.Run("non-http destination stays a link", func(t *testing.T) {
		t.Parallel()
		msg, err := r.Render([]byte("---\nsubject: s\n---\n[Button: Click](javascript:alert(1))\n"))
		require.NoError(t, err)
		assert.NotContains(t, msg.HTML, "display:inline-block", "no button for unsafe scheme")
		assert.NotContains(t, msg.HTML, "javascript:", "goldmark filters unsafe link schemes")
	})
	t.Run("surrounding text stays a paragraph", func(t *testing.T) {
		t.Parallel()
		msg, err := r.Render([]byte("---\nsubject: s\n---\nGo [Button: Click](https://acme.example) now\n"))
		require.NoError(t, err)
		assert.NotContains(t, msg.HTML, "display:inline-block")
		assert.Contains(t, msg.HTML, "Button: Click", "prefix stays visible so the mistake is seen")
	})
	t.Run("label is escaped", func(t *testing.T) {
		t.Parallel()
		// <b> is raw inline HTML, which the label extraction drops; the
		// remaining text must be entity-escaped in the button markup.
		msg, err := r.Render([]byte("---\nsubject: s\n---\n[Button: a<b>&c](https://acme.example)\n"))
		require.NoError(t, err)
		assert.Contains(t, msg.HTML, ">a&amp;c</a>")
	})
}

func TestRenderDropsRawHTML(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	msg, err := r.Render([]byte("---\nsubject: s\n---\nBefore\n\n<script>alert(1)</script>\n\nInline <em onload=x>markup</em> here.\n"))
	require.NoError(t, err)
	assert.NotContains(t, msg.HTML, "<script>")
	assert.NotContains(t, msg.HTML, "onload")
	assert.NotContains(t, msg.Text, "<script>")
	assert.Contains(t, msg.Text, "Inline markup here.")
}

func TestRenderDocumentErrors(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)

	docs := map[string]string{
		"missing frontmatter":  "# No header\n",
		"unterminated":         "---\nsubject: s\nbody without closing fence",
		"empty frontmatter":    "---\n---\nbody\n",
		"unknown key":          "---\nsubject: s\nsubjct: typo\n---\nbody\n",
		"missing subject":      "---\npreheader: p\n---\nbody\n",
		"whitespace subject":   "---\nsubject: '   '\n---\nbody\n",
		"multi-line preheader": "---\nsubject: s\npreheader: \"a\\nb\"\n---\nbody\n",
		"malformed yaml":       "---\nsubject: [\n---\nbody\n",
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := r.Render([]byte(doc))
			require.ErrorIs(t, err, markdown.ErrInvalidDocument, "doc: %q", doc)
		})
	}

	var zero markdown.Renderer
	_, err := zero.Render([]byte(welcomeDoc))
	require.ErrorIs(t, err, markdown.ErrInvalidDocument)
}

func TestRenderCRLFSource(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	doc := strings.ReplaceAll(welcomeDoc, "\n", "\r\n")
	msg, err := r.Render([]byte(doc))
	require.NoError(t, err)
	assert.Equal(t, "Confirm your email", msg.Subject)
}

func TestRenderNoPreheader(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	msg, err := r.Render([]byte("---\nsubject: Plain\n---\nBody.\n"))
	require.NoError(t, err)
	assert.NotContains(t, msg.HTML, "display:none;overflow:hidden", "no preheader div when frontmatter omits it")
}

func TestRenderOptions(t *testing.T) {
	t.Parallel()

	t.Run("custom layout", func(t *testing.T) {
		t.Parallel()
		r := newRenderer(t, markdown.WithLayout(`<main data-subject="{{.Subject}}">{{.Body}}</main>`))
		msg, err := r.Render([]byte("---\nsubject: Custom\n---\nHello.\n"))
		require.NoError(t, err)
		assert.Equal(t, `<main data-subject="Custom"><p>Hello.</p>
</main>`, msg.HTML)
	})
	t.Run("button color", func(t *testing.T) {
		t.Parallel()
		r := newRenderer(t, markdown.WithButtonColor("#16a34a"))
		msg, err := r.Render([]byte("---\nsubject: s\n---\n[Button: Go](https://acme.example)\n"))
		require.NoError(t, err)
		assert.Contains(t, msg.HTML, "#16a34a")
		assert.NotContains(t, msg.HTML, "#2563eb")
	})
	t.Run("broken layout fails New", func(t *testing.T) {
		t.Parallel()
		_, err := markdown.New(markdown.WithLayout(`{{.Body`))
		require.ErrorIs(t, err, markdown.ErrInvalidLayout)
	})
	t.Run("layout with unknown field fails New", func(t *testing.T) {
		t.Parallel()
		_, err := markdown.New(markdown.WithLayout(`{{.Nope}}`))
		require.ErrorIs(t, err, markdown.ErrInvalidLayout)
	})
}

func TestRenderedMessageEncodes(t *testing.T) {
	t.Parallel()
	r := newRenderer(t)
	msg, err := r.Render([]byte(welcomeDoc))
	require.NoError(t, err)
	msg.From = "Acme <no-reply@acme.example>"
	msg.To = []string{"ann@example.com"}
	assert.NoError(t, msg.Encode(&strings.Builder{}))
}
