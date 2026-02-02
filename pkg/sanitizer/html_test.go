package sanitizer_test

import (
	"testing"

	"github.com/microcosm-cc/bluemonday"
	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/pkg/sanitizer"
)

func TestStripHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips script injection",
			input:    `<p>Hello</p><script>alert('xss')</script>`,
			expected: "Hello",
		},
		{
			name:     "strips all HTML tags",
			input:    `<p>Hello <strong>world</strong></p>`,
			expected: "Hello world",
		},
		{
			name:     "strips event handlers",
			input:    `<img src="x" onerror="alert('xss')">`,
			expected: "",
		},
		{
			name:     "strips javascript URLs",
			input:    `<a href="javascript:alert('xss')">click</a>`,
			expected: "click",
		},
		{
			name:     "strips data URLs",
			input:    `<a href="data:text/html,<script>alert('xss')</script>">click</a>`,
			expected: "click",
		},
		{
			name:     "strips CSS injection",
			input:    `<div style="background:url(javascript:alert('xss'))">content</div>`,
			expected: "content",
		},
		{
			name:     "strips nested tags",
			input:    `<div><p>nested <span>content</span></p></div>`,
			expected: "nested content",
		},
		{
			name:     "handles plain text",
			input:    "normal text without HTML",
			expected: "normal text without HTML",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "strips style tags",
			input:    `Hello <STYLE>.XSS{background-image:url("javascript:alert('XSS')");}</STYLE>World`,
			expected: "Hello World",
		},
		{
			name:     "strips iframe",
			input:    `<iframe src="https://evil.com"></iframe>content`,
			expected: "content",
		},
		{
			name:     "strips object tags",
			input:    `<object data="data:text/html,<script>alert(1)</script>"></object>text`,
			expected: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizer.StripHTML(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips script injection but keeps safe tags",
			input:    `<p>Hello</p><script>alert('xss')</script>`,
			expected: "<p>Hello</p>",
		},
		{
			name:     "allows basic formatting",
			input:    `<p>Hello <strong>world</strong></p>`,
			expected: "<p>Hello <strong>world</strong></p>",
		},
		{
			name:     "allows emphasis tags",
			input:    `<p><em>italic</em> and <i>also italic</i></p>`,
			expected: "<p><em>italic</em> and <i>also italic</i></p>",
		},
		{
			name:     "allows lists",
			input:    `<ul><li>item 1</li><li>item 2</li></ul>`,
			expected: "<ul><li>item 1</li><li>item 2</li></ul>",
		},
		{
			name:     "allows code blocks",
			input:    `<pre><code>func main() {}</code></pre>`,
			expected: "<pre><code>func main() {}</code></pre>",
		},
		{
			name:     "allows blockquote",
			input:    `<blockquote>quoted text</blockquote>`,
			expected: "<blockquote>quoted text</blockquote>",
		},
		{
			name:     "allows safe links with nofollow",
			input:    `<a href="https://example.com">link</a>`,
			expected: `<a href="https://example.com" rel="nofollow">link</a>`,
		},
		{
			name:     "strips javascript URLs from links",
			input:    `<a href="javascript:alert('xss')">click</a>`,
			expected: "click",
		},
		{
			name:     "strips event handlers",
			input:    `<p onclick="alert('xss')">content</p>`,
			expected: "<p>content</p>",
		},
		{
			name:     "strips style attribute",
			input:    `<p style="background:url(javascript:alert('xss'))">content</p>`,
			expected: "<p>content</p>",
		},
		{
			name:     "strips img tags",
			input:    `<img src="x" onerror="alert('xss')">`,
			expected: "",
		},
		{
			name:     "strips div tags",
			input:    `<div>content</div>`,
			expected: "content",
		},
		{
			name:     "strips class and id attributes",
			input:    `<p class="xss" id="attack">content</p>`,
			expected: "<p>content</p>",
		},
		{
			name:     "handles plain text",
			input:    "normal text without HTML",
			expected: "normal text without HTML",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "allows line breaks",
			input:    `line1<br>line2`,
			expected: `line1<br>line2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizer.SanitizeHTML(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeHTMLCustom(t *testing.T) {
	t.Parallel()

	t.Run("with custom policy allowing img", func(t *testing.T) {
		t.Parallel()

		policy := bluemonday.NewPolicy()
		policy.AllowElements("img")
		policy.AllowAttrs("src", "alt").OnElements("img")

		input := `<img src="photo.jpg" alt="photo" onerror="alert('xss')">`
		result := sanitizer.SanitizeHTMLCustom(input, policy)
		assert.Equal(t, `<img src="photo.jpg" alt="photo">`, result)
	})

	t.Run("with nil policy returns input unchanged", func(t *testing.T) {
		t.Parallel()

		input := `<script>alert('xss')</script>`
		result := sanitizer.SanitizeHTMLCustom(input, nil)
		assert.Equal(t, input, result)
	})

	t.Run("with strict policy strips everything", func(t *testing.T) {
		t.Parallel()

		policy := bluemonday.StrictPolicy()
		input := `<p>Hello <strong>world</strong></p>`
		result := sanitizer.SanitizeHTMLCustom(input, policy)
		assert.Equal(t, "Hello world", result)
	})
}

func TestHTMLSanitizationXSSVectors(t *testing.T) {
	t.Parallel()

	// Common XSS attack vectors that should be neutralized
	vectors := []struct {
		name  string
		input string
	}{
		{
			name:  "script tag",
			input: `<script>alert('XSS')</script>`,
		},
		{
			name:  "script tag with src",
			input: `<script src="https://evil.com/xss.js"></script>`,
		},
		{
			name:  "img onerror",
			input: `<img src="x" onerror="alert('XSS')">`,
		},
		{
			name:  "img onload",
			input: `<img src="valid.jpg" onload="alert('XSS')">`,
		},
		{
			name:  "body onload",
			input: `<body onload="alert('XSS')">`,
		},
		{
			name:  "svg onload",
			input: `<svg onload="alert('XSS')">`,
		},
		{
			name:  "javascript protocol",
			input: `<a href="javascript:alert('XSS')">click</a>`,
		},
		{
			name:  "javascript protocol case variation",
			input: `<a href="JaVaScRiPt:alert('XSS')">click</a>`,
		},
		{
			name:  "data URL",
			input: `<a href="data:text/html;base64,PHNjcmlwdD5hbGVydCgnWFNTJyk8L3NjcmlwdD4=">click</a>`,
		},
		{
			name:  "vbscript protocol",
			input: `<a href="vbscript:msgbox('XSS')">click</a>`,
		},
		{
			name:  "style expression",
			input: `<div style="width:expression(alert('XSS'))">`,
		},
		{
			name:  "style background javascript",
			input: `<div style="background:url(javascript:alert('XSS'))">`,
		},
		{
			name:  "meta refresh",
			input: `<meta http-equiv="refresh" content="0;url=javascript:alert('XSS')">`,
		},
		{
			name:  "iframe",
			input: `<iframe src="javascript:alert('XSS')"></iframe>`,
		},
		{
			name:  "object tag",
			input: `<object data="data:text/html;base64,PHNjcmlwdD5hbGVydCgnWFNTJyk8L3NjcmlwdD4="></object>`,
		},
		{
			name:  "embed tag",
			input: `<embed src="javascript:alert('XSS')">`,
		},
		{
			name:  "form action",
			input: `<form action="javascript:alert('XSS')"><input type="submit"></form>`,
		},
		{
			name:  "input onfocus",
			input: `<input onfocus="alert('XSS')" autofocus>`,
		},
		{
			name:  "marquee onstart",
			input: `<marquee onstart="alert('XSS')">`,
		},
		{
			name:  "video onerror",
			input: `<video><source onerror="alert('XSS')">`,
		},
		{
			name:  "details ontoggle",
			input: `<details open ontoggle="alert('XSS')">`,
		},
		{
			name:  "math tag",
			input: `<math><mtext><table><mglyph><style><img src=x onerror="alert('XSS')">`,
		},
	}

	for _, v := range vectors {
		t.Run("StripHTML_"+v.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizer.StripHTML(v.input)
			// Should not contain any script or dangerous content
			assert.NotContains(t, result, "<script")
			assert.NotContains(t, result, "javascript:")
			assert.NotContains(t, result, "onerror=")
			assert.NotContains(t, result, "onload=")
			assert.NotContains(t, result, "onclick=")
			assert.NotContains(t, result, "alert(")
		})

		t.Run("SanitizeHTML_"+v.name, func(t *testing.T) {
			t.Parallel()

			result := sanitizer.SanitizeHTML(v.input)
			// Should not contain any script or dangerous content
			assert.NotContains(t, result, "<script")
			assert.NotContains(t, result, "javascript:")
			assert.NotContains(t, result, "onerror=")
			assert.NotContains(t, result, "onload=")
			assert.NotContains(t, result, "onclick=")
			assert.NotContains(t, result, "alert(")
		})
	}
}

func TestHTMLStructTag(t *testing.T) {
	t.Parallel()

	type Comment struct {
		Body string `sanitize:"html"`
	}

	tests := []struct {
		name     string
		input    Comment
		expected Comment
	}{
		{
			name: "sanitizes HTML in struct field",
			input: Comment{
				Body: `<p>Hello</p><script>alert('xss')</script>`,
			},
			expected: Comment{
				Body: "<p>Hello</p>",
			},
		},
		{
			name: "allows safe formatting",
			input: Comment{
				Body: `<p><strong>Bold</strong> and <em>italic</em></p>`,
			},
			expected: Comment{
				Body: `<p><strong>Bold</strong> and <em>italic</em></p>`,
			},
		},
		{
			name: "adds nofollow to links",
			input: Comment{
				Body: `<a href="https://example.com">link</a>`,
			},
			expected: Comment{
				Body: `<a href="https://example.com" rel="nofollow">link</a>`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			comment := tt.input
			err := sanitizer.SanitizeStruct(&comment)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, comment)
		})
	}
}
