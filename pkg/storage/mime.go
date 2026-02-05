package storage

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// MIME type constants.
const (
	MIMEOctetStream    = "application/octet-stream"
	mimeDetectionBytes = 512 // http.DetectContentType requires up to 512 bytes
)

// imageTypes contains all recognized image MIME types.
var imageTypes = map[string]struct{}{
	"image/jpeg":    {},
	"image/png":     {},
	"image/gif":     {},
	"image/webp":    {},
	"image/svg+xml": {},
	"image/bmp":     {},
	"image/tiff":    {},
	"image/x-icon":  {},
	"image/heic":    {},
	"image/heif":    {},
	"image/avif":    {},
}

// documentTypes contains all recognized document MIME types.
var documentTypes = map[string]struct{}{
	"application/pdf":    {},
	"application/msword": {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/vnd.ms-excel": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {},
	"application/vnd.ms-powerpoint":                                             {},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {},
	"text/plain":      {},
	"text/csv":        {},
	"application/rtf": {},
}

// videoTypes contains all recognized video MIME types.
var videoTypes = map[string]struct{}{
	"video/mp4":        {},
	"video/webm":       {},
	"video/ogg":        {},
	"video/quicktime":  {},
	"video/x-msvideo":  {},
	"video/x-matroska": {},
}

// audioTypes contains all recognized audio MIME types.
var audioTypes = map[string]struct{}{
	"audio/mpeg": {},
	"audio/wav":  {},
	"audio/ogg":  {},
	"audio/webm": {},
	"audio/aac":  {},
	"audio/flac": {},
	"audio/mp4":  {},
}

// mimeExtensions maps MIME types to preferred file extensions.
var mimeExtensions = map[string]string{
	// Images
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
	"image/bmp":     ".bmp",
	"image/tiff":    ".tiff",
	"image/x-icon":  ".ico",
	"image/heic":    ".heic",
	"image/heif":    ".heif",
	"image/avif":    ".avif",
	// Documents
	"application/pdf":    ".pdf",
	"application/msword": ".doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.ms-excel": ".xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.ms-powerpoint":                                             ".ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"text/plain":      ".txt",
	"text/csv":        ".csv",
	"text/html":       ".html",
	"text/css":        ".css",
	"application/rtf": ".rtf",
	// Data
	"application/json":       ".json",
	"application/xml":        ".xml",
	"application/javascript": ".js",
	// Video
	"video/mp4":        ".mp4",
	"video/webm":       ".webm",
	"video/ogg":        ".ogv",
	"video/quicktime":  ".mov",
	"video/x-msvideo":  ".avi",
	"video/x-matroska": ".mkv",
	// Audio
	"audio/mpeg": ".mp3",
	"audio/wav":  ".wav",
	"audio/ogg":  ".ogg",
	"audio/webm": ".weba",
	"audio/aac":  ".aac",
	"audio/flac": ".flac",
	"audio/mp4":  ".m4a",
	// Archives
	"application/zip":              ".zip",
	"application/gzip":             ".gz",
	"application/x-tar":            ".tar",
	"application/x-7z-compressed":  ".7z",
	"application/x-rar-compressed": ".rar",
}

// DetectMIME detects the MIME type of a multipart file header by reading magic bytes.
// Returns "application/octet-stream" if detection fails.
func DetectMIME(fh *multipart.FileHeader) string {
	if fh == nil {
		return MIMEOctetStream
	}

	f, err := fh.Open()
	if err != nil {
		return MIMEOctetStream
	}
	defer f.Close()

	return detectMIMEFromReader(f)
}

// ExtFromMIME returns the file extension for a MIME type.
// Returns empty string if MIME type is unknown.
func ExtFromMIME(mimeType string) string {
	return mimeExtensions[normalizeMIME(mimeType)]
}

// IsImage checks if the file is an image based on magic bytes.
func IsImage(fh *multipart.FileHeader) bool {
	return isImageMIME(DetectMIME(fh))
}

// IsDocument checks if the file is a document based on magic bytes.
func IsDocument(fh *multipart.FileHeader) bool {
	return isDocumentMIME(DetectMIME(fh))
}

// IsVideo checks if the file is a video based on magic bytes.
func IsVideo(fh *multipart.FileHeader) bool {
	return isVideoMIME(DetectMIME(fh))
}

// IsAudio checks if the file is audio based on magic bytes.
func IsAudio(fh *multipart.FileHeader) bool {
	return isAudioMIME(DetectMIME(fh))
}

// detectMIMEFromReader detects MIME type by reading magic bytes from an io.Reader.
// It reads up to 512 bytes (the amount needed by http.DetectContentType).
// Returns "application/octet-stream" if detection fails.
func detectMIMEFromReader(r io.Reader) string {
	buf := make([]byte, mimeDetectionBytes)
	n, err := r.Read(buf)
	if err != nil && n == 0 {
		return MIMEOctetStream
	}

	return http.DetectContentType(buf[:n])
}

// detectMIMEWithReader detects MIME type from a reader and returns a seekable reader.
// AWS SDK v2 requires io.ReadSeeker for computing payload hash.
// If input is already seekable, it seeks back to start after detection.
// Otherwise, it buffers the entire content into memory.
func detectMIMEWithReader(r io.Reader) (string, io.ReadSeeker) {
	if rs, ok := r.(io.ReadSeeker); ok {
		buf := make([]byte, mimeDetectionBytes)
		n, _ := rs.Read(buf)
		_, _ = rs.Seek(0, io.SeekStart)
		if n > 0 {
			return http.DetectContentType(buf[:n]), rs
		}
		return MIMEOctetStream, rs
	}

	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return MIMEOctetStream, bytes.NewReader(nil)
	}

	mimeType := http.DetectContentType(data)
	return mimeType, bytes.NewReader(data)
}

// normalizeMIME extracts the base MIME type, removing parameters like charset.
// Returns the lowercase MIME type.
func normalizeMIME(mimeType string) string {
	mimeType, _, _ = strings.Cut(mimeType, ";")
	return strings.TrimSpace(strings.ToLower(mimeType))
}

// isImageMIME checks if the MIME type is an image type.
func isImageMIME(mimeType string) bool {
	_, ok := imageTypes[normalizeMIME(mimeType)]
	return ok
}

// isDocumentMIME checks if the MIME type is a document type.
func isDocumentMIME(mimeType string) bool {
	_, ok := documentTypes[normalizeMIME(mimeType)]
	return ok
}

// isVideoMIME checks if the MIME type is a video type.
func isVideoMIME(mimeType string) bool {
	_, ok := videoTypes[normalizeMIME(mimeType)]
	return ok
}

// isAudioMIME checks if the MIME type is an audio type.
func isAudioMIME(mimeType string) bool {
	_, ok := audioTypes[normalizeMIME(mimeType)]
	return ok
}

// matchesMIME checks if a MIME type matches any of the allowed patterns.
// Supports wildcards like "image/*".
func matchesMIME(mimeType string, allowed []string) bool {
	mimeType = normalizeMIME(mimeType)

	for _, pattern := range allowed {
		pattern = strings.TrimSpace(strings.ToLower(pattern))

		if mimeType == pattern {
			return true
		}

		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(mimeType, prefix) {
				return true
			}
		}
	}

	return false
}
