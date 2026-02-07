package qrcode_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/pkg/qrcode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngHeader is the first 8 bytes of any valid PNG file.
var pngHeader = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func TestGenerate(t *testing.T) {
	t.Parallel()

	t.Run("valid content returns PNG bytes", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("https://example.com")
		require.NoError(t, err)
		require.True(t, len(png) > len(pngHeader), "PNG output too short")
		assert.Equal(t, pngHeader, png[:8], "output should start with PNG header")
	})

	t.Run("default size produces output", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("test")
		require.NoError(t, err)
		assert.NotEmpty(t, png)
	})

	t.Run("custom size produces output", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("test", 512)
		require.NoError(t, err)
		assert.NotEmpty(t, png)
	})

	t.Run("empty content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("")
		assert.Nil(t, png)
		require.Error(t, err)
		assert.True(t, errors.Is(err, qrcode.ErrEmptyContent))
	})

	t.Run("whitespace-only content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("   \t\n  ")
		assert.Nil(t, png)
		require.Error(t, err)
		assert.True(t, errors.Is(err, qrcode.ErrEmptyContent))
	})

	t.Run("zero size falls back to default", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("test", 0)
		require.NoError(t, err)
		assert.NotEmpty(t, png)
	})

	t.Run("negative size falls back to default", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("test", -1)
		require.NoError(t, err)
		assert.NotEmpty(t, png)
	})
}

func TestGenerateBase64Image(t *testing.T) {
	t.Parallel()

	t.Run("valid content returns data URI", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("https://example.com")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(dataURI, "data:image/png;base64,"))
	})

	t.Run("base64 portion decodes to valid PNG", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("test")
		require.NoError(t, err)

		b64 := strings.TrimPrefix(dataURI, "data:image/png;base64,")
		png, err := base64.StdEncoding.DecodeString(b64)
		require.NoError(t, err)
		require.True(t, len(png) > len(pngHeader))
		assert.Equal(t, pngHeader, png[:8])
	})

	t.Run("empty content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("")
		assert.Empty(t, dataURI)
		require.Error(t, err)
		assert.True(t, errors.Is(err, qrcode.ErrEmptyContent))
	})

	t.Run("custom size returns data URI", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("test", 128)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(dataURI, "data:image/png;base64,"))
	})
}
