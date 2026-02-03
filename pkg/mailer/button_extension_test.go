package mailer

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
)

func TestButtonExtension_RendersButton(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|Click Me](https://example.com)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	require.Contains(t, buf.String(), `<a href="https://example.com" class="btn">Click Me</a>`)
}

func TestButtonExtension_EscapesHTML(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|<script>alert("xss")</script>](javascript:alert("xss"))`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	// HTML should be escaped
	require.NotContains(t, buf.String(), "<script>")
	require.Contains(t, buf.String(), "&lt;script&gt;")
}

func TestButtonExtension_WithMarkdownSurrounding(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`# Welcome

Please verify your email:

[!button|Verify Email](https://example.com/verify)

Thank you!`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	require.Contains(t, result, "<h1>Welcome</h1>")
	require.Contains(t, result, `<a href="https://example.com/verify" class="btn">Verify Email</a>`)
	require.Contains(t, result, "Thank you!")
}

func TestButtonExtension_MultipleButtons(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|Accept](https://example.com/accept)
[!button|Decline](https://example.com/decline)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	require.Contains(t, result, `<a href="https://example.com/accept" class="btn">Accept</a>`)
	require.Contains(t, result, `<a href="https://example.com/decline" class="btn">Decline</a>`)
}

func TestButtonExtension_IgnoresRegularLinks(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[Regular Link](https://example.com)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	// Should render as regular link, not button
	require.NotContains(t, result, `class="btn"`)
	require.Contains(t, result, `<a href="https://example.com">Regular Link</a>`)
}

func TestButtonExtension_IgnoresIncompleteButton(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "missing URL",
			source: `[!button|Click Me]`,
		},
		{
			name:   "missing closing bracket",
			source: `[!button|Click Me(https://example.com)`,
		},
		{
			name:   "wrong prefix",
			source: `[button|Click Me](https://example.com)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			err := md.Convert([]byte(tt.source), &buf)

			require.NoError(t, err)
			result := buf.String()

			// Should not render as button
			require.NotContains(t, result, `class="btn"`)
		})
	}
}

func TestButtonExtension_EmptyText(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|](https://example.com)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	// Empty text is allowed but renders empty button
	require.Contains(t, result, `class="btn"`)
	require.Contains(t, result, `href="https://example.com"`)
}

func TestButtonExtension_URLWithQueryParams(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|Verify](https://example.com/verify?token=abc123&user=john)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	require.Contains(t, result, `class="btn"`)
	require.Contains(t, result, "Verify")
	// URL should be properly escaped
	require.Contains(t, result, "token=abc123")
}

func TestButtonExtension_LongButtonText(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|Click Here to Verify Your Email Address and Activate Account](https://example.com)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	require.Contains(t, result, `class="btn"`)
	require.Contains(t, result, "Click Here to Verify Your Email Address and Activate Account")
}

func TestButtonExtension_SpecialCharactersInText(t *testing.T) {
	t.Parallel()

	md := goldmark.New(
		goldmark.WithExtensions(NewButtonExtension()),
	)

	source := []byte(`[!button|Accept & Continue](https://example.com)`)

	var buf bytes.Buffer
	err := md.Convert(source, &buf)

	require.NoError(t, err)
	result := buf.String()

	require.Contains(t, result, `class="btn"`)
	// Ampersand should be escaped
	require.Contains(t, result, "Accept &amp; Continue")
}

func TestButtonNode_Kind(t *testing.T) {
	t.Parallel()

	node := &ButtonNode{
		URL:   []byte("https://example.com"),
		Label: []byte("Test"),
	}

	require.Equal(t, KindButton, node.Kind())
}

func TestButtonNode_Dump(t *testing.T) {
	t.Parallel()

	node := &ButtonNode{
		URL:   []byte("https://example.com"),
		Label: []byte("Test"),
	}

	// Verify Dump doesn't panic
	require.NotPanics(t, func() {
		node.Dump([]byte("source"), 0)
	})
}

func TestButtonParser_Trigger(t *testing.T) {
	t.Parallel()

	parser := NewButtonParser()

	trigger := parser.Trigger()

	require.Equal(t, []byte{'['}, trigger)
}
