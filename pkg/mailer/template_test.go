package mailer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTemplate_WithFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Welcome Email
Author: System
---
# Hello World

This is the email body.
`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Welcome Email", tmpl.Metadata["Subject"])
	require.Equal(t, "System", tmpl.Metadata["Author"])
	require.Equal(t, "# Hello World\n\nThis is the email body.\n", tmpl.Body)
}

func TestParseTemplate_WithoutFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte(`# Hello World

This is just plain markdown.`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Empty(t, tmpl.Metadata)
	require.Equal(t, string(content), tmpl.Body)
}

func TestParseTemplate_EmptyFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte(`---
---
Body content here.`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Empty(t, tmpl.Metadata)
	require.Equal(t, "Body content here.", tmpl.Body)
}

func TestParseTemplate_WhitespaceFrontmatter(t *testing.T) {
	t.Parallel()

	content := []byte(`---

---
Body content.`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Empty(t, tmpl.Metadata)
	require.Equal(t, "Body content.", tmpl.Body)
}

func TestParseTemplate_MissingClosingDelimiter(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Test
Body without closing delimiter`)

	tmpl, err := ParseTemplate(content)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFrontmatter)
	require.Nil(t, tmpl)
}

func TestParseTemplate_NoContentAfterOpening(t *testing.T) {
	t.Parallel()

	content := []byte(`---`)

	tmpl, err := ParseTemplate(content)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFrontmatter)
	require.Nil(t, tmpl)
}

func TestParseTemplate_InvalidYAML(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Test
InvalidYAML: [unclosed
---
Body`)

	tmpl, err := ParseTemplate(content)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidFrontmatter)
	require.Nil(t, tmpl)
}

func TestParseTemplate_ComplexMetadata(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Complex Email
Priority: high
Tags:
  - welcome
  - onboarding
Settings:
  tracking: true
  analytics: false
---
Email body here.`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Complex Email", tmpl.Metadata["Subject"])
	require.Equal(t, "high", tmpl.Metadata["Priority"])

	tags, ok := tmpl.Metadata["Tags"].([]any)
	require.True(t, ok)
	require.Len(t, tags, 2)
	require.Equal(t, "welcome", tags[0])
	require.Equal(t, "onboarding", tags[1])

	settings, ok := tmpl.Metadata["Settings"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, settings["tracking"])
	require.Equal(t, false, settings["analytics"])

	require.Equal(t, "Email body here.", tmpl.Body)
}

func TestParseTemplate_UnixLineEndings(t *testing.T) {
	t.Parallel()

	content := []byte("---\nSubject: Test\n---\nBody")

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Test", tmpl.Metadata["Subject"])
	require.Equal(t, "Body", tmpl.Body)
}

func TestParseTemplate_WindowsLineEndings(t *testing.T) {
	t.Parallel()

	content := []byte("---\r\nSubject: Test\r\n---\r\nBody")

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Test", tmpl.Metadata["Subject"])
	require.Equal(t, "Body", tmpl.Body)
}

func TestParseTemplate_MultilineBody(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Newsletter
---
# Welcome to Our Newsletter

This is paragraph 1.

This is paragraph 2.

* Item 1
* Item 2
`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Newsletter", tmpl.Metadata["Subject"])
	require.Contains(t, tmpl.Body, "# Welcome to Our Newsletter")
	require.Contains(t, tmpl.Body, "This is paragraph 1.")
	require.Contains(t, tmpl.Body, "* Item 1")
}

func TestParseTemplate_EmptyBody(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Test
---
`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Test", tmpl.Metadata["Subject"])
	require.Empty(t, tmpl.Body)
}

func TestParseTemplate_BodyWithDelimiters(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Code Example
---
Here's how to use frontmatter:

` + "```" + `
---
key: value
---
` + "```" + `
`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Code Example", tmpl.Metadata["Subject"])
	require.Contains(t, tmpl.Body, "---")
	require.Contains(t, tmpl.Body, "key: value")
}

func TestParseTemplate_NumericMetadata(t *testing.T) {
	t.Parallel()

	content := []byte(`---
Subject: Order Confirmation
OrderID: 12345
Amount: 99.99
---
Body`)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Equal(t, "Order Confirmation", tmpl.Metadata["Subject"])
	require.Equal(t, 12345, tmpl.Metadata["OrderID"])
	require.Equal(t, 99.99, tmpl.Metadata["Amount"])
}

func TestParseTemplate_EmptyContent(t *testing.T) {
	t.Parallel()

	content := []byte(``)

	tmpl, err := ParseTemplate(content)
	require.NoError(t, err)
	require.NotNil(t, tmpl)
	require.Empty(t, tmpl.Metadata)
	require.Empty(t, tmpl.Body)
}
