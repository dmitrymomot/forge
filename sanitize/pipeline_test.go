package sanitize_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/sanitize"
)

func TestComposePipelineWithRealSanitizers(t *testing.T) {
	clean := sanitize.Compose(sanitize.Trim, strings.ToLower, sanitize.Collapse)
	assert.Equal(t, "ann lee", clean("  Ann   Lee "))
}

func TestApplyPipelineWithRealSanitizers(t *testing.T) {
	// Strip control chars, collapse whitespace, then single-line a messy input.
	got := sanitize.Apply(
		"  Hello\x00\tWorld\n\nagain  ",
		sanitize.StripControl,
		sanitize.SingleLine,
	)
	assert.Equal(t, "Hello World again", got)
}

func TestComposeReusableAcrossInputs(t *testing.T) {
	norm := sanitize.Compose(sanitize.Trim, sanitize.KeepAlphanumeric)
	assert.Equal(t, "abc123", norm("  a-b_c!123  "))
	assert.Equal(t, "x9", norm("  x*9  "))
}
