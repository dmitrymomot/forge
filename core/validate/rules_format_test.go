package validate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/validate"
)

func TestFormats(t *testing.T) {
	assert.True(t, validate.Email("a@b.com").IsZero())
	assert.Equal(t, "validation.email", validate.Email("not-an-email").Key)

	assert.True(t, validate.URL("https://example.com/x").IsZero())
	assert.Equal(t, "validation.url", validate.URL("example.com").Key)       // no scheme
	assert.Equal(t, "validation.url", validate.URL("ftp://example.com").Key) // not http(s)

	assert.True(t, validate.UUID("f47ac10b-58cc-4372-a567-0e02b2c3d479").IsZero())
	assert.Equal(t, "validation.uuid", validate.UUID("not-a-uuid").Key)

	assert.True(t, validate.Slug("hello-world-2").IsZero())
	assert.Equal(t, "validation.slug", validate.Slug("Hello World").Key)

	assert.True(t, validate.Hex("deadBEEF").IsZero())
	assert.Equal(t, "validation.hex", validate.Hex("xyz").Key)
	assert.Equal(t, "validation.hex", validate.Hex("abc").Key) // odd length

	assert.True(t, validate.Base64("aGVsbG8=").IsZero())
	assert.Equal(t, "validation.base64", validate.Base64("not base64!!").Key)

	assert.True(t, validate.JSON(`{"a":1}`).IsZero())
	assert.Equal(t, "validation.json", validate.JSON(`{bad}`).Key)
}
