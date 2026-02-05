package storage

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtFromMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		{"jpeg", "image/jpeg", ".jpg"},
		{"png", "image/png", ".png"},
		{"gif", "image/gif", ".gif"},
		{"webp", "image/webp", ".webp"},
		{"pdf", "application/pdf", ".pdf"},
		{"json", "application/json", ".json"},
		{"mp4", "video/mp4", ".mp4"},
		{"mp3", "audio/mpeg", ".mp3"},
		{"unknown", "application/unknown", ""},
		{"empty", "", ""},
		{"with charset", "text/plain; charset=utf-8", ".txt"},
		{"uppercase", "IMAGE/JPEG", ".jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtFromMIME(tt.mimeType)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "image/jpeg", "image/jpeg"},
		{"with charset", "text/html; charset=utf-8", "text/html"},
		{"uppercase", "IMAGE/JPEG", "image/jpeg"},
		{"with spaces", " image/png ", "image/png"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeMIME(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		allowed  []string
		want     bool
	}{
		{"exact match", "image/jpeg", []string{"image/jpeg"}, true},
		{"wildcard match", "image/png", []string{"image/*"}, true},
		{"no match", "video/mp4", []string{"image/*"}, false},
		{"multiple allowed", "application/pdf", []string{"image/*", "application/pdf"}, true},
		{"case insensitive", "IMAGE/JPEG", []string{"image/jpeg"}, true},
		{"empty allowed", "image/jpeg", []string{}, false},
		{"empty mime", "", []string{"image/*"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchesMIME(tt.mimeType, tt.allowed)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsImageMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"jpeg", "image/jpeg", true},
		{"png", "image/png", true},
		{"gif", "image/gif", true},
		{"webp", "image/webp", true},
		{"svg", "image/svg+xml", true},
		{"heic", "image/heic", true},
		{"avif", "image/avif", true},
		{"pdf", "application/pdf", false},
		{"video", "video/mp4", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isImageMIME(tt.mimeType)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsDocumentMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"pdf", "application/pdf", true},
		{"word", "application/msword", true},
		{"docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"excel", "application/vnd.ms-excel", true},
		{"xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},
		{"text", "text/plain", true},
		{"csv", "text/csv", true},
		{"image", "image/jpeg", false},
		{"video", "video/mp4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isDocumentMIME(tt.mimeType)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsVideoMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"mp4", "video/mp4", true},
		{"webm", "video/webm", true},
		{"quicktime", "video/quicktime", true},
		{"image", "image/jpeg", false},
		{"audio", "audio/mpeg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isVideoMIME(tt.mimeType)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsAudioMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mimeType string
		want     bool
	}{
		{"mp3", "audio/mpeg", true},
		{"wav", "audio/wav", true},
		{"ogg", "audio/ogg", true},
		{"flac", "audio/flac", true},
		{"image", "image/jpeg", false},
		{"video", "video/mp4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isAudioMIME(tt.mimeType)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDetectMIMEFromReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{
			name:    "png magic bytes",
			content: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			want:    "image/png",
		},
		{
			name:    "jpeg magic bytes",
			content: []byte{0xFF, 0xD8, 0xFF, 0xE0},
			want:    "image/jpeg",
		},
		{
			name:    "gif magic bytes",
			content: []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61},
			want:    "image/gif",
		},
		{
			name:    "pdf magic bytes",
			content: []byte{0x25, 0x50, 0x44, 0x46, 0x2D},
			want:    "application/pdf",
		},
		{
			name:    "plain text",
			content: []byte("Hello, World!"),
			want:    "text/plain; charset=utf-8",
		},
		{
			name:    "empty",
			content: []byte{},
			want:    MIMEOctetStream,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := bytes.NewReader(tt.content)
			got := detectMIMEFromReader(r)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestDetectMIMEWithReader(t *testing.T) {
	t.Parallel()

	// PNG magic bytes followed by some data.
	content := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, bytes.Repeat([]byte{0x00}, 100)...)

	r := bytes.NewReader(content)
	mimeType, newReader := detectMIMEWithReader(r)

	require.Equal(t, "image/png", mimeType)

	// Verify we can read all content from the new reader.
	readContent, err := io.ReadAll(newReader)
	require.NoError(t, err)
	require.Equal(t, content, readContent)
}

func TestDetectMIME(t *testing.T) {
	t.Parallel()

	t.Run("nil file header returns octet-stream", func(t *testing.T) {
		t.Parallel()
		got := DetectMIME(nil)
		require.Equal(t, MIMEOctetStream, got)
	})

	t.Run("file header without content returns octet-stream", func(t *testing.T) {
		t.Parallel()
		// Create a FileHeader that will fail to open because it has no content
		// (no multipart form backing it).
		fh := &multipart.FileHeader{
			Filename: "test.txt",
			Size:     100,
			Header:   textproto.MIMEHeader{},
		}
		got := DetectMIME(fh)
		require.Equal(t, MIMEOctetStream, got)
	})
}
