package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPutOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithKey", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithKey("custom/path/file.jpg")(opts)
		require.Equal(t, "custom/path/file.jpg", opts.key)
	})

	t.Run("WithPrefix", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithPrefix("avatars")(opts)
		require.Equal(t, "avatars", opts.prefix)
	})

	t.Run("WithTenant", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithTenant("tenant123")(opts)
		require.Equal(t, "tenant123", opts.tenant)
	})

	t.Run("WithContentType", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithContentType("image/png")(opts)
		require.Equal(t, "image/png", opts.contentType)
	})

	t.Run("WithACL", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithACL(ACLPublicRead)(opts)
		require.Equal(t, ACLPublicRead, opts.acl)
	})

	t.Run("WithValidation single rule", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithValidation(MaxSize(5 << 20))(opts)
		require.Len(t, opts.validationRules, 1)
	})

	t.Run("WithValidation multiple rules", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithValidation(MaxSize(5<<20), ImageOnly())(opts)
		require.Len(t, opts.validationRules, 2)
	})

	t.Run("WithValidation appends rules", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithValidation(MaxSize(5 << 20))(opts)
		WithValidation(ImageOnly())(opts)
		require.Len(t, opts.validationRules, 2)
	})

	t.Run("multiple options", func(t *testing.T) {
		t.Parallel()
		opts := &putOptions{}
		WithTenant("tenant123")(opts)
		WithPrefix("avatars")(opts)
		WithACL(ACLPublicRead)(opts)

		require.Equal(t, "tenant123", opts.tenant)
		require.Equal(t, "avatars", opts.prefix)
		require.Equal(t, ACLPublicRead, opts.acl)
	})
}

func TestURLOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithExpiry", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{}
		WithExpiry(time.Hour)(opts)
		require.Equal(t, time.Hour, opts.expiry)
	})

	t.Run("WithDownload", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{}
		WithDownload("document.pdf")(opts)
		require.Equal(t, "document.pdf", opts.downloadName)
		require.True(t, opts.forceSigned)
	})

	t.Run("WithSigned with expiry", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{}
		WithSigned(30 * time.Minute)(opts)
		require.True(t, opts.forceSigned)
		require.Equal(t, 30*time.Minute, opts.expiry)
	})

	t.Run("WithSigned zero expiry", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{expiry: DefaultURLExpiry}
		WithSigned(0)(opts)
		require.True(t, opts.forceSigned)
		require.Equal(t, DefaultURLExpiry, opts.expiry) // Should not change.
	})

	t.Run("WithPublic", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{}
		WithPublic()(opts)
		require.True(t, opts.forcePublic)
	})

	t.Run("multiple options", func(t *testing.T) {
		t.Parallel()
		opts := &urlOptions{}
		WithExpiry(time.Hour)(opts)
		WithDownload("file.zip")(opts)

		require.Equal(t, time.Hour, opts.expiry)
		require.Equal(t, "file.zip", opts.downloadName)
		require.True(t, opts.forceSigned)
	})
}
