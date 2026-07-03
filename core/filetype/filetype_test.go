package filetype_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dmitrymomot/forge/core/filetype"
)

// Minimal but real magic-byte prefixes for each curated signature. Each byte
// slice is the leading bytes of a genuine file of that type — enough for the
// signature table to match.
var (
	pngHead  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	gifHead  = []byte("GIF89a\x00\x00")
	bmpHead  = []byte{'B', 'M', 0x36, 0x00, 0x00, 0x00}
	pdfHead  = []byte("%PDF-1.7\n")
	gzipHead = []byte{0x1F, 0x8B, 0x08, 0x00}
	zipHead  = []byte{'P', 'K', 0x03, 0x04, 0x14, 0x00}
	flacHead = []byte("fLaC\x00\x00\x00\x22")
	oggHead  = []byte("OggS\x00\x02\x00\x00")
	id3Head  = []byte{'I', 'D', '3', 0x03, 0x00, 0x00, 0x00}
	// WAV: RIFF....WAVE
	wavHead = []byte{'R', 'I', 'F', 'F', 0x24, 0x08, 0x00, 0x00, 'W', 'A', 'V', 'E'}
	// WEBP: RIFF....WEBP
	webpHead = []byte{'R', 'I', 'F', 'F', 0x1A, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'}
	// MP4: ....ftypisom
	mp4Head = []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	// MOV: ....ftypqt␣␣
	movHead = []byte{0x00, 0x00, 0x00, 0x14, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '}
	// WEBM: EBML header
	webmHead = []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01, 0x00, 0x00, 0x00}
	// ICO: 00 00 01 00
	icoHead = []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00}
	// TIFF little-endian: II*\0
	tiffLEHead = []byte{'I', 'I', 0x2A, 0x00, 0x08, 0x00}
	// TIFF big-endian: MM\0*
	tiffBEHead = []byte{'M', 'M', 0x00, 0x2A, 0x00, 0x08}
	// TAR: "ustar" magic at offset 257.
	tarHead = func() []byte {
		b := make([]byte, 265)
		copy(b[257:], []byte("ustar"))
		return b
	}()
)

func TestDetect_KnownSignatures(t *testing.T) {
	tests := []struct {
		name     string
		head     []byte
		wantMIME string
		wantExt  string
	}{
		{"png", pngHead, "image/png", "png"},
		{"jpeg", jpegHead, "image/jpeg", "jpg"},
		{"gif", gifHead, "image/gif", "gif"},
		{"webp", webpHead, "image/webp", "webp"},
		{"bmp", bmpHead, "image/bmp", "bmp"},
		{"tiff-le", tiffLEHead, "image/tiff", "tiff"},
		{"tiff-be", tiffBEHead, "image/tiff", "tiff"},
		{"ico", icoHead, "image/x-icon", "ico"},
		{"pdf", pdfHead, "application/pdf", "pdf"},
		{"zip", zipHead, "application/zip", "zip"},
		{"gzip", gzipHead, "application/gzip", "gz"},
		{"tar", tarHead, "application/x-tar", "tar"},
		{"mp3-id3", id3Head, "audio/mpeg", "mp3"},
		{"wav", wavHead, "audio/wav", "wav"},
		{"ogg", oggHead, "audio/ogg", "ogg"},
		{"flac", flacHead, "audio/flac", "flac"},
		{"mp4", mp4Head, "video/mp4", "mp4"},
		{"mov", movHead, "video/quicktime", "mov"},
		{"webm", webmHead, "video/webm", "webm"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := filetype.Detect(tc.head)
			assert.True(t, ok, "expected detection to succeed")
			assert.Equal(t, tc.wantMIME, got.MIME)
			assert.Equal(t, tc.wantExt, got.Ext)
		})
	}
}

func TestDetect_Mp3FrameSync(t *testing.T) {
	// MPEG audio frame sync (0xFF 0xFB) without an ID3 tag.
	head := []byte{0xFF, 0xFB, 0x90, 0x00}
	got, ok := filetype.Detect(head)
	assert.True(t, ok)
	assert.Equal(t, "audio/mpeg", got.MIME)
	assert.Equal(t, "mp3", got.Ext)
}

func TestDetect_FallbackHTML(t *testing.T) {
	// Not in the curated table, but net/http.DetectContentType recognizes it.
	got, ok := filetype.Detect([]byte("<!DOCTYPE html><html></html>"))
	assert.True(t, ok)
	assert.Contains(t, got.MIME, "text/html")
}

func TestDetect_UnrecognizedIsOctetStream(t *testing.T) {
	// Random binary that neither the table nor the fallback recognizes:
	// ok must be false and MIME the octet-stream sentinel.
	head := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x00, 0xFF}
	got, ok := filetype.Detect(head)
	assert.False(t, ok)
	assert.Equal(t, "application/octet-stream", got.MIME)
	assert.Equal(t, "", got.Ext)
}

func TestDetect_EmptyHead(t *testing.T) {
	got, ok := filetype.Detect(nil)
	assert.False(t, ok)
	assert.Equal(t, "application/octet-stream", got.MIME)
}

func TestIs(t *testing.T) {
	assert.True(t, filetype.Is(pngHead, "image/png"))
	assert.False(t, filetype.Is(pngHead, "image/jpeg"))
	// OOXML caveat: a docx (PK zip) reports application/zip.
	assert.True(t, filetype.Is(zipHead, "application/zip"))
	assert.False(t, filetype.Is(zipHead, "application/vnd.openxmlformats-officedocument.wordprocessingml.document"))
	// Unrecognized head is not "octet-stream" via Is (ok=false ⇒ no match).
	assert.False(t, filetype.Is([]byte{0x00, 0x01, 0x02}, "application/octet-stream"))
}
