package qrcode_test

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"github.com/dmitrymomot/forge/pkg/qrcode"

	"github.com/stretchr/testify/require"
)

// pngHeader is the first 8 bytes of any valid PNG file.
var pngHeader = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// decodedSize decodes PNG bytes and returns the square image's pixel dimension.
func decodedSize(t *testing.T, b []byte) int {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	bounds := img.Bounds()
	require.Equal(t, bounds.Dx(), bounds.Dy(), "QR code image should be square")
	return bounds.Dx()
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	t.Run("valid content returns PNG bytes", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("https://example.com")
		require.NoError(t, err)
		require.True(t, len(png) > len(pngHeader), "PNG output too short")
		require.Equal(t, pngHeader, png[:8], "output should start with PNG header")
	})

	t.Run("default size produces a 256px image", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test")
		require.NoError(t, err)
		require.Equal(t, 256, decodedSize(t, out), "default size should render a 256px square")
	})

	t.Run("custom size is reflected in image dimensions", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test", 512)
		require.NoError(t, err)
		require.Equal(t, 512, decodedSize(t, out), "requested size should drive image dimensions")
	})

	t.Run("different sizes produce different dimensions", func(t *testing.T) {
		t.Parallel()
		small, err := qrcode.Generate("https://example.com", 256)
		require.NoError(t, err)
		large, err := qrcode.Generate("https://example.com", 512)
		require.NoError(t, err)
		require.Equal(t, 256, decodedSize(t, small))
		require.Equal(t, 512, decodedSize(t, large))
		require.Greater(t, decodedSize(t, large), decodedSize(t, small),
			"a larger requested size must yield a larger image")
	})

	t.Run("empty content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("")
		require.Nil(t, png)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrEmptyContent)
	})

	t.Run("whitespace-only content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		png, err := qrcode.Generate("   \t\n  ")
		require.Nil(t, png)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrEmptyContent)
	})

	t.Run("zero size falls back to default", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test", 0)
		require.NoError(t, err)
		require.Equal(t, 256, decodedSize(t, out), "zero size should fall back to the 256px default")
	})

	t.Run("negative size falls back to default", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test", -1)
		require.NoError(t, err)
		require.Equal(t, 256, decodedSize(t, out), "negative size should fall back to the 256px default")
	})

	t.Run("size above maximum returns ErrSizeTooLarge", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test", 4097)
		require.Nil(t, out)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrSizeTooLarge)
	})

	t.Run("size at maximum is accepted", func(t *testing.T) {
		t.Parallel()
		out, err := qrcode.Generate("test", 4096)
		require.NoError(t, err)
		require.Equal(t, 4096, decodedSize(t, out), "maximum size should be honored exactly")
	})

	t.Run("content exceeding capacity returns ErrFailedToGenerateQRCode", func(t *testing.T) {
		t.Parallel()
		// A QR code maxes out at 2,953 bytes of content; anything larger
		// makes the underlying encoder fail, exercising the error-wrapping path.
		oversized := strings.Repeat("a", 4000)
		out, err := qrcode.Generate(oversized)
		require.Nil(t, out)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrFailedToGenerateQRCode)
	})
}

func TestGenerateBase64Image(t *testing.T) {
	t.Parallel()

	t.Run("valid content returns data URI", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("https://example.com")
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(dataURI, "data:image/png;base64,"))
	})

	t.Run("base64 portion decodes to valid PNG", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("test")
		require.NoError(t, err)

		b64 := strings.TrimPrefix(dataURI, "data:image/png;base64,")
		png, err := base64.StdEncoding.DecodeString(b64)
		require.NoError(t, err)
		require.True(t, len(png) > len(pngHeader))
		require.Equal(t, pngHeader, png[:8])
	})

	t.Run("empty content returns ErrEmptyContent", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("")
		require.Empty(t, dataURI)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrEmptyContent)
	})

	t.Run("custom size is reflected in decoded image", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("test", 128)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(dataURI, "data:image/png;base64,"))

		b64 := strings.TrimPrefix(dataURI, "data:image/png;base64,")
		out, err := base64.StdEncoding.DecodeString(b64)
		require.NoError(t, err)
		require.Equal(t, 128, decodedSize(t, out), "requested size should drive image dimensions")
	})

	t.Run("size above maximum returns ErrSizeTooLarge", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image("test", 4097)
		require.Empty(t, dataURI)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrSizeTooLarge)
	})

	t.Run("content exceeding capacity returns ErrFailedToGenerateQRCode", func(t *testing.T) {
		t.Parallel()
		dataURI, err := qrcode.GenerateBase64Image(strings.Repeat("a", 4000))
		require.Empty(t, dataURI)
		require.Error(t, err)
		require.ErrorIs(t, err, qrcode.ErrFailedToGenerateQRCode)
	})
}
